package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	roleUser  = "user"
	roleAdmin = "admin"
	roleOwner = "owner"
)

type adminOverview struct {
	Users             int
	Rooms             int
	ActiveRoomTimers  int
	TotalFocusMinutes int
}

type adminUser struct {
	ID             string
	Username       string
	Role           string
	JoinedAt       time.Time
	RoomsCount     int
	FocusMinutes   int
	LastActivityAt *time.Time
}

type adminRoom struct {
	ID           string
	Name         string
	Code         string
	Creator      string
	MembersCount int
	Active       bool
	CreatedAt    time.Time
}

type adminPageData struct {
	Overview adminOverview
	Users    []adminUser
	Rooms    []adminRoom
	IsOwner  bool
}

// formatFocusDuration renders accumulated focus time for the admin space.
//
// Hours, because minutes stopped being readable the moment the numbers got
// real -- "72,431" tells you nothing at a glance. Anything under an hour
// stays in minutes rather than rounding to "0 h", which would read as no
// activity at all rather than a little.
func formatFocusDuration(minutes int) string {
	if minutes < 60 {
		return fmt.Sprintf("%d min", minutes)
	}
	hours := float64(minutes) / 60
	if hours < 10 {
		// One decimal is worth keeping while the number is small: 1.5 h and
		// 1 h are meaningfully different, 340 h and 341 h are not.
		return fmt.Sprintf("%.1f h", hours)
	}
	return fmt.Sprintf("%s h", withThousands(int(hours+0.5)))
}

// withThousands groups an integer with commas.
func withThousands(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func validRole(role string) bool {
	return role == roleUser || role == roleAdmin || role == roleOwner
}

func canAccessAdmin(role string) bool {
	return role == roleAdmin || role == roleOwner
}

func canChangeRole(actor, target, requested string) bool {
	if actor != roleOwner || target == roleOwner {
		return false
	}
	return (target == roleUser && requested == roleAdmin) ||
		(target == roleAdmin && requested == roleUser)
}

func (a *app) bootstrapOwner(ctx context.Context, username string) error {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return nil
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin owner bootstrap: %w", err)
	}
	defer tx.Rollback(ctx)
	var targetID string
	err = tx.QueryRow(ctx, `
		UPDATE users
		SET role = 'owner'
		WHERE username = $1 AND role <> 'owner'
		RETURNING id`, username).Scan(&targetID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("bootstrap owner: %w", err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE username = $1)`, username).Scan(&exists); err != nil {
			return fmt.Errorf("check bootstrap owner: %w", err)
		}
		if !exists {
			log.Printf("owner bootstrap skipped: configured username was not found")
			return nil
		}
	} else {
		metadata, _ := json.Marshal(map[string]string{"method": "environment"})
		if _, err := tx.Exec(ctx, `
			INSERT INTO admin_audit_log (actor_user_id, action, target_type, target_id, metadata)
			VALUES (NULL, 'owner.bootstrapped', 'user', $1, $2::jsonb)`, targetID, metadata); err != nil {
			return fmt.Errorf("audit bootstrap owner: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit owner bootstrap: %w", err)
	}
	log.Printf("owner bootstrap confirmed for configured account")
	return nil
}

func (a *app) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := a.currentUser(r)
		if !ok {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		if !canAccessAdmin(u.Role) {
			if r.Method == http.MethodGet {
				// Serve the SPA shell with the real status: the admin view
				// re-asks /api/admin, gets the same verdict as 403 JSON, and
				// renders its forbidden card.
				if data, err := staticAssets.ReadFile("static/app/index.html"); err == nil {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write(data)
					return
				}
			}
			http.Error(w, "This space is limited to platform administrators.", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (a *app) requireAdminMutation(next http.HandlerFunc) http.HandlerFunc {
	return a.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			http.Error(w, "cross-site request rejected", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

func sameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	return err == nil && strings.EqualFold(parsed.Host, r.Host) &&
		(parsed.Scheme == "http" || parsed.Scheme == "https")
}

func (a *app) loadAdminPage(ctx context.Context, includeFocusSummaries bool) (adminPageData, error) {
	var data adminPageData
	data.IsOwner = includeFocusSummaries
	if err := a.db.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM users),
			(SELECT COUNT(*) FROM rooms),
			(SELECT COUNT(*) FROM timer_runs WHERE room_id IS NOT NULL AND ended_at IS NULL AND phase <> 'ended'),
			(SELECT COALESCE(SUM(focus_minutes), 0) FROM activity)
	`).Scan(&data.Overview.Users, &data.Overview.Rooms, &data.Overview.ActiveRoomTimers, &data.Overview.TotalFocusMinutes); err != nil {
		return data, err
	}

	userQuery := `
		SELECT u.id, u.username, u.role, u.created_at, COUNT(DISTINCT rm.room_id)
		FROM users u
		LEFT JOIN room_members rm ON rm.user_id = u.id
		GROUP BY u.id
		ORDER BY u.created_at DESC, u.username`
	if includeFocusSummaries {
		userQuery = `
			SELECT u.id, u.username, u.role, u.created_at, COUNT(DISTINCT rm.room_id),
			       COALESCE((SELECT SUM(a.focus_minutes) FROM activity a WHERE a.user_id = u.id), 0),
			       (SELECT MAX(a.activity_date)::timestamptz FROM activity a WHERE a.user_id = u.id)
			FROM users u
			LEFT JOIN room_members rm ON rm.user_id = u.id
			GROUP BY u.id
			ORDER BY u.created_at DESC, u.username`
	}
	rows, err := a.db.Query(ctx, userQuery)
	if err != nil {
		return data, err
	}
	for rows.Next() {
		var item adminUser
		if includeFocusSummaries {
			var last sql.NullTime
			err = rows.Scan(&item.ID, &item.Username, &item.Role, &item.JoinedAt, &item.RoomsCount, &item.FocusMinutes, &last)
			if last.Valid {
				item.LastActivityAt = &last.Time
			}
		} else {
			err = rows.Scan(&item.ID, &item.Username, &item.Role, &item.JoinedAt, &item.RoomsCount)
		}
		if err != nil {
			rows.Close()
			return data, err
		}
		data.Users = append(data.Users, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return data, err
	}
	rows.Close()

	rows, err = a.db.Query(ctx, `
		SELECT r.id, r.name, r.code, creator.username, COUNT(DISTINCT rm.user_id),
		       EXISTS (
		           SELECT 1 FROM timer_runs tr
		           WHERE tr.room_id = r.id AND tr.ended_at IS NULL AND tr.phase <> 'ended'
		       ),
		       r.created_at
		FROM rooms r
		JOIN users creator ON creator.id = r.creator_id
		LEFT JOIN room_members rm ON rm.room_id = r.id
		GROUP BY r.id, creator.username
		ORDER BY r.created_at DESC, r.name`)
	if err != nil {
		return data, err
	}
	defer rows.Close()
	for rows.Next() {
		var item adminRoom
		if err := rows.Scan(&item.ID, &item.Name, &item.Code, &item.Creator, &item.MembersCount, &item.Active, &item.CreatedAt); err != nil {
			return data, err
		}
		data.Rooms = append(data.Rooms, item)
	}
	return data, rows.Err()
}

func (a *app) adminChangeRole(w http.ResponseWriter, r *http.Request) {
	actor, _ := a.currentUser(r)
	targetID := r.PathValue("id")
	requested := strings.TrimSpace(r.FormValue("role"))
	if requested != roleUser && requested != roleAdmin {
		http.Error(w, "invalid role", http.StatusBadRequest)
		return
	}

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "could not update role", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	var actorRole, targetRole, targetUsername string
	if err = tx.QueryRow(r.Context(), `SELECT role FROM users WHERE id = $1 FOR UPDATE`, actor.ID).Scan(&actorRole); err != nil {
		http.Error(w, "could not verify actor", http.StatusForbidden)
		return
	}
	if err = tx.QueryRow(r.Context(), `SELECT role, username FROM users WHERE id = $1 FOR UPDATE`, targetID).Scan(&targetRole, &targetUsername); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load target", http.StatusInternalServerError)
		return
	}
	if !canChangeRole(actorRole, targetRole, requested) {
		http.Error(w, "role change is not permitted", http.StatusForbidden)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE users SET role = $1 WHERE id = $2`, requested, targetID); err != nil {
		http.Error(w, "could not update role", http.StatusInternalServerError)
		return
	}
	metadata, _ := json.Marshal(map[string]string{
		"from":     targetRole,
		"to":       requested,
		"username": targetUsername,
	})
	if _, err = tx.Exec(r.Context(), `
		INSERT INTO admin_audit_log (actor_user_id, action, target_type, target_id, metadata)
		VALUES ($1, 'user.role_changed', 'user', $2, $3::jsonb)`, actor.ID, targetID, metadata); err != nil {
		http.Error(w, "could not audit role update", http.StatusInternalServerError)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		http.Error(w, "could not update role", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (a *app) adminDeleteRoom(w http.ResponseWriter, r *http.Request) {
	actor, _ := a.currentUser(r)
	roomID := r.PathValue("id")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "could not delete room", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	var actorRole, name, code string
	if err = tx.QueryRow(r.Context(), `SELECT role FROM users WHERE id = $1 FOR UPDATE`, actor.ID).Scan(&actorRole); err != nil || !canAccessAdmin(actorRole) {
		http.Error(w, "room deletion is not permitted", http.StatusForbidden)
		return
	}
	if err = tx.QueryRow(r.Context(), `SELECT name, code FROM rooms WHERE id = $1 FOR UPDATE`, roomID).Scan(&name, &code); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load room", http.StatusInternalServerError)
		return
	}
	metadata, _ := json.Marshal(map[string]string{"name": name, "code": code})
	if _, err = tx.Exec(r.Context(), `
		INSERT INTO admin_audit_log (actor_user_id, action, target_type, target_id, metadata)
		VALUES ($1, 'room.deleted', 'room', $2, $3::jsonb)`, actor.ID, roomID, metadata); err != nil {
		http.Error(w, "could not audit room deletion", http.StatusInternalServerError)
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM rooms WHERE id = $1`, roomID); err != nil {
		http.Error(w, "could not delete room", http.StatusInternalServerError)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		http.Error(w, "could not delete room", http.StatusInternalServerError)
		return
	}
	a.hub.broadcast(code, "deleted")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// apiAdminUser is one person's detail page in the admin space.
//
// It exists so the role change lives somewhere deliberate. Promote used to be
// a button in a table row, one click from granting somebody this entire space
// -- every user, every room, every reset link. Behind a page you had to open
// first, it reads as the decision it is.
func (a *app) apiAdminUser(w http.ResponseWriter, r *http.Request) {
	actor, _ := a.currentUser(r)
	if !canAccessAdmin(actor.Role) {
		writeJSONError(w, http.StatusForbidden, "This space is limited to platform administrators.")
		return
	}
	var (
		u          user
		joinedAt   time.Time
		focus      int
		lastActive *time.Time
	)
	err := a.db.QueryRow(r.Context(), `
		SELECT u.id, u.username, u.display_name, u.role, u.avatar IS NOT NULL,
			COALESCE(EXTRACT(EPOCH FROM u.avatar_updated_at), 0)::bigint, u.created_at,
			COALESCE((SELECT SUM(focus_minutes)::int FROM activity WHERE user_id = u.id), 0),
			(SELECT MAX(activity_date) FROM activity WHERE user_id = u.id)
		FROM users u WHERE u.id = $1`, r.PathValue("id")).
		Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.HasAvatar, &u.AvatarVersion,
			&joinedAt, &focus, &lastActive)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Rooms they are in, and what they are in them. No timer state and no
	// door into the room: this page describes, it does not join.
	rows, err := a.db.Query(r.Context(), `
		SELECT r.id, r.code, r.name, m.role, r.creator_id = u.id
		FROM room_members m
		JOIN rooms r ON r.id = m.room_id
		JOIN users u ON u.id = m.user_id
		WHERE m.user_id = $1
		ORDER BY r.created_at DESC`, u.ID)
	rooms := []map[string]any{}
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, code, name, role string
			var creator bool
			if rows.Scan(&id, &code, &name, &role, &creator) == nil {
				rooms = append(rooms, map[string]any{
					"id": id, "code": code, "name": name,
					"admin": creator || role == roomRoleAdmin, "creator": creator,
				})
			}
		}
	}

	payload := map[string]any{
		"user": map[string]any{
			"id": u.ID, "username": u.Username, "displayName": u.DisplayName,
			"role": u.Role, "hasAvatar": u.HasAvatar, "avatarVersion": u.AvatarVersion,
		},
		"joined":       joinedAt.Format("Jan 2, 2006"),
		"focusMinutes": focus,
		"focusTime":    formatFocusDuration(focus),
		"rooms":        rooms,
		// Only the owner may change roles, and never the owner's own.
		"canChangeRole": actor.Role == roleOwner && u.Role != roleOwner,
		"isOwner":       actor.Role == roleOwner,
	}
	if lastActive != nil {
		payload["lastActivity"] = lastActive.Format("Jan 2, 2006")
	}
	writeJSON(w, payload)
}

// apiAdminRoom is one room's detail page: who is in it and what they are in
// it, so room admins can be marked and unmarked from the admin space.
//
// Read-only about the room itself. There is deliberately no way to join from
// here: staff access is for keeping the service working, not for turning up
// in other people's rooms.
func (a *app) apiAdminRoom(w http.ResponseWriter, r *http.Request) {
	actor, _ := a.currentUser(r)
	if !canAccessAdmin(actor.Role) {
		writeJSONError(w, http.StatusForbidden, "This space is limited to platform administrators.")
		return
	}
	var (
		rm        room
		createdAt time.Time
		creator   string
	)
	err := a.db.QueryRow(r.Context(), `
		SELECT r.id, r.code, r.name, r.creator_id, r.focus_minutes, r.break_minutes,
			r.auto_sessions, r.auto_roll, r.ephemeral, r.require_checkin, r.created_at,
			COALESCE(u.display_name, '')
		FROM rooms r LEFT JOIN users u ON u.id = r.creator_id
		WHERE r.id = $1`, r.PathValue("id")).
		Scan(&rm.ID, &rm.Code, &rm.Name, &rm.CreatorID, &rm.FocusMinutes, &rm.BreakMinutes,
			&rm.AutoSessions, &rm.AutoRoll, &rm.Ephemeral, &rm.RequireCheckin, &createdAt, &creator)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	roster, _ := a.roomRoster(r.Context(), rm)
	members := make([]map[string]any, 0, len(roster))
	for _, m := range roster {
		members = append(members, map[string]any{
			"id": m.User.ID, "username": m.User.Username, "displayName": m.User.DisplayName,
			"hasAvatar": m.User.HasAvatar, "avatarVersion": m.User.AvatarVersion,
			"admin": m.IsAdmin(), "creator": m.Creator,
		})
	}
	active := a.roomRunActive(r.Context(), rm.ID)
	writeJSON(w, map[string]any{
		"room": map[string]any{
			"id": rm.ID, "code": rm.Code, "name": rm.Name, "creator": creator,
			"creatorId": rm.CreatorID, "ephemeral": rm.Ephemeral,
			"focusMinutes": rm.FocusMinutes, "breakMinutes": rm.BreakMinutes,
			"autoSessions": rm.AutoSessions, "autoRoll": rm.AutoRoll,
			"requireCheckin": rm.RequireCheckin,
			"created":        createdAt.Format("Jan 2, 2006"),
			"active":         active,
		},
		"members": members,
		"isOwner": actor.Role == roleOwner,
	})
}

// adminSetRoomRole marks or unmarks a room admin from the admin space.
//
// It goes through setRoomMemberRole but not canAdminRoom: staff authority is
// its own thing and does not come from being in the room. Audited, because
// this is one person changing another's standing somewhere they aren't.
func (a *app) adminSetRoomRole(w http.ResponseWriter, r *http.Request) {
	actor, _ := a.currentUser(r)
	roomID := r.PathValue("id")
	var rm room
	if err := a.db.QueryRow(r.Context(),
		`SELECT id, code, name, creator_id FROM rooms WHERE id = $1`, roomID).
		Scan(&rm.ID, &rm.Code, &rm.Name, &rm.CreatorID); err != nil {
		http.NotFound(w, r)
		return
	}
	targetID, role := r.FormValue("user_id"), r.FormValue("role")
	if err := a.setRoomMemberRole(r.Context(), rm, targetID, role); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	metadata, _ := json.Marshal(map[string]string{"room": rm.Code, "role": role, "user": targetID})
	if _, err := a.db.Exec(r.Context(), `
		INSERT INTO admin_audit_log (actor_user_id, action, target_type, target_id, metadata)
		VALUES ($1, 'room.role_changed', 'room', $2, $3::jsonb)`, actor.ID, rm.ID, metadata); err != nil {
		log.Printf("admin: audit room role change: %v", err)
	}
	a.hub.broadcast(rm.Code, "members")
	w.WriteHeader(http.StatusNoContent)
}
