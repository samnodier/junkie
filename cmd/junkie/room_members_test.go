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

// postRoomAction drives POST /r/{code}/{action} the way the SPA's postForm
// does, and returns the redirect's ?error= -- which is exactly what the user
// would see as a toast.
func postRoomAction(t *testing.T, a *app, code, action string, fields url.Values) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/r/"+code+"/"+action, strings.NewReader(fields.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.roomAction(rec, req)
	dest := rec.Header().Get("Location")
	errMsg := ""
	if u, err := url.Parse(dest); err == nil {
		errMsg = u.Query().Get("error")
	}
	return rec.Code, errMsg
}

func TestCreatorCanPromoteAndDemoteAMember(t *testing.T) {
	a := newTestApp(t)
	creator := makeUser(t, a, "Creator")
	member := makeUser(t, a, "Member")
	rm := makeRoom(t, a, creator)
	a.addRoomMember(context.Background(), rm.ID, member.ID)

	a.asUser(creator)
	if _, errMsg := postRoomAction(t, a, rm.Code, "make-admin", url.Values{"user_id": {member.ID}}); errMsg != "" {
		t.Fatalf("make-admin: %s", errMsg)
	}
	if got := memberRole(t, a, rm.ID, member.ID); got != roomRoleAdmin {
		t.Errorf("role after promote = %q, want %q", got, roomRoleAdmin)
	}
	if !a.canAdminRoom(context.Background(), rm, member.ID) {
		t.Error("promoted member should pass canAdminRoom")
	}

	if _, errMsg := postRoomAction(t, a, rm.Code, "remove-admin", url.Values{"user_id": {member.ID}}); errMsg != "" {
		t.Fatalf("remove-admin: %s", errMsg)
	}
	if got := memberRole(t, a, rm.ID, member.ID); got != roomRoleMember {
		t.Errorf("role after demote = %q, want %q", got, roomRoleMember)
	}
	if a.canAdminRoom(context.Background(), rm, member.ID) {
		t.Error("demoted member should no longer pass canAdminRoom")
	}
}

// An admin can appoint further admins -- the group-chat model Sam asked for.
func TestAdminCanPromoteAnotherMember(t *testing.T) {
	a := newTestApp(t)
	creator := makeUser(t, a, "Creator")
	admin := makeUser(t, a, "Admin")
	member := makeUser(t, a, "Member")
	rm := makeRoom(t, a, creator)
	ctx := context.Background()
	a.addRoomMember(ctx, rm.ID, admin.ID)
	a.addRoomMember(ctx, rm.ID, member.ID)
	if err := a.setRoomMemberRole(ctx, rm, admin.ID, roomRoleAdmin); err != nil {
		t.Fatal(err)
	}

	a.asUser(admin)
	if _, errMsg := postRoomAction(t, a, rm.Code, "make-admin", url.Values{"user_id": {member.ID}}); errMsg != "" {
		t.Fatalf("admin promoting a member: %s", errMsg)
	}
	if got := memberRole(t, a, rm.ID, member.ID); got != roomRoleAdmin {
		t.Errorf("role = %q, want %q", got, roomRoleAdmin)
	}
}

func TestPlainMemberCannotChangeRoles(t *testing.T) {
	a := newTestApp(t)
	creator := makeUser(t, a, "Creator")
	member := makeUser(t, a, "Member")
	other := makeUser(t, a, "Other")
	ctx := context.Background()
	rm := makeRoom(t, a, creator)
	a.addRoomMember(ctx, rm.ID, member.ID)
	a.addRoomMember(ctx, rm.ID, other.ID)

	a.asUser(member)
	_, errMsg := postRoomAction(t, a, rm.Code, "make-admin", url.Values{"user_id": {other.ID}})
	if errMsg == "" {
		t.Fatal("a plain member must not be able to promote anyone")
	}
	if got := memberRole(t, a, rm.ID, other.ID); got != roomRoleMember {
		t.Errorf("role = %q, want it unchanged", got)
	}
}

// The creator's authority is rooms.creator_id. Demoting them through the role
// column would leave the two disagreeing, so it is refused outright.
func TestCreatorCannotBeDemoted(t *testing.T) {
	a := newTestApp(t)
	creator := makeUser(t, a, "Creator")
	admin := makeUser(t, a, "Admin")
	ctx := context.Background()
	rm := makeRoom(t, a, creator)
	a.addRoomMember(ctx, rm.ID, admin.ID)
	if err := a.setRoomMemberRole(ctx, rm, admin.ID, roomRoleAdmin); err != nil {
		t.Fatal(err)
	}

	a.asUser(admin)
	_, errMsg := postRoomAction(t, a, rm.Code, "remove-admin", url.Values{"user_id": {creator.ID}})
	if errMsg == "" {
		t.Fatal("the creator must not be demotable")
	}
	if !a.canAdminRoom(ctx, rm, creator.ID) {
		t.Error("creator lost admin rights")
	}
}

func TestPromotingSomeoneOutsideTheRoomIsRefused(t *testing.T) {
	a := newTestApp(t)
	creator := makeUser(t, a, "Creator")
	stranger := makeUser(t, a, "Stranger")
	rm := makeRoom(t, a, creator)

	a.asUser(creator)
	_, errMsg := postRoomAction(t, a, rm.Code, "make-admin", url.Values{"user_id": {stranger.ID}})
	if errMsg == "" {
		t.Fatal("promoting a non-member must be refused")
	}
	if got := memberRole(t, a, rm.ID, stranger.ID); got != "" {
		t.Errorf("stranger gained a role row: %q", got)
	}
}

// The roster drives the page's ordering and badges, and must never carry
// anything about what a member has been doing.
func TestRoomRosterOrdersAndFlagsWithoutLeakingActivity(t *testing.T) {
	a := newTestApp(t)
	creator := makeUser(t, a, "Zoe Creator")
	admin := makeUser(t, a, "Yara Admin")
	member := makeUser(t, a, "Alice Member")
	ctx := context.Background()
	rm := makeRoom(t, a, creator)
	a.addRoomMember(ctx, rm.ID, admin.ID)
	a.addRoomMember(ctx, rm.ID, member.ID)
	if err := a.setRoomMemberRole(ctx, rm, admin.ID, roomRoleAdmin); err != nil {
		t.Fatal(err)
	}

	roster, err := a.roomRoster(ctx, rm)
	if err != nil {
		t.Fatal(err)
	}
	if len(roster) != 3 {
		t.Fatalf("roster has %d members, want 3", len(roster))
	}
	// Creator first, then admins, then everyone else -- despite the creator
	// and admin sorting last alphabetically.
	if !roster[0].Creator || roster[0].User.ID != creator.ID {
		t.Errorf("first row = %+v, want the creator", roster[0].User.DisplayName)
	}
	if roster[1].User.ID != admin.ID || !roster[1].IsAdmin() {
		t.Errorf("second row = %q, want the admin", roster[1].User.DisplayName)
	}
	if roster[2].User.ID != member.ID || roster[2].IsAdmin() {
		t.Errorf("third row = %q, want the plain member", roster[2].User.DisplayName)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/room/"+rm.Code+"/members", nil)
	req.SetPathValue("code", rm.Code)
	rec := httptest.NewRecorder()
	a.asUser(admin).apiRoomMembers(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, leaked := range []string{"focusMinutes", "lastActivity", "heatmap", "minutes"} {
		if strings.Contains(body, leaked) {
			t.Errorf("members payload leaks %q to a room admin: %s", leaked, body)
		}
	}
	var payload struct {
		ViewerAdmin bool `json:"viewerAdmin"`
		ViewerOwner bool `json:"viewerOwner"`
		Members     []struct {
			Admin   bool `json:"admin"`
			Creator bool `json:"creator"`
		} `json:"members"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.ViewerAdmin {
		t.Error("an admin viewer should be told they can act")
	}
	if payload.ViewerOwner {
		t.Error("an admin is not the owner")
	}
	if !payload.Members[0].Creator || !payload.Members[1].Admin || payload.Members[2].Admin {
		t.Errorf("badge flags wrong: %+v", payload.Members)
	}
}

// A plain member gets the roster but no ability to act, so the page renders
// without buttons rather than with buttons that would be refused.
func TestPlainMemberSeesNoAdminRights(t *testing.T) {
	a := newTestApp(t)
	creator := makeUser(t, a, "Creator")
	member := makeUser(t, a, "Member")
	rm := makeRoom(t, a, creator)
	a.addRoomMember(context.Background(), rm.ID, member.ID)

	req := httptest.NewRequest(http.MethodGet, "/api/room/"+rm.Code+"/members", nil)
	req.SetPathValue("code", rm.Code)
	rec := httptest.NewRecorder()
	a.asUser(member).apiRoomMembers(rec, req)
	var payload struct {
		ViewerAdmin bool `json:"viewerAdmin"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ViewerAdmin {
		t.Error("a plain member must not be told they can administer the room")
	}
}
