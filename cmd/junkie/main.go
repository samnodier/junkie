package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type app struct {
	db                  *pgxpool.Pool
	templates           *template.Template
	hub                 *hub
	limiter             *rateLimiter
	currentUserOverride func(*http.Request) (user, bool)
	discord             *discordBot
}

type user struct {
	ID          string
	Username    string
	DisplayName string
	Role        string
	HasAvatar   bool
	// AvatarVersion is the avatar's upload time as a unix timestamp, used
	// as a cache-busting query parameter on /avatar/{id} URLs.
	AvatarVersion int64
}

type room struct {
	ID           string
	Code         string
	Name         string
	CreatorID    string
	FocusMinutes int
	BreakMinutes int
	AutoSessions int
	// AutoRoll starts breaks automatically when a focus session ends. When
	// off, the break waits paused at full length so members can adjust it
	// and start it deliberately.
	AutoRoll bool
}

type todo struct {
	ID          string
	Text        string
	Done        bool
	Removed     bool
	DisplayName string
	UserID      string
	HideAuthor  bool
	ReadOnly    bool
	RoomCode    string
	CreatedAt   time.Time
}

type roomTodosSplit struct {
	Mine   []todo
	Others []todo
}

type todoGroupsView struct {
	RoomCode string
	UserName string
	Mine     []todo
	Others   []todo
}

func (s roomTodosSplit) View(roomCode, userName string) todoGroupsView {
	return todoGroupsView{RoomCode: roomCode, UserName: userName, Mine: s.Mine, Others: s.Others}
}

func groupRoomTodos(todos []todo, userID string) roomTodosSplit {
	var split roomTodosSplit
	for _, t := range todos {
		if t.UserID == userID {
			t.HideAuthor = true
			split.Mine = append(split.Mine, t)
		} else {
			t.ReadOnly = true
			split.Others = append(split.Others, t)
		}
	}
	return split
}

type roomTodosGroup struct {
	Room    room
	Grouped roomTodosSplit
	Timer   *timerRun
}

type timerRun struct {
	ID                     string
	Phase                  string
	FocusMinutes           int
	BreakMinutes           int
	TotalSessions          int
	CurrentSession         int
	PhaseStartedAt         time.Time
	PhaseEndsAt            time.Time
	PausedAt               *time.Time
	PausedRemainingSeconds *int
	Participant            bool
	Participants           []string
	Transitioned           bool
}

// BreakPending reports a break that arrived with auto-roll off and hasn't
// been started yet: it was paused at the exact moment it began. A break
// paused manually mid-run has a later paused_at than phase_started_at.
func (t *timerRun) BreakPending() bool {
	return t != nil && t.Phase == "break" && t.PausedAt != nil && t.PausedAt.Equal(t.PhaseStartedAt)
}

type timerStatus struct {
	RunID                  string `json:"runId"`
	Phase                  string `json:"phase"`
	EndsAt                 string `json:"endsAt"`
	LobbyDeadline          string `json:"lobbyDeadline,omitempty"`
	Paused                 bool   `json:"paused"`
	PausedAt               string `json:"pausedAt,omitempty"`
	PausedRemainingSeconds int    `json:"pausedRemainingSeconds,omitempty"`
	CurrentSession         int    `json:"currentSession"`
	TotalSessions          int    `json:"totalSessions"`
	Participant            bool   `json:"participant"`
	ParticipantCount       int    `json:"participantCount"`
}

// publicProfileView is what a connection is allowed to see of a user: the
// identity basics and the focus heatmap. Field names mirror pageData's
// activity fields so the shared "heatmap" template renders either.
type publicProfileView struct {
	ProfileUser          user
	Activity             []activityDay
	ActivityMonths       []activityMonth
	ActivityWeeks        int
	ActivityTotalMinutes int
}

// roomMemberView pairs a room member with whether the viewer may open their
// public profile: only for themselves or an existing connection, matching
// the privacy rule in publicProfilePage.
type roomMemberView struct {
	Member    user
	Self      bool
	Connected bool
}

type pageData struct {
	Title                string
	User                 user
	Error                string
	Notice               string
	PublicProfile        *publicProfileView
	Connections          []publicProfileView
	Rooms                []room
	Room                 room
	PersonalTodos        []todo
	RoomTodosGrouped     roomTodosSplit
	DeskRoomTodos        []roomTodosGroup
	Timer                *timerRun
	SoloTimer            *timerRun
	Activity             []activityDay
	ActivityMonths       []activityMonth
	ActivityWeeks        int
	ActivityTotalMinutes int
	FocusMode            bool
	GuestMode            bool
	Next                 string
	AuthSignup           bool
	AuthBanner           string
	MemberCount          int
	RoomMembers          []roomMemberView
	Admin                adminPageData
	ForbiddenMessage     string
	// TimerWaiting: the viewer is parked in the room's waiting list, to be
	// absorbed into the next joinable window (run start or break).
	TimerWaiting bool
	// DiscordUsername is the Discord account linked to the viewer, if any
	// ("" when unlinked); shown on the profile page.
	DiscordUsername string
	DiscordLinked   bool
}

type activityDay struct {
	Date    string
	Minutes int
	Level   int
	Empty   bool
}

type activityMonth struct {
	Label string
	Col   int
}

type heatmapData struct {
	Cells        []activityDay
	Months       []activityMonth
	Weeks        int
	TotalMinutes int
}

type hub struct {
	mu    sync.Mutex
	rooms map[string]map[*websocket.Conn]struct{}
}

func main() {
	ctx := context.Background()
	databaseURL := getenv("DATABASE_URL", "postgres://junkie:junkie@localhost:5432/junkie?sslmode=disable")

	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	a := &app{
		db:        db,
		templates: parseTemplates(),
		hub:       &hub{rooms: map[string]map[*websocket.Conn]struct{}{}},
		limiter:   newRateLimiter(),
	}
	if err := a.migrate(ctx); err != nil {
		log.Fatal(err)
	}
	if err := a.bootstrapOwner(ctx, os.Getenv("JUNKIE_OWNER_USERNAME")); err != nil {
		log.Fatal(err)
	}

	// Opt-in: no-op unless DISCORD_BOT_TOKEN is set, so existing deployments
	// are unaffected.
	discordBot, err := newDiscordBot(a)
	if err != nil {
		log.Fatal(err)
	}
	if discordBot != nil {
		a.discord = discordBot
		defer discordBot.Close()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /assets/app.css", a.css)
	mux.HandleFunc("GET /assets/guest.js", a.serveStaticAsset("guest.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("GET /assets/htmx.min.js", a.serveStaticAsset("htmx.min.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("GET /assets/notifications.js", a.serveStaticAsset("notifications.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("GET /assets/icon.svg", a.serveStaticAsset("icon.svg", "image/svg+xml"))
	mux.HandleFunc("GET /healthz", a.healthz)
	mux.HandleFunc("GET /", a.home)
	mux.HandleFunc("GET /signup", a.signupForm)
	mux.HandleFunc("POST /signup", a.signup)
	mux.HandleFunc("GET /login", a.loginForm)
	mux.HandleFunc("POST /login", a.login)
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("GET /dashboard", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.RawQuery
		dest := "/"
		if q != "" {
			dest += "?" + q
		}
		http.Redirect(w, r, dest, http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /profile", a.profilePage)
	mux.HandleFunc("POST /profile/password", a.requireAuth(a.changePassword))
	mux.HandleFunc("POST /profile/delete", a.requireAuth(a.deleteAccount))
	mux.HandleFunc("POST /profile/username", a.requireAuth(a.changeUsername))
	mux.HandleFunc("POST /profile/avatar", a.requireAuth(a.uploadAvatar))
	mux.HandleFunc("POST /profile/avatar/remove", a.requireAuth(a.removeAvatar))
	mux.HandleFunc("GET /avatar/{id}", a.requireAuth(a.serveAvatar))
	mux.HandleFunc("POST /profile/connect-link", a.requireAuth(a.createConnectLink))
	mux.HandleFunc("GET /connections", a.requireAuth(a.connectionsPage))
	mux.HandleFunc("GET /privacy", func(w http.ResponseWriter, r *http.Request) {
		u, _ := a.currentUser(r)
		a.render(w, "privacy", pageData{Title: "Privacy", User: u})
	})
	mux.HandleFunc("GET /terms", func(w http.ResponseWriter, r *http.Request) {
		u, _ := a.currentUser(r)
		a.render(w, "terms", pageData{Title: "Terms of Service", User: u})
	})
	mux.HandleFunc("GET /discord/link/{token}", a.requireAuth(a.discordLinkConfirm))
	mux.HandleFunc("POST /profile/discord/unlink", a.requireAuth(a.discordUnlink))
	// Exact routes above win over this single-segment pattern; usernames
	// that would collide with them are reserved at signup.
	mux.HandleFunc("GET /{username}", a.publicProfilePage)
	mux.HandleFunc("POST /todos", a.requireAuth(a.createPersonalTodo))
	mux.HandleFunc("POST /solo/start", a.requireAuth(a.startSoloTimer))
	mux.HandleFunc("POST /solo/cancel", a.requireAuth(a.cancelSoloTimer))
	mux.HandleFunc("POST /solo/break/start", a.requireAuth(a.startSoloBreak))
	mux.HandleFunc("POST /solo/break/skip", a.requireAuth(a.skipSoloBreak))
	mux.HandleFunc("POST /todo/", a.requireAuth(a.todoAction))
	mux.HandleFunc("POST /rooms", a.requireAuth(a.createRoom))
	mux.HandleFunc("POST /rooms/join", a.requireAuth(a.joinRoom))
	mux.HandleFunc("POST /rooms/join-intent", a.joinRoomIntent)
	mux.HandleFunc("GET /join/confirm", a.requireAuth(a.joinRoomConfirm))
	mux.HandleFunc("POST /join/confirm", a.requireAuth(a.joinRoomConfirmPost))
	mux.HandleFunc("GET /r/{code}/timer-status", a.requireAuth(a.roomTimerStatus))
	mux.HandleFunc("GET /r/{code}/todos-fragment", a.requireAuth(a.roomTodosFragment))
	mux.HandleFunc("GET /r/{code}/members", a.requireAuth(a.roomMembersPage))
	mux.HandleFunc("GET /r/", a.requireAuth(a.roomPage))
	mux.HandleFunc("POST /r/", a.requireAuth(a.roomAction))
	mux.HandleFunc("GET /ws/r/", a.requireAuth(a.roomWS))
	mux.HandleFunc("GET /ws/me", a.requireAuth(a.userWS))
	mux.HandleFunc("GET /todos-fragment", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		u, _ := a.currentUser(r)
		a.renderPersonalTodosFragment(w, r, u.ID)
	}))
	mux.HandleFunc("GET /admin", a.requireAdmin(a.adminPage))
	mux.HandleFunc("POST /admin/users/{id}/role", a.requireAdminMutation(a.adminChangeRole))
	mux.HandleFunc("POST /admin/rooms/{id}/delete", a.requireAdminMutation(a.adminDeleteRoom))

	go a.sweepExpiredSessions(ctx)

	// Reject state-changing requests from other origins (CSRF). Requests
	// without browser origin metadata (curl, health checks) still pass.
	csrf := http.NewCrossOriginProtection()
	handler := securityHeaders(csrf.Handler(mux))

	addr := getenv("ADDR", ":8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		<-sigCtx.Done()
		log.Println("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			// Long-lived WebSocket handlers may not drain in time; close them.
			_ = srv.Close()
		}
	}()

	log.Printf("junkie listening on http://localhost%s", addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	<-shutdownDone
}

func (a *app) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.db.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintln(w, "ok")
}

func (a *app) sweepExpiredSessions(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if _, err := a.db.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`); err != nil {
			log.Printf("sweep expired sessions: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *app) migrate(ctx context.Context) error {
	entries, err := os.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		sql, err := os.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		if _, err := a.db.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("%s: %w", entry.Name(), err)
		}
	}
	return nil
}

func (a *app) home(w http.ResponseWriter, r *http.Request) {
	a.dashboard(w, r)
}

func (a *app) signupForm(w http.ResponseWriter, r *http.Request) {
	dest := "/login?mode=signup"
	if next := r.URL.Query().Get("next"); next != "" {
		dest += "&next=" + url.QueryEscape(next)
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (a *app) signup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	username := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	password := r.FormValue("password")
	next := safeNext(r.FormValue("next"))
	if username == "" || password == "" {
		a.render(w, "login", a.authPageData(r, true, next, "Username and password are required."))
		return
	}
	if !validUsername(username) {
		a.render(w, "login", a.authPageData(r, true, next, "Usernames are 2–32 characters: lowercase letters, numbers, dots, dashes, underscores."))
		return
	}
	if len([]rune(password)) < minPasswordLength {
		a.render(w, "login", a.authPageData(r, true, next, fmt.Sprintf("Passwords must be at least %d characters.", minPasswordLength)))
		return
	}
	if len(password) > maxPasswordBytes {
		a.render(w, "login", a.authPageData(r, true, next, "That password is too long."))
		return
	}
	if !a.limiter.allow("signup:"+clientIP(r), 10, time.Hour) {
		a.renderStatus(w, http.StatusTooManyRequests, "login", a.authPageData(r, true, next, "Too many new accounts from this address. Try again later."))
		return
	}
	displayName := displayNameFromUsername(username)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "could not hash password", http.StatusInternalServerError)
		return
	}
	var id string
	err = a.db.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash) VALUES ($1, $2, $3) RETURNING id`, username, displayName, string(hash)).Scan(&id)
	if err != nil {
		a.render(w, "login", a.authPageData(r, true, next, "That username is already taken."))
		return
	}
	a.createSession(w, r, id)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (a *app) loginForm(w http.ResponseWriter, r *http.Request) {
	signup := r.URL.Query().Get("mode") == "signup"
	data := a.authPageData(r, signup, safeNext(r.URL.Query().Get("next")), "")
	data.Notice = r.URL.Query().Get("notice")
	a.render(w, "login", data)
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	username := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	password := r.FormValue("password")
	next := safeNext(r.FormValue("next"))
	if !a.limiter.allow("login:"+clientIP(r), 20, 5*time.Minute) ||
		(username != "" && !a.limiter.allow("login-user:"+username, 10, 15*time.Minute)) {
		a.renderStatus(w, http.StatusTooManyRequests, "login", a.authPageData(r, false, next, "Too many sign-in attempts. Try again in a few minutes."))
		return
	}
	var id, hash string
	err := a.db.QueryRow(ctx, `SELECT id, password_hash FROM users WHERE username = $1`, username).Scan(&id, &hash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		a.render(w, "login", a.authPageData(r, false, next, "Username or password is incorrect."))
		return
	}
	a.createSession(w, r, id)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (a *app) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("junkie_session"); err == nil {
		_, _ = a.db.Exec(r.Context(), `DELETE FROM sessions WHERE token = $1`, hashToken(cookie.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: "junkie_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: isSecureRequest(r), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *app) profilePage(w http.ResponseWriter, r *http.Request) {
	u, ok := a.currentUser(r)
	if !ok {
		a.render(w, "profile", pageData{Title: "Profile", GuestMode: true})
		return
	}
	rooms, _ := a.roomsForUser(r.Context(), u.ID)
	heatmap, _ := a.activity(r.Context(), u.ID)
	discordUsername, discordLinked := a.discordLinkForUser(r.Context(), u.ID)
	a.render(w, "profile", pageData{
		Title:                "Profile",
		User:                 u,
		Rooms:                rooms,
		Activity:             heatmap.Cells,
		ActivityMonths:       heatmap.Months,
		ActivityWeeks:        heatmap.Weeks,
		ActivityTotalMinutes: heatmap.TotalMinutes,
		Connections:          a.connectionViews(r.Context(), u.ID),
		DiscordUsername:      discordUsername,
		DiscordLinked:        discordLinked,
		Error:                r.URL.Query().Get("error"),
		Notice:               r.URL.Query().Get("notice"),
	})
}

func (a *app) changePassword(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	fail := func(msg string) {
		http.Redirect(w, r, "/profile?error="+url.QueryEscape(msg), http.StatusSeeOther)
	}
	if !a.limiter.allow("pwchange:"+u.ID, 10, 15*time.Minute) {
		fail("Too many attempts. Try again in a few minutes.")
		return
	}
	current := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")
	var hash string
	if err := a.db.QueryRow(r.Context(), `SELECT password_hash FROM users WHERE id = $1`, u.ID).Scan(&hash); err != nil {
		fail("Could not verify your password.")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(current)) != nil {
		fail("Current password is incorrect.")
		return
	}
	if len([]rune(newPassword)) < minPasswordLength {
		fail(fmt.Sprintf("New password must be at least %d characters.", minPasswordLength))
		return
	}
	if len(newPassword) > maxPasswordBytes {
		fail("That new password is too long.")
		return
	}
	if newPassword != confirm {
		fail("New passwords do not match.")
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		fail("Could not update your password.")
		return
	}
	if _, err := a.db.Exec(r.Context(), `UPDATE users SET password_hash = $1 WHERE id = $2`, string(newHash), u.ID); err != nil {
		fail("Could not update your password.")
		return
	}
	// Sign out every other session so a compromised login cannot survive a
	// password change; only the session that made the change stays valid.
	if cookie, err := r.Cookie("junkie_session"); err == nil {
		_, _ = a.db.Exec(r.Context(), `DELETE FROM sessions WHERE user_id = $1 AND token <> $2`, u.ID, hashToken(cookie.Value))
	}
	http.Redirect(w, r, "/profile?notice="+url.QueryEscape("Password updated. All other devices were signed out."), http.StatusSeeOther)
}

func (a *app) deleteAccount(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	fail := func(msg string) {
		http.Redirect(w, r, "/profile?error="+url.QueryEscape(msg), http.StatusSeeOther)
	}
	if !a.limiter.allow("acctdelete:"+u.ID, 5, 15*time.Minute) {
		fail("Too many attempts. Try again in a few minutes.")
		return
	}
	if u.Role == roleOwner {
		fail("The owner account can't be deleted here. Reassign JUNKIE_OWNER_USERNAME first.")
		return
	}
	password := r.FormValue("password")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		fail("Could not delete your account.")
		return
	}
	defer tx.Rollback(r.Context())
	var hash string
	if err := tx.QueryRow(r.Context(), `SELECT password_hash FROM users WHERE id = $1 FOR UPDATE`, u.ID).Scan(&hash); err != nil {
		fail("Could not verify your password.")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		fail("Password is incorrect.")
		return
	}
	var ownedRoomCodes []string
	rows, err := tx.Query(r.Context(), `SELECT code FROM rooms WHERE creator_id = $1`, u.ID)
	if err != nil {
		fail("Could not delete your account.")
		return
	}
	for rows.Next() {
		var code string
		if rows.Scan(&code) == nil {
			ownedRoomCodes = append(ownedRoomCodes, code)
		}
	}
	rows.Close()
	metadata, _ := json.Marshal(map[string]string{"username": u.Username})
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO admin_audit_log (actor_user_id, action, target_type, target_id, metadata)
		VALUES ($1, 'user.self_deleted', 'user', $1, $2::jsonb)`, u.ID, metadata); err != nil {
		fail("Could not delete your account.")
		return
	}
	// Cascades to sessions, rooms this user created (and their members/todos/
	// timers), room memberships, personal todos, and activity history.
	if _, err := tx.Exec(r.Context(), `DELETE FROM users WHERE id = $1`, u.ID); err != nil {
		fail("Could not delete your account.")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		fail("Could not delete your account.")
		return
	}
	for _, code := range ownedRoomCodes {
		a.hub.broadcast(code, "deleted")
	}
	http.SetCookie(w, &http.Cookie{Name: "junkie_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: isSecureRequest(r), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/login?notice="+url.QueryEscape("Your account and all its data have been deleted."), http.StatusSeeOther)
}

func (a *app) changeUsername(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	fail := func(msg string) {
		http.Redirect(w, r, "/profile?error="+url.QueryEscape(msg), http.StatusSeeOther)
	}
	// The owner's username is pinned: JUNKIE_OWNER_USERNAME locates the
	// owner account by name at startup, so a rename would silently detach
	// the bootstrap. Renaming stays disabled for that account.
	if u.Role == roleOwner {
		fail("The owner account's username can't be changed.")
		return
	}
	if !a.limiter.allow("unamechange:"+u.ID, 5, time.Hour) {
		fail("Too many username changes. Try again later.")
		return
	}
	username := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	if username == u.Username {
		http.Redirect(w, r, "/profile", http.StatusSeeOther)
		return
	}
	if !validUsername(username) {
		fail("Usernames are 2–32 characters: lowercase letters, numbers, dots, dashes, underscores.")
		return
	}
	displayName := displayNameFromUsername(username)
	if _, err := a.db.Exec(r.Context(), `UPDATE users SET username = $1, display_name = $2 WHERE id = $3`, username, displayName, u.ID); err != nil {
		fail("That username is already taken.")
		return
	}
	http.Redirect(w, r, "/profile?notice="+url.QueryEscape("Username updated. You now sign in as "+username+"."), http.StatusSeeOther)
}

// maxAvatarBytes stays under the global maxRequestBody cap (64 KiB) that
// securityHeaders applies to every request; a 128px JPEG runs 3-10 KiB.
const maxAvatarBytes = 48 << 10

func (a *app) uploadAvatar(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	fail := func(msg string) {
		http.Redirect(w, r, "/profile?error="+url.QueryEscape(msg), http.StatusSeeOther)
	}
	if !a.limiter.allow("avatar:"+u.ID, 10, time.Hour) {
		fail("Too many avatar changes. Try again later.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarBytes+4096)
	if err := r.ParseMultipartForm(maxAvatarBytes + 4096); err != nil {
		fail("That image is too large. Pick a smaller one.")
		return
	}
	file, _, err := r.FormFile("avatar")
	if err != nil {
		fail("Choose an image to upload.")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxAvatarBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxAvatarBytes {
		fail("That image is too large. Pick a smaller one.")
		return
	}
	// The client downscales to a small JPEG before uploading; verify what
	// actually arrived regardless: sniffed type and sane pixel dimensions.
	contentType := http.DetectContentType(data)
	if contentType != "image/jpeg" && contentType != "image/png" {
		fail("Avatars must be a JPEG or PNG image.")
		return
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 1024 || cfg.Height > 1024 {
		fail("That image can't be used. Try a different one.")
		return
	}
	if _, err := a.db.Exec(r.Context(), `UPDATE users SET avatar = $1, avatar_updated_at = now() WHERE id = $2`, data, u.ID); err != nil {
		fail("Could not save your picture.")
		return
	}
	http.Redirect(w, r, "/profile?notice="+url.QueryEscape("Profile picture updated."), http.StatusSeeOther)
}

func (a *app) removeAvatar(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	_, _ = a.db.Exec(r.Context(), `UPDATE users SET avatar = NULL, avatar_updated_at = NULL WHERE id = $1`, u.ID)
	http.Redirect(w, r, "/profile?notice="+url.QueryEscape("Profile picture removed."), http.StatusSeeOther)
}

// orderPair returns the two ids with the smaller first, matching the
// connections table's (user_a < user_b) storage convention.
func orderPair(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

func (a *app) areConnected(ctx context.Context, userA, userB string) bool {
	first, second := orderPair(userA, userB)
	var exists bool
	err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM connections WHERE user_a = $1 AND user_b = $2)`, first, second).Scan(&exists)
	return err == nil && exists
}

// consumeConnectToken redeems a single-use connect invite belonging to
// ownerID: the token row is deleted and the pair becomes connected. Returns
// false for unknown, expired, or self-redeemed tokens.
func (a *app) consumeConnectToken(ctx context.Context, ownerID, token, visitorID string) bool {
	if token == "" || ownerID == visitorID {
		return false
	}
	tag, err := a.db.Exec(ctx, `DELETE FROM connect_tokens WHERE user_id = $1 AND token_hash = $2 AND expires_at > now()`, ownerID, hashToken(token))
	if err != nil || tag.RowsAffected() == 0 {
		return false
	}
	first, second := orderPair(ownerID, visitorID)
	_, err = a.db.Exec(ctx, `INSERT INTO connections (user_a, user_b) VALUES ($1, $2) ON CONFLICT DO NOTHING`, first, second)
	return err == nil
}

// createConnectLink mints (or replaces) the caller's single outstanding
// connect invite and returns the full URL as plain text for the copy button.
func (a *app) createConnectLink(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	if !a.limiter.allow("connectlink:"+u.ID, 10, time.Hour) {
		http.Error(w, "too many links generated; try again later", http.StatusTooManyRequests)
		return
	}
	token := randomHex(32)
	expires := time.Now().Add(7 * 24 * time.Hour)
	if _, err := a.db.Exec(r.Context(), `
		INSERT INTO connect_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET token_hash = EXCLUDED.token_hash, created_at = now(), expires_at = EXCLUDED.expires_at`,
		u.ID, hashToken(token), expires); err != nil {
		http.Error(w, "could not create link", http.StatusInternalServerError)
		return
	}
	scheme := "http"
	if isSecureRequest(r) {
		scheme = "https"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "%s://%s/%s?connect=%s", scheme, r.Host, u.Username, token)
}

// publicProfilePage serves /<username>. Privacy rule: unless the viewer is
// connected to (or is) that user, the response is indistinguishable from a
// username that does not exist.
func (a *app) publicProfilePage(w http.ResponseWriter, r *http.Request) {
	username := strings.ToLower(strings.TrimSpace(r.PathValue("username")))
	if !usernamePattern.MatchString(username) {
		http.NotFound(w, r)
		return
	}
	token := r.URL.Query().Get("connect")
	u, authed := a.currentUser(r)
	if !authed {
		if token != "" {
			// Signing up or in first, then returning here, completes the
			// connection -- same intent-preserving flow as room invites.
			dest := "/" + username + "?connect=" + url.QueryEscape(token)
			http.Redirect(w, r, "/login?mode=signup&next="+url.QueryEscape(dest), http.StatusSeeOther)
			return
		}
		http.NotFound(w, r)
		return
	}
	var target user
	err := a.db.QueryRow(r.Context(), `
		SELECT id, username, display_name, avatar IS NOT NULL,
			COALESCE(EXTRACT(EPOCH FROM avatar_updated_at), 0)::bigint
		FROM users WHERE username = $1`, username).Scan(
		&target.ID, &target.Username, &target.DisplayName, &target.HasAvatar, &target.AvatarVersion)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if target.ID == u.ID {
		http.Redirect(w, r, "/profile", http.StatusSeeOther)
		return
	}
	if token != "" {
		if a.consumeConnectToken(r.Context(), target.ID, token, u.ID) {
			http.Redirect(w, r, "/"+target.Username+"?notice="+url.QueryEscape("You are now connected with "+target.DisplayName+"."), http.StatusSeeOther)
			return
		}
		// A dead token still lands connected visitors on the profile; for
		// everyone else it must look like nothing is here.
	}
	if !a.areConnected(r.Context(), u.ID, target.ID) {
		http.NotFound(w, r)
		return
	}
	heat, _ := a.activity(r.Context(), target.ID)
	view := publicProfileView{
		ProfileUser:          target,
		Activity:             heat.Cells,
		ActivityMonths:       heat.Months,
		ActivityWeeks:        heat.Weeks,
		ActivityTotalMinutes: heat.TotalMinutes,
	}
	a.render(w, "public-profile", pageData{
		Title:         target.DisplayName,
		User:          u,
		PublicProfile: &view,
		Notice:        r.URL.Query().Get("notice"),
	})
}

// connectionsPage is the feed of the caller's connections, one heatmap
// card per person, newest connection first.
func (a *app) connectionsPage(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	a.render(w, "connections", pageData{
		Title:       "Connections",
		User:        u,
		Connections: a.connectionViews(r.Context(), u.ID),
		Notice:      r.URL.Query().Get("notice"),
	})
}

// connectionViews loads the caller's connections with each one's heatmap,
// newest connection first, for the profile page list.
func (a *app) connectionViews(ctx context.Context, userID string) []publicProfileView {
	rows, err := a.db.Query(ctx, `
		SELECT u.id, u.username, u.display_name, u.avatar IS NOT NULL,
			COALESCE(EXTRACT(EPOCH FROM u.avatar_updated_at), 0)::bigint
		FROM connections c
		JOIN users u ON u.id = CASE WHEN c.user_a = $1 THEN c.user_b ELSE c.user_a END
		WHERE c.user_a = $1 OR c.user_b = $1
		ORDER BY c.created_at DESC`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var people []user
	for rows.Next() {
		var p user
		if rows.Scan(&p.ID, &p.Username, &p.DisplayName, &p.HasAvatar, &p.AvatarVersion) == nil {
			people = append(people, p)
		}
	}
	var views []publicProfileView
	for _, p := range people {
		heat, err := a.activity(ctx, p.ID)
		if err != nil {
			continue
		}
		views = append(views, publicProfileView{
			ProfileUser:          p,
			Activity:             heat.Cells,
			ActivityMonths:       heat.Months,
			ActivityWeeks:        heat.Weeks,
			ActivityTotalMinutes: heat.TotalMinutes,
		})
	}
	return views
}

// serveAvatar returns a user's avatar bytes. Any signed-in user can fetch
// any avatar by user id: ids are UUIDs already exposed to room members, and
// avatars render next to shared todos and timers.
func (a *app) serveAvatar(w http.ResponseWriter, r *http.Request) {
	var data []byte
	err := a.db.QueryRow(r.Context(), `SELECT avatar FROM users WHERE id = $1 AND avatar IS NOT NULL`, r.PathValue("id")).Scan(&data)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	serveStatic(w, r, http.DetectContentType(data), data)
}

func (a *app) dashboard(w http.ResponseWriter, r *http.Request) {
	u, ok := a.currentUser(r)
	if !ok {
		a.render(w, "dashboard", pageData{Title: "Dashboard", GuestMode: true})
		return
	}
	soloTimer, _ := a.normalizeSoloTimer(r.Context(), u.ID)
	todos, _ := a.personalTodos(r.Context(), u.ID)
	rooms, _ := a.roomsForUser(r.Context(), u.ID)
	deskRoomTodos := make([]roomTodosGroup, 0, len(rooms))
	for _, rm := range rooms {
		roomTodoList, _ := a.roomTodos(r.Context(), rm.ID)
		for i := range roomTodoList {
			roomTodoList[i].RoomCode = rm.Code
		}
		roomTimer, transitioned, _ := a.normalizeTimer(r.Context(), rm.ID, u.ID)
		if transitioned {
			a.broadcastTimerPhase(rm, roomTimer)
		}
		deskRoomTodos = append(deskRoomTodos, roomTodosGroup{Room: rm, Grouped: groupRoomTodos(roomTodoList, u.ID), Timer: roomTimer})
	}
	a.render(w, "dashboard", pageData{
		Title:         "Dashboard",
		User:          u,
		PersonalTodos: todos,
		Rooms:         rooms,
		DeskRoomTodos: deskRoomTodos,
		SoloTimer:     soloTimer,
		Error:         r.URL.Query().Get("error"),
	})
}

// userChannel names a per-user hub channel for cross-device sync of
// personal state (private todos, solo timer). Room codes are uppercase
// alphanumerics, so the lowercase prefix can never collide with one.
func userChannel(userID string) string {
	return "user:" + userID
}

func (a *app) startSoloTimer(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	if active, _ := a.activeSoloTimer(r.Context(), u.ID); active != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	focus := clampInt(r.FormValue("focus_minutes"), 5, 180, 50)
	ends := time.Now().Add(time.Duration(focus) * time.Minute)
	_, _ = a.db.Exec(r.Context(), `INSERT INTO timer_runs (user_id, phase, focus_minutes, break_minutes, total_sessions, phase_ends_at) VALUES ($1, 'focus', $2, 0, 1, $3)`, u.ID, focus, ends)
	a.hub.broadcast(userChannel(u.ID), "solo-timer")
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (a *app) cancelSoloTimer(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	_, _ = a.db.Exec(r.Context(), `UPDATE timer_runs SET phase = 'ended', ended_at = now() WHERE user_id = $1 AND room_id IS NULL AND ended_at IS NULL AND phase = 'focus'`, u.ID)
	a.hub.broadcast(userChannel(u.ID), "solo-timer")
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (a *app) startSoloBreak(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	timer, _ := a.normalizeSoloTimer(r.Context(), u.ID)
	if timer == nil || timer.Phase != "break" || !soloBreakPending(timer) {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	ends := time.Now().Add(time.Duration(timer.BreakMinutes) * time.Minute)
	_, _ = a.db.Exec(r.Context(), `UPDATE timer_runs SET phase_started_at = now(), phase_ends_at = $1 WHERE id = $2`, ends, timer.ID)
	a.hub.broadcast(userChannel(u.ID), "solo-timer")
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (a *app) skipSoloBreak(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	timer, _ := a.normalizeSoloTimer(r.Context(), u.ID)
	if timer == nil || timer.Phase != "break" {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	focus := timer.FocusMinutes
	if focus < 5 {
		focus = 5
	} else if focus > 180 {
		focus = 180
	}
	// End the break, then immediately start the next private focus with the
	// same duration. Solo runs are single-session, so "next session" is a
	// fresh run rather than bumping current_session.
	_, _ = a.db.Exec(r.Context(), `UPDATE timer_runs SET phase = 'ended', ended_at = now() WHERE id = $1 AND phase = 'break' AND ended_at IS NULL`, timer.ID)
	ends := time.Now().Add(time.Duration(focus) * time.Minute)
	_, _ = a.db.Exec(r.Context(), `INSERT INTO timer_runs (user_id, phase, focus_minutes, break_minutes, total_sessions, phase_ends_at) VALUES ($1, 'focus', $2, 0, 1, $3)`, u.ID, focus, ends)
	a.hub.broadcast(userChannel(u.ID), "solo-timer")
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (a *app) createPersonalTodo(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	text := limitRunes(strings.TrimSpace(r.FormValue("text")), maxTodoTextLen)
	if text != "" {
		_, _ = a.db.Exec(r.Context(), `INSERT INTO todos (user_id, text) VALUES ($1, $2)`, u.ID, text)
	}
	a.hub.broadcast(userChannel(u.ID), "todos")
	if isHTMXRequest(r) {
		a.renderPersonalTodosFragment(w, r, u.ID)
		return
	}
	http.Redirect(w, r, "/dashboard?todos=private", http.StatusSeeOther)
}

// isHTMXRequest reports whether the request was made by htmx (hx-post/hx-get
// etc.), which expects an HTML fragment back instead of a full-page redirect.
func isHTMXRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// renderFragment writes a single named template directly, bypassing the
// "shell" page wrapper -- used for htmx partial swaps.
func (a *app) renderFragment(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := a.templates.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("render fragment %s: %v", name, err)
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

type personalTodosView struct {
	Todos    []todo
	HasRooms bool
}

func (a *app) renderPersonalTodosFragment(w http.ResponseWriter, r *http.Request, userID string) {
	todos, _ := a.personalTodos(r.Context(), userID)
	// The focus-desk peek panel renders personal todos with a different row
	// template than the dashboard list; the form says which one it needs.
	if r.FormValue("view") == "focus" {
		a.renderFragment(w, "focus-todos-list", todos)
		return
	}
	rooms, _ := a.roomsForUser(r.Context(), userID)
	a.renderFragment(w, "personal-todos-list", personalTodosView{Todos: todos, HasRooms: len(rooms) > 0})
}

func (a *app) renderRoomTodosFragment(w http.ResponseWriter, r *http.Request, roomID, roomCode string, u user, desk bool) {
	todos, _ := a.roomTodos(r.Context(), roomID)
	view := groupRoomTodos(todos, u.ID).View(roomCode, u.DisplayName)
	name := "todo-groups-room"
	if desk {
		name = "todo-groups-desk-room"
	}
	a.renderFragment(w, name, view)
}

func (a *app) todoAction(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	path := strings.TrimPrefix(r.URL.Path, "/todo/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	id, action := parts[0], parts[1]
	var roomID, roomCode string
	_ = a.db.QueryRow(r.Context(), `SELECT COALESCE(r.id::text, ''), COALESCE(r.code, '') FROM todos t LEFT JOIN rooms r ON r.id = t.room_id WHERE t.id = $1`, id).Scan(&roomID, &roomCode)
	var completedText string
	switch action {
	case "toggle":
		var nowDone bool
		var todoText string
		err := a.db.QueryRow(r.Context(), `UPDATE todos SET done = NOT done, updated_at = now() WHERE id = $1 AND user_id = $2 RETURNING done, text`, id, u.ID).Scan(&nowDone, &todoText)
		if err != nil {
			if isHTMXRequest(r) {
				if roomCode != "" {
					a.renderRoomTodosFragment(w, r, roomID, roomCode, u, r.FormValue("desk") == "1")
				} else {
					a.renderPersonalTodosFragment(w, r, u.ID)
				}
				return
			}
			a.todoActionDenied(w, r, roomCode, "You can only complete your own todos.")
			return
		}
		if nowDone {
			completedText = todoText
		}
	case "edit":
		text := limitRunes(strings.TrimSpace(r.FormValue("text")), maxTodoTextLen)
		if text == "" {
			// An emptied todo is a no-op, not a delete; re-render so the
			// client falls back to the stored text.
			break
		}
		// Only active todos are editable: completed and removed ones keep
		// the text they had when they changed state.
		tag, err := a.db.Exec(r.Context(), `UPDATE todos SET text = $2, updated_at = now() WHERE id = $1 AND user_id = $3 AND NOT done AND NOT removed`, id, text, u.ID)
		if err != nil || tag.RowsAffected() == 0 {
			if isHTMXRequest(r) {
				if roomCode != "" {
					a.renderRoomTodosFragment(w, r, roomID, roomCode, u, r.FormValue("desk") == "1")
				} else {
					a.renderPersonalTodosFragment(w, r, u.ID)
				}
				return
			}
			a.todoActionDenied(w, r, roomCode, "You can only edit your own todos.")
			return
		}
	// Every mutation is author-only: room members see each other's todos
	// but can never remove, restore, or delete them.
	case "remove":
		_, _ = a.db.Exec(r.Context(), `UPDATE todos SET removed = true, updated_at = now() WHERE id = $1 AND user_id = $2`, id, u.ID)
	case "restore":
		_, _ = a.db.Exec(r.Context(), `UPDATE todos SET removed = false, updated_at = now() WHERE id = $1 AND user_id = $2`, id, u.ID)
	case "delete":
		_, _ = a.db.Exec(r.Context(), `DELETE FROM todos WHERE id = $1 AND user_id = $2`, id, u.ID)
	default:
		http.NotFound(w, r)
		return
	}
	desk := r.FormValue("desk") == "1"
	if roomCode != "" {
		a.hub.broadcast(roomCode, "todos")
		if completedText != "" {
			// Separate from the "todos" patch signal: a small celebration
			// event so other members see progress happening live.
			a.hub.broadcastJSON(roomCode, map[string]string{
				"type": "todo-done", "actorId": u.ID, "actor": u.DisplayName, "text": completedText,
			})
		}
		if isHTMXRequest(r) {
			a.renderRoomTodosFragment(w, r, roomID, roomCode, u, desk)
			return
		}
		if desk {
			code := strings.TrimSpace(r.FormValue("room"))
			if code == "" {
				code = roomCode
			}
			http.Redirect(w, r, "/dashboard?todos=room&room="+url.QueryEscape(code), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/r/"+roomCode, http.StatusSeeOther)
		return
	}
	a.hub.broadcast(userChannel(u.ID), "todos")
	if isHTMXRequest(r) {
		a.renderPersonalTodosFragment(w, r, u.ID)
		return
	}
	http.Redirect(w, r, "/dashboard?todos=private", http.StatusSeeOther)
}

func (a *app) todoActionDenied(w http.ResponseWriter, r *http.Request, roomCode, message string) {
	errMsg := url.QueryEscape(message)
	if roomCode != "" {
		if r.FormValue("desk") == "1" {
			code := strings.TrimSpace(r.FormValue("room"))
			if code == "" {
				code = roomCode
			}
			http.Redirect(w, r, "/dashboard?todos=room&room="+url.QueryEscape(code)+"&error="+errMsg, http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/r/"+roomCode+"?error="+errMsg, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/dashboard?todos=private&error="+errMsg, http.StatusSeeOther)
}

func (a *app) createRoom(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	name := limitRunes(strings.TrimSpace(r.FormValue("name")), maxRoomNameLen)
	if name == "" {
		name = u.DisplayName + "'s focus room"
	}
	var code, roomID string
	err := errors.New("no attempt")
	for range 5 {
		code = randomCode()
		err = a.db.QueryRow(r.Context(), `INSERT INTO rooms (code, name, creator_id) VALUES ($1, $2, $3) RETURNING id`, code, name, u.ID).Scan(&roomID)
		if err == nil || !isUniqueViolation(err) {
			break
		}
	}
	if err != nil {
		log.Printf("create room: %v", err)
		http.Error(w, "could not create room", http.StatusInternalServerError)
		return
	}
	_, _ = a.db.Exec(r.Context(), `INSERT INTO room_members (room_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, roomID, u.ID)
	http.Redirect(w, r, "/r/"+code, http.StatusSeeOther)
}

func (a *app) joinRoom(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	code := normalizeRoomCode(r.FormValue("code"))
	back := safeNext(r.FormValue("next"))
	if back == "/" {
		back = "/dashboard"
	}
	if code == "" {
		http.Redirect(w, r, back+"?error="+url.QueryEscape("Enter a room code to join."), http.StatusSeeOther)
		return
	}
	rm, ok := a.findRoom(r.Context(), code)
	if !ok {
		http.Redirect(w, r, back+"?error="+url.QueryEscape("No room found with that code."), http.StatusSeeOther)
		return
	}
	a.addRoomMember(r.Context(), rm.ID, u.ID)
	http.Redirect(w, r, "/r/"+rm.Code, http.StatusSeeOther)
}

func (a *app) joinRoomIntent(w http.ResponseWriter, r *http.Request) {
	code := normalizeRoomCode(r.FormValue("code"))
	back := safeNext(r.FormValue("next"))
	if back == "/" {
		back = "/dashboard"
	}
	if code == "" {
		http.Redirect(w, r, back+"?error="+url.QueryEscape("Enter a room code to join."), http.StatusSeeOther)
		return
	}
	rm, ok := a.findRoom(r.Context(), code)
	if !ok {
		http.Redirect(w, r, back+"?error="+url.QueryEscape("No room found with that code."), http.StatusSeeOther)
		return
	}
	if u, ok := a.currentUser(r); ok {
		if a.isRoomMember(r.Context(), rm.ID, u.ID) {
			http.Redirect(w, r, "/r/"+rm.Code, http.StatusSeeOther)
			return
		}
		a.addRoomMember(r.Context(), rm.ID, u.ID)
		http.Redirect(w, r, "/r/"+rm.Code, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login?next="+url.QueryEscape("/join/confirm?code="+rm.Code), http.StatusSeeOther)
}

func (a *app) joinRoomConfirm(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	code := normalizeRoomCode(r.URL.Query().Get("code"))
	if code == "" {
		http.Redirect(w, r, "/dashboard?error="+url.QueryEscape("Enter a room code to join."), http.StatusSeeOther)
		return
	}
	rm, ok := a.findRoom(r.Context(), code)
	if !ok {
		http.Redirect(w, r, "/dashboard?error="+url.QueryEscape("No room found with that code."), http.StatusSeeOther)
		return
	}
	if a.isRoomMember(r.Context(), rm.ID, u.ID) {
		http.Redirect(w, r, "/r/"+rm.Code, http.StatusSeeOther)
		return
	}
	a.render(w, "room-invite", pageData{Title: "Join room", User: u, Room: rm})
}

func (a *app) joinRoomConfirmPost(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	if r.FormValue("action") == "cancel" {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	code := normalizeRoomCode(r.FormValue("code"))
	if code == "" {
		http.Redirect(w, r, "/dashboard?error="+url.QueryEscape("Enter a room code to join."), http.StatusSeeOther)
		return
	}
	rm, ok := a.findRoom(r.Context(), code)
	if !ok {
		http.Redirect(w, r, "/dashboard?error="+url.QueryEscape("No room found with that code."), http.StatusSeeOther)
		return
	}
	a.addRoomMember(r.Context(), rm.ID, u.ID)
	http.Redirect(w, r, "/r/"+rm.Code, http.StatusSeeOther)
}

func (a *app) roomPage(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	code := normalizeRoomCode(strings.TrimPrefix(r.URL.Path, "/r/"))
	if strings.Contains(code, "/") {
		http.NotFound(w, r)
		return
	}
	rm, ok := a.findRoom(r.Context(), code)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !a.isRoomMember(r.Context(), rm.ID, u.ID) {
		a.render(w, "room-invite", pageData{Title: "Join room", User: u, Room: rm})
		return
	}
	timer, transitioned, _ := a.normalizeTimer(r.Context(), rm.ID, u.ID)
	if transitioned {
		a.broadcastTimerPhase(rm, timer)
	}
	todos, _ := a.roomTodos(r.Context(), rm.ID)
	rooms, _ := a.roomsForUser(r.Context(), u.ID)
	focusMode := timer != nil && timer.Phase == "focus" && timer.Participant
	memberCount, _ := a.roomMemberCount(r.Context(), rm.ID)
	waiting := false
	if timer == nil || (timer.Phase == "focus" && !timer.Participant) {
		waiting = a.roomWaiting(r.Context(), rm.ID, u.ID)
	}
	a.render(w, "room", pageData{Title: rm.Name, User: u, Room: rm, Rooms: rooms, RoomTodosGrouped: groupRoomTodos(todos, u.ID), Timer: timer, FocusMode: focusMode, MemberCount: memberCount, TimerWaiting: waiting, Error: r.URL.Query().Get("error")})
}

// roomTodosFragment serves the current todo-groups markup for a room so
// clients can patch their DOM after a WebSocket "todos" broadcast instead
// of reloading the page. hub.broadcast includes the sender's own
// connection, so this also covers the acting user's own tab.
func (a *app) roomTodosFragment(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	rm, ok := a.findRoom(r.Context(), r.PathValue("code"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !a.isRoomMember(r.Context(), rm.ID, u.ID) {
		http.Error(w, "room membership required", http.StatusForbidden)
		return
	}
	a.renderRoomTodosFragment(w, r, rm.ID, rm.Code, u, r.URL.Query().Get("desk") == "1")
}

func (a *app) roomTimerStatus(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	rm, ok := a.findRoom(r.Context(), r.PathValue("code"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !a.isRoomMember(r.Context(), rm.ID, u.ID) {
		http.Error(w, "room membership required", http.StatusForbidden)
		return
	}

	timer, transitioned, err := a.normalizeTimer(r.Context(), rm.ID, u.ID)
	if err != nil {
		http.Error(w, "could not read timer status", http.StatusInternalServerError)
		return
	}
	if transitioned {
		a.broadcastTimerPhase(rm, timer)
	}

	status := timerStatus{Phase: "idle"}
	if timer != nil {
		status = timerStatus{
			RunID:            timer.ID,
			Phase:            timer.Phase,
			EndsAt:           timer.PhaseEndsAt.UTC().Format(time.RFC3339Nano),
			CurrentSession:   timer.CurrentSession,
			TotalSessions:    timer.TotalSessions,
			Participant:      timer.Participant,
			ParticipantCount: len(timer.Participants),
		}
		if timer.Phase == "lobby" {
			status.LobbyDeadline = status.EndsAt
		}
		if timer.PausedAt != nil {
			status.Paused = true
			status.PausedAt = timer.PausedAt.UTC().Format(time.RFC3339Nano)
			if timer.PausedRemainingSeconds != nil {
				status.PausedRemainingSeconds = *timer.PausedRemainingSeconds
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		log.Printf("encode room timer status %s: %v", rm.Code, err)
	}
}

func (a *app) roomAction(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/r/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	code, action := normalizeRoomCode(parts[0]), parts[1]
	rm, ok := a.findRoom(r.Context(), code)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !a.isRoomMember(r.Context(), rm.ID, u.ID) {
		http.Redirect(w, r, "/r/"+rm.Code, http.StatusSeeOther)
		return
	}
	switch action {
	case "rename":
		name := limitRunes(strings.TrimSpace(r.FormValue("name")), maxRoomNameLen)
		if name != "" {
			_, _ = a.db.Exec(r.Context(), `UPDATE rooms SET name = $1, updated_at = now() WHERE id = $2`, name, rm.ID)
		}
	case "settings":
		focus := clampInt(r.FormValue("focus_minutes"), 5, 180, rm.FocusMinutes)
		breaks := clampInt(r.FormValue("break_minutes"), 1, 60, rm.BreakMinutes)
		sessions := clampInt(r.FormValue("auto_sessions"), 1, 12, rm.AutoSessions)
		autoRoll := rm.AutoRoll
		if v := r.FormValue("auto_roll"); v != "" {
			autoRoll = v == "1"
		}
		_ = a.applyRoomSettings(r.Context(), rm.ID, focus, breaks, sessions, autoRoll)
	case "delete":
		if rm.CreatorID != u.ID {
			http.Error(w, "only the creator can delete this room", http.StatusForbidden)
			return
		}
		_, _ = a.db.Exec(r.Context(), `DELETE FROM rooms WHERE id = $1`, rm.ID)
		a.hub.broadcast(code, "deleted")
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	case "todos":
		text := limitRunes(strings.TrimSpace(r.FormValue("text")), maxTodoTextLen)
		if text != "" {
			_, _ = a.db.Exec(r.Context(), `INSERT INTO todos (user_id, room_id, text) VALUES ($1, $2, $3)`, u.ID, rm.ID, text)
		}
		a.hub.broadcast(code, action)
		if isHTMXRequest(r) {
			a.renderRoomTodosFragment(w, r, rm.ID, rm.Code, u, r.FormValue("desk") == "1")
			return
		}
		if next := strings.TrimSpace(r.FormValue("next")); next != "" {
			http.Redirect(w, r, safeNext(next), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/r/"+code, http.StatusSeeOther)
		return
	case "timer-start":
		focusMinutes, err := requestedRoomFocusMinutes(r, rm.FocusMinutes)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, _, err := a.startRoomTimerAndSchedule(r.Context(), rm, u.ID, focusMinutes, u.DisplayName); err != nil {
			log.Printf("start room timer %s: %v", rm.Code, err)
			http.Error(w, "could not start timer", http.StatusInternalServerError)
			return
		}
		action = ""
	case "timer-join":
		_, _, _ = a.joinTimer(r.Context(), rm, u.ID)
	case "timer-pause":
		if timer, changed, err := a.pauseRoomBreak(r.Context(), rm.ID, u.ID); err != nil {
			http.Error(w, "could not pause break", http.StatusInternalServerError)
			return
		} else if !changed {
			action = ""
		} else {
			a.notifyDiscord(rm, timer)
		}
	case "timer-resume":
		if timer, changed, err := a.resumeRoomBreak(r.Context(), rm.ID, u.ID); err != nil {
			http.Error(w, "could not resume break", http.StatusInternalServerError)
			return
		} else if !changed {
			action = ""
		} else {
			a.notifyDiscord(rm, timer)
		}
	case "timer-skip-break":
		if timer, changed, err := a.skipRoomBreak(r.Context(), rm.ID, u.ID); err != nil {
			http.Error(w, "could not skip break", http.StatusInternalServerError)
			return
		} else if !changed {
			action = ""
		} else {
			action = "timer-phase"
			a.notifyDiscord(rm, timer)
		}
	case "timer-break-length":
		minutes := clampInt(r.FormValue("minutes"), 1, 60, rm.BreakMinutes)
		// Set the length and start in one motion; only meaningful while the
		// break is paused (which includes the auto-roll-off pending state).
		var runID string
		err := a.db.QueryRow(r.Context(), `
			UPDATE timer_runs
			SET phase_started_at = now(),
				phase_ends_at = now() + make_interval(mins => $2),
				paused_at = NULL,
				paused_remaining_seconds = NULL
			WHERE room_id = $1 AND ended_at IS NULL AND phase = 'break' AND paused_at IS NOT NULL
			RETURNING id`, rm.ID, minutes).Scan(&runID)
		if errors.Is(err, pgx.ErrNoRows) {
			action = ""
		} else if err != nil {
			http.Error(w, "could not start break", http.StatusInternalServerError)
			return
		} else {
			action = "timer-phase"
			if timer, err := a.activeTimer(r.Context(), rm.ID, u.ID); err == nil {
				a.notifyDiscord(rm, timer)
			}
		}
	case "timer-leave":
		if ended, _ := a.leaveTimer(r.Context(), rm, u.ID); ended {
			action = "timer-end"
			a.notifyDiscord(rm, nil)
		}
	default:
		http.NotFound(w, r)
		return
	}
	if action != "" {
		a.hub.broadcast(code, action)
	}
	if next := strings.TrimSpace(r.FormValue("next")); next != "" {
		http.Redirect(w, r, safeNext(next), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/r/"+code, http.StatusSeeOther)
}

func (a *app) roomWS(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	code := normalizeRoomCode(strings.TrimPrefix(r.URL.Path, "/ws/r/"))
	rm, ok := a.findRoom(r.Context(), code)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !a.isRoomMember(r.Context(), rm.ID, u.ID) {
		http.Error(w, "room membership required", http.StatusForbidden)
		return
	}
	// Default options enforce a same-origin handshake, blocking
	// cross-site WebSocket hijacking.
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	a.hub.join(code, c)
	defer a.hub.leave(code, c)
	for {
		_, _, err := c.Read(r.Context())
		if err != nil {
			return
		}
	}
}

// userWS subscribes the connection to the caller's own channel so personal
// state (private todos, solo timer) syncs live across the user's devices.
func (a *app) userWS(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	// Default options enforce a same-origin handshake, blocking
	// cross-site WebSocket hijacking.
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	channel := userChannel(u.ID)
	a.hub.join(channel, c)
	defer a.hub.leave(channel, c)
	for {
		_, _, err := c.Read(r.Context())
		if err != nil {
			return
		}
	}
}

// broadcastTimerPhase tells a room's live viewers a phase changed so they
// can refresh, and -- when the run just moved into break -- also sends a
// richer invite so members who never joined the ending focus block (and
// aren't looking at the app) get a device notification that there's a
// window to join before the next block starts.
func (a *app) broadcastTimerPhase(rm room, timer *timerRun) {
	a.hub.broadcast(rm.Code, "timer-phase")
	if timer != nil && timer.Phase == "break" {
		a.hub.broadcastJSON(rm.Code, map[string]interface{}{
			"type": "timer-break-invite", "roomCode": rm.Code, "roomName": rm.Name,
			"breakDeadline": timer.PhaseEndsAt.UTC().Format(time.RFC3339Nano),
		})
	}
	a.notifyDiscord(rm, timer)
}

func (a *app) normalizeTimer(ctx context.Context, roomID, userID string) (*timerRun, bool, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)

	var timer timerRun
	err = tx.QueryRow(ctx, `
		SELECT tr.id, tr.phase, tr.focus_minutes, tr.break_minutes, tr.total_sessions, tr.current_session,
			tr.phase_started_at, tr.phase_ends_at, tr.paused_at, tr.paused_remaining_seconds,
			EXISTS (SELECT 1 FROM timer_participants tp WHERE tp.timer_run_id = tr.id AND tp.user_id = $2)
		FROM timer_runs tr
		WHERE tr.room_id = $1 AND tr.ended_at IS NULL AND tr.phase <> 'ended'
		ORDER BY tr.created_at DESC
		LIMIT 1
		FOR UPDATE`, roomID, userID).Scan(
		&timer.ID, &timer.Phase, &timer.FocusMinutes, &timer.BreakMinutes, &timer.TotalSessions,
		&timer.CurrentSession, &timer.PhaseStartedAt, &timer.PhaseEndsAt, &timer.PausedAt,
		&timer.PausedRemainingSeconds, &timer.Participant,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	var autoRoll bool
	if err = tx.QueryRow(ctx, `SELECT auto_roll FROM rooms WHERE id = $1`, roomID).Scan(&autoRoll); err != nil {
		return nil, false, err
	}

	now := time.Now()
	for timer.Phase != "ended" && timer.PausedAt == nil && !now.Before(timer.PhaseEndsAt) {
		if timer.Phase == "lobby" {
			timer.Phase = "focus"
			timer.PhaseStartedAt = now
			timer.PhaseEndsAt = now.Add(time.Duration(timer.FocusMinutes) * time.Minute)
			if _, err = tx.Exec(ctx, `UPDATE timer_runs SET phase = 'focus', phase_started_at = $1, phase_ends_at = $2 WHERE id = $3`, timer.PhaseStartedAt, timer.PhaseEndsAt, timer.ID); err != nil {
				return nil, false, err
			}
			timer.Transitioned = true
		} else if timer.Phase == "focus" {
			if _, err = tx.Exec(ctx, `
				INSERT INTO activity (user_id, activity_date, focus_minutes)
				SELECT user_id, CURRENT_DATE, $1 FROM timer_participants WHERE timer_run_id = $2
				ON CONFLICT (user_id, activity_date)
				DO UPDATE SET focus_minutes = activity.focus_minutes + EXCLUDED.focus_minutes`, timer.FocusMinutes, timer.ID); err != nil {
				return nil, false, err
			}
			if timer.CurrentSession >= timer.TotalSessions {
				if _, err = tx.Exec(ctx, `UPDATE timer_runs SET phase = 'ended', ended_at = now() WHERE id = $1`, timer.ID); err != nil {
					return nil, false, err
				}
				timer.Phase = "ended"
				timer.Transitioned = true
				break
			}
			timer.Phase = "break"
			timer.PhaseStartedAt = now
			timer.PhaseEndsAt = now.Add(time.Duration(timer.BreakMinutes) * time.Minute)
			if autoRoll {
				if _, err = tx.Exec(ctx, `UPDATE timer_runs SET phase = 'break', phase_started_at = $1, phase_ends_at = $2, paused_at = NULL, paused_remaining_seconds = NULL WHERE id = $3`, timer.PhaseStartedAt, timer.PhaseEndsAt, timer.ID); err != nil {
					return nil, false, err
				}
			} else {
				// Auto-roll off: the break arrives paused at full length so
				// members can adjust it and start it deliberately. The paused
				// state also halts this loop, exactly like a manual pause.
				remaining := timer.BreakMinutes * 60
				if _, err = tx.Exec(ctx, `UPDATE timer_runs SET phase = 'break', phase_started_at = $1, phase_ends_at = $2, paused_at = $1, paused_remaining_seconds = $3 WHERE id = $4`, timer.PhaseStartedAt, timer.PhaseEndsAt, remaining, timer.ID); err != nil {
					return nil, false, err
				}
				pausedAt := now
				timer.PausedAt = &pausedAt
				timer.PausedRemainingSeconds = &remaining
			}
			// The break is the joinable window: pull in everyone who asked to
			// join while focus was running (or before the run existed).
			if _, err = tx.Exec(ctx, `
				INSERT INTO timer_participants (timer_run_id, user_id)
				SELECT $1, user_id FROM room_waiting WHERE room_id = $2
				ON CONFLICT DO NOTHING`, timer.ID, roomID); err != nil {
				return nil, false, err
			}
			if _, err = tx.Exec(ctx, `DELETE FROM room_waiting WHERE room_id = $1`, roomID); err != nil {
				return nil, false, err
			}
			timer.Transitioned = true
		} else if timer.Phase == "break" {
			timer.Phase = "focus"
			timer.CurrentSession++
			timer.PhaseStartedAt = now
			timer.PhaseEndsAt = now.Add(time.Duration(timer.FocusMinutes) * time.Minute)
			if _, err = tx.Exec(ctx, `UPDATE timer_runs SET phase = 'focus', current_session = $1, phase_started_at = $2, phase_ends_at = $3, paused_at = NULL, paused_remaining_seconds = NULL WHERE id = $4`, timer.CurrentSession, timer.PhaseStartedAt, timer.PhaseEndsAt, timer.ID); err != nil {
				return nil, false, err
			}
			timer.Transitioned = true
		}
	}
	if timer.Phase == "ended" {
		if err = tx.Commit(ctx); err != nil {
			return nil, false, err
		}
		return nil, true, nil
	}
	// A focus->break transition above may have absorbed userID from the
	// waiting list, making the flag read at the top of the query stale.
	if timer.Transitioned && !timer.Participant {
		if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM timer_participants WHERE timer_run_id = $1 AND user_id = $2)`, timer.ID, userID).Scan(&timer.Participant); err != nil {
			return nil, false, err
		}
	}
	rows, err := tx.Query(ctx, `SELECT u.display_name FROM timer_participants tp JOIN users u ON u.id = tp.user_id WHERE tp.timer_run_id = $1 ORDER BY tp.joined_at`, timer.ID)
	if err != nil {
		return nil, false, err
	}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return nil, false, err
		}
		timer.Participants = append(timer.Participants, name)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	if len(timer.Participants) == 0 {
		if _, err = tx.Exec(ctx, `UPDATE timer_runs SET phase = 'ended', ended_at = now() WHERE id = $1`, timer.ID); err != nil {
			return nil, false, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, false, err
		}
		return nil, true, nil
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return &timer, timer.Transitioned, nil
}

func requestedRoomFocusMinutes(r *http.Request, fallback int) (int, error) {
	value := strings.TrimSpace(r.FormValue("focus_minutes"))
	if value == "" {
		value = strings.TrimSpace(r.FormValue("minutes"))
	}
	if value == "" {
		return fallback, nil
	}
	minutes, err := strconv.Atoi(value)
	if err != nil || minutes < 5 || minutes > 180 {
		return 0, errors.New("focus minutes must be a whole number from 5 to 180")
	}
	return minutes, nil
}

// applyRoomSettings persists a room's timer configuration. Shared by the web
// "settings" room action and the Discord /junkie config command so the two
// surfaces can't drift.
func (a *app) applyRoomSettings(ctx context.Context, roomID string, focusMinutes, breakMinutes, autoSessions int, autoRoll bool) error {
	_, err := a.db.Exec(ctx, `UPDATE rooms SET focus_minutes = $1, break_minutes = $2, auto_sessions = $3, auto_roll = $4, updated_at = now() WHERE id = $5`,
		focusMinutes, breakMinutes, autoSessions, autoRoll, roomID)
	return err
}

// joinOutcome tells the caller what a join attempt actually did, so both
// surfaces (web and Discord) can word their feedback honestly instead of
// claiming "you're in" while focus is running.
type joinOutcome int

const (
	joinedNow         joinOutcome = iota // participant of the current lobby/break
	joinedQueuedStart                    // no active run; in when the next one starts
	joinedQueuedBreak                    // focus running; in when the break starts
	joinedAlready                        // was already a participant
)

// joinTimer joins userID to rm's active run, or — Forest-style — parks them
// in the room's waiting list when there's nothing joinable right now (no run,
// or mid-focus). Waiters are absorbed as participants by startRoomTimer and
// by the focus->break transition in normalizeTimer, so joining never requires
// catching the lobby countdown live. Shared by the web "timer-join" room
// action and the Discord /junkie join command (and its Join button).
func (a *app) joinTimer(ctx context.Context, rm room, userID string) (*timerRun, joinOutcome, error) {
	timer, _, err := a.normalizeTimer(ctx, rm.ID, userID)
	if err != nil {
		return nil, joinedNow, err
	}
	wait := func(outcome joinOutcome) (*timerRun, joinOutcome, error) {
		_, err := a.db.Exec(ctx, `INSERT INTO room_waiting (room_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, rm.ID, userID)
		return timer, outcome, err
	}
	if timer == nil {
		return wait(joinedQueuedStart)
	}
	if timer.Participant {
		return timer, joinedAlready, nil
	}
	if timer.Phase == "focus" {
		return wait(joinedQueuedBreak)
	}
	if _, err := a.db.Exec(ctx, `INSERT INTO timer_participants (timer_run_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, timer.ID, userID); err != nil {
		return timer, joinedNow, err
	}
	_, _ = a.db.Exec(ctx, `DELETE FROM room_waiting WHERE room_id = $1 AND user_id = $2`, rm.ID, userID)
	return timer, joinedNow, nil
}

// leaveTimer removes userID from rm's active run (ending the run if that was
// the last participant) and from the waiting list, so it also cancels a
// queued join. Shared by the web "timer-leave" room action and the Discord
// /junkie leave command.
func (a *app) leaveTimer(ctx context.Context, rm room, userID string) (ended bool, err error) {
	_, _ = a.db.Exec(ctx, `DELETE FROM room_waiting WHERE room_id = $1 AND user_id = $2`, rm.ID, userID)
	timer, _, err := a.normalizeTimer(ctx, rm.ID, userID)
	if err != nil || timer == nil || !timer.Participant {
		return false, err
	}
	if _, err := a.db.Exec(ctx, `DELETE FROM timer_participants WHERE timer_run_id = $1 AND user_id = $2`, timer.ID, userID); err != nil {
		return false, err
	}
	return a.endTimerIfNoParticipants(ctx, timer.ID), nil
}

// roomWaiting reports whether userID is parked in rm's waiting list.
func (a *app) roomWaiting(ctx context.Context, roomID, userID string) bool {
	var waiting bool
	err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM room_waiting WHERE room_id = $1 AND user_id = $2)`, roomID, userID).Scan(&waiting)
	return err == nil && waiting
}

func (a *app) startRoomTimer(ctx context.Context, rm room, userID string, focusMinutes int) (bool, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	// Serialize starts for this room. The partial unique index remains the final
	// invariant even if another code path attempts an insert.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, rm.ID); err != nil {
		return false, err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM timer_runs
			WHERE room_id = $1 AND ended_at IS NULL AND phase <> 'ended'
		)`, rm.ID).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, tx.Commit(ctx)
	}

	ends := time.Now().Add(30 * time.Second)
	var runID string
	if err = tx.QueryRow(ctx, `
		INSERT INTO timer_runs (room_id, host_user_id, phase, focus_minutes, break_minutes, total_sessions, phase_ends_at)
		VALUES ($1, $2, 'lobby', $3, $4, $5, $6)
		RETURNING id`, rm.ID, userID, focusMinutes, rm.BreakMinutes, rm.AutoSessions, ends).Scan(&runID); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO timer_participants (timer_run_id, user_id) VALUES ($1, $2)`, runID, userID); err != nil {
		return false, err
	}
	// Anyone parked in the waiting list is in from the first session, no
	// need to catch the lobby countdown live.
	if _, err = tx.Exec(ctx, `
		INSERT INTO timer_participants (timer_run_id, user_id)
		SELECT $1, user_id FROM room_waiting WHERE room_id = $2
		ON CONFLICT DO NOTHING`, runID, rm.ID); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM room_waiting WHERE room_id = $1`, rm.ID); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// startRoomTimerAndSchedule starts rm's timer (if none is active), broadcasts
// the lobby countdown over the web hub, and schedules the lobby->focus
// transition. Shared by the web "timer-start" room action and the Discord
// /junkie start command, which additionally uses the returned timer to build
// its own reply rather than relying on the hub broadcast.
func (a *app) startRoomTimerAndSchedule(ctx context.Context, rm room, userID string, focusMinutes int, starterName string) (*timerRun, bool, error) {
	created, err := a.startRoomTimer(ctx, rm, userID, focusMinutes)
	if err != nil || !created {
		return nil, created, err
	}
	timer, err := a.activeTimer(ctx, rm.ID, userID)
	if err != nil || timer == nil {
		return timer, created, err
	}
	a.hub.broadcastJSON(rm.Code, map[string]interface{}{
		"type": "timer-lobby", "roomCode": rm.Code, "roomName": rm.Name,
		"runId": timer.ID, "lobbyDeadline": timer.PhaseEndsAt.UTC().Format(time.RFC3339Nano),
		"starterName": starterName, "starterUserId": userID,
	})
	a.notifyDiscord(rm, timer)
	a.scheduleLobbyDeadline(rm, userID, timer.PhaseEndsAt)
	return timer, created, nil
}

func (a *app) scheduleLobbyDeadline(rm room, userID string, deadline time.Time) {
	delay := time.Until(deadline)
	if delay < 0 {
		delay = 0
	}
	time.AfterFunc(delay, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if timer, transitioned, err := a.normalizeTimer(ctx, rm.ID, userID); err != nil {
			log.Printf("normalize room lobby %s: %v", rm.Code, err)
		} else if transitioned {
			a.broadcastTimerPhase(rm, timer)
		}
	})
}

func (a *app) pauseRoomBreak(ctx context.Context, roomID, userID string) (*timerRun, bool, error) {
	if _, _, err := a.normalizeTimer(ctx, roomID, userID); err != nil {
		return nil, false, err
	}
	var runID string
	err := a.db.QueryRow(ctx, `
		UPDATE timer_runs
		SET paused_at = now(),
			paused_remaining_seconds = GREATEST(0, CEIL(EXTRACT(EPOCH FROM (phase_ends_at - now())))::integer)
		WHERE room_id = $1 AND ended_at IS NULL AND phase = 'break' AND paused_at IS NULL
		RETURNING id`, roomID).Scan(&runID)
	if errors.Is(err, pgx.ErrNoRows) {
		timer, readErr := a.activeTimer(ctx, roomID, userID)
		return timer, false, readErr
	}
	if err != nil {
		return nil, false, err
	}
	timer, err := a.activeTimer(ctx, roomID, userID)
	return timer, true, err
}

func (a *app) resumeRoomBreak(ctx context.Context, roomID, userID string) (*timerRun, bool, error) {
	var runID string
	err := a.db.QueryRow(ctx, `
		UPDATE timer_runs
		SET phase_started_at = now(),
			phase_ends_at = now() + make_interval(secs => paused_remaining_seconds),
			paused_at = NULL,
			paused_remaining_seconds = NULL
		WHERE room_id = $1 AND ended_at IS NULL AND phase = 'break' AND paused_at IS NOT NULL
		RETURNING id`, roomID).Scan(&runID)
	if errors.Is(err, pgx.ErrNoRows) {
		timer, readErr := a.activeTimer(ctx, roomID, userID)
		return timer, false, readErr
	}
	if err != nil {
		return nil, false, err
	}
	timer, err := a.activeTimer(ctx, roomID, userID)
	return timer, true, err
}

// skipRoomBreak ends the current break early and advances to the next focus
// session (same transition path as a natural break expiry). Available to any
// room member, matching pause/resume.
func (a *app) skipRoomBreak(ctx context.Context, roomID, userID string) (*timerRun, bool, error) {
	timer, _, err := a.normalizeTimer(ctx, roomID, userID)
	if err != nil {
		return nil, false, err
	}
	if timer == nil || timer.Phase != "break" {
		return timer, false, nil
	}
	// Force the break window closed (including while paused) so normalizeTimer
	// performs the shared break → focus / end transition.
	if _, err = a.db.Exec(ctx, `
		UPDATE timer_runs
		SET paused_at = NULL,
			paused_remaining_seconds = NULL,
			phase_ends_at = now() - interval '1 second'
		WHERE id = $1 AND ended_at IS NULL AND phase = 'break'`, timer.ID); err != nil {
		return nil, false, err
	}
	timer, transitioned, err := a.normalizeTimer(ctx, roomID, userID)
	if err != nil {
		return nil, false, err
	}
	return timer, transitioned, nil
}

func soloBreakMinutes(focusMinutes int) int {
	switch {
	case focusMinutes < 30:
		return 5
	case focusMinutes < 120:
		return 10
	case focusMinutes < 180:
		return 20
	default:
		return 30
	}
}

func soloBreakPending(t *timerRun) bool {
	return t.Phase == "break" && !t.PhaseEndsAt.After(t.PhaseStartedAt)
}

func (a *app) normalizeSoloTimer(ctx context.Context, userID string) (*timerRun, error) {
	timer, err := a.activeSoloTimer(ctx, userID)
	if err != nil || timer == nil {
		return timer, err
	}
	now := time.Now()
	if timer.Phase == "focus" && now.After(timer.PhaseEndsAt) {
		breakMins := soloBreakMinutes(timer.FocusMinutes)
		_, _ = a.db.Exec(ctx, `
			INSERT INTO activity (user_id, activity_date, focus_minutes)
			VALUES ($1, CURRENT_DATE, $2)
			ON CONFLICT (user_id, activity_date)
			DO UPDATE SET focus_minutes = activity.focus_minutes + EXCLUDED.focus_minutes`, userID, timer.FocusMinutes)
		timer.Phase = "break"
		timer.BreakMinutes = breakMins
		timer.PhaseStartedAt = now
		timer.PhaseEndsAt = now
		_, _ = a.db.Exec(ctx, `UPDATE timer_runs SET phase = 'break', break_minutes = $1, phase_started_at = $2, phase_ends_at = $2 WHERE id = $3`, breakMins, now, timer.ID)
	} else if timer.Phase == "break" && timer.PhaseEndsAt.After(timer.PhaseStartedAt) && now.After(timer.PhaseEndsAt) {
		_, _ = a.db.Exec(ctx, `UPDATE timer_runs SET phase = 'ended', ended_at = now() WHERE id = $1`, timer.ID)
		return nil, nil
	}
	timer.Participant = true
	timer.Participants = []string{"You"}
	return timer, nil
}

func (a *app) activeSoloTimer(ctx context.Context, userID string) (*timerRun, error) {
	var t timerRun
	err := a.db.QueryRow(ctx, `
		SELECT id, phase, focus_minutes, break_minutes, total_sessions, current_session, phase_started_at, phase_ends_at
		FROM timer_runs
		WHERE user_id = $1 AND room_id IS NULL AND ended_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1`, userID).Scan(&t.ID, &t.Phase, &t.FocusMinutes, &t.BreakMinutes, &t.TotalSessions, &t.CurrentSession, &t.PhaseStartedAt, &t.PhaseEndsAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (a *app) activeTimer(ctx context.Context, roomID, userID string) (*timerRun, error) {
	var t timerRun
	err := a.db.QueryRow(ctx, `
		SELECT tr.id, tr.phase, tr.focus_minutes, tr.break_minutes, tr.total_sessions, tr.current_session, tr.phase_started_at, tr.phase_ends_at,
			tr.paused_at, tr.paused_remaining_seconds,
			EXISTS (SELECT 1 FROM timer_participants tp WHERE tp.timer_run_id = tr.id AND tp.user_id = $2)
		FROM timer_runs tr
		WHERE tr.room_id = $1 AND tr.ended_at IS NULL AND tr.phase <> 'ended'
		ORDER BY tr.created_at DESC
		LIMIT 1`, roomID, userID).Scan(&t.ID, &t.Phase, &t.FocusMinutes, &t.BreakMinutes, &t.TotalSessions, &t.CurrentSession, &t.PhaseStartedAt, &t.PhaseEndsAt, &t.PausedAt, &t.PausedRemainingSeconds, &t.Participant)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.Participants, _ = a.timerParticipants(ctx, t.ID)
	return &t, nil
}

func (a *app) endTimerIfNoParticipants(ctx context.Context, runID string) bool {
	var count int
	if err := a.db.QueryRow(ctx, `SELECT COUNT(*) FROM timer_participants WHERE timer_run_id = $1`, runID).Scan(&count); err != nil || count > 0 {
		return false
	}
	_, _ = a.db.Exec(ctx, `UPDATE timer_runs SET phase = 'ended', ended_at = now() WHERE id = $1 AND ended_at IS NULL`, runID)
	return true
}

func (a *app) timerParticipants(ctx context.Context, runID string) ([]string, error) {
	rows, err := a.db.Query(ctx, `SELECT u.display_name FROM timer_participants tp JOIN users u ON u.id = tp.user_id WHERE tp.timer_run_id = $1 ORDER BY tp.joined_at`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			names = append(names, name)
		}
	}
	return names, nil
}

func (a *app) authPageData(r *http.Request, signup bool, next, errMsg string) pageData {
	title := "Sign in"
	if signup {
		title = "Sign up"
	}
	data := pageData{Title: title, Next: next, Error: errMsg, AuthSignup: signup, AuthBanner: a.authBanner(r, next, signup)}
	if u, ok := a.currentUser(r); ok {
		data.User = u
	}
	return data
}

func (a *app) authBanner(r *http.Request, next string, signup bool) string {
	if next == "" || next == "/" {
		return ""
	}
	if u, ok := url.Parse(next); ok == nil && strings.HasPrefix(u.Path, "/join/confirm") {
		if rm, found := a.findRoom(r.Context(), u.Query().Get("code")); found {
			if signup {
				return "Create an account to join " + rm.Name
			}
			return "Sign in to join " + rm.Name
		}
	}
	if signup {
		return ""
	}
	return "Sign in to continue"
}

func (a *app) roomMemberCount(ctx context.Context, roomID string) (int, error) {
	var count int
	err := a.db.QueryRow(ctx, `SELECT COUNT(*) FROM room_members WHERE room_id = $1`, roomID).Scan(&count)
	return count, err
}

// roomMemberUsers lists the users belonging to a room, alphabetically by
// display name.
func (a *app) roomMemberUsers(ctx context.Context, roomID string) ([]user, error) {
	rows, err := a.db.Query(ctx, `
		SELECT u.id, u.username, u.display_name, u.avatar IS NOT NULL,
			COALESCE(EXTRACT(EPOCH FROM u.avatar_updated_at), 0)::bigint
		FROM room_members rm
		JOIN users u ON u.id = rm.user_id
		WHERE rm.room_id = $1
		ORDER BY u.display_name`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []user
	for rows.Next() {
		var m user
		if err := rows.Scan(&m.ID, &m.Username, &m.DisplayName, &m.HasAvatar, &m.AvatarVersion); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// roomMembersPage serves /r/{code}/members: the room roster. Each member's
// profile link is gated the same way publicProfilePage is -- only the
// viewer's own row or an existing connection is clickable, everyone else is
// name and avatar only.
func (a *app) roomMembersPage(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	rm, ok := a.findRoom(r.Context(), r.PathValue("code"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !a.isRoomMember(r.Context(), rm.ID, u.ID) {
		http.Error(w, "room membership required", http.StatusForbidden)
		return
	}
	members, _ := a.roomMemberUsers(r.Context(), rm.ID)
	views := make([]roomMemberView, len(members))
	for i, m := range members {
		views[i] = roomMemberView{
			Member:    m,
			Self:      m.ID == u.ID,
			Connected: m.ID == u.ID || a.areConnected(r.Context(), u.ID, m.ID),
		}
	}
	a.render(w, "room-members", pageData{Title: "Room members", User: u, Room: rm, RoomMembers: views})
}

func (a *app) currentUser(r *http.Request) (user, bool) {
	if a.currentUserOverride != nil {
		return a.currentUserOverride(r)
	}
	cookie, err := r.Cookie("junkie_session")
	if err != nil {
		return user{}, false
	}
	var u user
	err = a.db.QueryRow(r.Context(), `
		SELECT u.id, u.username, u.display_name, u.role, u.avatar IS NOT NULL,
			COALESCE(EXTRACT(EPOCH FROM u.avatar_updated_at), 0)::bigint
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = $1 AND s.expires_at > now()`, hashToken(cookie.Value)).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.HasAvatar, &u.AvatarVersion)
	return u, err == nil
}

func (a *app) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.currentUser(r); !ok {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func displayNameFromUsername(username string) string {
	if username == "" {
		return ""
	}
	return strings.ToUpper(username[:1]) + username[1:]
}

func safeNext(raw string) string {
	// Reject anything but a local path: browsers treat both "//host" and
	// "/\host" as protocol-relative URLs, which would be open redirects.
	if raw == "" || !strings.HasPrefix(raw, "/") ||
		strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "/\\") {
		return "/"
	}
	return raw
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (a *app) createSession(w http.ResponseWriter, r *http.Request, userID string) {
	token := randomHex(32)
	expires := time.Now().Add(30 * 24 * time.Hour)
	_, _ = a.db.Exec(r.Context(), `INSERT INTO sessions (token, user_id, expires_at) VALUES ($1, $2, $3)`, hashToken(token), userID, expires)
	http.SetCookie(w, &http.Cookie{Name: "junkie_session", Value: token, Path: "/", Expires: expires, HttpOnly: true, Secure: isSecureRequest(r), SameSite: http.SameSiteLaxMode})
}

func (a *app) findRoom(ctx context.Context, code string) (room, bool) {
	code = normalizeRoomCode(code)
	if code == "" {
		return room{}, false
	}
	var rm room
	err := a.db.QueryRow(ctx, `SELECT id, code, name, creator_id, focus_minutes, break_minutes, auto_sessions, auto_roll FROM rooms WHERE UPPER(code) = $1`, code).Scan(&rm.ID, &rm.Code, &rm.Name, &rm.CreatorID, &rm.FocusMinutes, &rm.BreakMinutes, &rm.AutoSessions, &rm.AutoRoll)
	return rm, err == nil
}

func (a *app) isRoomMember(ctx context.Context, roomID, userID string) bool {
	var exists bool
	err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM room_members WHERE room_id = $1 AND user_id = $2)`, roomID, userID).Scan(&exists)
	return err == nil && exists
}

func (a *app) addRoomMember(ctx context.Context, roomID, userID string) {
	_, _ = a.db.Exec(ctx, `INSERT INTO room_members (room_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, roomID, userID)
}

func (a *app) roomsForUser(ctx context.Context, userID string) ([]room, error) {
	rows, err := a.db.Query(ctx, `SELECT r.id, r.code, r.name, r.creator_id, r.focus_minutes, r.break_minutes, r.auto_sessions, r.auto_roll FROM room_members rm JOIN rooms r ON r.id = rm.room_id WHERE rm.user_id = $1 ORDER BY r.updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rooms []room
	for rows.Next() {
		var rm room
		if rows.Scan(&rm.ID, &rm.Code, &rm.Name, &rm.CreatorID, &rm.FocusMinutes, &rm.BreakMinutes, &rm.AutoSessions, &rm.AutoRoll) == nil {
			rooms = append(rooms, rm)
		}
	}
	return rooms, nil
}

func (a *app) personalTodos(ctx context.Context, userID string) ([]todo, error) {
	rows, err := a.db.Query(ctx, `SELECT id, text, done, removed, created_at FROM todos WHERE user_id = $1 AND room_id IS NULL ORDER BY removed, done, created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var todos []todo
	for rows.Next() {
		var t todo
		if rows.Scan(&t.ID, &t.Text, &t.Done, &t.Removed, &t.CreatedAt) == nil {
			todos = append(todos, t)
		}
	}
	return todos, nil
}

func (a *app) roomTodos(ctx context.Context, roomID string) ([]todo, error) {
	rows, err := a.db.Query(ctx, `SELECT t.id, t.text, t.done, t.removed, u.display_name, t.user_id, t.created_at FROM todos t JOIN users u ON u.id = t.user_id WHERE t.room_id = $1 ORDER BY t.removed, t.done, t.created_at DESC`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var todos []todo
	for rows.Next() {
		var t todo
		if rows.Scan(&t.ID, &t.Text, &t.Done, &t.Removed, &t.DisplayName, &t.UserID, &t.CreatedAt) == nil {
			todos = append(todos, t)
		}
	}
	return todos, nil
}

func (a *app) activity(ctx context.Context, userID string) (heatmapData, error) {
	rows, err := a.db.Query(ctx, `
		SELECT d::date, COALESCE(a.focus_minutes, 0)
		FROM generate_series(CURRENT_DATE - INTERVAL '364 days', CURRENT_DATE, INTERVAL '1 day') d
		LEFT JOIN activity a ON a.user_id = $1 AND a.activity_date = d::date
		ORDER BY d`, userID)
	if err != nil {
		return heatmapData{}, err
	}
	defer rows.Close()
	var days []activityDay
	var dates []time.Time
	for rows.Next() {
		var date time.Time
		var minutes int
		if rows.Scan(&date, &minutes) == nil {
			dates = append(dates, date)
			days = append(days, activityDay{Date: date.Format("Jan 2"), Minutes: minutes, Level: heatLevel(minutes)})
		}
	}
	return buildYearHeatmap(days, dates), nil
}

func buildYearHeatmap(days []activityDay, dates []time.Time) heatmapData {
	if len(dates) == 0 {
		return heatmapData{}
	}

	firstDate := dates[0]
	lastDate := dates[len(dates)-1]
	gridStart := firstDate.AddDate(0, 0, -int(firstDate.Weekday()))

	totalMinutes := 0
	for _, day := range days {
		totalMinutes += day.Minutes
	}

	dayByKey := make(map[string]activityDay, len(days))
	for i, day := range days {
		dayByKey[dates[i].Format("2006-01-02")] = day
	}

	totalDays := int(lastDate.Sub(gridStart).Hours()/24) + 1
	numWeeks := (totalDays + 6) / 7

	cells := make([]activityDay, numWeeks*7)
	for i := range cells {
		cells[i] = activityDay{Empty: true}
	}

	for week := 0; week < numWeeks; week++ {
		for dow := 0; dow < 7; dow++ {
			d := gridStart.AddDate(0, 0, week*7+dow)
			if d.Before(firstDate) || d.After(lastDate) {
				continue
			}
			idx := week*7 + dow
			if day, ok := dayByKey[d.Format("2006-01-02")]; ok {
				cells[idx] = day
			} else {
				cells[idx] = activityDay{Date: d.Format("Jan 2"), Minutes: 0, Level: 0}
			}
		}
	}

	var months []activityMonth
	labeled := map[string]bool{}
	for week := 0; week < numWeeks; week++ {
		for dow := 0; dow < 7; dow++ {
			d := gridStart.AddDate(0, 0, week*7+dow)
			if d.Before(firstDate) || d.After(lastDate) {
				continue
			}
			if d.Day() == 1 {
				key := d.Format("2006-01")
				if !labeled[key] {
					months = append(months, activityMonth{Label: d.Format("Jan"), Col: week})
					labeled[key] = true
				}
				break
			}
		}
	}

	return heatmapData{
		Cells:        cells,
		Months:       months,
		Weeks:        numWeeks,
		TotalMinutes: totalMinutes,
	}
}

func (a *app) render(w http.ResponseWriter, name string, data pageData) {
	a.renderStatus(w, http.StatusOK, name, data)
}

// renderStatus buffers template execution so a failure can be logged and
// reported as a clean 500 instead of leaking template internals to the
// client mid-response.
func (a *app) renderStatus(w http.ResponseWriter, status int, name string, data pageData) {
	var buf bytes.Buffer
	if err := a.templates.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

func (a *app) css(w http.ResponseWriter, r *http.Request) {
	serveStatic(w, r, "text/css; charset=utf-8", []byte(appCSS))
}

// serveStatic writes an in-memory asset with a content-derived ETag and a
// no-cache/must-revalidate policy. Browsers keep the copy but must revalidate
// on every load, so a changed asset (e.g. updated CSS) is picked up
// immediately instead of being served stale from the HTTP cache.
func serveStatic(w http.ResponseWriter, r *http.Request, contentType string, data []byte) {
	sum := sha256.Sum256(data)
	etag := `"` + hex.EncodeToString(sum[:]) + `"`
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	w.Header().Set("ETag", etag)
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(data)
}

func (h *hub) join(code string, c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[code] == nil {
		h.rooms[code] = map[*websocket.Conn]struct{}{}
	}
	h.rooms[code][c] = struct{}{}
}

func (h *hub) leave(code string, c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.rooms[code], c)
}

func (h *hub) broadcast(code, msg string) {
	h.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(h.rooms[code]))
	for c := range h.rooms[code] {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, c := range conns {
		_ = c.Write(ctx, websocket.MessageText, []byte(msg))
	}
}

func (h *hub) broadcastJSON(code string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	h.broadcast(code, string(data))
}

func normalizeRoomCode(raw string) string {
	raw = strings.TrimSpace(raw)
	if idx := strings.Index(raw, "/r/"); idx >= 0 {
		raw = raw[idx+3:]
	}
	if i := strings.IndexAny(raw, "/?#"); i >= 0 {
		raw = raw[:i]
	}
	return strings.ToUpper(raw)
}

func randomCode() string {
	return fmt.Sprintf("%s-%s", randomHex(2), randomHex(2))
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b))
}

func clampInt(raw string, min, max, fallback int) int {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func heatLevel(minutes int) int {
	switch {
	case minutes >= 180:
		return 4
	case minutes >= 90:
		return 3
	case minutes >= 30:
		return 2
	case minutes > 0:
		return 1
	default:
		return 0
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
