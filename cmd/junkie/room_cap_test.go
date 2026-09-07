package main

import (
	"context"
	"net/url"
	"sync"
	"testing"

	"github.com/coder/websocket"
)

// A full room refuses the next person, and says so rather than silently
// dropping them.
func TestRoomsFillUp(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	rm := makeRoom(t, a, owner)
	ctx := context.Background()

	// The creator is already a member, so this fills the rest.
	for i := 1; i < maxRoomMembers; i++ {
		u := makeUser(t, a, "Filler")
		if err := a.addRoomMember(ctx, rm.ID, u.ID); err != nil {
			t.Fatalf("member %d refused early: %v", i+1, err)
		}
	}
	count, _ := a.roomMemberCount(ctx, rm.ID)
	if count != maxRoomMembers {
		t.Fatalf("room holds %d, want %d", count, maxRoomMembers)
	}

	spare := makeUser(t, a, "Spare")
	if err := a.addRoomMember(ctx, rm.ID, spare.ID); err == nil {
		t.Fatal("a full room should refuse the next person")
	}
	if a.isRoomMember(ctx, rm.ID, spare.ID) {
		t.Error("the refused person was added anyway")
	}

	// Someone already in is not refused just because the room is full.
	if err := a.addRoomMember(ctx, rm.ID, owner.ID); err != nil {
		t.Errorf("an existing member was refused: %v", err)
	}

	// And a departure makes room again.
	member := makeUser(t, a, "Member")
	a.asUser(owner)
	var anyone string
	_ = a.db.QueryRow(ctx,
		`SELECT user_id FROM room_members WHERE room_id = $1 AND user_id <> $2 LIMIT 1`, rm.ID, owner.ID).Scan(&anyone)
	if _, errMsg := postRoomAction(t, a, rm.Code, "remove-member", url.Values{"user_id": {anyone}}); errMsg != "" {
		t.Fatalf("remove: %s", errMsg)
	}
	if err := a.addRoomMember(ctx, rm.ID, member.ID); err != nil {
		t.Errorf("a room with a free place still refused someone: %v", err)
	}
}

// Two people arriving at once must not both slip past the cap.
func TestRoomCapHoldsUnderConcurrentJoins(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	rm := makeRoom(t, a, owner)
	ctx := context.Background()
	for i := 1; i < maxRoomMembers; i++ {
		u := makeUser(t, a, "Filler")
		if err := a.addRoomMember(ctx, rm.ID, u.ID); err != nil {
			t.Fatal(err)
		}
	}

	// One free place, eight people going for it.
	if _, err := a.db.Exec(ctx,
		`DELETE FROM room_members WHERE room_id = $1 AND user_id <> $2 AND user_id IN (
			SELECT user_id FROM room_members WHERE room_id = $1 AND user_id <> $2 LIMIT 1)`, rm.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	hopefuls := make([]user, 8)
	for i := range hopefuls {
		hopefuls[i] = makeUser(t, a, "Hopeful")
	}
	var wg sync.WaitGroup
	admitted := make(chan struct{}, len(hopefuls))
	for _, u := range hopefuls {
		wg.Add(1)
		go func(u user) {
			defer wg.Done()
			if err := a.addRoomMember(context.Background(), rm.ID, u.ID); err == nil {
				admitted <- struct{}{}
			}
		}(u)
	}
	wg.Wait()
	close(admitted)
	if got := len(admitted); got != 1 {
		t.Errorf("%d people were admitted to one free place", got)
	}
	if count, _ := a.roomMemberCount(ctx, rm.ID); count > maxRoomMembers {
		t.Errorf("room holds %d, past the cap of %d", count, maxRoomMembers)
	}
}

// Connections are capped separately from members, and higher: one person can
// have two devices open, and a temporary room's overlay is nobody's member.
func TestConnectionCapIsSeparateFromTheMemberCap(t *testing.T) {
	if maxRoomConnections <= maxRoomMembers {
		t.Errorf("connection cap (%d) should exceed the member cap (%d)", maxRoomConnections, maxRoomMembers)
	}
	h := &hub{rooms: map[string]map[*websocket.Conn]struct{}{}}
	conns := make([]*websocket.Conn, maxRoomConnections)
	for i := range conns {
		conns[i] = &websocket.Conn{}
		if !h.join("ROOM", conns[i]) {
			t.Fatalf("connection %d refused early", i+1)
		}
	}
	if h.join("ROOM", &websocket.Conn{}) {
		t.Error("a channel past its ceiling should refuse the next connection")
	}
	// A departure frees a place.
	h.leave("ROOM", conns[0])
	if !h.join("ROOM", &websocket.Conn{}) {
		t.Error("a freed place should be reusable")
	}
}
