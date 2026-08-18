package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func testClient(t *testing.T, h http.HandlerFunc) *client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return newClient(config{BaseURL: srv.URL, Token: "test-token"})
}

// The client's whole right to exist without a server change rests on two
// headers it must never send: the server runs Go's CrossOriginProtection,
// which allows a request carrying neither Sec-Fetch-Site nor Origin
// ("not a browser request") and rejects a mismatched one. Sending either
// would opt this client into a check it would then have to pass.
func TestPostSendsNoBrowserOriginHeaders(t *testing.T) {
	var gotOrigin, gotFetchSite, gotCookie string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotOrigin = r.Header.Get("Origin")
		gotFetchSite = r.Header.Get("Sec-Fetch-Site")
		if ck, err := r.Cookie(sessionCookie); err == nil {
			gotCookie = ck.Value
		}
		w.WriteHeader(http.StatusSeeOther)
	})
	if err := c.post("/solo/start", url.Values{"focus_minutes": {"25"}}); err != nil {
		t.Fatalf("post: %v", err)
	}
	if gotOrigin != "" {
		t.Errorf("sent Origin %q; CrossOriginProtection would then check it", gotOrigin)
	}
	if gotFetchSite != "" {
		t.Errorf("sent Sec-Fetch-Site %q; only browsers set this", gotFetchSite)
	}
	if gotCookie != "test-token" {
		t.Errorf("session cookie = %q, want the stored token", gotCookie)
	}
}

func TestPostSendsForm(t *testing.T) {
	var got url.Values
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r.PostForm
		w.WriteHeader(http.StatusSeeOther)
	})
	if err := c.post("/solo/break/start", url.Values{"minutes": {"7"}}); err != nil {
		t.Fatalf("post: %v", err)
	}
	if got.Get("minutes") != "7" {
		t.Errorf("form = %v, want minutes=7", got)
	}
}

// requireAuth expresses a dead session as a 303 to /login rather than a 401,
// so a client that only checked status codes would read it as success.
func TestExpiredSessionIsRecognised(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login?next=%2Fdashboard", http.StatusSeeOther)
	})
	if err := c.post("/solo/start", nil); !errors.Is(err, errSessionExpired) {
		t.Errorf("post error = %v, want errSessionExpired", err)
	}
	if err := c.getJSON("/api/desk", &deskResponse{}); !errors.Is(err, errSessionExpired) {
		t.Errorf("getJSON error = %v, want errSessionExpired", err)
	}
}

// A refused mutation redirects with ?error=..., which is the message the web
// renders as a banner. Following the redirect would discard it.
func TestRedirectErrorSurfaces(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/r/ABC?error="+url.QueryEscape("You're already in 3 rooms' live sessions — leave one first."), http.StatusSeeOther)
	})
	err := c.post("/r/ABC/timer-join", nil)
	if err == nil || !strings.Contains(err.Error(), "already in 3 rooms") {
		t.Errorf("error = %v, want the server's banner text", err)
	}
}

// A plain redirect to a page is how every successful mutation answers.
func TestRedirectWithoutErrorIsSuccess(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	})
	if err := c.post("/solo/cancel", nil); err != nil {
		t.Errorf("plain redirect should be success, got %v", err)
	}
}

func TestJSONErrorMessagePreferred(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"Too many sign-in attempts. Try again in a few minutes."}`))
	})
	err := c.getJSON("/api/desk", &deskResponse{})
	if err == nil || !strings.Contains(err.Error(), "Too many sign-in attempts") {
		t.Errorf("error = %v, want the server's own message", err)
	}
}

func TestLoginReturnsSessionCookie(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("username") != "sam" || r.PostForm.Get("password") != "hunter22" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"Username or password is incorrect."}`))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "fresh-token", Path: "/"})
		_, _ = w.Write([]byte(`{"next":"/"}`))
	})
	token, err := c.login("sam", "hunter22")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if token != "fresh-token" {
		t.Errorf("token = %q", token)
	}
	if _, err := c.login("sam", "wrong"); err == nil || !strings.Contains(err.Error(), "incorrect") {
		t.Errorf("bad password error = %v", err)
	}
}

// A 200 with no Set-Cookie would otherwise store an empty token and leave
// every later command failing with an unexplained "session expired".
func TestLoginWithoutCookieFails(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"next":"/"}`))
	})
	if _, err := c.login("sam", "pw"); err == nil {
		t.Fatal("expected an error when no session cookie comes back")
	}
}

func TestDeskDecodes(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"soloTimer": {"phase":"focus","focusMinutes":50,"breakMinutes":10,
				"endsAt":"2026-08-18T12:00:00Z","secondsLeft":1500,"breakPending":false},
			"todos": [{"id":"1","text":"ship the CLI","done":false,"removed":false}],
			"rooms": [{"code":"ABCD","name":"Deep work","timer":{"phase":"lobby","secondsLeft":22,
				"participants":[{"id":"u1","displayName":"Sam"}],"participant":true}}]
		}`))
	})
	desk, err := c.desk()
	if err != nil {
		t.Fatalf("desk: %v", err)
	}
	if desk.SoloTimer == nil || desk.SoloTimer.SecondsLeft != 1500 || desk.SoloTimer.FocusMinutes != 50 {
		t.Errorf("solo timer decoded as %+v", desk.SoloTimer)
	}
	if len(desk.Todos) != 1 || desk.Todos[0].Text != "ship the CLI" {
		t.Errorf("todos decoded as %+v", desk.Todos)
	}
	if len(desk.Rooms) != 1 || desk.Rooms[0].Timer == nil || desk.Rooms[0].Timer.Phase != "lobby" {
		t.Errorf("rooms decoded as %+v", desk.Rooms)
	}
}

// The server validates timezones against pg_timezone_names, so a machine
// reporting something it doesn't know is simply declined. Reporting nothing
// at all shouldn't even be a request.
func TestSetTimezoneSkipsUnnamedZone(t *testing.T) {
	called := false
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusSeeOther)
	})
	if err := c.setTimezone("Local"); err != nil {
		t.Fatalf("setTimezone: %v", err)
	}
	if called {
		t.Error("an unnamed zone should not be sent")
	}
	if err := c.setTimezone("Europe/London"); err != nil {
		t.Fatalf("setTimezone: %v", err)
	}
	if !called {
		t.Error("a real zone should be sent")
	}
}

func TestUnreachableServerNamesTheURL(t *testing.T) {
	c := newClient(config{BaseURL: "http://127.0.0.1:1", Token: "t"})
	err := c.getJSON("/api/desk", &deskResponse{})
	if err == nil || !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("error = %v, want it to name the unreachable server", err)
	}
}
