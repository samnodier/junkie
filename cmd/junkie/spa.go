package main

import (
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// The Vue SPA (built from web/ by Vite into static/app/) is cut over one page
// route at a time: a ported route's GET handler serves spaPage instead of the
// legacy Go template. Until the build output exists (npm --prefix web run
// build, or the Docker web-build stage), SPA routes return 404 and every
// legacy route keeps working.

// spaPage serves the SPA's index.html: no-cache + ETag like every other HTML
// response, so deploys propagate immediately.
func (a *app) spaPage(w http.ResponseWriter, r *http.Request) {
	data, err := staticAssets.ReadFile("static/app/index.html")
	if err != nil {
		http.Error(w, "app build missing", http.StatusNotFound)
		return
	}
	serveStatic(w, r, "text/html; charset=utf-8", data)
}

// spaAsset serves the Vite build output under /app/. Filenames are
// content-hashed, so they are safe to cache forever.
func (a *app) spaAsset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/app/")
	if name == "" || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	data, err := staticAssets.ReadFile("static/app/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(data)
}

type apiUser struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	DisplayName   string `json:"displayName"`
	Role          string `json:"role"`
	HasAvatar     bool   `json:"hasAvatar"`
	AvatarVersion int64  `json:"avatarVersion"`
}

// apiMe reports the current session's user, or user:null for guests. Every
// SPA page loads this first; it is the JSON equivalent of the session lookup
// the Go templates did server-side.
func (a *app) apiMe(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		User *apiUser `json:"user"`
	}
	if u, ok := a.currentUser(r); ok {
		payload.User = &apiUser{
			ID:            u.ID,
			Username:      u.Username,
			DisplayName:   u.DisplayName,
			Role:          u.Role,
			HasAvatar:     u.HasAvatar,
			AvatarVersion: u.AvatarVersion,
		}
	}
	writeJSON(w, payload)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode json response: %v", err)
	}
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	writeJSON(w, map[string]string{"error": msg})
}

// apiAuthContext supplies the auth card's contextual banner (e.g. "Sign in to
// join <room>"), mirroring authBanner for the SPA login page.
func (a *app) apiAuthContext(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.URL.Query().Get("next"))
	signup := r.URL.Query().Get("mode") == "signup"
	writeJSON(w, map[string]string{"banner": a.authBanner(r, next, signup)})
}

type apiHeatmapCell struct {
	Empty   bool   `json:"empty,omitempty"`
	Date    string `json:"date,omitempty"`
	Minutes int    `json:"minutes"`
	Level   int    `json:"level"`
}

type apiHeatmap struct {
	Cells        []apiHeatmapCell `json:"cells"`
	Months       []map[string]any `json:"months"`
	Weeks        int              `json:"weeks"`
	TotalMinutes int              `json:"totalMinutes"`
}

func heatmapJSON(h heatmapData) apiHeatmap {
	out := apiHeatmap{Weeks: h.Weeks, TotalMinutes: h.TotalMinutes, Months: []map[string]any{}, Cells: []apiHeatmapCell{}}
	for _, m := range h.Months {
		out.Months = append(out.Months, map[string]any{"label": m.Label, "col": m.Col})
	}
	for _, c := range h.Cells {
		out.Cells = append(out.Cells, apiHeatmapCell{Empty: c.Empty, Date: c.Date, Minutes: c.Minutes, Level: c.Level})
	}
	return out
}

// apiProfile feeds the SPA profile page: the same data profilePage passed to
// the template, minus what /api/me already carries.
func (a *app) apiProfile(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	rooms, _ := a.roomsForUser(r.Context(), u.ID)
	heat, _ := a.activity(r.Context(), u.ID)
	discordUsername, discordLinked := a.discordLinkForUser(r.Context(), u.ID)
	writeJSON(w, map[string]any{
		"roomsCount": len(rooms),
		"heatmap":    heatmapJSON(heat),
		"discord":    map[string]any{"linked": discordLinked, "username": discordUsername},
	})
}

type apiTodo struct {
	ID          string `json:"id"`
	Text        string `json:"text"`
	Done        bool   `json:"done"`
	Removed     bool   `json:"removed"`
	UserID      string `json:"userId,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	ReadOnly    bool   `json:"readOnly,omitempty"`
}

func apiTodos(todos []todo) []apiTodo {
	out := make([]apiTodo, 0, len(todos))
	for _, t := range todos {
		out = append(out, apiTodo{
			ID: t.ID, Text: t.Text, Done: t.Done, Removed: t.Removed,
			UserID: t.UserID, DisplayName: t.DisplayName, ReadOnly: t.ReadOnly,
		})
	}
	return out
}

func apiUsers(users []user) []apiUser {
	out := make([]apiUser, 0, len(users))
	for _, p := range users {
		out = append(out, apiUser{
			ID: p.ID, Username: p.Username, DisplayName: p.DisplayName,
			HasAvatar: p.HasAvatar, AvatarVersion: p.AvatarVersion,
		})
	}
	return out
}

// apiRoomTimer serializes a room timer run for the SPA: the same fields the
// desk/room templates rendered, including what timerStatus carries for the
// legacy reconcile endpoint.
func apiRoomTimer(timer *timerRun) map[string]any {
	if timer == nil {
		return nil
	}
	seconds := int(time.Until(timer.PhaseEndsAt).Seconds())
	if seconds < 0 {
		seconds = 0
	}
	paused := timer.PausedAt != nil
	if paused && timer.PausedRemainingSeconds != nil {
		seconds = *timer.PausedRemainingSeconds
	}
	return map[string]any{
		"runId":          timer.ID,
		"phase":          timer.Phase,
		"endsAt":         timer.PhaseEndsAt.UTC().Format(time.RFC3339Nano),
		"secondsLeft":    seconds,
		"focusMinutes":   timer.FocusMinutes,
		"breakMinutes":   timer.BreakMinutes,
		"currentSession": timer.CurrentSession,
		"totalSessions":  timer.TotalSessions,
		"participant":    timer.Participant,
		"participants":   apiUsers(timer.Participants),
		"paused":         paused,
		"breakPending":   timer.BreakPending(),
	}
}

// apiRooms lists the caller's rooms for the menu drawer on non-desk pages.
func (a *app) apiRooms(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	rooms, _ := a.roomsForUser(r.Context(), u.ID)
	out := make([]map[string]any, 0, len(rooms))
	for _, rm := range rooms {
		out = append(out, map[string]any{
			"code":         rm.Code,
			"name":         rm.Name,
			"focusMinutes": rm.FocusMinutes,
			"breakMinutes": rm.BreakMinutes,
			"autoSessions": rm.AutoSessions,
		})
	}
	writeJSON(w, map[string]any{"rooms": out})
}

// apiDesk feeds the signed-in SPA desk: solo timer, private todos, and each
// room's grouped todos + timer — the JSON twin of the dashboard handler.
func (a *app) apiDesk(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	ctx := r.Context()
	soloTimer, _ := a.normalizeSoloTimer(ctx, u.ID)
	todos, _ := a.personalTodos(ctx, u.ID)
	rooms, _ := a.roomsForUser(ctx, u.ID)

	var solo map[string]any
	if soloTimer != nil {
		seconds := int(time.Until(soloTimer.PhaseEndsAt).Seconds())
		if seconds < 0 {
			seconds = 0
		}
		solo = map[string]any{
			"phase":        soloTimer.Phase,
			"focusMinutes": soloTimer.FocusMinutes,
			"breakMinutes": soloTimer.BreakMinutes,
			"secondsLeft":  seconds,
			"breakPending": soloBreakPending(soloTimer),
		}
	}

	roomsOut := make([]map[string]any, 0, len(rooms))
	for _, rm := range rooms {
		roomTodoList, _ := a.roomTodos(ctx, rm.ID)
		grouped := groupRoomTodos(roomTodoList, u.ID)
		roomTimer, transitioned, _ := a.normalizeTimer(ctx, rm.ID, u.ID)
		if transitioned {
			a.broadcastTimerPhase(rm, roomTimer)
		}
		roomsOut = append(roomsOut, map[string]any{
			"code":         rm.Code,
			"name":         rm.Name,
			"focusMinutes": rm.FocusMinutes,
			"breakMinutes": rm.BreakMinutes,
			"autoSessions": rm.AutoSessions,
			"timer":        apiRoomTimer(roomTimer),
			"mine":         apiTodos(grouped.Mine),
			"others":       apiTodos(grouped.Others),
		})
	}

	writeJSON(w, map[string]any{
		"soloTimer": solo,
		"todos":     apiTodos(todos),
		"rooms":     roomsOut,
	})
}

// apiRoom feeds the SPA room page. Non-members get the invite verdict (the
// page-level handler already 404s unknown rooms); members get the full room
// state — the JSON twin of roomPage.
func (a *app) apiRoom(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	rm, ok := a.findRoom(r.Context(), r.PathValue("code"))
	if !ok {
		writeJSONError(w, http.StatusNotFound, "room not found")
		return
	}
	if !a.isRoomMember(r.Context(), rm.ID, u.ID) {
		writeJSON(w, map[string]any{"invite": map[string]string{"name": rm.Name, "code": rm.Code}})
		return
	}
	timer, transitioned, _ := a.normalizeTimer(r.Context(), rm.ID, u.ID)
	if transitioned {
		a.broadcastTimerPhase(rm, timer)
	}
	todos, _ := a.roomTodos(r.Context(), rm.ID)
	grouped := groupRoomTodos(todos, u.ID)
	memberCount, _ := a.roomMemberCount(r.Context(), rm.ID)
	waiting := false
	if timer == nil || (timer.Phase == "focus" && !timer.Participant) {
		waiting = a.roomWaiting(r.Context(), rm.ID, u.ID)
	}
	writeJSON(w, map[string]any{
		"room": map[string]any{
			"code":         rm.Code,
			"name":         rm.Name,
			"focusMinutes": rm.FocusMinutes,
			"breakMinutes": rm.BreakMinutes,
			"autoSessions": rm.AutoSessions,
			"autoRoll":     rm.AutoRoll,
		},
		"isCreator":   rm.CreatorID == u.ID,
		"memberCount": memberCount,
		"timer":       apiRoomTimer(timer),
		"waiting":     waiting,
		"mine":        apiTodos(grouped.Mine),
		"others":      apiTodos(grouped.Others),
	})
}

// apiRoomMembers mirrors roomMembersPage: each member plus whether the
// viewer may open their profile (self or an existing connection).
func (a *app) apiRoomMembers(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	rm, ok := a.findRoom(r.Context(), r.PathValue("code"))
	if !ok {
		writeJSONError(w, http.StatusNotFound, "room not found")
		return
	}
	if !a.isRoomMember(r.Context(), rm.ID, u.ID) {
		writeJSONError(w, http.StatusForbidden, "room membership required")
		return
	}
	members, _ := a.roomMemberUsers(r.Context(), rm.ID)
	out := make([]map[string]any, 0, len(members))
	for _, m := range members {
		out = append(out, map[string]any{
			"user":      apiUsers([]user{m})[0],
			"self":      m.ID == u.ID,
			"connected": m.ID != u.ID && a.areConnected(r.Context(), u.ID, m.ID),
		})
	}
	writeJSON(w, map[string]any{
		"room":    map[string]string{"code": rm.Code, "name": rm.Name},
		"members": out,
	})
}

// apiConnections lists the caller's connections with their heatmaps, newest
// first — the JSON twin of connectionViews for the SPA connections page.
func (a *app) apiConnections(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	views := a.connectionViews(r.Context(), u.ID)
	out := []map[string]any{}
	for _, v := range views {
		out = append(out, map[string]any{
			"user": apiUser{
				ID:            v.ProfileUser.ID,
				Username:      v.ProfileUser.Username,
				DisplayName:   v.ProfileUser.DisplayName,
				HasAvatar:     v.ProfileUser.HasAvatar,
				AvatarVersion: v.ProfileUser.AvatarVersion,
			},
			"heatmap": heatmapJSON(heatmapData{
				Cells:        v.Activity,
				Months:       v.ActivityMonths,
				Weeks:        v.ActivityWeeks,
				TotalMinutes: v.ActivityTotalMinutes,
			}),
		})
	}
	writeJSON(w, map[string]any{"connections": out})
}

// apiPublicProfile mirrors publicProfilePage's decision tree for the SPA:
// the page-level handler already resolved guest/invalid-username cases with
// real 404s/redirects, so this only serves signed-in viewers. A non-connection
// gets the same body as a nonexistent username, preserving the privacy rule.
func (a *app) apiPublicProfile(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	username := strings.ToLower(strings.TrimSpace(r.PathValue("username")))
	if !usernamePattern.MatchString(username) {
		writeJSON(w, map[string]any{"notFound": true})
		return
	}
	var target user
	err := a.db.QueryRow(r.Context(), `
		SELECT id, username, display_name, avatar IS NOT NULL,
			COALESCE(EXTRACT(EPOCH FROM avatar_updated_at), 0)::bigint
		FROM users WHERE username = $1`, username).Scan(
		&target.ID, &target.Username, &target.DisplayName, &target.HasAvatar, &target.AvatarVersion)
	if err != nil {
		writeJSON(w, map[string]any{"notFound": true})
		return
	}
	if target.ID == u.ID {
		writeJSON(w, map[string]any{"redirect": "/profile"})
		return
	}
	token := r.URL.Query().Get("connect")
	connected := a.areConnected(r.Context(), u.ID, target.ID)
	if token != "" && !connected && a.peekConnectToken(r.Context(), target.ID, token) {
		writeJSON(w, map[string]any{"confirm": map[string]any{
			"username":    target.Username,
			"displayName": target.DisplayName,
			"token":       token,
		}})
		return
	}
	if !connected {
		writeJSON(w, map[string]any{"notFound": true})
		return
	}
	heat, _ := a.activity(r.Context(), target.ID)
	writeJSON(w, map[string]any{"profile": map[string]any{
		"user": apiUser{
			ID:            target.ID,
			Username:      target.Username,
			DisplayName:   target.DisplayName,
			HasAvatar:     target.HasAvatar,
			AvatarVersion: target.AvatarVersion,
		},
		"heatmap": heatmapJSON(heat),
	}})
}

// apiJoinContext mirrors joinRoomConfirm's decision tree for the SPA join
// page: a `redirect` verdict for the cases the legacy handler solved with
// http.Redirect, or the room to confirm joining.
func (a *app) apiJoinContext(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	code := normalizeRoomCode(r.URL.Query().Get("code"))
	if code == "" {
		writeJSON(w, map[string]string{"redirect": "/dashboard?error=" + url.QueryEscape("Enter a room code to join.")})
		return
	}
	rm, ok := a.findRoom(r.Context(), code)
	if !ok {
		writeJSON(w, map[string]string{"redirect": "/dashboard?error=" + url.QueryEscape("No room found with that code.")})
		return
	}
	if a.isRoomMember(r.Context(), rm.ID, u.ID) {
		writeJSON(w, map[string]string{"redirect": "/r/" + rm.Code})
		return
	}
	writeJSON(w, map[string]any{"room": map[string]string{"name": rm.Name, "code": rm.Code}})
}

// apiDiscordLinkContext peeks (never consumes) a Discord link token, exactly
// like discordLinkPage; redemption stays with the legacy POST handler.
func (a *app) apiDiscordLinkContext(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	var discordUsername string
	err := a.db.QueryRow(r.Context(), `
		SELECT discord_username FROM discord_link_tokens
		WHERE token_hash = $1 AND expires_at > now()`, hashToken(token)).Scan(&discordUsername)
	if err != nil {
		writeJSON(w, map[string]string{"redirect": "/profile?error=" + url.QueryEscape("That Discord link is invalid or has expired — run /junkie link again.")})
		return
	}
	writeJSON(w, map[string]string{"discordUsername": discordUsername})
}

// apiLogin and apiSignup are the JSON twins of login/signup: identical
// validation messages, rate-limit keys, and session behavior, but a JSON
// verdict instead of a rendered template. The legacy form handlers stay
// untouched until cutover.
func (a *app) apiLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	username := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	password := r.FormValue("password")
	next := safeNext(r.FormValue("next"))
	if !a.limiter.allow("login:"+clientIP(r), 20, 5*time.Minute) ||
		(username != "" && !a.limiter.allow("login-user:"+username, 10, 15*time.Minute)) {
		writeJSONError(w, http.StatusTooManyRequests, "Too many sign-in attempts. Try again in a few minutes.")
		return
	}
	var id, hash string
	err := a.db.QueryRow(ctx, `SELECT id, password_hash FROM users WHERE username = $1`, username).Scan(&id, &hash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		writeJSONError(w, http.StatusUnauthorized, "Username or password is incorrect.")
		return
	}
	a.createSession(w, r, id)
	writeJSON(w, map[string]string{"next": next})
}

func (a *app) apiSignup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	username := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	password := r.FormValue("password")
	next := safeNext(r.FormValue("next"))
	if username == "" || password == "" {
		writeJSONError(w, http.StatusBadRequest, "Username and password are required.")
		return
	}
	if !validUsername(username) {
		writeJSONError(w, http.StatusBadRequest, "Usernames are 2–32 characters: lowercase letters, numbers, dots, dashes, underscores.")
		return
	}
	if len([]rune(password)) < minPasswordLength {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Passwords must be at least %d characters.", minPasswordLength))
		return
	}
	if len(password) > maxPasswordBytes {
		writeJSONError(w, http.StatusBadRequest, "That password is too long.")
		return
	}
	if !a.limiter.allow("signup:"+clientIP(r), 10, time.Hour) {
		writeJSONError(w, http.StatusTooManyRequests, "Too many new accounts from this address. Try again later.")
		return
	}
	displayName := displayNameFromUsername(username)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not hash password")
		return
	}
	var id string
	err = a.db.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash) VALUES ($1, $2, $3) RETURNING id`, username, displayName, string(hash)).Scan(&id)
	if err != nil {
		writeJSONError(w, http.StatusConflict, "That username is already taken.")
		return
	}
	a.createSession(w, r, id)
	writeJSON(w, map[string]string{"next": next})
}
