package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// client talks to a junkie server the way the browser does, minus the
// browser. Two things make that work without any server-side change:
//
//   - CSRF: the server runs Go's http.CrossOriginProtection, which allows a
//     request carrying neither Sec-Fetch-Site nor Origin ("not a browser
//     request"). We send neither, deliberately — adding an Origin header
//     would opt us into the cross-origin check we currently sidestep.
//   - Auth: the session is a plain cookie, so replaying the stored token as
//     a cookie header is exactly what a second browser tab would do.
type client struct {
	baseURL string
	token   string
	http    *http.Client
}

// errSessionExpired is what an authenticated call gets when the token is no
// longer good. The server expresses this as a 303 to /login (requireAuth)
// rather than a 401, so the client has to recognise the redirect.
var errSessionExpired = errors.New("session expired — run `junkie login` again")

func newClient(cfg config) *client {
	return &client{
		baseURL: strings.TrimSuffix(cfg.BaseURL, "/"),
		token:   cfg.Token,
		http: &http.Client{
			// Redirects are data, not something to chase: a 303 to /login is
			// how an expired session is reported, and a 303 carrying
			// ?error=... is how a refused mutation is. Following them would
			// throw away both signals and fetch an HTML page we can't use.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			// Generous, because Render's free tier sleeps: the first request
			// after an idle period pays a cold start of roughly 30 seconds.
			Timeout: 60 * time.Second,
		},
	}
}

func (c *client) newRequest(method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "junkie-cli/"+version)
	if c.token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: c.token})
	}
	return req, nil
}

const sessionCookie = "junkie_session"

// getJSON fetches one of the /api/* endpoints the SPA uses and decodes it
// into out.
func (c *client) getJSON(path string, out any) error {
	req, err := c.newRequest(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return reachError(c.baseURL, err)
	}
	defer resp.Body.Close()
	if isLoginRedirect(resp) {
		return errSessionExpired
	}
	if resp.StatusCode != http.StatusOK {
		return httpError(resp)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// post sends a form-encoded mutation. junkie's mutation handlers answer 303
// to a page rather than JSON, so "worked" means a 2xx or a redirect that
// isn't to /login and doesn't carry an ?error= the web would have rendered
// as a banner.
func (c *client) post(path string, form url.Values) error {
	req, err := c.newRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return reachError(c.baseURL, err)
	}
	defer resp.Body.Close()
	if isLoginRedirect(resp) {
		return errSessionExpired
	}
	if msg, ok := redirectError(resp); ok {
		return errors.New(msg)
	}
	if resp.StatusCode >= 400 {
		return httpError(resp)
	}
	return nil
}

// isLoginRedirect spots requireAuth bouncing an unauthenticated caller.
func isLoginRedirect(resp *http.Response) bool {
	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return false
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	return err == nil && strings.HasPrefix(loc.Path, "/login")
}

// redirectError extracts the ?error= a refused mutation redirects with —
// the room cap, or settings changed mid-run. The web shows these as a
// banner; the terminal shows them as the command's error.
func redirectError(resp *http.Response) (string, bool) {
	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return "", false
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		return "", false
	}
	msg := sanitize(strings.TrimSpace(loc.Query().Get("error")))
	return msg, msg != ""
}

// httpError prefers the server's own {"error": "..."} message, which is
// written for humans, over a bare status line.
func httpError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Error != "" {
		// The server writes these for humans, but a few interpolate names
		// somebody else chose.
		return errors.New(sanitize(payload.Error))
	}
	return fmt.Errorf("%s said %s", resp.Request.URL.Path, resp.Status)
}

// reachError names the server in connection failures, because the usual
// cause is JUNKIE_URL pointing somewhere that isn't running.
func reachError(baseURL string, err error) error {
	return fmt.Errorf("could not reach %s: %w", baseURL, err)
}

// login exchanges a username and password for a session cookie. It returns
// the token rather than storing it, so the caller decides whether this login
// is worth persisting.
func (c *client) login(username, password string) (string, error) {
	form := url.Values{"username": {username}, "password": {password}}
	req, err := c.newRequest(http.MethodPost, "/api/login", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", reachError(c.baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", httpError(resp)
	}
	for _, ck := range resp.Cookies() {
		if ck.Name == sessionCookie && ck.Value != "" {
			return ck.Value, nil
		}
	}
	return "", errors.New("server accepted the sign-in but sent no session cookie")
}

func (c *client) logout() error { return c.post("/logout", nil) }

func (c *client) me() (meResponse, error) {
	var out meResponse
	err := c.getJSON("/api/me", &out)
	if out.User != nil {
		sanitizeUser(out.User)
	}
	return out, err
}

func (c *client) room(code string) (roomResponse, error) {
	var out roomResponse
	err := c.getJSON("/api/room/"+url.PathEscape(code), &out)
	out.Room.Code = sanitize(out.Room.Code)
	out.Room.Name = sanitize(out.Room.Name)
	if out.Timer != nil {
		for i := range out.Timer.Participants {
			sanitizeUser(&out.Timer.Participants[i])
		}
	}
	return out, err
}

func (c *client) profile() (profileResponse, error) {
	var out profileResponse
	err := c.getJSON("/api/profile", &out)
	for i := range out.Heatmap.Cells {
		out.Heatmap.Cells[i].Date = sanitize(out.Heatmap.Cells[i].Date)
	}
	for i := range out.Heatmap.Months {
		out.Heatmap.Months[i].Label = sanitize(out.Heatmap.Months[i].Label)
	}
	return out, err
}

func (c *client) desk() (deskResponse, error) {
	var out deskResponse
	err := c.getJSON("/api/desk", &out)
	sanitizeDesk(&out)
	return out, err
}

// setTimezone tells the server which zone this machine is in, so focus
// minutes land on the day the user actually had. The server validates the
// name against pg_timezone_names and rejects anything it doesn't know, so a
// machine reporting "Local" or "UTC" is simply declined — not worth
// surfacing, which is why the caller ignores the error.
func (c *client) setTimezone(zone string) error {
	if zone == "" || zone == "Local" {
		return nil
	}
	return c.post("/api/timezone", url.Values{"timezone": {zone}})
}
