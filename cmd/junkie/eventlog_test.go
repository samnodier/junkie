package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func roomEvents(t *testing.T, a *app, rm room) []map[string]any {
	t.Helper()
	events, err := a.readEvents(context.Background(), rm.ID, maxEventPage)
	if err != nil {
		t.Fatal(err)
	}
	out := []map[string]any{}
	for _, e := range eventsJSON(events) {
		out = append(out, e)
	}
	return out
}

func hasEvent(events []map[string]any, action string) map[string]any {
	for _, e := range events {
		if e["action"] == action {
			return e
		}
	}
	return nil
}

// The events worth having are the ones someone later disputes: who joined,
// who was removed, who was made an admin, when the room changed hands.
func TestRoomActionsAreRecorded(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	heir := makeUser(t, a, "Heir")
	spare := makeUser(t, a, "Spare")
	ctx := context.Background()
	rm := makeRoom(t, a, owner)
	a.addRoomMember(ctx, rm.ID, heir.ID)
	a.addRoomMember(ctx, rm.ID, spare.ID)

	a.asUser(owner)
	postRoomAction(t, a, rm.Code, "make-admin", url.Values{"user_id": {heir.ID}})
	postRoomAction(t, a, rm.Code, "transfer-ownership", url.Values{"user_id": {heir.ID}})
	postRoomAction(t, a, rm.Code, "cancel-transfer", nil)
	postRoomAction(t, a, rm.Code, "remove-member", url.Values{"user_id": {spare.ID}})
	postRoomAction(t, a, rm.Code, "remove-admin", url.Values{"user_id": {heir.ID}})

	events := roomEvents(t, a, rm)
	for _, action := range []string{
		eventRoomJoined, eventRoomAdminAdded, eventRoomTransferStart,
		eventRoomTransferUndo, eventRoomMemberRemoved, eventRoomAdminRemoved,
	} {
		if hasEvent(events, action) == nil {
			t.Errorf("no %s event recorded", action)
		}
	}
	// The removal names who was removed, not just who did it.
	if e := hasEvent(events, eventRoomMemberRemoved); e != nil && e["target"] != spare.Username {
		t.Errorf("removal target = %v, want %q", e["target"], spare.Username)
	}
	// Newest first, so the log reads top-down as most-recent-first.
	if len(events) > 1 && events[0]["at"].(string) < events[len(events)-1]["at"].(string) {
		t.Error("events are not newest first")
	}
}

// Sam's explicit cut: the log records what happened, not what people did with
// their time.
func TestFocusSessionsAreNotLogged(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	ctx := context.Background()
	rm := makeRoom(t, a, owner)

	if _, err := a.startRoomTimer(ctx, rm, owner.ID, rm.FocusMinutes); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(roomEvents(t, a, rm))
	for _, noisy := range []string{"focus", "session", "timer"} {
		if strings.Contains(strings.ToLower(string(raw)), noisy) {
			t.Errorf("the log records %q events: %s", noisy, raw)
		}
	}
}

// A room's history is for the people who run that room, and it must not carry
// anything from any other room.
func TestRoomHistoryIsScopedAndAdminOnly(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	member := makeUser(t, a, "Member")
	ctx := context.Background()
	mine := makeRoom(t, a, owner)
	elsewhere := makeRoom(t, a, owner)
	a.addRoomMember(ctx, mine.ID, member.ID)
	a.addRoomMember(ctx, elsewhere.ID, member.ID)

	read := func(as user, code string) (int, map[string]any) {
		req := httptest.NewRequest(http.MethodGet, "/api/room/"+code+"/events", nil)
		req.SetPathValue("code", code)
		rec := httptest.NewRecorder()
		a.asUser(as).apiRoomEvents(rec, req)
		var payload map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &payload)
		return rec.Code, payload
	}

	if status, _ := read(member, mine.Code); status != http.StatusForbidden {
		t.Errorf("a plain member got %d, want %d", status, http.StatusForbidden)
	}
	status, payload := read(owner, mine.Code)
	if status != http.StatusOK {
		t.Fatalf("the owner got %d", status)
	}
	raw, _ := json.Marshal(payload)
	if strings.Contains(string(raw), elsewhere.Code) {
		t.Errorf("this room's history leaks another room: %s", raw)
	}
}

func TestAdminLogIsStaffOnlyAndServiceWide(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	rm := makeRoom(t, a, owner)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/events", nil)
	rec := httptest.NewRecorder()
	a.asUser(owner).apiAdminEvents(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a plain user got %d, want %d", rec.Code, http.StatusForbidden)
	}

	rec = httptest.NewRecorder()
	a.asUser(staffUser(t, a, roleOwner)).apiAdminEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("staff got %d", rec.Code)
	}
	var payload struct {
		Events        []map[string]any `json:"events"`
		RetentionDays int              `json:"retentionDays"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.RetentionDays != 90 {
		t.Errorf("retention = %d days, want 90", payload.RetentionDays)
	}
	var found bool
	for _, e := range payload.Events {
		if e["room"] == rm.Code {
			found = true
		}
	}
	if !found {
		t.Error("the service-wide log should carry every room's events")
	}
}

// Signing in is logged, but never with an address attached.
func TestSignInIsLoggedWithoutAnAddress(t *testing.T) {
	a := newTestApp(t)
	u := makeUser(t, a, "Someone")
	ctx := context.Background()
	a.logEvent(ctx, u.ID, eventSignedIn, "user", u.ID, "", nil)

	events, err := a.readEvents(ctx, "", maxEventPage)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(eventsJSON(events))
	if !strings.Contains(string(raw), eventSignedIn) {
		t.Fatal("sign-in was not recorded")
	}
	for _, leaked := range []string{"ip", "addr", "192.", "127.0"} {
		if strings.Contains(strings.ToLower(string(raw)), leaked) {
			t.Errorf("sign-in events carry %q: %s", leaked, raw)
		}
	}
}

// Role grants and handovers outlive the retention window; ordinary noise
// does not.
func TestRetentionKeepsWhatPeopleArgueAbout(t *testing.T) {
	for _, action := range []string{eventRoomAdminAdded, eventRoomAdminRemoved, eventRoomTransferred, eventAccountDeleted} {
		if !permanentEvents[action] {
			t.Errorf("%s should be kept permanently", action)
		}
	}
	for _, action := range []string{eventSignedIn, eventRoomJoined, eventRoomSettings, eventAccountCreated} {
		if permanentEvents[action] {
			t.Errorf("%s should expire with the retention window", action)
		}
	}
	if days := int(eventRetention.Hours() / 24); days != 90 {
		t.Errorf("retention = %d days, want 90", days)
	}
}

// The sweep must delete expired ordinary events and spare the permanent ones.
func TestSweepDropsExpiredEventsButKeepsThePermanentOnes(t *testing.T) {
	a := newTestApp(t)
	u := makeUser(t, a, "Someone")
	ctx := context.Background()
	a.logEvent(ctx, u.ID, eventSignedIn, "user", u.ID, "", nil)
	a.logEvent(ctx, u.ID, eventRoomAdminAdded, "user", u.ID, "", nil)
	if _, err := a.db.Exec(ctx,
		`UPDATE admin_audit_log SET created_at = now() - interval '200 days' WHERE actor_user_id = $1`, u.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := a.pruneEventLog(ctx); err != nil {
		t.Fatal(err)
	}

	var signIns, grants int
	_ = a.db.QueryRow(ctx, `SELECT count(*) FROM admin_audit_log WHERE actor_user_id = $1 AND action = $2`, u.ID, eventSignedIn).Scan(&signIns)
	_ = a.db.QueryRow(ctx, `SELECT count(*) FROM admin_audit_log WHERE actor_user_id = $1 AND action = $2`, u.ID, eventRoomAdminAdded).Scan(&grants)
	if signIns != 0 {
		t.Errorf("expired sign-in survived the sweep (%d rows)", signIns)
	}
	if grants != 1 {
		t.Errorf("a permanent event was swept (%d rows, want 1)", grants)
	}
}
