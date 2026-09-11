package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// startLink runs the real start handler and hands back what the CLI would hold.
func startLink(t *testing.T, a *app) (deviceCode, userCode string) {
	t.Helper()
	rec := httptest.NewRecorder()
	a.apiCLILinkStart(rec, httptest.NewRequest(http.MethodPost, "/api/cli/link/start", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("start = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var out struct {
		DeviceCode, UserCode, VerifyURL, VerifyURLFull string
		ExpiresIn, Interval                            int
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.DeviceCode == "" || out.UserCode == "" {
		t.Fatalf("start returned no codes: %s", rec.Body.String())
	}
	t.Cleanup(func() {
		_, _ = a.db.Exec(context.Background(), `DELETE FROM cli_link_codes WHERE user_code = $1`, out.UserCode)
	})
	return out.DeviceCode, out.UserCode
}

func pollLink(t *testing.T, a *app, deviceCode string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/cli/link/poll",
		strings.NewReader(url.Values{"device_code": {deviceCode}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.apiCLILinkPoll(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func approveLink(t *testing.T, a *app, code string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/cli/link/approve",
		strings.NewReader(url.Values{"code": {code}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.apiCLILinkApprove(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// The whole point of the flow: a terminal ends up holding a working session
// without ever having seen the password.
func TestCLILinkApprovalMintsAWorkingSession(t *testing.T) {
	a := newTestApp(t)
	u := makeUser(t, a, "Terminal Person")
	deviceCode, userCode := startLink(t, a)

	// Nothing is approved yet.
	if _, out := pollLink(t, a, deviceCode); out["status"] != "pending" {
		t.Fatalf("poll before approval = %v, want pending", out["status"])
	}

	a.asUser(u)
	if status, out := approveLink(t, a, userCode); status != http.StatusOK {
		t.Fatalf("approve = %d (%v)", status, out)
	}

	status, out := pollLink(t, a, deviceCode)
	if status != http.StatusOK || out["status"] != "approved" {
		t.Fatalf("poll after approval = %d %v, want approved", status, out)
	}
	token, _ := out["token"].(string)
	if token == "" {
		t.Fatal("approved poll returned no token")
	}
	if out["username"] != u.Username {
		t.Errorf("username = %v, want %s", out["username"], u.Username)
	}

	// The token must be a real session for that user, the same as a browser's.
	var gotID string
	if err := a.db.QueryRow(context.Background(),
		`SELECT user_id FROM sessions WHERE token = $1 AND expires_at > now()`, hashToken(token)).Scan(&gotID); err != nil {
		t.Fatalf("minted token is not a live session: %v", err)
	}
	if gotID != u.ID {
		t.Errorf("session belongs to %s, want %s", gotID, u.ID)
	}
	_, _ = a.db.Exec(context.Background(), `DELETE FROM sessions WHERE token = $1`, hashToken(token))
}

// A device code is single use: the row is deleted as the session is minted, so
// a copy of the code found later is worth nothing.
func TestCLILinkCodeCannotBeRedeemedTwice(t *testing.T) {
	a := newTestApp(t)
	u := makeUser(t, a, "Terminal Person")
	deviceCode, userCode := startLink(t, a)
	a.asUser(u)
	approveLink(t, a, userCode)

	_, first := pollLink(t, a, deviceCode)
	if first["status"] != "approved" {
		t.Fatalf("first poll = %v, want approved", first["status"])
	}
	token, _ := first["token"].(string)
	t.Cleanup(func() {
		_, _ = a.db.Exec(context.Background(), `DELETE FROM sessions WHERE token = $1`, hashToken(token))
	})

	_, second := pollLink(t, a, deviceCode)
	if second["status"] == "approved" {
		t.Error("a device code minted a second session; it must be single use")
	}
	if second["status"] != "expired" {
		t.Errorf("second poll = %v, want expired", second["status"])
	}
}

// Approving is what binds the request to an account, so an unapproved code
// must never hand out a session however often it is polled.
func TestCLILinkUnapprovedCodeNeverMintsASession(t *testing.T) {
	a := newTestApp(t)
	deviceCode, _ := startLink(t, a)
	for i := 0; i < 3; i++ {
		status, out := pollLink(t, a, deviceCode)
		if status != http.StatusOK || out["status"] != "pending" {
			t.Fatalf("poll %d = %d %v, want pending", i, status, out)
		}
		if _, ok := out["token"]; ok {
			t.Fatal("a pending poll returned a token")
		}
	}
}

// The session belongs to whoever approved, not to whoever started the request
// -- that is what makes approving from a phone safe to offer.
func TestCLILinkSessionBelongsToTheApprover(t *testing.T) {
	a := newTestApp(t)
	approver := makeUser(t, a, "Approver")
	other := makeUser(t, a, "Someone Else")
	deviceCode, userCode := startLink(t, a)

	a.asUser(approver)
	approveLink(t, a, userCode)
	_, out := pollLink(t, a, deviceCode)
	token, _ := out["token"].(string)
	t.Cleanup(func() {
		_, _ = a.db.Exec(context.Background(), `DELETE FROM sessions WHERE token = $1`, hashToken(token))
	})

	var gotID string
	_ = a.db.QueryRow(context.Background(),
		`SELECT user_id FROM sessions WHERE token = $1`, hashToken(token)).Scan(&gotID)
	if gotID == other.ID {
		t.Fatal("session was minted for the wrong account")
	}
	if gotID != approver.ID {
		t.Errorf("session user = %s, want the approver %s", gotID, approver.ID)
	}
}

// A second approval of the same code must not re-arm a request that has
// already been consumed, and must not move it to another account.
func TestCLILinkApprovedCodeCannotBeApprovedAgain(t *testing.T) {
	a := newTestApp(t)
	first := makeUser(t, a, "First")
	second := makeUser(t, a, "Second")
	_, userCode := startLink(t, a)

	a.asUser(first)
	if status, _ := approveLink(t, a, userCode); status != http.StatusOK {
		t.Fatal("first approval should succeed")
	}
	a.asUser(second)
	if status, _ := approveLink(t, a, userCode); status == http.StatusOK {
		t.Error("a code already approved was approved again, by someone else")
	}
}

// An expired request is dead even if it is approved afterwards: the approve
// statement and the poll both carry the deadline.
func TestCLILinkExpiredCodeIsUnusable(t *testing.T) {
	a := newTestApp(t)
	u := makeUser(t, a, "Latecomer")
	deviceCode, userCode := startLink(t, a)
	if _, err := a.db.Exec(context.Background(),
		`UPDATE cli_link_codes SET expires_at = now() - interval '1 minute' WHERE user_code = $1`, userCode); err != nil {
		t.Fatal(err)
	}

	a.asUser(u)
	if status, _ := approveLink(t, a, userCode); status == http.StatusOK {
		t.Error("an expired code was approved")
	}
	if _, out := pollLink(t, a, deviceCode); out["status"] != "expired" {
		t.Errorf("poll of an expired code = %v, want expired", out["status"])
	}
}

// Guessing the short code is the attack the rate limiter exists for.
func TestCLILinkApproveIsRateLimited(t *testing.T) {
	a := newTestApp(t)
	u := makeUser(t, a, "Guesser")
	a.asUser(u)
	limited := false
	for i := 0; i < 40; i++ {
		if status, _ := approveLink(t, a, "ABCD-2345"); status == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Error("approve never rate limited; a short code could be brute forced")
	}
}

// Only the hash of the device token is stored, like every other credential
// here -- a database read must not hand someone a working device code.
func TestCLILinkStoresOnlyTheDeviceHash(t *testing.T) {
	a := newTestApp(t)
	deviceCode, userCode := startLink(t, a)
	var stored string
	if err := a.db.QueryRow(context.Background(),
		`SELECT device_hash FROM cli_link_codes WHERE user_code = $1`, userCode).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == deviceCode {
		t.Fatal("the device code is stored in plaintext")
	}
	if stored != hashToken(deviceCode) {
		t.Errorf("device_hash is not hashToken(deviceCode)")
	}
}

func TestNormalizeUserCode(t *testing.T) {
	for _, in := range []string{"WXYZ-2345", "wxyz2345", " wxyz-2345 ", "WXYZ 2345", "wXyZ-2345"} {
		if got := normalizeUserCode(in); got != "WXYZ-2345" {
			t.Errorf("normalizeUserCode(%q) = %q, want WXYZ-2345", in, got)
		}
	}
	// Too short, too long, or carrying characters the alphabet excludes on
	// purpose -- a typed O or I should not silently become something else.
	for _, in := range []string{"", "WXYZ-234", "WXYZ-23456", "OOOO-1111", "!!!!-????"} {
		if got := normalizeUserCode(in); got != "" {
			t.Errorf("normalizeUserCode(%q) = %q, want rejection", in, got)
		}
	}
}

// The code a person reads off a screen must not contain the characters that
// are ambiguous in a terminal font.
func TestUserCodesAvoidAmbiguousCharacters(t *testing.T) {
	for i := 0; i < 200; i++ {
		code := newUserCode()
		if len(code) != 9 || code[4] != '-' {
			t.Fatalf("newUserCode() = %q, want XXXX-XXXX", code)
		}
		if strings.ContainsAny(code, "OIL01") {
			t.Fatalf("newUserCode() = %q, which contains an ambiguous character", code)
		}
	}
}

// Two requests in flight at once must not be able to collide on a code, since
// approving names one request by it.
func TestUserCodesAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		code := newUserCode()
		if seen[code] {
			t.Fatalf("newUserCode() repeated %q within 500 draws", code)
		}
		seen[code] = true
	}
}

// The row carries its own deadline, and start sweeps what has passed it, so a
// long-lived server does not accumulate dead pairing rows.
func TestCLILinkStartSweepsExpiredRows(t *testing.T) {
	a := newTestApp(t)
	_, userCode := startLink(t, a)
	if _, err := a.db.Exec(context.Background(),
		`UPDATE cli_link_codes SET expires_at = now() - interval '1 hour' WHERE user_code = $1`, userCode); err != nil {
		t.Fatal(err)
	}
	startLink(t, a)

	var still bool
	_ = a.db.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM cli_link_codes WHERE user_code = $1)`, userCode).Scan(&still)
	if still {
		t.Error("an expired pairing row survived a later start")
	}
}

// The context endpoint is for showing what is about to happen. It must not
// approve anything by being read.
func TestCLILinkContextDoesNotApprove(t *testing.T) {
	a := newTestApp(t)
	u := makeUser(t, a, "Reader")
	deviceCode, userCode := startLink(t, a)

	a.asUser(u)
	rec := httptest.NewRecorder()
	a.apiCLILinkContext(rec, httptest.NewRequest(http.MethodGet, "/api/cli/link/context?code="+url.QueryEscape(userCode), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("context = %d (%s)", rec.Code, rec.Body.String())
	}
	if _, out := pollLink(t, a, deviceCode); out["status"] != "pending" {
		t.Errorf("reading the context approved the code: poll = %v", out["status"])
	}
}

func TestCLILinkTTLIsShort(t *testing.T) {
	// A pairing code is a credential in waiting; the window should be minutes.
	if cliLinkTTL > 15*time.Minute {
		t.Errorf("cliLinkTTL = %v, which is too long for a pairing code", cliLinkTTL)
	}
}
