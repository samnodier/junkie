package main

import (
	"sync"
	"testing"

	"github.com/coder/websocket"
)

// join/leave only ever use the connection as a map key, so bare pointers are
// enough to exercise the hub's bookkeeping without a live socket.
func fakeConn() *websocket.Conn { return &websocket.Conn{} }

// Every room code and per-user channel used to keep a permanent empty map
// entry after its last viewer left, so a process that runs for weeks grew one
// entry per ephemeral room ever created and per user who ever connected.
func TestHubDropsChannelWhenLastConnLeaves(t *testing.T) {
	h := &hub{rooms: map[string]map[*websocket.Conn]struct{}{}}
	a, b := fakeConn(), fakeConn()

	h.join("EPHEMERAL-1", a)
	h.join("EPHEMERAL-1", b)
	if len(h.rooms) != 1 {
		t.Fatalf("expected 1 registered channel, got %d", len(h.rooms))
	}

	h.leave("EPHEMERAL-1", a)
	if len(h.rooms) != 1 {
		t.Fatal("channel dropped while a viewer was still connected")
	}

	h.leave("EPHEMERAL-1", b)
	if len(h.rooms) != 0 {
		t.Fatalf("hub leaked %d empty channel(s) after the last viewer left", len(h.rooms))
	}
}

// Leaving a channel that was never joined must not resurrect it as an empty
// entry — leave runs from a deferred call on every socket teardown.
func TestHubLeaveUnknownChannelDoesNotLeak(t *testing.T) {
	h := &hub{rooms: map[string]map[*websocket.Conn]struct{}{}}
	h.leave("NEVER-JOINED", fakeConn())
	if len(h.rooms) != 0 {
		t.Fatalf("leave on an unknown channel left %d entry(ies)", len(h.rooms))
	}
}

// The hub is touched from every request goroutine and every socket teardown;
// run the bookkeeping concurrently so -race can prove the locking holds. The
// broadcast targets an empty channel so no write is attempted on a fake conn.
func TestHubConcurrentJoinLeave(t *testing.T) {
	h := &hub{rooms: map[string]map[*websocket.Conn]struct{}{}}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := fakeConn()
			for range 50 {
				h.join("ROOM", c)
				h.broadcast("EMPTY", "timer-phase")
				h.leave("ROOM", c)
			}
		}()
	}
	wg.Wait()
	if len(h.rooms) != 0 {
		t.Fatalf("hub retained %d channel(s) after every viewer left", len(h.rooms))
	}
}
