package main

import (
	"crypto/rand"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Device-code pairing, the flow `gh auth login` uses: the terminal shows a
// short code, you approve it in a browser that is already signed in, and the
// terminal collects a session. The password never passes through the terminal,
// and the approving browser need not be on the same machine — which is the
// point on a server you reached over SSH.
//
// Three moves: the CLI starts a request (start), a signed-in browser approves
// the code it displays (approve), and the CLI exchanges its device token for a
// session (poll).
const (
	cliLinkTTL = 10 * time.Minute
	// How often the CLI should ask. Slow enough that a stuck client is not a
	// load problem, fast enough that approving feels immediate.
	cliLinkPollSeconds = 2
)

// cliCodeAlphabet omits O/0 and I/1/L: this code gets read off one screen and
// typed into another, sometimes out loud.
const cliCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// newUserCode returns a grouped code like "WXYZ-2345". Eight characters of
// this alphabet is ~40 bits, which would be thin for a long-lived secret and
// is ample for one that expires in ten minutes behind a rate limiter.
func newUserCode() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	out := make([]byte, 0, 9)
	for i, v := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, cliCodeAlphabet[int(v)%len(cliCodeAlphabet)])
	}
	return string(out)
}

// normalizeUserCode accepts what a person actually types: any case, with or
// without the dash, with stray spaces.
func normalizeUserCode(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		if strings.ContainsRune(cliCodeAlphabet, r) {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) != 8 {
		return ""
	}
	return s[:4] + "-" + s[4:]
}

// apiCLILinkStart opens a pairing request. Unauthenticated by nature — the
// caller has no session yet, that being the entire problem.
func (a *app) apiCLILinkStart(w http.ResponseWriter, r *http.Request) {
	// Anonymous and cheap to call, so it is capped per IP. A pairing request
	// costs a row and a code; a flood of them would cost the code space.
	if !a.limiter.allow("clilinkstart:"+clientIP(r), 10, 10*time.Minute) {
		writeJSONError(w, http.StatusTooManyRequests, "Too many sign-in attempts. Try again in a few minutes.")
		return
	}
	deviceToken := randomHex(32)
	// Sweep expired rows here rather than on a timer: this is the only path
	// that creates them, so it is the only place they can accumulate.
	_, _ = a.db.Exec(r.Context(), `DELETE FROM cli_link_codes WHERE expires_at < now()`)

	var userCode string
	// A unique-violation retry rather than a lookup-then-insert, so two
	// simultaneous starts cannot agree on the same code.
	for attempt := 0; attempt < 5; attempt++ {
		userCode = newUserCode()
		_, err := a.db.Exec(r.Context(), `
			INSERT INTO cli_link_codes (device_hash, user_code, expires_at)
			VALUES ($1, $2, $3)`,
			hashToken(deviceToken), userCode, time.Now().Add(cliLinkTTL))
		if err == nil {
			break
		}
		if !isUniqueViolation(err) {
			writeJSONError(w, http.StatusInternalServerError, "Could not start sign-in. Try again.")
			return
		}
		userCode = ""
	}
	if userCode == "" {
		writeJSONError(w, http.StatusInternalServerError, "Could not start sign-in. Try again.")
		return
	}

	base := strings.TrimRight(publicBaseURL(r), "/")
	writeJSON(w, map[string]any{
		"deviceCode":    deviceToken,
		"userCode":      userCode,
		"verifyUrl":     base + "/cli",
		"verifyUrlFull": base + "/cli?code=" + userCode,
		"expiresIn":     int(cliLinkTTL.Seconds()),
		"interval":      cliLinkPollSeconds,
	})
}

// apiCLILinkPoll exchanges a device token for a session, once the request it
// belongs to has been approved.
func (a *app) apiCLILinkPoll(w http.ResponseWriter, r *http.Request) {
	deviceCode := strings.TrimSpace(r.FormValue("device_code"))
	if deviceCode == "" {
		writeJSONError(w, http.StatusBadRequest, "Missing device code.")
		return
	}
	// Keyed on the device token, so one client's polling cannot exhaust
	// another's budget. Generous: a client polling every 2s for the full ten
	// minutes makes 300 calls.
	if !a.limiter.allow("clilinkpoll:"+hashToken(deviceCode), 400, cliLinkTTL) {
		writeJSONError(w, http.StatusTooManyRequests, "Polling too fast.")
		return
	}

	// Consume and mint in one statement: DELETE ... RETURNING means two
	// simultaneous polls cannot both walk away with a session.
	var userID, username string
	err := a.db.QueryRow(r.Context(), `
		DELETE FROM cli_link_codes
		WHERE device_hash = $1 AND approved_at IS NOT NULL AND expires_at > now()
		RETURNING user_id,
			(SELECT username FROM users WHERE users.id = cli_link_codes.user_id)`,
		hashToken(deviceCode)).Scan(&userID, &username)
	if err == nil {
		token := randomHex(32)
		expires := time.Now().Add(30 * 24 * time.Hour)
		if _, err := a.db.Exec(r.Context(),
			`INSERT INTO sessions (token, user_id, expires_at) VALUES ($1, $2, $3)`,
			hashToken(token), userID, expires); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "Could not finish sign-in.")
			return
		}
		a.logEvent(r.Context(), userID, eventSignedIn, "user", userID, "", map[string]string{"via": "cli"})
		writeJSON(w, map[string]any{"status": "approved", "token": token, "username": username})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeJSONError(w, http.StatusInternalServerError, "Could not check sign-in.")
		return
	}

	// Nothing to consume: either it is still waiting, or it is gone. The two
	// are worth telling apart — "expired" lets the CLI stop and say so rather
	// than poll a dead code until its own deadline.
	var pending bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM cli_link_codes WHERE device_hash = $1 AND expires_at > now())`,
		hashToken(deviceCode)).Scan(&pending); err != nil || !pending {
		writeJSON(w, map[string]any{"status": "expired"})
		return
	}
	writeJSON(w, map[string]any{"status": "pending"})
}

// apiCLILinkContext peeks at a code so the approval page can say what it is
// about to do before anyone clicks. It never approves — that is a POST.
func (a *app) apiCLILinkContext(w http.ResponseWriter, r *http.Request) {
	code := normalizeUserCode(r.URL.Query().Get("code"))
	if code == "" {
		writeJSONError(w, http.StatusBadRequest, "That doesn't look like a sign-in code.")
		return
	}
	var approved bool
	err := a.db.QueryRow(r.Context(),
		`SELECT approved_at IS NOT NULL FROM cli_link_codes WHERE user_code = $1 AND expires_at > now()`,
		code).Scan(&approved)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "That code has expired or was never issued. Start again in your terminal.")
		return
	}
	writeJSON(w, map[string]any{"code": code, "approved": approved})
}

// apiCLILinkApprove is the click. It requires a session, so whoever approves
// is whoever the terminal will be signed in as.
func (a *app) apiCLILinkApprove(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	// The code is short by design, so guessing is the attack. Ten tries per
	// account per ten minutes makes it useless without inconveniencing anyone
	// typing a code off their own screen.
	if !a.limiter.allow("clilinkapprove:"+u.ID, 10, 10*time.Minute) {
		writeJSONError(w, http.StatusTooManyRequests, "Too many attempts. Try again in a few minutes.")
		return
	}
	code := normalizeUserCode(r.FormValue("code"))
	if code == "" {
		writeJSONError(w, http.StatusBadRequest, "That doesn't look like a sign-in code.")
		return
	}
	tag, err := a.db.Exec(r.Context(), `
		UPDATE cli_link_codes SET user_id = $1, approved_at = now()
		WHERE user_code = $2 AND expires_at > now() AND approved_at IS NULL`, u.ID, code)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not approve that code.")
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSONError(w, http.StatusNotFound, "That code has expired, was already used, or was never issued. Start again in your terminal.")
		return
	}
	writeJSON(w, map[string]any{"ok": true, "username": u.Username})
}

// publicBaseURL is where the CLI should send someone to approve. PUBLIC_BASE_URL
// when it is set (production, and what the Discord links already use); the
// request's own host otherwise, so a self-hosted or local server points at
// itself rather than at junkie-blin.onrender.com.
func publicBaseURL(r *http.Request) string {
	if base := strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")); base != "" {
		return base
	}
	scheme := "http"
	if isSecureRequest(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
