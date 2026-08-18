package main

import (
	"net/http"
	"strings"
	"testing"
)

// A room's name is set by whoever created it and a todo's text by any member
// of that room. The server stores both verbatim — right for HTML, wrong for
// a terminal, where an escape sequence is executed rather than shown.
func TestSanitizeStripsTerminalEscapes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"colour sequence", "Deep \x1b[31mwork\x1b[0m", "Deep [31mwork[0m"},
		{"cursor move", "room\x1b[2J\x1b[H", "room[2J[H"},
		// OSC 52 writes the user's clipboard on terminals that support it.
		{"clipboard write", "x\x1b]52;c;ZXZpbA==\x07y", "x]52;c;ZXZpbA==y"},
		{"bare escape", "a\x1bb", "ab"},
		{"del", "a\x7fb", "ab"},
		{"c1 control", "a\u0090b", "ab"},
		// Whitespace becomes a space so words either side stay apart in a
		// display that is single-line anyway.
		{"newline", "line one\nline two", "line one line two"},
		{"tab", "a\tb", "a b"},
		{"carriage return", "real\rfake", "real fake"},
		// Reordering a line into reading as something it is not.
		{"bidi override", "todo\u202egnihtemos", "todognihtemos"},
		{"isolate", "a\u2066b\u2069c", "abc"},
		// Ordinary text, including anything non-ASCII, is left alone.
		{"plain", "Deep work · 深い作業 · café", "Deep work · 深い作業 · café"},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		if got := sanitize(tc.in); got != tc.want {
			t.Errorf("%s: sanitize(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

// The escape must be gone before it is rendered, whichever screen renders
// it — so it is stripped where it enters, not at each call site.
func TestDeskIsSanitizedOnArrival(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{\"todos\":[{\"id\":\"1\",\"text\":\"pwn\\u001b[2Jed\"}]," +
			"\"rooms\":[{\"code\":\"ABC\",\"name\":\"evil\\u001b]52;c;x\\u0007room\"," +
			"\"timer\":{\"phase\":\"focus\",\"participants\":[{\"displayName\":\"a\\u001b[31mb\"}]}}]}"))
	})
	desk, err := c.desk()
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(desk.Todos[0].Text, 0x1b) {
		t.Errorf("todo text kept an escape: %q", desk.Todos[0].Text)
	}
	if strings.ContainsRune(desk.Rooms[0].Name, 0x1b) {
		t.Errorf("room name kept an escape: %q", desk.Rooms[0].Name)
	}
	if strings.ContainsRune(desk.Rooms[0].Timer.Participants[0].DisplayName, 0x1b) {
		t.Errorf("participant name kept an escape: %q", desk.Rooms[0].Timer.Participants[0].DisplayName)
	}
	// And nothing that renders it can reintroduce one.
	for _, out := range []string{
		renderStatus(desk, 80),
		renderTodos(desk.Todos, 80),
		renderRooms(desk.Rooms, 80),
	} {
		if strings.Contains(out, "\x1b[2J") || strings.Contains(out, "\x1b]52") {
			t.Errorf("a rendered view carried an escape through:\n%q", out)
		}
	}
}

// Pushed events carry the same user-typed strings as the REST payloads.
func TestSignalFieldsAreSanitized(t *testing.T) {
	sig := parseSignal("ABC", []byte(`{"type":"timer-lobby","roomName":"evil\u001b[2Jroom",
		"starterName":"a\u001b]52;c;x\u0007b"}`))
	if strings.ContainsRune(sig.str("roomName"), 0x1b) {
		t.Errorf("room name kept an escape: %q", sig.str("roomName"))
	}
	if strings.ContainsRune(sig.str("starterName"), 0x1b) {
		t.Errorf("starter name kept an escape: %q", sig.str("starterName"))
	}
}

// Some server messages interpolate names somebody else chose.
func TestServerErrorsAreSanitized(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("{\"error\":\"no room called \\u001b[2Jevil\"}"))
	})
	err := c.getJSON("/api/desk", &deskResponse{})
	if err == nil || strings.ContainsRune(err.Error(), 0x1b) {
		t.Errorf("error kept an escape: %v", err)
	}
}

// The password goes over that connection and the session comes back over it.
func TestInsecureURLWarning(t *testing.T) {
	if got := insecureURLWarning("https://junkie-blin.onrender.com"); got != "" {
		t.Errorf("HTTPS should not warn, got %q", got)
	}
	// The development server never leaves the machine.
	for _, local := range []string{"http://localhost:8899", "http://127.0.0.1:8080", "http://[::1]:3000"} {
		if got := insecureURLWarning(local); got != "" {
			t.Errorf("%s should not warn, got %q", local, got)
		}
	}
	if got := insecureURLWarning("http://junkie.example.com"); !strings.Contains(got, "not HTTPS") {
		t.Errorf("plaintext remote should warn, got %q", got)
	}
}

// The session is sent as an explicit header, which Go would carry across a
// redirect to another host. Not following redirects is what stops that, so
// it is asserted rather than assumed.
func TestSessionIsNotFollowedToAnotherHost(t *testing.T) {
	var leakedTo string
	thief := httptestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie(sessionCookie); err == nil {
			leakedTo = r.Host
		}
		w.WriteHeader(http.StatusOK)
	})
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, thief+"/steal", http.StatusFound)
	})
	_ = c.post("/solo/start", nil)
	_ = c.getJSON("/api/desk", &deskResponse{})
	if leakedTo != "" {
		t.Errorf("the session cookie was sent to %s", leakedTo)
	}
}
