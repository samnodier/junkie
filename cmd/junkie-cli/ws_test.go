package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// The server sends two shapes down the same channel: bare strings for
// "something changed, read it again", and JSON when the message carries
// something the client has to act on.
func TestParseSignal(t *testing.T) {
	tests := []struct {
		name string
		data string
		kind string
		room string
	}{
		{"bare signal", "todos", "todos", "ABC"},
		{"bare solo timer", "solo-timer", "solo-timer", ""},
		{"typed event", `{"type":"timer-lobby","roomCode":"ABC"}`, "timer-lobby", "ABC"},
		// Malformed JSON is not worth a crash or a dropped signal; treating
		// the raw text as the kind degrades to "refresh", which is safe.
		{"broken json", `{"type":`, `{"type":`, "ABC"},
	}
	for _, tc := range tests {
		got := parseSignal(tc.room, []byte(tc.data))
		if got.kind != tc.kind {
			t.Errorf("%s: kind = %q, want %q", tc.name, got.kind, tc.kind)
		}
		if got.room != tc.room {
			t.Errorf("%s: room = %q, want %q", tc.name, got.room, tc.room)
		}
	}
}

func TestSignalFields(t *testing.T) {
	sig := parseSignal("ABC", []byte(`{"type":"timer-lobby","starterName":"Sam",
		"lobbyDeadline":"2026-08-18T12:00:30.5Z"}`))
	if got := sig.str("starterName"); got != "Sam" {
		t.Errorf("starterName = %q", got)
	}
	if got := sig.str("missing"); got != "" {
		t.Errorf("a missing field should be empty, got %q", got)
	}
	at, ok := sig.at("lobbyDeadline")
	if !ok || at.Second() != 30 {
		t.Errorf("lobbyDeadline = %v, ok = %v", at, ok)
	}
	if _, ok := sig.at("missing"); ok {
		t.Error("a missing deadline should not parse")
	}
}

func TestWebsocketURL(t *testing.T) {
	tests := map[string]string{
		"https://junkie-blin.onrender.com": "wss://junkie-blin.onrender.com",
		"http://localhost:8899":            "ws://localhost:8899",
	}
	for in, want := range tests {
		if got := websocketURL(in); got != want {
			t.Errorf("websocketURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// Matching web/src/lib/ws.js so a flapping server sees the same load from
// either client.
func TestBackoffGrowsAndIsCapped(t *testing.T) {
	if got := backoff(0); got != 500*time.Millisecond {
		t.Errorf("first retry = %v", got)
	}
	if backoff(2) <= backoff(1) {
		t.Error("backoff should grow")
	}
	if got := backoff(50); got > 15*time.Second {
		t.Errorf("backoff should cap at 15s, got %v", got)
	}
}

// End to end over a real WebSocket: the client must present the session as a
// cookie, and must send no Origin — the handshake check that guards the
// server accepts an absent Origin and rejects a mismatched one.
func TestSocketsReceiveSignals(t *testing.T) {
	var gotCookie, gotOrigin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOrigin = r.Header.Get("Origin")
		if ck, err := r.Cookie(sessionCookie); err == nil {
			gotCookie = ck.Value
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		_ = c.Write(r.Context(), websocket.MessageText, []byte("todos"))
		_ = c.Write(r.Context(), websocket.MessageText,
			[]byte(`{"type":"timer-lobby","roomCode":"ABC","starterName":"Sam"}`))
		// Hold the connection open so the reads above are not raced by the
		// handler returning.
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()

	c := newClient(config{BaseURL: srv.URL, Token: "test-token"})
	s := openSockets(c, nil)
	defer s.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var kinds []string
	for len(kinds) < 2 {
		select {
		case sig := <-s.events:
			kinds = append(kinds, sig.kind)
		case <-ctx.Done():
			t.Fatalf("timed out; received %v", kinds)
		}
	}
	if kinds[0] != "todos" || kinds[1] != "timer-lobby" {
		t.Errorf("signals = %v", kinds)
	}
	if gotCookie != "test-token" {
		t.Errorf("session cookie = %q, want the stored token", gotCookie)
	}
	if gotOrigin != "" {
		t.Errorf("sent Origin %q; the handshake check would then have to pass it", gotOrigin)
	}
}

// A server that is down must not hold up the desk: openSockets starts the
// connections and returns, and close stops them however far they got.
func TestSocketsDoNotBlockOnADeadServer(t *testing.T) {
	c := newClient(config{BaseURL: "http://127.0.0.1:1", Token: "t"})
	done := make(chan *sockets, 1)
	go func() { done <- openSockets(c, []string{"ABC", "DEF"}) }()
	select {
	case s := <-done:
		s.close()
	case <-time.After(3 * time.Second):
		t.Fatal("openSockets blocked on an unreachable server")
	}
}

// Closing must stop every goroutine, or a desk that reopens its sockets on a
// room change would leak one set per change.
func TestSocketsCloseIsIdempotent(t *testing.T) {
	c := newClient(config{BaseURL: "http://127.0.0.1:1", Token: "t"})
	s := openSockets(c, []string{"ABC"})
	s.close()
	s.close()
	var nilSockets *sockets
	nilSockets.close()
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", "third"); got != "third" {
		t.Errorf("firstNonEmpty = %q", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("all empty = %q", got)
	}
}

func TestSameCodes(t *testing.T) {
	if !sameCodes([]string{"A", "B"}, []string{"A", "B"}) {
		t.Error("identical code sets should match")
	}
	if sameCodes([]string{"A"}, []string{"A", "B"}) {
		t.Error("a joined room should not match")
	}
	if sameCodes([]string{"A", "B"}, []string{"A", "C"}) {
		t.Error("a swapped room should not match")
	}
}

func TestParseSignalHandlesLeadingSpace(t *testing.T) {
	if got := parseSignal("", []byte(` {"type":"todo-done"}`)); got.kind != "todo-done" {
		t.Errorf("kind = %q", got.kind)
	}
	if got := parseSignal("", []byte(strings.Repeat(" ", 3))); got.kind != "   " {
		t.Errorf("whitespace should stay a bare signal, got %q", got.kind)
	}
}
