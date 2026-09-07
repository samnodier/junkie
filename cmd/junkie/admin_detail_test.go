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

func TestFormatFocusDuration(t *testing.T) {
	for _, tc := range []struct {
		minutes int
		want    string
	}{
		{0, "0 min"},
		{45, "45 min"}, // under an hour stays minutes: "0 h" would read as none
		{59, "59 min"},
		{60, "1.0 h"},
		{90, "1.5 h"},
		{599, "10.0 h"}, // one decimal while the number is small
		{600, "10 h"},
		{72431, "1,207 h"},
	} {
		if got := formatFocusDuration(tc.minutes); got != tc.want {
			t.Errorf("formatFocusDuration(%d) = %q, want %q", tc.minutes, got, tc.want)
		}
	}
}

func TestWithThousands(t *testing.T) {
	for in, want := range map[int]string{0: "0", 7: "7", 999: "999", 1000: "1,000", 1234567: "1,234,567"} {
		if got := withThousands(in); got != want {
			t.Errorf("withThousands(%d) = %q, want %q", in, got, want)
		}
	}
}

func staffUser(t *testing.T, a *app, role string) user {
	t.Helper()
	u := makeUser(t, a, "Staff")
	if _, err := a.db.Exec(context.Background(), `UPDATE users SET role = $1 WHERE id = $2`, role, u.ID); err != nil {
		t.Fatal(err)
	}
	u.Role = role
	return u
}

func getJSON(t *testing.T, a *app, h http.HandlerFunc, path, idValue string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.SetPathValue("id", idValue)
	rec := httptest.NewRecorder()
	h(rec, req)
	var payload map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	return rec.Code, payload
}

func TestAdminUserPageIsStaffOnly(t *testing.T) {
	a := newTestApp(t)
	subject := makeUser(t, a, "Subject")
	nosy := makeUser(t, a, "Nosy")

	a.asUser(nosy)
	if status, _ := getJSON(t, a, a.apiAdminUser, "/api/admin/users/"+subject.ID, subject.ID); status != http.StatusForbidden {
		t.Errorf("a plain user got %d, want %d", status, http.StatusForbidden)
	}
	a.asUser(staffUser(t, a, roleAdmin))
	if status, _ := getJSON(t, a, a.apiAdminUser, "/api/admin/users/"+subject.ID, subject.ID); status != http.StatusOK {
		t.Errorf("staff got %d, want 200", status)
	}
}

// Only the owner may change platform roles, and the page has to say so --
// that flag is what decides whether the button renders at all.
func TestOnlyTheOwnerIsOfferedRoleChanges(t *testing.T) {
	a := newTestApp(t)
	subject := makeUser(t, a, "Subject")

	a.asUser(staffUser(t, a, roleAdmin))
	_, payload := getJSON(t, a, a.apiAdminUser, "/api/admin/users/"+subject.ID, subject.ID)
	if payload["canChangeRole"] != false {
		t.Error("an admin should not be offered role changes")
	}
	a.asUser(staffUser(t, a, roleOwner))
	_, payload = getJSON(t, a, a.apiAdminUser, "/api/admin/users/"+subject.ID, subject.ID)
	if payload["canChangeRole"] != true {
		t.Error("the owner should be offered role changes")
	}
	// ...but never on the owner's own account.
	owner := staffUser(t, a, roleOwner)
	a.asUser(owner)
	_, payload = getJSON(t, a, a.apiAdminUser, "/api/admin/users/"+owner.ID, owner.ID)
	if payload["canChangeRole"] != false {
		t.Error("the owner's own role must not be changeable here")
	}
}

func TestAdminUserPageListsTheirRoomsAndStanding(t *testing.T) {
	a := newTestApp(t)
	subject := makeUser(t, a, "Subject")
	other := makeUser(t, a, "Other")
	ctx := context.Background()
	own := makeRoom(t, a, subject)
	joined := makeRoom(t, a, other)
	a.addRoomMember(ctx, joined.ID, subject.ID)

	a.asUser(staffUser(t, a, roleOwner))
	status, payload := getJSON(t, a, a.apiAdminUser, "/api/admin/users/"+subject.ID, subject.ID)
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	rooms, _ := payload["rooms"].([]any)
	if len(rooms) != 2 {
		t.Fatalf("got %d rooms, want 2", len(rooms))
	}
	byCode := map[string]map[string]any{}
	for _, r := range rooms {
		row := r.(map[string]any)
		byCode[row["code"].(string)] = row
	}
	if byCode[own.Code]["creator"] != true {
		t.Error("their own room should be flagged as theirs")
	}
	if byCode[joined.Code]["creator"] != false || byCode[joined.Code]["admin"] != false {
		t.Error("a room they merely joined should carry no standing")
	}
	if _, ok := payload["focusTime"]; !ok {
		t.Error("focus time should be rendered for staff")
	}
}

func TestAdminRoomPageShowsMembersAndNoWayIn(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	admin := makeUser(t, a, "Admin")
	rm := makeRoom(t, a, owner)
	makeAdmin(t, a, rm, admin)

	a.asUser(staffUser(t, a, roleOwner))
	status, payload := getJSON(t, a, a.apiAdminRoom, "/api/admin/rooms/"+rm.ID, rm.ID)
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	members, _ := payload["members"].([]any)
	if len(members) != 2 {
		t.Fatalf("got %d members, want 2", len(members))
	}
	if members[0].(map[string]any)["creator"] != true {
		t.Error("the creator should come first and be flagged")
	}
	if members[1].(map[string]any)["admin"] != true {
		t.Error("the room admin should be flagged")
	}
	// C4: nothing in this payload may amount to a door into the room.
	raw, _ := json.Marshal(payload)
	for _, forbidden := range []string{"join", "timerId", "participants"} {
		if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(forbidden)) {
			t.Errorf("the admin room payload offers %q: %s", forbidden, raw)
		}
	}
}

func TestStaffCanMarkAndUnmarkRoomAdmins(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	member := makeUser(t, a, "Member")
	ctx := context.Background()
	rm := makeRoom(t, a, owner)
	a.addRoomMember(ctx, rm.ID, member.ID)
	staff := staffUser(t, a, roleOwner)

	post := func(role string) int {
		req := httptest.NewRequest(http.MethodPost, "/admin/rooms/"+rm.ID+"/room-role",
			strings.NewReader(url.Values{"user_id": {member.ID}, "role": {role}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetPathValue("id", rm.ID)
		rec := httptest.NewRecorder()
		a.asUser(staff).adminSetRoomRole(rec, req)
		return rec.Code
	}

	if got := post(roomRoleAdmin); got != http.StatusNoContent {
		t.Fatalf("promote returned %d", got)
	}
	if role := memberRole(t, a, rm.ID, member.ID); role != roomRoleAdmin {
		t.Errorf("role = %q, want %q", role, roomRoleAdmin)
	}
	if got := post(roomRoleMember); got != http.StatusNoContent {
		t.Fatalf("demote returned %d", got)
	}
	if role := memberRole(t, a, rm.ID, member.ID); role != roomRoleMember {
		t.Errorf("role = %q, want %q", role, roomRoleMember)
	}

	// Staff acting on a room leaves a trail, since it is one person changing
	// another's standing somewhere they aren't.
	var logged int
	if err := a.db.QueryRow(ctx,
		`SELECT count(*) FROM admin_audit_log WHERE actor_user_id = $1 AND action = 'room.role_changed'`,
		staff.ID).Scan(&logged); err != nil {
		t.Fatal(err)
	}
	if logged != 2 {
		t.Errorf("audit rows = %d, want 2", logged)
	}
}
