package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// The service's event log.
//
// What is worth a row is what someone might later dispute, or need to
// reconstruct: who joined, who was removed, who was made an admin, when a
// room changed hands. What is not worth a row is what merely happened a lot.
// Focus sessions are deliberately absent -- they are the most numerous event
// in the system and the least useful to read back, and the work map tells
// that story better than a log line ever would.
const (
	eventAccountCreated    = "account.created"
	eventAccountDeleted    = "user.self_deleted"
	eventSignedIn          = "account.signed_in"
	eventPasswordReset     = "account.password_reset"
	eventRoomCreated       = "room.created"
	eventRoomDeleted       = "room.deleted"
	eventRoomJoined        = "room.joined"
	eventRoomLeft          = "room.left"
	eventRoomMemberRemoved = "room.member_removed"
	eventRoomAdminAdded    = "room.admin_added"
	eventRoomAdminRemoved  = "room.admin_removed"
	eventRoomTransferStart = "room.transfer_started"
	eventRoomTransferUndo  = "room.transfer_cancelled"
	eventRoomTransferred   = "room.transferred"
	eventRoomSettings      = "room.settings_changed"
	eventRoomSound         = "room.sound_changed"
)

// eventRetention is how long ordinary events are kept.
const eventRetention = 90 * 24 * time.Hour

// permanentEvents outlive the retention window. Role grants and handovers are
// the ones people actually argue about later, and they are rare enough that
// keeping them forever costs nothing.
var permanentEvents = map[string]bool{
	eventRoomAdminAdded:   true,
	eventRoomAdminRemoved: true,
	eventRoomTransferred:  true,
	eventAccountDeleted:   true,
}

// logEvent records one event. It never fails a request: a lost log line is
// worth less than the action it describes, so errors are logged and swallowed.
//
// actorID may be empty for events with no signed-in actor. roomID scopes the
// event to a room, which is what lets a room's own admins read their room's
// history without seeing anyone else's.
func (a *app) logEvent(ctx context.Context, actorID, action, targetType, targetID, roomID string, metadata map[string]string) {
	blob, err := json.Marshal(metadata)
	if err != nil {
		blob = []byte("{}")
	}
	if _, err := a.db.Exec(ctx, `
		INSERT INTO admin_audit_log (actor_user_id, action, target_type, target_id, room_id, metadata)
		VALUES (NULLIF($1, '')::uuid, $2, $3, NULLIF($4, '')::uuid, NULLIF($5, '')::uuid, $6::jsonb)`,
		actorID, action, targetType, targetID, roomID, string(blob)); err != nil {
		log.Printf("event log %s: %v", action, err)
	}
}

// logRoomEvent is the common case: something happened in a room, sometimes to
// a particular person. When there is a person, they are the target -- so a
// user's history reads back as well as a room's -- and room_id still scopes
// the row to the room either way.
func (a *app) logRoomEvent(ctx context.Context, actorID, action string, rm room, targetID string, metadata map[string]string) {
	if metadata == nil {
		metadata = map[string]string{}
	}
	metadata["room"] = rm.Code
	targetType, target := "room", rm.ID
	if targetID != "" {
		targetType, target = "user", targetID
	}
	a.logEvent(ctx, actorID, action, targetType, target, rm.ID, metadata)
}

// pruneEventLog deletes events past the retention window, sparing the ones
// worth keeping forever. Separate from the ticker so the retention rule is
// exercised by tests rather than by a copy of this statement.
func (a *app) pruneEventLog(ctx context.Context) (int64, error) {
	keep := make([]string, 0, len(permanentEvents))
	for action := range permanentEvents {
		keep = append(keep, action)
	}
	tag, err := a.db.Exec(ctx,
		`DELETE FROM admin_audit_log WHERE created_at < now() - $1::interval AND action <> ALL($2)`,
		eventRetention.String(), keep)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// sweepEventLog applies the retention rule on a schedule.
func (a *app) sweepEventLog(ctx context.Context) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		if n, err := a.pruneEventLog(ctx); err != nil {
			log.Printf("sweep event log: %v", err)
		} else if n > 0 {
			log.Printf("swept %d expired events", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// eventRow is one line of the log as the viewer reads it.
type eventRow struct {
	At     time.Time
	Actor  string
	Action string
	Room   string
	Target string
	Detail map[string]string
}

// readEvents returns the newest events, optionally narrowed to one room.
// limit is clamped by the caller.
func (a *app) readEvents(ctx context.Context, roomID string, limit int) ([]eventRow, error) {
	rows, err := a.db.Query(ctx, `
		SELECT l.created_at, COALESCE(actor.username, ''), l.action,
			COALESCE(r.code, ''), COALESCE(target.username, ''), l.metadata
		FROM admin_audit_log l
		LEFT JOIN users actor ON actor.id = l.actor_user_id
		LEFT JOIN users target ON target.id = l.target_id AND l.target_type = 'user'
		LEFT JOIN rooms r ON r.id = l.room_id
		WHERE ($1 = '' OR l.room_id = NULLIF($1, '')::uuid)
		ORDER BY l.created_at DESC
		LIMIT $2`, roomID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []eventRow{}
	for rows.Next() {
		var e eventRow
		var raw []byte
		if err := rows.Scan(&e.At, &e.Actor, &e.Action, &e.Room, &e.Target, &raw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &e.Detail)
		out = append(out, e)
	}
	return out, rows.Err()
}

// maxEventPage is how many lines a single request returns. High enough to
// scroll through a real stretch of history, low enough that the response
// stays small on a free instance.
const maxEventPage = 300

// eventsJSON renders rows for either viewer.
func eventsJSON(events []eventRow) []map[string]any {
	out := make([]map[string]any, 0, len(events))
	for _, e := range events {
		row := map[string]any{
			"at":     e.At.UTC().Format(time.RFC3339),
			"action": e.Action,
			"actor":  e.Actor,
			"room":   e.Room,
			"target": e.Target,
		}
		if len(e.Detail) > 0 {
			row["detail"] = e.Detail
		}
		out = append(out, row)
	}
	return out
}

// apiAdminEvents is the whole service's log, for staff.
func (a *app) apiAdminEvents(w http.ResponseWriter, r *http.Request) {
	actor, _ := a.currentUser(r)
	if !canAccessAdmin(actor.Role) {
		writeJSONError(w, http.StatusForbidden, "This space is limited to platform administrators.")
		return
	}
	events, err := a.readEvents(r.Context(), "", maxEventPage)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not read the log")
		return
	}
	writeJSON(w, map[string]any{"events": eventsJSON(events), "retentionDays": int(eventRetention.Hours() / 24)})
}

// apiRoomEvents is one room's own history, for the people who run it.
//
// Scoped by room_id, so it carries what happened to the room and nothing
// about what its members do elsewhere -- the same line a room admin's
// authority stops at everywhere else.
func (a *app) apiRoomEvents(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	rm, ok := a.findRoom(r.Context(), r.PathValue("code"))
	if !ok {
		writeJSONError(w, http.StatusNotFound, "room not found")
		return
	}
	if !a.canAdminRoom(r.Context(), rm, u.ID) {
		writeJSONError(w, http.StatusForbidden, "only this room's admins can read its history")
		return
	}
	events, err := a.readEvents(r.Context(), rm.ID, maxEventPage)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not read the log")
		return
	}
	writeJSON(w, map[string]any{"events": eventsJSON(events), "room": map[string]string{"code": rm.Code, "name": rm.Name}})
}
