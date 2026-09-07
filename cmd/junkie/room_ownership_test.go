package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// makeAdmin promotes someone the way the members page would.
func makeAdmin(t *testing.T, a *app, rm room, u user) {
	t.Helper()
	ctx := context.Background()
	a.addRoomMember(ctx, rm.ID, u.ID)
	if err := a.setRoomMemberRole(ctx, rm, u.ID, roomRoleAdmin); err != nil {
		t.Fatal(err)
	}
}

// expireTransfer drags a pending transfer's deadline into the past, standing
// in for the ten minutes actually elapsing.
func expireTransfer(t *testing.T, a *app, rm room) {
	t.Helper()
	if _, err := a.db.Exec(context.Background(),
		`UPDATE rooms SET ownership_transfer_at = now() - interval '1 second' WHERE id = $1`, rm.ID); err != nil {
		t.Fatal(err)
	}
}

func TestOwnershipTransferSettlesOnlyAfterTheWindow(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	heir := makeUser(t, a, "Heir")
	rm := makeRoom(t, a, owner)
	makeAdmin(t, a, rm, heir)
	ctx := context.Background()

	a.asUser(owner)
	if _, errMsg := postRoomAction(t, a, rm.Code, "transfer-ownership", url.Values{"user_id": {heir.ID}}); errMsg != "" {
		t.Fatalf("transfer: %s", errMsg)
	}

	// Inside the window the room has not changed hands.
	got, _ := a.findRoom(ctx, rm.Code)
	if got.CreatorID != owner.ID {
		t.Fatal("ownership moved before the window elapsed")
	}
	if got.PendingOwnerID != heir.ID || got.OwnershipTransferAt.IsZero() {
		t.Fatalf("transfer not recorded: %+v", got)
	}
	if left := time.Until(got.OwnershipTransferAt); left > ownershipTransferWindow || left < ownershipTransferWindow-time.Minute {
		t.Errorf("window is %v, want about %v", left, ownershipTransferWindow)
	}

	// Once it has, the next read is what applies it -- no timer involved,
	// which is what makes this survive a sleeping instance.
	expireTransfer(t, a, rm)
	got, _ = a.findRoom(ctx, rm.Code)
	if got.CreatorID != heir.ID {
		t.Fatal("ownership did not settle on read")
	}
	if got.PendingOwnerID != "" || !got.OwnershipTransferAt.IsZero() {
		t.Errorf("pending transfer not cleared: %+v", got)
	}
	// The outgoing owner keeps a foothold rather than dropping to a member.
	if role := memberRole(t, a, rm.ID, owner.ID); role != roomRoleAdmin {
		t.Errorf("previous owner's role = %q, want %q", role, roomRoleAdmin)
	}
}

func TestOwnershipTransferCanBeTakenBackInsideTheWindow(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	heir := makeUser(t, a, "Heir")
	rm := makeRoom(t, a, owner)
	makeAdmin(t, a, rm, heir)
	ctx := context.Background()

	a.asUser(owner)
	if _, errMsg := postRoomAction(t, a, rm.Code, "transfer-ownership", url.Values{"user_id": {heir.ID}}); errMsg != "" {
		t.Fatalf("transfer: %s", errMsg)
	}
	if _, errMsg := postRoomAction(t, a, rm.Code, "cancel-transfer", nil); errMsg != "" {
		t.Fatalf("cancel: %s", errMsg)
	}

	got, _ := a.findRoom(ctx, rm.Code)
	if got.CreatorID != owner.ID || got.PendingOwnerID != "" || !got.OwnershipTransferAt.IsZero() {
		t.Fatalf("cancel left the room in %+v", got)
	}
	// And the window really is gone: time passing must not revive it.
	time.Sleep(10 * time.Millisecond)
	got, _ = a.findRoom(ctx, rm.Code)
	if got.CreatorID != owner.ID {
		t.Fatal("a cancelled transfer settled anyway")
	}
}

// Once it has settled there is nothing left to undo, and saying so beats
// silently doing nothing.
func TestCancellingASettledTransferIsRefused(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	heir := makeUser(t, a, "Heir")
	rm := makeRoom(t, a, owner)
	makeAdmin(t, a, rm, heir)

	a.asUser(owner)
	postRoomAction(t, a, rm.Code, "transfer-ownership", url.Values{"user_id": {heir.ID}})
	expireTransfer(t, a, rm)
	a.findRoom(context.Background(), rm.Code) // this read is what settles it

	_, errMsg := postRoomAction(t, a, rm.Code, "cancel-transfer", nil)
	if errMsg == "" {
		t.Fatal("cancelling a settled transfer should be refused")
	}
}

func TestOnlyTheOwnerCanTransferAndOnlyToAnAdmin(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	admin := makeUser(t, a, "Admin")
	member := makeUser(t, a, "Member")
	rm := makeRoom(t, a, owner)
	makeAdmin(t, a, rm, admin)
	a.addRoomMember(context.Background(), rm.ID, member.ID)

	// A plain member is not a valid target: promoting has to be a separate,
	// deliberate step first.
	a.asUser(owner)
	if _, errMsg := postRoomAction(t, a, rm.Code, "transfer-ownership", url.Values{"user_id": {member.ID}}); errMsg == "" {
		t.Error("transferring to a non-admin should be refused")
	}
	// An admin is not the owner, so cannot hand the room to themselves.
	a.asUser(admin)
	if _, errMsg := postRoomAction(t, a, rm.Code, "transfer-ownership", url.Values{"user_id": {admin.ID}}); errMsg == "" {
		t.Error("an admin must not be able to transfer the room")
	}
	if got, _ := a.findRoom(context.Background(), rm.Code); got.CreatorID != owner.ID {
		t.Fatal("ownership changed when it should not have")
	}
}

func TestTemporaryRoomsCannotChangeHands(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	heir := makeUser(t, a, "Heir")
	rm := makeRoom(t, a, owner)
	ctx := context.Background()
	if _, err := a.db.Exec(ctx, `UPDATE rooms SET ephemeral = true WHERE id = $1`, rm.ID); err != nil {
		t.Fatal(err)
	}
	rm, _ = a.findRoom(ctx, rm.Code)
	makeAdmin(t, a, rm, heir)

	a.asUser(owner)
	if _, errMsg := postRoomAction(t, a, rm.Code, "transfer-ownership", url.Values{"user_id": {heir.ID}}); errMsg == "" {
		t.Fatal("a temporary room must not be transferable")
	}
}

// A transfer whose target has left the room can never complete, so it clears
// itself rather than sitting there forever.
func TestTransferToSomeoneWhoLeftIsAbandoned(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	heir := makeUser(t, a, "Heir")
	rm := makeRoom(t, a, owner)
	makeAdmin(t, a, rm, heir)
	ctx := context.Background()

	a.asUser(owner)
	postRoomAction(t, a, rm.Code, "transfer-ownership", url.Values{"user_id": {heir.ID}})
	if _, err := a.db.Exec(ctx, `DELETE FROM room_members WHERE room_id = $1 AND user_id = $2`, rm.ID, heir.ID); err != nil {
		t.Fatal(err)
	}
	expireTransfer(t, a, rm)

	got, _ := a.findRoom(ctx, rm.Code)
	if got.CreatorID != owner.ID {
		t.Fatal("the room went to someone who had left it")
	}
	if got.PendingOwnerID != "" || !got.OwnershipTransferAt.IsZero() {
		t.Errorf("dead transfer not cleared: %+v", got)
	}
}

// The recipient must not learn about a handover that might still be undone.
func TestPendingTransferIsVisibleOnlyToTheOwner(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	heir := makeUser(t, a, "Heir")
	rm := makeRoom(t, a, owner)
	makeAdmin(t, a, rm, heir)

	a.asUser(owner)
	postRoomAction(t, a, rm.Code, "transfer-ownership", url.Values{"user_id": {heir.ID}})

	membersPayload := func(as user) map[string]any {
		req := httptest.NewRequest(http.MethodGet, "/api/room/"+rm.Code+"/members", nil)
		req.SetPathValue("code", rm.Code)
		rec := httptest.NewRecorder()
		a.asUser(as).apiRoomMembers(rec, req)
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}

	if _, ok := membersPayload(owner)["pendingTransfer"]; !ok {
		t.Error("the owner who started the transfer should see it")
	}
	if _, ok := membersPayload(heir)["pendingTransfer"]; ok {
		t.Error("the recipient must not be shown a transfer that could still be undone")
	}
}

// Two readers arriving at once must not both believe they performed the
// handover -- the update is guarded on the deadline it read.
func TestConcurrentSettleHandsOverExactlyOnce(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	heir := makeUser(t, a, "Heir")
	rm := makeRoom(t, a, owner)
	makeAdmin(t, a, rm, heir)
	ctx := context.Background()

	a.asUser(owner)
	postRoomAction(t, a, rm.Code, "transfer-ownership", url.Values{"user_id": {heir.ID}})
	expireTransfer(t, a, rm)

	done := make(chan string, 8)
	for range cap(done) {
		go func() {
			got, _ := a.findRoom(ctx, rm.Code)
			done <- got.CreatorID
		}()
	}
	for range cap(done) {
		if id := <-done; id != owner.ID && id != heir.ID {
			t.Fatalf("unexpected owner %q", id)
		}
	}
	got, _ := a.findRoom(ctx, rm.Code)
	if got.CreatorID != heir.ID {
		t.Fatalf("final owner = %q, want the heir", got.CreatorID)
	}
	if role := memberRole(t, a, rm.ID, owner.ID); role != roomRoleAdmin {
		t.Errorf("previous owner's role = %q, want %q", role, roomRoleAdmin)
	}
}
