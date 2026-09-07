package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAdminCanRemoveAPlainMember(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	admin := makeUser(t, a, "Admin")
	member := makeUser(t, a, "Member")
	ctx := context.Background()
	rm := makeRoom(t, a, owner)
	makeAdmin(t, a, rm, admin)
	a.addRoomMember(ctx, rm.ID, member.ID)

	a.asUser(admin)
	if _, errMsg := postRoomAction(t, a, rm.Code, "remove-member", url.Values{"user_id": {member.ID}}); errMsg != "" {
		t.Fatalf("remove: %s", errMsg)
	}
	if a.isRoomMember(ctx, rm.ID, member.ID) {
		t.Error("member is still in the room")
	}
	// Membership is all that goes: rejoining puts them back as before.
	a.addRoomMember(ctx, rm.ID, member.ID)
	if role := memberRole(t, a, rm.ID, member.ID); role != roomRoleMember {
		t.Errorf("role on rejoin = %q, want %q", role, roomRoleMember)
	}
}

func TestRemoveMemberGuards(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	admin := makeUser(t, a, "Admin")
	member := makeUser(t, a, "Member")
	stranger := makeUser(t, a, "Stranger")
	ctx := context.Background()
	rm := makeRoom(t, a, owner)
	makeAdmin(t, a, rm, admin)
	a.addRoomMember(ctx, rm.ID, member.ID)

	for _, tc := range []struct {
		name   string
		actor  user
		target string
	}{
		{"a plain member can't remove anyone", member, admin.ID},
		{"the owner can't be removed", admin, owner.ID},
		{"you can't remove yourself", admin, admin.ID},
		{"someone outside the room isn't removable", admin, stranger.ID},
		{"no target at all", admin, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a.asUser(tc.actor)
			if _, errMsg := postRoomAction(t, a, rm.Code, "remove-member", url.Values{"user_id": {tc.target}}); errMsg == "" {
				t.Error("should have been refused")
			}
		})
	}
	// Everyone who should still be there, is.
	for _, u := range []user{owner, admin, member} {
		if !a.isRoomMember(ctx, rm.ID, u.ID) {
			t.Errorf("%s was removed by a refused call", u.DisplayName)
		}
	}
}

// Removing someone mid-run must take them out of the run too, or the timer
// keeps a participant who is no longer in the room.
func TestRemovingAMemberDropsThemFromTheRun(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	member := makeUser(t, a, "Member")
	ctx := context.Background()
	rm := makeRoom(t, a, owner)
	a.addRoomMember(ctx, rm.ID, member.ID)

	if _, err := a.startRoomTimer(ctx, rm, owner.ID, rm.FocusMinutes); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.joinTimer(ctx, rm, member.ID); err != nil {
		t.Fatal(err)
	}
	timer, err := a.activeTimer(ctx, rm.ID, member.ID)
	if err != nil || timer == nil || !timer.Participant {
		t.Fatalf("member did not join the run: %v %+v", err, timer)
	}

	a.asUser(owner)
	if _, errMsg := postRoomAction(t, a, rm.Code, "remove-member", url.Values{"user_id": {member.ID}}); errMsg != "" {
		t.Fatalf("remove: %s", errMsg)
	}
	timer, err = a.activeTimer(ctx, rm.ID, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if timer != nil && timer.Participant {
		t.Error("removed member is still a participant in the run")
	}
}

// A handover to someone who is then removed can never complete, so removal
// clears it rather than leaving the room pointing at an outsider.
func TestRemovingThePendingOwnerClearsTheTransfer(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	heir := makeUser(t, a, "Heir")
	rm := makeRoom(t, a, owner)
	makeAdmin(t, a, rm, heir)

	a.asUser(owner)
	postRoomAction(t, a, rm.Code, "transfer-ownership", url.Values{"user_id": {heir.ID}})
	if _, errMsg := postRoomAction(t, a, rm.Code, "remove-member", url.Values{"user_id": {heir.ID}}); errMsg != "" {
		t.Fatalf("remove: %s", errMsg)
	}

	got, _ := a.findRoom(context.Background(), rm.Code)
	if got.PendingOwnerID != "" || !got.OwnershipTransferAt.IsZero() {
		t.Errorf("transfer survived the recipient's removal: %+v", got)
	}
	if got.CreatorID != owner.ID {
		t.Error("ownership moved to someone who had been removed")
	}
}

// The members payload must not offer a Remove button to people who can't use
// one; viewerAdmin is what the page keys on.
func TestRemoveIsNotOfferedToPlainMembers(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	member := makeUser(t, a, "Member")
	rm := makeRoom(t, a, owner)
	a.addRoomMember(context.Background(), rm.ID, member.ID)

	req := httptest.NewRequest(http.MethodGet, "/api/room/"+rm.Code+"/members", nil)
	req.SetPathValue("code", rm.Code)
	rec := httptest.NewRecorder()
	a.asUser(member).apiRoomMembers(rec, req)
	if got := rec.Body.String(); !strings.Contains(got, `"viewerAdmin":false`) {
		t.Errorf("plain member sees viewerAdmin true: %s", got)
	}
}
