package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// postProfile drives a profile form post and returns the redirect's ?error=.
func postProfile(t *testing.T, a *app, path string, h http.HandlerFunc, fields url.Values) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(fields.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h(rec, req)
	if u, err := url.Parse(rec.Header().Get("Location")); err == nil {
		return u.Query().Get("error")
	}
	return ""
}

func userExists(t *testing.T, a *app, id string) bool {
	t.Helper()
	var exists bool
	_ = a.db.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`, id).Scan(&exists)
	return exists
}

func roomExists(t *testing.T, a *app, code string) bool {
	t.Helper()
	var exists bool
	_ = a.db.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM rooms WHERE code = $1)`, code).Scan(&exists)
	return exists
}

// The whole point of A5: closing an account must not silently take a room
// full of other people's work with it.
func TestDeletingAnAccountIsBlockedByRoomsWithOtherPeople(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	member := makeUser(t, a, "Member")
	rm := makeRoom(t, a, owner)
	a.addRoomMember(context.Background(), rm.ID, member.ID)

	a.asUser(owner)
	errMsg := postProfile(t, a, "/profile/delete", a.deleteAccount, url.Values{"password": {testUserPassword}})
	if errMsg == "" || !strings.Contains(errMsg, "own") {
		t.Fatalf("deletion should have been blocked, got %q", errMsg)
	}
	if !userExists(t, a, owner.ID) {
		t.Fatal("account was deleted despite owning a shared room")
	}
	if !roomExists(t, a, rm.Code) {
		t.Fatal("the room was deleted")
	}
}

// A room you're alone in is never worth a question: nothing is lost.
func TestRoomsYouAreAloneInDoNotBlockDeletion(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	makeRoom(t, a, owner)

	pending, err := a.roomsNeedingDisposition(context.Background(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("a solo room should not block deletion, got %d", len(pending))
	}
}

func TestHandingOverARoomClearsTheWayForDeletion(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	heir := makeUser(t, a, "Heir")
	ctx := context.Background()
	rm := makeRoom(t, a, owner)
	makeAdmin(t, a, rm, heir)

	a.asUser(owner)
	if errMsg := postProfile(t, a, "/profile/rooms/hand-over", a.handOverRoom,
		url.Values{"code": {rm.Code}, "user_id": {heir.ID}}); errMsg != "" {
		t.Fatalf("hand over: %s", errMsg)
	}

	got, _ := a.findRoom(ctx, rm.Code)
	if got.CreatorID != heir.ID {
		t.Fatalf("owner is %q, want the heir", got.CreatorID)
	}
	// Immediate, with no pending window: there would be nobody left to undo it.
	if got.PendingOwnerID != "" || !got.OwnershipTransferAt.IsZero() {
		t.Error("hand-over on the deletion path should not leave a pending window")
	}
	pending, err := a.roomsNeedingDisposition(ctx, owner.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("room still blocking deletion: %v %d", err, len(pending))
	}
}

func TestHandOverGuards(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	member := makeUser(t, a, "Member")
	stranger := makeUser(t, a, "Stranger")
	rm := makeRoom(t, a, owner)
	a.addRoomMember(context.Background(), rm.ID, member.ID)

	// Someone who isn't in the room can't be handed it.
	a.asUser(owner)
	if errMsg := postProfile(t, a, "/profile/rooms/hand-over", a.handOverRoom,
		url.Values{"code": {rm.Code}, "user_id": {stranger.ID}}); errMsg == "" {
		t.Error("handing the room to an outsider should be refused")
	}
	// A member who doesn't own it can't hand it to themselves.
	a.asUser(member)
	if errMsg := postProfile(t, a, "/profile/rooms/hand-over", a.handOverRoom,
		url.Values{"code": {rm.Code}, "user_id": {member.ID}}); errMsg == "" {
		t.Error("a non-owner should not be able to hand the room over")
	}
	if got, _ := a.findRoom(context.Background(), rm.Code); got.CreatorID != owner.ID {
		t.Fatal("ownership moved when it should not have")
	}
}

// Unlike the members-page transfer, this one names the room's members so the
// profile page can offer a pick list -- admins first.
func TestRoomsNeedingDispositionListsCandidatesAdminsFirst(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	admin := makeUser(t, a, "Zed Admin")
	member := makeUser(t, a, "Ann Member")
	ctx := context.Background()
	rm := makeRoom(t, a, owner)
	makeAdmin(t, a, rm, admin)
	a.addRoomMember(ctx, rm.ID, member.ID)

	pending, err := a.roomsNeedingDisposition(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("got %d rooms, want 1", len(pending))
	}
	got := pending[0]
	if got.Room.Code != rm.Code {
		t.Errorf("room = %q, want %q", got.Room.Code, rm.Code)
	}
	if len(got.Members) != 2 {
		t.Fatalf("got %d candidates, want 2 (the owner excluded)", len(got.Members))
	}
	if got.Members[0].User.ID != admin.ID {
		t.Errorf("first candidate = %q, want the admin", got.Members[0].User.DisplayName)
	}
	for _, m := range got.Members {
		if m.User.ID == owner.ID {
			t.Error("the departing owner is listed as a candidate to receive the room")
		}
	}
}

// And once nothing is in the way, deletion still works -- the guard must not
// have made accounts undeletable.
func TestDeletingAnAccountWorksOnceRoomsAreDealtWith(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	heir := makeUser(t, a, "Heir")
	rm := makeRoom(t, a, owner)
	solo := makeRoom(t, a, owner)
	makeAdmin(t, a, rm, heir)

	a.asUser(owner)
	if errMsg := postProfile(t, a, "/profile/rooms/hand-over", a.handOverRoom,
		url.Values{"code": {rm.Code}, "user_id": {heir.ID}}); errMsg != "" {
		t.Fatalf("hand over: %s", errMsg)
	}
	if errMsg := postProfile(t, a, "/profile/delete", a.deleteAccount,
		url.Values{"password": {testUserPassword}}); errMsg != "" {
		t.Fatalf("delete: %s", errMsg)
	}

	if userExists(t, a, owner.ID) {
		t.Error("account was not deleted")
	}
	// The room they handed over survives them; the one they were alone in does not.
	if !roomExists(t, a, rm.Code) {
		t.Error("the handed-over room went with the account")
	}
	if roomExists(t, a, solo.Code) {
		t.Error("a room they were alone in should have gone with the account")
	}
}

// A wrong password still stops deletion, even with nothing else in the way.
func TestDeletingAnAccountStillNeedsThePassword(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	a.asUser(owner)
	if errMsg := postProfile(t, a, "/profile/delete", a.deleteAccount,
		url.Values{"password": {"not-the-password"}}); errMsg == "" {
		t.Fatal("deletion should have been refused")
	}
	if !userExists(t, a, owner.ID) {
		t.Fatal("account deleted with the wrong password")
	}
}
