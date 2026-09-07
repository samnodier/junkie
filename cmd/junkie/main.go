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
	"sync/atomic"
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
	hub                 *hub
	limiter             *rateLimiter
	currentUserOverride func(*http.Request) (user, bool)
	// discord is nil until the gateway connects, and is published from the
	// background retry goroutine while request handlers read it, so it's
	// atomic rather than a plain field.
	discord atomic.Pointer[discordBot]

	// wakeups dedupes the phase-end wakeups (schedulePhaseWakeup) so
	// overlapping notify calls don't stack timers for the same room+phase.
	wakeupMu sync.Mutex
	wakeups  map[string]string

	// discordLive holds, per room, the text its Discord live message was last
	// sent with, so refreshDiscordLive edits only when the render actually
	// changed instead of every tick.
	discordLiveMu sync.Mutex
	discordLive   map[string]string
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
	// ConfirmedSession is only populated on timer-participant queries: the
	// highest session number this participant has claimed a seat in.
	ConfirmedSession int
	// Timezone is the IANA zone the user's browser reported, empty until one
	// has been. Deliberately not part of apiUser: it is the viewer's own
	// setting, and apiUser is what participant lists serialize to everyone
	// else in the room.
	Timezone string
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
	// Ephemeral marks a temporary Forest-style focus room: joined by link,
	// shown on the stripped /f/{code} screen, and deleted the moment its run
	// completes or empties out. Kept out of the room list and the room cap.
	Ephemeral bool
	// RequireCheckin makes every participant confirm during each break that
	// they're still there; the break->focus transition drops anyone who
	// didn't. Nobody is exempt after session 1 — presence is proven by
	// actions (starting, joining, checking in), never by role.
	RequireCheckin bool

	// PendingOwnerID and OwnershipTransferAt hold an ownership transfer that
	// has been started but not yet settled. Both are zero for the vast
	// majority of rooms; settleOwnershipTransfer applies them on read.
	PendingOwnerID      string
	OwnershipTransferAt time.Time
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
	// HasAvatar and AvatarVersion mirror the author's profile so a read-only
	// row can decide whether to request /avatar/{id} at all. Personal todos
	// leave them zero: those rows render a checkbox, not an author.
	HasAvatar     bool
	AvatarVersion int64
}

type roomTodosSplit struct {
	Mine   []todo
	Others []todo
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
	Participants           []user
	Transitioned           bool
	// ViewerConfirmedSession is the viewer's confirmed_session row value
	// (0 when they aren't a participant).
	ViewerConfirmedSession int
	// Kicked lists user IDs dropped at a break->focus transition for not
	// checking in, so the caller can broadcast who was removed.
	Kicked []string
}

// BreakPending reports a break that arrived with auto-roll off and hasn't
// been started yet: it was paused at the exact moment it began. A break
// paused manually mid-run has a later paused_at than phase_started_at.
func (t *timerRun) BreakPending() bool {
	return t != nil && t.Phase == "break" && t.PausedAt != nil && t.PausedAt.Equal(t.PhaseStartedAt)
}

// stalePauseTimeout is how long a paused break may sit untouched before the
// run counts as abandoned — the pauser left, or an auto-breaks-off room's
// pending break was never started — and is ended so the room is free for a
// fresh run instead of blocking on a resume that's never coming.
const stalePauseTimeout = time.Hour

// maxPhaseAdvances bounds one normalizeTimer call's phase transitions. A run
// tops out at 12 sessions and each pass through the loop moves it forward, so
// real traffic never gets near this; it is a backstop against a future state
// that fails to advance, which would otherwise spin inside a transaction.
const maxPhaseAdvances = 64

// PauseExpired reports a pause that has outlived stalePauseTimeout.
func (t *timerRun) PauseExpired(now time.Time) bool {
	return t != nil && t.PausedAt != nil && now.Sub(*t.PausedAt) >= stalePauseTimeout
}

// publicProfileView is what a connection is allowed to see of a user: the
// identity basics and the focus heatmap.
type publicProfileView struct {
	ProfileUser          user
	Activity             []activityDay
	ActivityMonths       []activityMonth
	ActivityWeeks        int
	ActivityTotalMinutes int
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
		db:      db,
		hub:     &hub{rooms: map[string]map[*websocket.Conn]struct{}{}},
		limiter: newRateLimiter(),
	}
	if err := a.migrate(ctx); err != nil {
		log.Fatal(err)
	}
	if err := a.bootstrapOwner(ctx, os.Getenv("JUNKIE_OWNER_USERNAME")); err != nil {
		log.Fatal(err)
	}

	// Opt-in: no-op unless DISCORD_BOT_TOKEN is set, so existing deployments
	// are unaffected. A misconfiguration (token without application ID) is a
	// deploy mistake worth failing on, but the *connection* is made in the
	// background: when Discord is down or rate-limiting us, junkie serves the
	// web app without the bot and picks the gateway up when it recovers.
	discordBot, err := newDiscordBot(a)
	if err != nil {
		log.Fatal(err)
	}
	if discordBot != nil {
		discordBot.startWithRetry()
		defer discordBot.Close()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /assets/icon.svg", a.serveStaticAsset("icon.svg", "image/svg+xml"))
	mux.HandleFunc("GET /assets/icon-192.png", a.serveStaticAsset("icon-192.png", "image/png"))
	mux.HandleFunc("GET /assets/icon-512.png", a.serveStaticAsset("icon-512.png", "image/png"))
	mux.HandleFunc("GET /assets/icon-maskable-512.png", a.serveStaticAsset("icon-maskable-512.png", "image/png"))
	mux.HandleFunc("GET /assets/apple-touch-icon.png", a.serveStaticAsset("apple-touch-icon.png", "image/png"))
	mux.HandleFunc("GET /assets/og-image.png", a.serveStaticAsset("og-image.png", "image/png"))
	// Phase-end notification sounds plus the manifest listing them; see sounds.go.
	mux.HandleFunc("GET /assets/sounds/{name}", a.soundAsset)
	// Browsers and link scrapers request /favicon.ico by convention even when
	// the page declares an SVG icon; without this it fell through to the
	// /{username} route and 404d.
	mux.HandleFunc("GET /favicon.ico", a.serveStaticAsset("favicon.ico", "image/x-icon"))
	mux.HandleFunc("GET /robots.txt", a.serveStaticAsset("robots.txt", "text/plain; charset=utf-8"))
	mux.HandleFunc("GET /manifest.webmanifest", a.serveStaticAsset("manifest.webmanifest", "application/manifest+json"))
	mux.HandleFunc("GET /sw.js", a.serveStaticAsset("sw.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("GET /offline", a.serveStaticAsset("offline.html", "text/html; charset=utf-8"))
	mux.HandleFunc("GET /.well-known/assetlinks.json", a.serveStaticAsset("assetlinks.json", "application/json"))
	mux.HandleFunc("GET /.well-known/security.txt", a.serveStaticAsset("security.txt", "text/plain; charset=utf-8"))
	mux.HandleFunc("GET /app/", a.spaAsset)
	mux.HandleFunc("GET /api/me", a.apiMe)
	mux.HandleFunc("GET /api/auth-context", a.apiAuthContext)
	mux.HandleFunc("POST /api/login", a.apiLogin)
	mux.HandleFunc("POST /api/signup", a.apiSignup)
	mux.HandleFunc("GET /__vue", a.spaPage)
	mux.HandleFunc("GET /healthz", a.healthz)
	mux.HandleFunc("GET /", a.spaPage)
	mux.HandleFunc("GET /signup", a.signupForm)
	mux.HandleFunc("GET /login", a.spaPage)
	mux.HandleFunc("GET /reset-password/{token}", a.spaPage)
	mux.HandleFunc("GET /api/password-reset-context/{token}", a.apiPasswordResetContext)
	mux.HandleFunc("POST /api/password-reset/{token}", a.apiPasswordReset)
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("GET /dashboard", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.RawQuery
		dest := "/"
		if q != "" {
			dest += "?" + q
		}
		http.Redirect(w, r, dest, http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /profile", a.spaPage)
	mux.HandleFunc("POST /api/timezone", a.requireAuth(a.apiSetTimezone))
	mux.HandleFunc("GET /api/profile", a.requireAuth(a.apiProfile))
	mux.HandleFunc("POST /profile/password", a.requireAuth(a.changePassword))
	mux.HandleFunc("POST /profile/delete", a.requireAuth(a.deleteAccount))
	mux.HandleFunc("POST /profile/username", a.requireAuth(a.changeUsername))
	mux.HandleFunc("POST /profile/avatar", a.requireAuth(a.uploadAvatar))
	mux.HandleFunc("POST /profile/avatar/remove", a.requireAuth(a.removeAvatar))
	mux.HandleFunc("GET /avatar/{id}", a.requireAuth(a.serveAvatar))
	mux.HandleFunc("POST /profile/connect-link", a.requireAuth(a.createConnectLink))
	mux.HandleFunc("GET /connections", a.spaPage)
	mux.HandleFunc("GET /api/connections", a.requireAuth(a.apiConnections))
	mux.HandleFunc("GET /api/desk", a.requireAuth(a.apiDesk))
	mux.HandleFunc("GET /api/rooms", a.requireAuth(a.apiRooms))
	mux.HandleFunc("GET /api/room/{code}", a.requireAuth(a.apiRoom))
	mux.HandleFunc("GET /api/room/{code}/members", a.requireAuth(a.apiRoomMembers))
	mux.HandleFunc("GET /api/room/{code}/embed", a.apiFocusEmbed)
	mux.HandleFunc("GET /api/public-profile/{username}", a.requireAuth(a.apiPublicProfile))
	mux.HandleFunc("GET /privacy", a.spaPage)
	mux.HandleFunc("GET /terms", a.spaPage)
	// The page is public shell (the Vue guard bounces guests to
	// /login?next= exactly like requireAuth did); the data API keeps the auth.
	mux.HandleFunc("GET /discord/link/{token}", a.spaPage)
	mux.HandleFunc("GET /api/discord-link-context/{token}", a.requireAuth(a.apiDiscordLinkContext))
	mux.HandleFunc("POST /discord/link/{token}", a.requireAuth(a.discordLinkConfirm))
	mux.HandleFunc("POST /profile/discord/unlink", a.requireAuth(a.discordUnlink))
	mux.HandleFunc("POST /connect/{username}", a.requireAuth(a.connectConfirmPost))
	// Exact routes above win over this single-segment pattern; usernames
	// that would collide with them are reserved at signup.
	mux.HandleFunc("GET /{username}", a.publicProfilePage)
	mux.HandleFunc("POST /todos", a.requireAuth(a.createPersonalTodo))
	// Private focus screen. Public shell like the pages above; the Vue guard
	// bounces guests to /login?next=/solo and /api/desk keeps the auth.
	mux.HandleFunc("GET /solo", a.spaPage)
	mux.HandleFunc("POST /solo/start", a.requireAuth(a.startSoloTimer))
	mux.HandleFunc("POST /solo/cancel", a.requireAuth(a.cancelSoloTimer))
	mux.HandleFunc("POST /solo/break/start", a.requireAuth(a.startSoloBreak))
	mux.HandleFunc("POST /solo/break/skip", a.requireAuth(a.skipSoloBreak))
	mux.HandleFunc("POST /todo/", a.requireAuth(a.todoAction))
	mux.HandleFunc("POST /rooms", a.requireAuth(a.createRoom))
	mux.HandleFunc("POST /rooms/join", a.requireAuth(a.joinRoom))
	mux.HandleFunc("POST /rooms/join-intent", a.joinRoomIntent)
	mux.HandleFunc("GET /join/confirm", a.spaPage)
	mux.HandleFunc("GET /api/join-context", a.requireAuth(a.apiJoinContext))
	mux.HandleFunc("POST /join/confirm", a.requireAuth(a.joinRoomConfirmPost))
	mux.HandleFunc("GET /r/{code}/members", a.requireAuth(a.roomMembersPage))
	mux.HandleFunc("GET /r/", a.requireAuth(a.roomPage))
	mux.HandleFunc("POST /r/", a.requireAuth(a.roomAction))
	mux.HandleFunc("GET /f/{code}", a.requireAuth(a.focusRoomPage))
	// The OBS overlay twin of the screen above. Deliberately unauthenticated
	// and read-only, and temporary rooms only — see focusembed.go.
	mux.HandleFunc("GET /f/{code}/embed", a.focusEmbedPage)
	mux.HandleFunc("POST /f/{code}/join", a.requireAuth(a.enterFocusRoom))
	mux.HandleFunc("GET /ws/r/", a.requireAuth(a.roomWS))
	mux.HandleFunc("GET /ws/f/", a.focusEmbedWS)
	mux.HandleFunc("GET /ws/me", a.requireAuth(a.userWS))
	mux.HandleFunc("GET /admin", a.requireAdmin(a.spaPage))
	mux.HandleFunc("GET /api/admin", a.requireAuth(a.apiAdmin))
	mux.HandleFunc("POST /admin/users/{id}/role", a.requireAdminMutation(a.adminChangeRole))
	mux.HandleFunc("POST /admin/users/{id}/reset-link", a.requireAdminMutation(a.adminCreateResetLink))
	mux.HandleFunc("POST /admin/rooms/{id}/delete", a.requireAdminMutation(a.adminDeleteRoom))

	go a.sweepExpiredSessions(ctx)
	go a.sweepInactiveTodos(ctx)
	go a.sweepAbandonedEphemeralRooms(ctx)
	go a.sweepStaleRoomWaiting(ctx)
	go a.refreshDiscordLive(ctx)

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
		if _, err := a.db.Exec(ctx, `DELETE FROM password_reset_tokens WHERE expires_at < now()`); err != nil {
			log.Printf("sweep expired reset tokens: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// sweepInactiveTodos permanently deletes todos that have been done or removed
// for over 24 hours, for every user and room. junkie deliberately doesn't
// keep finished work around — the work map records the focus, not the list.
// updated_at is safe to read as "when it became inactive": edits are rejected
// on done/removed todos, so only state flips touch it, and un-completing or
// restoring resets the clock.
func (a *app) sweepInactiveTodos(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		tag, err := a.db.Exec(ctx, `DELETE FROM todos WHERE (done OR removed) AND updated_at < now() - interval '24 hours'`)
		if err != nil {
			log.Printf("sweep inactive todos: %v", err)
		} else if n := tag.RowsAffected(); n > 0 {
			log.Printf("swept %d inactive todos", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *app) migrate(ctx context.Context) error {
	return a.migrateFrom(ctx, "migrations")
}

// migrateFrom applies every .sql file in dir in filename order. Split from
// migrate so tests can point at the repo's migrations from inside the package
// directory; every file re-runs on every boot, so each one has to be
// idempotent.
func (a *app) migrateFrom(ctx context.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		sql, err := os.ReadFile(dir + "/" + entry.Name())
		if err != nil {
			return err
		}
		if _, err := a.db.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("%s: %w", entry.Name(), err)
		}
	}
	return nil
}

func (a *app) signupForm(w http.ResponseWriter, r *http.Request) {
	dest := "/login?mode=signup"
	if next := r.URL.Query().Get("next"); next != "" {
		dest += "&next=" + url.QueryEscape(next)
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (a *app) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("junkie_session"); err == nil {
		_, _ = a.db.Exec(r.Context(), `DELETE FROM sessions WHERE token = $1`, hashToken(cookie.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: "junkie_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: isSecureRequest(r), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
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
	if strings.EqualFold(newPassword, u.Username) {
		fail("Your password can't be your username.")
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
	_, _ = a.db.Exec(r.Context(), `DELETE FROM password_reset_tokens WHERE user_id = $1`, u.ID)
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

// peekConnectToken reports whether a connect invite belonging to ownerID is
// currently redeemable, without consuming it — the GET side of the confirm
// step, so rendering the page never spends the token.
func (a *app) peekConnectToken(ctx context.Context, ownerID, token string) bool {
	if token == "" {
		return false
	}
	var ok bool
	err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM connect_tokens WHERE user_id = $1 AND token_hash = $2 AND expires_at > now())`, ownerID, hashToken(token)).Scan(&ok)
	return err == nil && ok
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

// connectConfirmPost completes a connect invite the viewer accepted on the
// confirm page; the token is only consumed here, never on a GET.
func (a *app) connectConfirmPost(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	username := strings.ToLower(strings.TrimSpace(r.PathValue("username")))
	var target user
	err := a.db.QueryRow(r.Context(), `SELECT id, username, display_name FROM users WHERE username = $1`, username).Scan(&target.ID, &target.Username, &target.DisplayName)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if a.consumeConnectToken(r.Context(), target.ID, r.FormValue("token"), u.ID) {
		http.Redirect(w, r, "/"+target.Username+"?notice="+url.QueryEscape("You are now connected with "+target.DisplayName+"."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/?error="+url.QueryEscape("That connect link is invalid or has expired — ask for a fresh one."), http.StatusSeeOther)
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
	if token != "" && !a.areConnected(r.Context(), u.ID, target.ID) {
		if a.peekConnectToken(r.Context(), target.ID, token) {
			// Confirm step: connecting is a state change, so the GET only
			// offers it and the POST (connectConfirmPost) performs it.
			// A drive-by fetch of this URL can no longer force a connection.
			// The SPA fetches /api/public-profile, which re-runs this
			// decision tree and answers with the confirm card.
			a.spaPage(w, r)
			return
		}
		// A dead token still lands connected visitors on the profile; for
		// everyone else it must look like nothing is here.
	}
	if !a.areConnected(r.Context(), u.ID, target.ID) {
		http.NotFound(w, r)
		return
	}
	// The gates above keep their exact HTTP semantics (404 for
	// non-connections, redirects for guests/self) so the privacy rule is
	// still enforced at page level, not just in the data API.
	a.spaPage(w, r)
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
	// Set the length and start in one motion, the same shape (and the same
	// 1-60 bounds) as a room's timer-break-length: the private break ring is
	// the room's break ring, so it has to accept what that ring can produce.
	// A request without minutes — an older tab — keeps the length derived
	// from the focus block.
	minutes := clampInt(r.FormValue("minutes"), 1, 60, timer.BreakMinutes)
	ends := time.Now().Add(time.Duration(minutes) * time.Minute)
	_, _ = a.db.Exec(r.Context(), `UPDATE timer_runs SET break_minutes = $1, phase_started_at = now(), phase_ends_at = $2 WHERE id = $3 AND phase = 'break' AND ended_at IS NULL`, minutes, ends, timer.ID)
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
	http.Redirect(w, r, "/dashboard?todos=private", http.StatusSeeOther)
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

// maxRoomsPerUser caps how many rooms one account can have created at a
// time. Deleting a room frees its slot; the cap exists so a single account
// can't flood the instance with rooms.
const maxRoomsPerUser = 5

// userAtRoomCap reports whether userID currently owns the maximum number of
// rooms. Checked on both creation surfaces (web form and /junkie register).
func (a *app) userAtRoomCap(ctx context.Context, userID string) bool {
	var count int
	if err := a.db.QueryRow(ctx, `SELECT COUNT(*) FROM rooms WHERE creator_id = $1 AND ephemeral = false`, userID).Scan(&count); err != nil {
		return false
	}
	return count >= maxRoomsPerUser
}

func (a *app) createRoom(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	if !a.limiter.allow("createroom:"+u.ID, 20, time.Hour) {
		http.Redirect(w, r, "/?error="+url.QueryEscape("Too many rooms created; try again later."), http.StatusSeeOther)
		return
	}
	// Temporary Forest-style focus rooms take the config right here (they have
	// no settings page) and are exempt from the room cap since they delete
	// themselves when their run ends. Redirect lands on the /f/{code} screen.
	if r.FormValue("ephemeral") == "1" {
		a.createEphemeralRoom(w, r, u)
		return
	}
	if a.userAtRoomCap(r.Context(), u.ID) {
		http.Redirect(w, r, "/?error="+url.QueryEscape(fmt.Sprintf("You can have up to %d rooms — delete one you no longer need first.", maxRoomsPerUser)), http.StatusSeeOther)
		return
	}
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

// createEphemeralRoom spins up a temporary focus room from the create-modal
// config (focus/break/sessions + auto-roll), makes the creator a member, and
// sends them to the /f/{code} screen. No name field — these rooms are
// disposable, so we auto-name from the creator.
func (a *app) createEphemeralRoom(w http.ResponseWriter, r *http.Request, u user) {
	focus := clampInt(r.FormValue("focus_minutes"), 5, 180, 25)
	breaks := clampInt(r.FormValue("break_minutes"), 1, 60, 5)
	sessions := clampInt(r.FormValue("auto_sessions"), 1, 12, 4)
	autoRoll := r.FormValue("auto_roll") == "1"
	requireCheckin := r.FormValue("require_checkin") == "1"
	name := limitRunes(strings.TrimSpace(r.FormValue("name")), maxRoomNameLen)
	if name == "" {
		name = u.DisplayName + "'s focus room"
	}
	var code, roomID string
	err := errors.New("no attempt")
	for range 5 {
		code = randomCode()
		err = a.db.QueryRow(r.Context(), `
			INSERT INTO rooms (code, name, creator_id, focus_minutes, break_minutes, auto_sessions, auto_roll, ephemeral, require_checkin)
			VALUES ($1, $2, $3, $4, $5, $6, $7, true, $8) RETURNING id`,
			code, name, u.ID, focus, breaks, sessions, autoRoll, requireCheckin).Scan(&roomID)
		if err == nil || !isUniqueViolation(err) {
			break
		}
	}
	if err != nil {
		log.Printf("create ephemeral room: %v", err)
		http.Error(w, "could not create room", http.StatusInternalServerError)
		return
	}
	_, _ = a.db.Exec(r.Context(), `INSERT INTO room_members (room_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, roomID, u.ID)
	http.Redirect(w, r, "/f/"+code, http.StatusSeeOther)
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
	// Codes are the room credential, so lookups must not be free to brute
	// force. Generous for humans mistyping, hostile to enumeration.
	if !a.limiter.allow("joincode:"+u.ID, 20, 10*time.Minute) {
		http.Redirect(w, r, back+"?error="+url.QueryEscape("Too many join attempts. Try again in a few minutes."), http.StatusSeeOther)
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
	// Guests reach this without an account, so the enumeration guard keys on
	// IP here; the authed join path keys on the user id.
	if !a.limiter.allow("joincode:"+clientIP(r), 20, 10*time.Minute) {
		http.Redirect(w, r, back+"?error="+url.QueryEscape("Too many join attempts. Try again in a few minutes."), http.StatusSeeOther)
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
	// The SPA fetches /api/room/{code}, which re-runs the member gate and
	// answers with either the invite card or the room state. The 404 gates
	// above keep their exact HTTP semantics.
	_ = u
	_ = rm
	a.spaPage(w, r)
}

// focusRoomPage serves the SPA shell for a temporary focus room at /f/{code}.
// It 404s anything that isn't a live ephemeral room (unknown code, a normal
// room's code, or a room already deleted when its run ended), so the stripped
// screen only ever loads for a room it fits.
func (a *app) focusRoomPage(w http.ResponseWriter, r *http.Request) {
	rm, ok := a.findRoom(r.Context(), r.PathValue("code"))
	if !ok || !rm.Ephemeral {
		http.NotFound(w, r)
		return
	}
	a.spaPage(w, r)
}

// enterFocusRoom is the click-the-link join for a temporary room: it makes the
// viewer a member and queues them into the run (joined now during a lobby or
// break, parked in the waiting list otherwise), so opening the share link drops
// them straight onto the waiting screen without an invite step. Idempotent, so
// the /f/{code} view can call it on every mount, including for the creator.
func (a *app) enterFocusRoom(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	rm, ok := a.findRoom(r.Context(), r.PathValue("code"))
	if !ok || !rm.Ephemeral {
		writeJSONError(w, http.StatusNotFound, "room not found")
		return
	}
	a.addRoomMember(r.Context(), rm.ID, u.ID)
	if timer, outcome, err := a.joinTimer(r.Context(), rm, u.ID); err != nil {
		log.Printf("enter focus room %s: %v", rm.Code, err)
	} else if outcome == joinedNow && timer != nil {
		a.notifyDiscord(rm, timer, false)
	}
	// Tell the room's other tabs the participant/waiting set changed.
	a.hub.broadcast(rm.Code, "timer-phase")
	writeJSON(w, map[string]any{"ok": true})
}

// closeEphemeralRoom deletes a temporary room once its run is over — completed
// or emptied — and signals its viewers (via the "deleted" broadcast the room
// socket already understands) to head back home. The row delete cascades to
// members, todos, and timer rows. No-op for normal rooms.
func (a *app) closeEphemeralRoom(ctx context.Context, rm room) {
	if !rm.Ephemeral {
		return
	}
	if _, err := a.db.Exec(ctx, `DELETE FROM rooms WHERE id = $1`, rm.ID); err != nil {
		log.Printf("close ephemeral room %s: %v", rm.Code, err)
		return
	}
	a.hub.broadcast(rm.Code, "deleted")
}

// sweepAbandonedEphemeralRooms is the backstop for temporary rooms that end
// without anyone's tab there to trigger closeEphemeralRoom — created and never
// started, or left mid-block after every viewer closed their tab. It removes
// ephemeral rooms with no *live* run (none whose deadline is still recent),
// leaving actively-running blocks of any length untouched.
func (a *app) sweepAbandonedEphemeralRooms(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		tag, err := a.db.Exec(ctx, `
			DELETE FROM rooms r
			WHERE r.ephemeral = true
				AND r.created_at < now() - interval '1 hour'
				AND NOT EXISTS (
					SELECT 1 FROM timer_runs tr
					WHERE tr.room_id = r.id
						AND tr.ended_at IS NULL AND tr.phase <> 'ended'
						AND tr.phase_ends_at > now() - interval '1 hour'
				)`)
		if err != nil {
			log.Printf("sweep abandoned focus rooms: %v", err)
		} else if n := tag.RowsAffected(); n > 0 {
			log.Printf("swept %d abandoned focus rooms", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// sweepStaleRoomWaiting drops queued joins that never got their run. The
// absorption queries already ignore entries past the one-hour cutoff
// (stalePauseTimeout); this sweep removes them outright so a forgotten Join
// stops showing a "waiting" badge and stops holding one of the user's
// maxActiveRooms slots.
func (a *app) sweepStaleRoomWaiting(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		if _, err := a.db.Exec(ctx, `DELETE FROM room_waiting WHERE created_at < now() - interval '1 hour'`); err != nil {
			log.Printf("sweep stale room waiting: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// membersRedirect returns to the room's member list, carrying an error for
// postForm.js to toast. Role changes all land back on the same page, so the
// caller never has to decide where to go.
func membersRedirect(w http.ResponseWriter, r *http.Request, code, errMsg string) {
	dest := "/r/" + code + "/members"
	if errMsg != "" {
		dest += "?error=" + url.QueryEscape(errMsg)
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
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
		if a.roomRunActive(r.Context(), rm.ID) {
			http.Redirect(w, r, "/r/"+code+"?error="+url.QueryEscape("Timer settings can't change while a run is active."), http.StatusSeeOther)
			return
		}
		focus := clampInt(r.FormValue("focus_minutes"), 5, 180, rm.FocusMinutes)
		breaks := clampInt(r.FormValue("break_minutes"), 1, 60, rm.BreakMinutes)
		sessions := clampInt(r.FormValue("auto_sessions"), 1, 12, rm.AutoSessions)
		autoRoll := rm.AutoRoll
		if v := r.FormValue("auto_roll"); v != "" {
			autoRoll = v == "1"
		}
		requireCheckin := rm.RequireCheckin
		if v := r.FormValue("require_checkin"); v != "" {
			requireCheckin = v == "1"
		}
		_ = a.applyRoomSettings(r.Context(), rm.ID, focus, breaks, sessions, autoRoll, requireCheckin)
	case "transfer-ownership":
		if err := a.startOwnershipTransfer(r.Context(), rm, u.ID, r.FormValue("user_id")); err != nil {
			membersRedirect(w, r, rm.Code, err.Error())
			return
		}
		membersRedirect(w, r, rm.Code, "")
		return
	case "cancel-transfer":
		if err := a.cancelOwnershipTransfer(r.Context(), rm, u.ID); err != nil {
			membersRedirect(w, r, rm.Code, err.Error())
			return
		}
		membersRedirect(w, r, rm.Code, "")
		return
	case "make-admin", "remove-admin":
		// Room-admin rights are handed out by people who already hold them --
		// the creator, or an existing admin -- exactly like a Discord or
		// WhatsApp group. Members see no buttons at all.
		if !a.canAdminRoom(r.Context(), rm, u.ID) {
			membersRedirect(w, r, rm.Code, "Only the room's admins can change roles.")
			return
		}
		role := roomRoleAdmin
		if action == "remove-admin" {
			role = roomRoleMember
		}
		if err := a.setRoomMemberRole(r.Context(), rm, r.FormValue("user_id"), role); err != nil {
			membersRedirect(w, r, rm.Code, err.Error())
			return
		}
		a.hub.broadcast(code, "members")
		membersRedirect(w, r, rm.Code, "")
		return
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
		if _, _, err := a.startRoomTimerAndSchedule(r.Context(), rm, u.ID, focusMinutes, u.DisplayName); errors.Is(err, errTooManyRooms) {
			http.Redirect(w, r, "/r/"+code+"?error="+url.QueryEscape(fmt.Sprintf("You're already in %d rooms' live sessions — leave one first.", maxActiveRooms)), http.StatusSeeOther)
			return
		} else if err != nil {
			log.Printf("start room timer %s: %v", rm.Code, err)
			http.Error(w, "could not start timer", http.StatusInternalServerError)
			return
		}
		action = ""
	case "timer-join":
		timer, outcome, _ := a.joinTimer(r.Context(), rm, u.ID)
		if outcome == joinedLimit {
			http.Redirect(w, r, "/r/"+code+"?error="+url.QueryEscape(fmt.Sprintf("You're already in %d rooms' live sessions — leave one first.", maxActiveRooms)), http.StatusSeeOther)
			return
		}
		if outcome == joinedNow && timer != nil {
			// Refresh the Discord live message's who's-in line.
			a.notifyDiscord(rm, timer, false)
		}
	case "timer-checkin":
		if a.confirmCheckin(r.Context(), rm.ID, u.ID) {
			action = "timer-phase"
			// A check-in is exactly what the live message's "In:" line and its
			// you're-about-to-be-dropped mentions are built from, so checking
			// in from the web has to refresh it — otherwise Discord goes on
			// pinging someone who already confirmed, and the room reads as
			// though they never showed up.
			if timer, err := a.activeTimer(r.Context(), rm.ID, u.ID); err == nil && timer != nil {
				a.notifyDiscord(rm, timer, false)
			}
		} else {
			action = ""
		}
	case "timer-pause":
		a.confirmCheckin(r.Context(), rm.ID, u.ID)
		if timer, changed, err := a.pauseRoomBreak(r.Context(), rm.ID, u.ID); err != nil {
			http.Error(w, "could not pause break", http.StatusInternalServerError)
			return
		} else if !changed {
			action = ""
		} else {
			a.notifyDiscord(rm, timer, false)
		}
	case "timer-resume":
		a.confirmCheckin(r.Context(), rm.ID, u.ID)
		if timer, changed, err := a.resumeRoomBreak(r.Context(), rm.ID, u.ID); err != nil {
			http.Error(w, "could not resume break", http.StatusInternalServerError)
			return
		} else if !changed {
			action = ""
		} else {
			a.notifyDiscord(rm, timer, false)
		}
	case "timer-skip-break":
		// Skipping ends the window in which everyone else confirms they're
		// staying, so a check-in room can't offer it — unless the skipper is
		// the only one in the run, where there is no one else's window to
		// close and a long break is just time they're sitting out alone.
		if rm.RequireCheckin && !a.soleParticipant(r.Context(), rm.ID, u.ID) {
			http.Redirect(w, r, "/r/"+code+"?error="+url.QueryEscape("Breaks can't be skipped while session check-in is on."), http.StatusSeeOther)
			return
		}
		// Cutting the break short to get back to work is about as clear a
		// "I'm here" as exists, so it counts as one — same as pause, resume
		// and setting the break length. Without it the skipper is dropped at
		// the very transition they asked for, and since they're necessarily
		// alone in a check-in room, that empties the run and ends it.
		a.confirmCheckin(r.Context(), rm.ID, u.ID)
		if timer, changed, err := a.skipRoomBreak(r.Context(), rm.ID, u.ID); err != nil {
			http.Error(w, "could not skip break", http.StatusInternalServerError)
			return
		} else if !changed {
			action = ""
		} else {
			action = "timer-phase"
			a.notifyDiscord(rm, timer, false)
		}
	case "timer-break-length":
		// Normalize first so a stale tab can't restart a run whose pause
		// already expired.
		if _, _, err := a.normalizeTimer(r.Context(), rm.ID, u.ID); err != nil {
			http.Error(w, "could not start break", http.StatusInternalServerError)
			return
		}
		a.confirmCheckin(r.Context(), rm.ID, u.ID)
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
				a.notifyDiscord(rm, timer, false)
			}
		}
	case "timer-leave":
		if ended, _ := a.leaveTimer(r.Context(), rm, u.ID); ended {
			// The last participant left: for a temporary room that empties the
			// block, so delete it and send everyone home rather than ending to
			// an idle state that can't be restarted.
			if rm.Ephemeral {
				a.closeEphemeralRoom(r.Context(), rm)
				return
			}
			action = "timer-end"
			a.notifyDiscord(rm, nil, false)
		} else if timer, err := a.activeTimer(r.Context(), rm.ID, u.ID); err == nil && timer != nil {
			a.notifyDiscord(rm, timer, false)
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
	// A temporary room whose run just ended (completed all sessions or emptied
	// out) is done for good — delete it and send its viewers home instead of
	// broadcasting an idle phase they'd never act on.
	if timer == nil && rm.Ephemeral {
		a.closeEphemeralRoom(context.Background(), rm)
		return
	}
	a.hub.broadcast(rm.Code, "timer-phase")
	if timer != nil && len(timer.Kicked) > 0 {
		// Name who was dropped for missing check-in so their own tabs can
		// explain the removal instead of silently losing the leave button.
		a.hub.broadcastJSON(rm.Code, map[string]interface{}{
			"type": "timer-checkin-kick", "roomCode": rm.Code, "userIds": timer.Kicked,
		})
	}
	if timer != nil && timer.Phase == "break" {
		a.hub.broadcastJSON(rm.Code, map[string]interface{}{
			"type": "timer-break-invite", "roomCode": rm.Code, "roomName": rm.Name,
			"breakDeadline": timer.PhaseEndsAt.UTC().Format(time.RFC3339Nano),
		})
	}
	a.notifyDiscord(rm, timer, false)
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
			COALESCE((SELECT tp.confirmed_session FROM timer_participants tp WHERE tp.timer_run_id = tr.id AND tp.user_id = $2), 0)
		FROM timer_runs tr
		WHERE tr.room_id = $1 AND tr.ended_at IS NULL AND tr.phase <> 'ended'
		ORDER BY tr.created_at DESC
		LIMIT 1
		FOR UPDATE`, roomID, userID).Scan(
		&timer.ID, &timer.Phase, &timer.FocusMinutes, &timer.BreakMinutes, &timer.TotalSessions,
		&timer.CurrentSession, &timer.PhaseStartedAt, &timer.PhaseEndsAt, &timer.PausedAt,
		&timer.PausedRemainingSeconds, &timer.ViewerConfirmedSession,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	timer.Participant = timer.ViewerConfirmedSession > 0

	var autoRoll, requireCheckin bool
	if err = tx.QueryRow(ctx, `SELECT auto_roll, require_checkin FROM rooms WHERE id = $1`, roomID).Scan(&autoRoll, &requireCheckin); err != nil {
		return nil, false, err
	}

	now := time.Now()
	// An expired pause ends the run: the completed focus sessions were
	// already credited at their break transitions, so nothing is lost, and
	// the room opens up for a fresh start.
	if timer.PauseExpired(now) {
		if _, err = tx.Exec(ctx, `UPDATE timer_runs SET phase = 'ended', ended_at = now(), paused_at = NULL, paused_remaining_seconds = NULL WHERE id = $1`, timer.ID); err != nil {
			return nil, false, err
		}
		timer.Phase = "ended"
		timer.Transitioned = true
	}
	// The loop is already bounded in principle: every break->focus pass
	// increments current_session, and the focus branch ends the run once it
	// reaches total_sessions (12 max), so even a row with zero-length phases
	// drains to 'ended' rather than spinning. This cap is insurance against a
	// future edit to the state machine introducing a pass that doesn't
	// advance — spinning inside a transaction holding FOR UPDATE would wedge
	// the request, so stop and log instead. Real traffic settles in one or
	// two passes and never approaches this.
	for advances := 0; timer.Phase != "ended" && timer.PausedAt == nil && !now.Before(timer.PhaseEndsAt); advances++ {
		if advances >= maxPhaseAdvances {
			log.Printf("normalize timer %s: phase loop made %d advances without settling (phase %q, focus %d, break %d) — stopping",
				timer.ID, advances, timer.Phase, timer.FocusMinutes, timer.BreakMinutes)
			break
		}
		if timer.Phase == "lobby" {
			timer.Phase = "focus"
			timer.PhaseStartedAt = now
			timer.PhaseEndsAt = now.Add(time.Duration(timer.FocusMinutes) * time.Minute)
			if _, err = tx.Exec(ctx, `UPDATE timer_runs SET phase = 'focus', phase_started_at = $1, phase_ends_at = $2 WHERE id = $3`, timer.PhaseStartedAt, timer.PhaseEndsAt, timer.ID); err != nil {
				return nil, false, err
			}
			timer.Transitioned = true
		} else if timer.Phase == "focus" {
			// Users may sit in up to maxActiveRooms rooms' runs at once. A
			// finished session credits each participant its wall-clock window
			// minus whatever other completed sessions already claimed of it
			// (focus_credits), then claims the window itself. Overlapping
			// rooms therefore never count the same minute twice and never
			// drop a real one; whichever session completes first claims the
			// time. Solo timers are separate and unaffected.
			if err = creditFocusSession(ctx, tx, timer.ID, timer.PhaseStartedAt, timer.PhaseEndsAt); err != nil {
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
			// join while focus was running (or before the run existed). They
			// asked to be in the *next* session, which also counts as their
			// check-in for it. Requests older than an hour (stalePauseTimeout)
			// don't board — whoever tapped Join has long since moved on.
			if _, err = tx.Exec(ctx, `
				INSERT INTO timer_participants (timer_run_id, user_id, confirmed_session)
				SELECT $1, user_id, $3 FROM room_waiting
				WHERE room_id = $2 AND created_at > now() - interval '1 hour'
				ON CONFLICT DO NOTHING`, timer.ID, roomID, timer.CurrentSession+1); err != nil {
				return nil, false, err
			}
			if _, err = tx.Exec(ctx, `DELETE FROM room_waiting WHERE room_id = $1`, roomID); err != nil {
				return nil, false, err
			}
			timer.Transitioned = true
		} else if timer.Phase == "break" {
			// Check-in rooms: the break was the window to claim a seat in the
			// next session. Drop everyone who didn't; if that empties the run,
			// the no-participants guard below ends it rather than letting
			// ghost sessions tick on.
			if requireCheckin {
				kickRows, kickErr := tx.Query(ctx, `
					DELETE FROM timer_participants
					WHERE timer_run_id = $1 AND confirmed_session <= $2
					RETURNING user_id`, timer.ID, timer.CurrentSession)
				if kickErr != nil {
					return nil, false, kickErr
				}
				for kickRows.Next() {
					var id string
					if kickErr = kickRows.Scan(&id); kickErr != nil {
						kickRows.Close()
						return nil, false, kickErr
					}
					timer.Kicked = append(timer.Kicked, id)
				}
				kickRows.Close()
				if kickErr = kickRows.Err(); kickErr != nil {
					return nil, false, kickErr
				}
			}
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
		// The run's end voids every queued join: "you'll be in when the break
		// starts" pointed at this run, and carrying the queue into whatever
		// run starts later would seat people who long since walked away.
		if _, err = tx.Exec(ctx, `DELETE FROM room_waiting WHERE room_id = $1`, roomID); err != nil {
			return nil, false, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, false, err
		}
		return nil, true, nil
	}
	// A transition above may have absorbed userID from the waiting list or
	// kicked them for not checking in, making the read at the top stale.
	if timer.Transitioned {
		if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT confirmed_session FROM timer_participants WHERE timer_run_id = $1 AND user_id = $2), 0)`, timer.ID, userID).Scan(&timer.ViewerConfirmedSession); err != nil {
			return nil, false, err
		}
		timer.Participant = timer.ViewerConfirmedSession > 0
	}
	rows, err := tx.Query(ctx, `
		SELECT u.id, u.display_name, u.avatar IS NOT NULL,
			COALESCE(EXTRACT(EPOCH FROM u.avatar_updated_at), 0)::bigint, tp.confirmed_session
		FROM timer_participants tp JOIN users u ON u.id = tp.user_id
		WHERE tp.timer_run_id = $1 ORDER BY tp.joined_at`, timer.ID)
	if err != nil {
		return nil, false, err
	}
	for rows.Next() {
		var m user
		if err = rows.Scan(&m.ID, &m.DisplayName, &m.HasAvatar, &m.AvatarVersion, &m.ConfirmedSession); err != nil {
			rows.Close()
			return nil, false, err
		}
		timer.Participants = append(timer.Participants, m)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	if len(timer.Participants) == 0 {
		if _, err = tx.Exec(ctx, `UPDATE timer_runs SET phase = 'ended', ended_at = now() WHERE id = $1`, timer.ID); err != nil {
			return nil, false, err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM room_waiting WHERE room_id = $1`, roomID); err != nil {
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

// creditFocusSession adds a completed room focus session's window to each
// participant's daily activity, prorated against focus_credits: minutes some
// other completed session already claimed are subtracted, and the session then
// claims its own full window so later completions subtract it in turn. Runs
// inside normalizeTimer's transaction — the caller holds the run's row lock,
// so one session can never be credited twice.
func creditFocusSession(ctx context.Context, tx pgx.Tx, runID string, start, end time.Time) error {
	if !end.After(start) {
		return nil
	}
	// One row per participant per overlapping claim (NULLs for participants
	// with none), ordered so each user's claims arrive sorted by start.
	rows, err := tx.Query(ctx, `
		SELECT tp.user_id, fc.started_at, fc.ended_at
		FROM timer_participants tp
		LEFT JOIN focus_credits fc ON fc.user_id = tp.user_id
			AND fc.ended_at > $2 AND fc.started_at < $3
		WHERE tp.timer_run_id = $1
		ORDER BY tp.user_id, fc.started_at`, runID, start, end)
	if err != nil {
		return err
	}
	credited := map[string]time.Duration{} // unclaimed focus time per user
	cursor := map[string]time.Time{}       // sweep position per user
	for rows.Next() {
		var userID string
		var claimStart, claimEnd *time.Time
		if err = rows.Scan(&userID, &claimStart, &claimEnd); err != nil {
			rows.Close()
			return err
		}
		if _, seen := credited[userID]; !seen {
			credited[userID] = 0
			cursor[userID] = start
		}
		if claimStart == nil {
			continue
		}
		// Claims are sorted by start, so the gap before this claim is
		// unclaimed; advance the sweep past the claim's end.
		if claimStart.After(cursor[userID]) {
			credited[userID] += claimStart.Sub(cursor[userID])
		}
		if claimEnd.After(cursor[userID]) {
			cursor[userID] = *claimEnd
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for userID, unclaimed := range credited {
		if pos := cursor[userID]; end.After(pos) {
			unclaimed += end.Sub(pos)
		}
		minutes := int((unclaimed + 30*time.Second) / time.Minute)
		if minutes <= 0 {
			continue
		}
		// Each participant lands on their own calendar day: one room can hold
		// people in different zones, so the date is looked up per user rather
		// than taken from the server's CURRENT_DATE.
		if _, err = tx.Exec(ctx, `
			INSERT INTO activity (user_id, activity_date, focus_minutes)
			SELECT id, (now() AT TIME ZONE COALESCE(timezone, 'UTC'))::date, $2 FROM users WHERE id = $1
			ON CONFLICT (user_id, activity_date)
			DO UPDATE SET focus_minutes = activity.focus_minutes + EXCLUDED.focus_minutes`, userID, minutes); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO focus_credits (user_id, started_at, ended_at)
		SELECT user_id, $2, $3 FROM timer_participants WHERE timer_run_id = $1`, runID, start, end); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM focus_credits WHERE ended_at < now() - interval '2 days'`)
	return err
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

// roomRunActive reports whether the room has a live (unended) timer run in
// any phase. Timer settings are locked while one exists: a run snapshots its
// config at start, so a mid-run change would silently apply to the *next*
// run while looking like it changed the current one.
func (a *app) roomRunActive(ctx context.Context, roomID string) bool {
	var active bool
	err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM timer_runs WHERE room_id = $1 AND ended_at IS NULL AND phase <> 'ended')`, roomID).Scan(&active)
	return err == nil && active
}

// applyRoomSettings persists a room's timer configuration. Shared by the web
// "settings" room action and the Discord /junkie config command so the two
// surfaces can't drift.
func (a *app) applyRoomSettings(ctx context.Context, roomID string, focusMinutes, breakMinutes, autoSessions int, autoRoll, requireCheckin bool) error {
	_, err := a.db.Exec(ctx, `UPDATE rooms SET focus_minutes = $1, break_minutes = $2, auto_sessions = $3, auto_roll = $4, require_checkin = $5, updated_at = now() WHERE id = $6`,
		focusMinutes, breakMinutes, autoSessions, autoRoll, requireCheckin, roomID)
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
	joinedLimit                          // refused: already in maxActiveRooms rooms
)

// maxActiveRooms caps how many rooms' live sessions one user can be part of
// at once (participating or waiting). Three is not a magic number — it's the
// sanity ceiling Sam picked so "join everything" can't get silly, and it
// pairs with the proration in creditFocusSession: however many you're in,
// each wall-clock minute of focus counts exactly once.
const maxActiveRooms = 3

var errTooManyRooms = errors.New("already in the maximum number of rooms' live sessions")

// activeRoomTimerCount counts the rooms other than roomID where userID is
// currently part of a live run or parked in the waiting list — the number
// the maxActiveRooms cap is checked against.
func (a *app) activeRoomTimerCount(ctx context.Context, userID, roomID string) int {
	var n int
	err := a.db.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM timer_participants tp
				JOIN timer_runs tr ON tr.id = tp.timer_run_id
				WHERE tp.user_id = $1 AND tr.room_id IS NOT NULL AND tr.room_id <> $2
					AND tr.ended_at IS NULL AND tr.phase <> 'ended')
			+
			(SELECT COUNT(*) FROM room_waiting WHERE user_id = $1 AND room_id <> $2)`,
		userID, roomID).Scan(&n)
	if err != nil {
		return 0
	}
	return n
}

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
	if timer != nil && timer.Participant {
		return timer, joinedAlready, nil
	}
	// The cap counts other rooms only, so re-joining or re-queueing the same
	// room is never blocked by the user's own presence here.
	if a.activeRoomTimerCount(ctx, userID, rm.ID) >= maxActiveRooms {
		return timer, joinedLimit, nil
	}
	if timer == nil {
		return wait(joinedQueuedStart)
	}
	if timer.Phase == "focus" {
		return wait(joinedQueuedBreak)
	}
	// Joining the lobby claims a seat in session 1; joining during a break
	// claims the next session — either way the join is also the check-in.
	confirmFor := timer.CurrentSession
	if timer.Phase == "break" {
		confirmFor++
	}
	if _, err := a.db.Exec(ctx, `INSERT INTO timer_participants (timer_run_id, user_id, confirmed_session) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, timer.ID, userID, confirmFor); err != nil {
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

// confirmCheckin marks userID as staying for the room's next session while
// its run is on break (paused or running — the whole break is the window).
// Reports whether anything changed, i.e. the user was an unconfirmed
// participant of a live break. Every deliberate break action (the check-in
// button, pause/resume, starting the break) routes through here, so acting
// on the break proves presence without a second tap.
func (a *app) confirmCheckin(ctx context.Context, roomID, userID string) bool {
	tag, err := a.db.Exec(ctx, `
		UPDATE timer_participants tp
		SET confirmed_session = tr.current_session + 1
		FROM timer_runs tr
		WHERE tr.id = tp.timer_run_id AND tr.room_id = $1
			AND tr.ended_at IS NULL AND tr.phase = 'break'
			AND tp.user_id = $2 AND tp.confirmed_session <= tr.current_session`, roomID, userID)
	return err == nil && tag.RowsAffected() > 0
}

func (a *app) checkedInForNextSession(ctx context.Context, roomID, userID string) bool {
	var ok bool
	err := a.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM timer_participants tp
			JOIN timer_runs tr ON tr.id = tp.timer_run_id
			WHERE tr.room_id = $1 AND tr.ended_at IS NULL AND tr.phase = 'break'
				AND tp.user_id = $2 AND tp.confirmed_session > tr.current_session
		)`, roomID, userID).Scan(&ok)
	return err == nil && ok
}

// roomWaiting reports whether userID is parked in rm's waiting list.
func (a *app) roomWaiting(ctx context.Context, roomID, userID string) bool {
	var waiting bool
	err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM room_waiting WHERE room_id = $1 AND user_id = $2)`, roomID, userID).Scan(&waiting)
	return err == nil && waiting
}

// roomWaitingUsers lists everyone parked in a room's waiting list, with the
// avatar bits the participant stack needs. Used by the /f/{code} screen to show
// who's here before the block starts, when there's no run yet to draw heads from.
func (a *app) roomWaitingUsers(ctx context.Context, roomID string) ([]user, error) {
	rows, err := a.db.Query(ctx, `
		SELECT u.id, u.display_name, u.avatar IS NOT NULL,
			COALESCE(EXTRACT(EPOCH FROM u.avatar_updated_at), 0)::bigint
		FROM room_waiting rw JOIN users u ON u.id = rw.user_id
		WHERE rw.room_id = $1 ORDER BY rw.created_at`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []user
	for rows.Next() {
		var m user
		if rows.Scan(&m.ID, &m.DisplayName, &m.HasAvatar, &m.AvatarVersion) == nil {
			members = append(members, m)
		}
	}
	return members, rows.Err()
}

func (a *app) startRoomTimer(ctx context.Context, rm room, userID string, focusMinutes int) (bool, error) {
	// Starting makes you a participant, so the maxActiveRooms cap applies
	// here as much as to joins. Checked outside the transaction: a race can
	// briefly overshoot the cap, which is harmless — creditFocusSession
	// prorates overlap so each minute counts once regardless.
	if a.activeRoomTimerCount(ctx, userID, rm.ID) >= maxActiveRooms {
		return false, errTooManyRooms
	}
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
	// need to catch the lobby countdown live. Entries older than an hour
	// (stalePauseTimeout) are skipped: a Join tapped that long ago is a
	// leftover intention, not someone actually here for this block.
	if _, err = tx.Exec(ctx, `
		INSERT INTO timer_participants (timer_run_id, user_id)
		SELECT $1, user_id FROM room_waiting
		WHERE room_id = $2 AND created_at > now() - interval '1 hour'
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

// startRoomTimerAndSchedule starts rm's timer (if none is active) and
// broadcasts the lobby countdown over the web hub; notifyDiscord arms the
// lobby->focus wakeup. Shared by the web "timer-start" room action and the
// Discord /junkie start command, which additionally uses the returned timer
// to build its own reply rather than relying on the hub broadcast.
func (a *app) startRoomTimerAndSchedule(ctx context.Context, rm room, userID string, focusMinutes int, starterName string) (*timerRun, bool, error) {
	// Clear any run whose pause has expired, so starting fresh doesn't
	// require someone to have viewed the room since the expiry.
	if _, _, err := a.normalizeTimer(ctx, rm.ID, userID); err != nil {
		return nil, false, err
	}
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
	a.notifyDiscord(rm, timer, true)
	return timer, created, nil
}

// schedulePhaseWakeup arms a wakeup just past the timer's phase end that
// advances the state machine and broadcasts the change, so a room nobody has
// foregrounded — every tab backgrounded during the break, or members only on
// Discord — still transitions on time and still fires its end-of-phase
// notifications. normalizeTimer stays the single source of truth; this only
// pokes it. Deduped per room+run+phase so overlapping notify calls (web
// viewers polling plus this chain) don't stack timers.
func (a *app) schedulePhaseWakeup(rm room, timer *timerRun) {
	if timer == nil {
		return
	}
	// A running phase wakes just past its deadline. A paused break has no
	// deadline, so wake at the stale-pause cutoff instead — the moment
	// normalizeTimer ends an abandoned run — so the room frees up on time
	// without anyone viewing it.
	deadline := timer.PhaseEndsAt
	if timer.PausedAt != nil {
		deadline = timer.PausedAt.Add(stalePauseTimeout)
	}
	key := timer.ID + ":" + timer.Phase + ":" + deadline.UTC().Format(time.RFC3339Nano)
	a.wakeupMu.Lock()
	if a.wakeups == nil {
		a.wakeups = map[string]string{}
	}
	if a.wakeups[rm.ID] == key {
		a.wakeupMu.Unlock()
		return
	}
	a.wakeups[rm.ID] = key
	a.wakeupMu.Unlock()
	delay := max(time.Until(deadline), 0) + time.Second
	time.AfterFunc(delay, func() {
		a.wakeupMu.Lock()
		delete(a.wakeups, rm.ID)
		a.wakeupMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// The user id only shapes the Participant flag, which nothing in the
		// broadcast path reads; the creator is a stable stand-in.
		next, transitioned, err := a.normalizeTimer(ctx, rm.ID, rm.CreatorID)
		if err != nil {
			log.Printf("phase wakeup %s: %v", rm.Code, err)
			return
		}
		if transitioned {
			a.broadcastTimerPhase(rm, next)
		} else if next != nil {
			// Deadline moved (pause, break-length change) — track the new one.
			a.notifyDiscord(rm, next, false)
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
	// Normalize first so a stale tab's Resume can't resurrect a run whose
	// pause already expired — the run ends there and the update below
	// matches nothing.
	if _, _, err := a.normalizeTimer(ctx, roomID, userID); err != nil {
		return nil, false, err
	}
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

// soloBreakStale reports a pending private break left untouched past
// stalePauseTimeout — the private twin of a room's expired pause. A private
// break arrives waiting to be started, exactly like an auto-roll-off room's
// does, so it ages on the same clock and ends the same way: the run is over,
// the focus minutes it earned are already banked, and the screen goes back to
// the idle ring rather than offering a break from yesterday.
//
// The clock is phase_started_at, which is when focus actually ended (see
// normalizeSoloTimer) rather than when the flip was recorded.
func soloBreakStale(t *timerRun, now time.Time) bool {
	return t != nil && soloBreakPending(t) && now.Sub(t.PhaseStartedAt) >= stalePauseTimeout
}

func (a *app) normalizeSoloTimer(ctx context.Context, userID string) (*timerRun, error) {
	timer, err := a.activeSoloTimer(ctx, userID)
	if err != nil || timer == nil {
		return timer, err
	}
	now := time.Now()
	if timer.Phase == "focus" && now.After(timer.PhaseEndsAt) {
		breakMins := soloBreakMinutes(timer.FocusMinutes)
		// The break is stamped at the moment focus ended, not at this read.
		// A private run has no server-side wakeup pushing it along the way a
		// room does (schedulePhaseWakeup), so the boundary is only recorded
		// whenever its owner next looks — dating it "now" would restart an
		// abandoned break's clock on every visit and it could never go stale.
		boundary := timer.PhaseEndsAt
		// Claim the focus->break flip first: the WHERE phase='focus' guard
		// means exactly one of several racing requests (two tabs polling the
		// boundary) wins the row, and only the winner credits the minutes —
		// the activity add is cumulative, so crediting per-caller would
		// double-count.
		tag, err := a.db.Exec(ctx, `UPDATE timer_runs SET phase = 'break', break_minutes = $1, phase_started_at = $2, phase_ends_at = $2 WHERE id = $3 AND phase = 'focus'`, breakMins, boundary, timer.ID)
		if err == nil && tag.RowsAffected() > 0 {
			_, _ = a.db.Exec(ctx, `
				INSERT INTO activity (user_id, activity_date, focus_minutes)
				SELECT id, (now() AT TIME ZONE COALESCE(timezone, 'UTC'))::date, $2 FROM users WHERE id = $1
				ON CONFLICT (user_id, activity_date)
				DO UPDATE SET focus_minutes = activity.focus_minutes + EXCLUDED.focus_minutes`, userID, timer.FocusMinutes)
		}
		timer.Phase = "break"
		timer.BreakMinutes = breakMins
		timer.PhaseStartedAt = boundary
		timer.PhaseEndsAt = boundary
	} else if timer.Phase == "break" && timer.PhaseEndsAt.After(timer.PhaseStartedAt) && now.After(timer.PhaseEndsAt) {
		_, _ = a.db.Exec(ctx, `UPDATE timer_runs SET phase = 'ended', ended_at = now() WHERE id = $1 AND phase = 'break'`, timer.ID)
		return nil, nil
	}
	// A break nobody came back to ends the run, same as a room's expired
	// pause: whoever started it walked away, and the next visit should offer
	// a fresh block rather than a stale break. Runs against both the flip
	// above and a break that was already pending on an earlier visit.
	if soloBreakStale(timer, now) {
		_, _ = a.db.Exec(ctx, `UPDATE timer_runs SET phase = 'ended', ended_at = now() WHERE id = $1 AND phase = 'break' AND ended_at IS NULL`, timer.ID)
		return nil, nil
	}
	timer.Participant = true
	timer.Participants = []user{{DisplayName: "You"}}
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
			COALESCE((SELECT tp.confirmed_session FROM timer_participants tp WHERE tp.timer_run_id = tr.id AND tp.user_id = $2), 0)
		FROM timer_runs tr
		WHERE tr.room_id = $1 AND tr.ended_at IS NULL AND tr.phase <> 'ended'
		ORDER BY tr.created_at DESC
		LIMIT 1`, roomID, userID).Scan(&t.ID, &t.Phase, &t.FocusMinutes, &t.BreakMinutes, &t.TotalSessions, &t.CurrentSession, &t.PhaseStartedAt, &t.PhaseEndsAt, &t.PausedAt, &t.PausedRemainingSeconds, &t.ViewerConfirmedSession)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.Participant = t.ViewerConfirmedSession > 0
	t.Participants, _ = a.timerParticipants(ctx, t.ID)
	return &t, nil
}

func (a *app) endTimerIfNoParticipants(ctx context.Context, runID string) bool {
	var count int
	if err := a.db.QueryRow(ctx, `SELECT COUNT(*) FROM timer_participants WHERE timer_run_id = $1`, runID).Scan(&count); err != nil || count > 0 {
		return false
	}
	_, _ = a.db.Exec(ctx, `UPDATE timer_runs SET phase = 'ended', ended_at = now() WHERE id = $1 AND ended_at IS NULL`, runID)
	// Same rule as normalizeTimer's ended paths: a dead run takes its queued
	// joins with it, so the next start doesn't seat people from a past run.
	_, _ = a.db.Exec(ctx, `DELETE FROM room_waiting WHERE room_id = (SELECT room_id FROM timer_runs WHERE id = $1)`, runID)
	return true
}

// timerParticipants returns a run's participants with the identity bits the
// soleParticipant reports whether userID is the only one in the room's live
// run. It gates the controls whose cost is borne by everyone else — skipping a
// check-in room's break, so far — where being alone means there is no one else
// to bear it. False when nothing is running or the query fails, so the caller
// falls back to the shared-room rule rather than the permissive one.
func (a *app) soleParticipant(ctx context.Context, roomID, userID string) bool {
	var alone bool
	err := a.db.QueryRow(ctx, `
		SELECT count(*) = 1 AND bool_and(tp.user_id = $2)
		FROM timer_participants tp
		JOIN timer_runs tr ON tr.id = tp.timer_run_id
		WHERE tr.room_id = $1 AND tr.ended_at IS NULL AND tr.phase <> 'ended'`,
		roomID, userID).Scan(&alone)
	return err == nil && alone
}

// avatar stack needs (id + avatar presence/version), not just display names,
// so participant lists can show profile pictures like every other surface.
func (a *app) timerParticipants(ctx context.Context, runID string) ([]user, error) {
	rows, err := a.db.Query(ctx, `
		SELECT u.id, u.display_name, u.avatar IS NOT NULL,
			COALESCE(EXTRACT(EPOCH FROM u.avatar_updated_at), 0)::bigint, tp.confirmed_session
		FROM timer_participants tp JOIN users u ON u.id = tp.user_id
		WHERE tp.timer_run_id = $1 ORDER BY tp.joined_at`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []user
	for rows.Next() {
		var m user
		if rows.Scan(&m.ID, &m.DisplayName, &m.HasAvatar, &m.AvatarVersion, &m.ConfirmedSession) == nil {
			members = append(members, m)
		}
	}
	return members, rows.Err()
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
	roster, err := a.roomRoster(ctx, room{ID: roomID})
	if err != nil {
		return nil, err
	}
	members := make([]user, 0, len(roster))
	for _, m := range roster {
		members = append(members, m.User)
	}
	return members, nil
}

// roomMember is a roster row: who they are, and what they may do in this room.
type roomMember struct {
	User user
	// Role is the stored room_members.role. The creator's row usually reads
	// "member" -- Creator is what actually confers their authority, so never
	// infer rights from Role alone; use IsAdmin.
	Role    string
	Creator bool
}

// IsAdmin reports whether this member holds room-admin rights. It is the same
// rule canAdminRoomAs applies, read from a roster row instead of a lookup: the
// creator always, anyone else only by role.
func (m roomMember) IsAdmin() bool {
	return m.Creator || m.Role == roomRoleAdmin
}

// roomRoster lists a room's members with their roles, creator first, then
// admins, then everyone else alphabetically -- the order the members page
// reads top to bottom, so the people who can act on the room are together.
func (a *app) roomRoster(ctx context.Context, rm room) ([]roomMember, error) {
	creatorID := rm.CreatorID
	if creatorID == "" {
		// Callers that only have a room id (roomMemberUsers) still get a
		// correct roster; one extra read is cheaper than a wrong Creator flag.
		_ = a.db.QueryRow(ctx, `SELECT creator_id FROM rooms WHERE id = $1`, rm.ID).Scan(&creatorID)
	}
	rows, err := a.db.Query(ctx, `
		SELECT u.id, u.username, u.display_name, u.avatar IS NOT NULL,
			COALESCE(EXTRACT(EPOCH FROM u.avatar_updated_at), 0)::bigint, rm.role
		FROM room_members rm
		JOIN users u ON u.id = rm.user_id
		WHERE rm.room_id = $1
		ORDER BY (u.id = $2) DESC, (rm.role = 'admin') DESC, u.display_name`, rm.ID, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []roomMember
	for rows.Next() {
		var m roomMember
		if err := rows.Scan(&m.User.ID, &m.User.Username, &m.User.DisplayName,
			&m.User.HasAvatar, &m.User.AvatarVersion, &m.Role); err != nil {
			return nil, err
		}
		m.Creator = m.User.ID == creatorID && creatorID != ""
		members = append(members, m)
	}
	return members, rows.Err()
}

// setRoomMemberRole promotes or demotes a member. The creator is refused
// outright: their authority comes from rooms.creator_id, so a role row could
// only ever disagree with it.
func (a *app) setRoomMemberRole(ctx context.Context, rm room, targetID, role string) error {
	if targetID == rm.CreatorID {
		return errors.New("the room's creator is always an admin")
	}
	if role != roomRoleMember && role != roomRoleAdmin {
		return errors.New("unknown room role")
	}
	tag, err := a.db.Exec(ctx,
		`UPDATE room_members SET role = $1 WHERE room_id = $2 AND user_id = $3`, role, rm.ID, targetID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("that person isn't in this room")
	}
	return nil
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
	// Ported to Vue; data comes from /api/room/{code}/members with the same
	// gates, which stayed above at page level.
	a.spaPage(w, r)
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
			COALESCE(EXTRACT(EPOCH FROM u.avatar_updated_at), 0)::bigint,
			COALESCE(u.timezone, '')
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = $1 AND s.expires_at > now()`, hashToken(cookie.Value)).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.HasAvatar, &u.AvatarVersion, &u.Timezone)
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
	var pendingOwner *string
	var transferAt *time.Time
	err := a.db.QueryRow(ctx, `SELECT id, code, name, creator_id, focus_minutes, break_minutes, auto_sessions, auto_roll, ephemeral, require_checkin, pending_owner_id, ownership_transfer_at FROM rooms WHERE UPPER(code) = $1`, code).Scan(&rm.ID, &rm.Code, &rm.Name, &rm.CreatorID, &rm.FocusMinutes, &rm.BreakMinutes, &rm.AutoSessions, &rm.AutoRoll, &rm.Ephemeral, &rm.RequireCheckin, &pendingOwner, &transferAt)
	if err != nil {
		return room{}, false
	}
	if pendingOwner != nil {
		rm.PendingOwnerID = *pendingOwner
	}
	if transferAt != nil {
		rm.OwnershipTransferAt = *transferAt
	}
	// Settling here, on the read, is what makes the window survive a sleeping
	// instance: a room whose deadline passed while nobody was looking hands
	// over the moment someone next opens it, rather than waiting for a timer
	// that died with the process.
	return a.settleOwnershipTransfer(ctx, rm), true
}

// ownershipTransferWindow is how long a transfer can be taken back. Short
// enough that the new owner isn't left waiting, long enough to undo the click
// you didn't mean to make.
const ownershipTransferWindow = 10 * time.Minute

// settleOwnershipTransfer applies a due transfer and returns the room as it
// now stands. A transfer that is still within its window, or whose target has
// since left or deleted their account, is left alone -- the latter clears
// itself so the room doesn't carry a transfer that can never complete.
func (a *app) settleOwnershipTransfer(ctx context.Context, rm room) room {
	if rm.OwnershipTransferAt.IsZero() || time.Now().Before(rm.OwnershipTransferAt) {
		return rm
	}
	if rm.PendingOwnerID == "" || !a.isRoomMember(ctx, rm.ID, rm.PendingOwnerID) {
		_, _ = a.db.Exec(ctx, `UPDATE rooms SET pending_owner_id = NULL, ownership_transfer_at = NULL WHERE id = $1`, rm.ID)
		rm.PendingOwnerID, rm.OwnershipTransferAt = "", time.Time{}
		return rm
	}
	previous := rm.CreatorID
	// One statement, guarded on the deadline still being the one we read, so
	// two concurrent readers can't both believe they performed the handover.
	tag, err := a.db.Exec(ctx, `
		UPDATE rooms
		SET creator_id = pending_owner_id, pending_owner_id = NULL, ownership_transfer_at = NULL, updated_at = now()
		WHERE id = $1 AND ownership_transfer_at = $2`, rm.ID, rm.OwnershipTransferAt)
	if err != nil || tag.RowsAffected() == 0 {
		return rm
	}
	// The outgoing owner stays in the room as an admin: they were its
	// authority a moment ago, and dropping them to a plain member would be a
	// surprise nobody asked for.
	_, _ = a.db.Exec(ctx, `UPDATE room_members SET role = $1 WHERE room_id = $2 AND user_id = $3`, roomRoleAdmin, rm.ID, previous)
	rm.CreatorID, rm.PendingOwnerID, rm.OwnershipTransferAt = rm.PendingOwnerID, "", time.Time{}
	a.hub.broadcast(rm.Code, "members")
	return rm
}

// startOwnershipTransfer records a handover for later. Only the current owner
// may start one, only onto an existing room admin -- which is what keeps
// ownership off every row of the roster -- and never on a temporary room,
// which will not outlive the window.
func (a *app) startOwnershipTransfer(ctx context.Context, rm room, actorID, targetID string) error {
	if rm.CreatorID != actorID {
		return errors.New("only the room's owner can transfer it")
	}
	if rm.Ephemeral {
		return errors.New("a temporary room can't change hands — it disappears when its run ends")
	}
	if targetID == "" || targetID == rm.CreatorID {
		return errors.New("choose someone else to hand the room to")
	}
	if memberRole := a.roomMemberRole(ctx, rm.ID, targetID); memberRole != roomRoleAdmin {
		return errors.New("make them an admin first, then hand the room over")
	}
	_, err := a.db.Exec(ctx, `
		UPDATE rooms SET pending_owner_id = $1, ownership_transfer_at = now() + $2::interval WHERE id = $3`,
		targetID, ownershipTransferWindow.String(), rm.ID)
	return err
}

// cancelOwnershipTransfer takes back a transfer that hasn't settled yet. Only
// the owner who started it can: the recipient is never shown the pending
// handover, so there is nothing for them to accept or refuse.
func (a *app) cancelOwnershipTransfer(ctx context.Context, rm room, actorID string) error {
	if rm.CreatorID != actorID {
		return errors.New("only the room's owner can cancel a transfer")
	}
	tag, err := a.db.Exec(ctx, `
		UPDATE rooms SET pending_owner_id = NULL, ownership_transfer_at = NULL
		WHERE id = $1 AND ownership_transfer_at IS NOT NULL`, rm.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("that transfer has already gone through")
	}
	return nil
}

// roomMemberRole reads a member's stored role, or "" when they aren't in the
// room at all.
func (a *app) roomMemberRole(ctx context.Context, roomID, userID string) string {
	var role string
	if err := a.db.QueryRow(ctx,
		`SELECT role FROM room_members WHERE room_id = $1 AND user_id = $2`, roomID, userID).Scan(&role); err != nil {
		return ""
	}
	return role
}

func (a *app) isRoomMember(ctx context.Context, roomID, userID string) bool {
	var exists bool
	err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM room_members WHERE room_id = $1 AND user_id = $2)`, roomID, userID).Scan(&exists)
	return err == nil && exists
}

func (a *app) addRoomMember(ctx context.Context, roomID, userID string) {
	_, _ = a.db.Exec(ctx, `INSERT INTO room_members (room_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, roomID, userID)
}

// Room-level roles (migration 018). These are not the site-wide users.role
// values from 005: a room admin's authority stops at the edge of one room.
const (
	roomRoleMember = "member"
	roomRoleAdmin  = "admin"
)

// canAdminRoomAs is the room-admin decision itself, split out from the lookup
// so it can be tested without a database -- the same shape as mergeRoomConfig.
// memberRole is the caller's room_members.role, or "" when they aren't a
// member at all.
//
// The creator is always an admin, and deliberately without consulting
// memberRole: rooms.creator_id is the ownership record, so authority survives
// a membership row that is missing, or that predates migration 018.
func canAdminRoomAs(creatorID, userID, memberRole string) bool {
	if userID == "" {
		return false
	}
	if userID == creatorID {
		return true
	}
	return memberRole == roomRoleAdmin
}

// canAdminRoom reports whether a user may take room-admin actions in rm:
// changing the room's sound, and managing its membership.
//
// It is narrower than the name suggests, on purpose. Room settings (timer,
// sessions, check-in, auto-breaks) stay open to every member so a room can
// organise itself when no admin is around, deleting a room stays with the
// creator alone, and nothing here exposes another member's focus data. Add a
// caller only for an action that genuinely needs that narrow authority.
func (a *app) canAdminRoom(ctx context.Context, rm room, userID string) bool {
	if userID == "" {
		return false
	}
	if userID == rm.CreatorID {
		return true
	}
	var role string
	if err := a.db.QueryRow(ctx,
		`SELECT role FROM room_members WHERE room_id = $1 AND user_id = $2`, rm.ID, userID).Scan(&role); err != nil {
		return false
	}
	return canAdminRoomAs(rm.CreatorID, userID, role)
}

func (a *app) roomsForUser(ctx context.Context, userID string) ([]room, error) {
	// Ephemeral focus rooms are deliberately excluded: they're disposable and
	// live only on their own /f/{code} screen, never in the room list.
	rows, err := a.db.Query(ctx, `SELECT r.id, r.code, r.name, r.creator_id, r.focus_minutes, r.break_minutes, r.auto_sessions, r.auto_roll, r.require_checkin FROM room_members rm JOIN rooms r ON r.id = rm.room_id WHERE rm.user_id = $1 AND r.ephemeral = false ORDER BY r.updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rooms []room
	for rows.Next() {
		var rm room
		if rows.Scan(&rm.ID, &rm.Code, &rm.Name, &rm.CreatorID, &rm.FocusMinutes, &rm.BreakMinutes, &rm.AutoSessions, &rm.AutoRoll, &rm.RequireCheckin) == nil {
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
	rows, err := a.db.Query(ctx, `SELECT t.id, t.text, t.done, t.removed, u.display_name, t.user_id, t.created_at,
			u.avatar IS NOT NULL, COALESCE(EXTRACT(EPOCH FROM u.avatar_updated_at), 0)::bigint
		FROM todos t JOIN users u ON u.id = t.user_id WHERE t.room_id = $1 ORDER BY t.removed, t.done, t.created_at DESC`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var todos []todo
	for rows.Next() {
		var t todo
		if rows.Scan(&t.ID, &t.Text, &t.Done, &t.Removed, &t.DisplayName, &t.UserID, &t.CreatedAt,
			&t.HasAvatar, &t.AvatarVersion) == nil {
			todos = append(todos, t)
		}
	}
	return todos, nil
}

// userToday is the calendar date it currently is where the user is, derived
// from the IANA zone their browser reported. Users who have never loaded the
// SPA (Discord-only accounts, or anyone since before timezones were recorded)
// have no zone stored and read as UTC — the behaviour the whole app had
// before. Callers that can express the date in SQL should do so inline; this
// exists for the ones that need it back in Go, like streak arithmetic.
func (a *app) userToday(ctx context.Context, userID string) time.Time {
	var d time.Time
	if err := a.db.QueryRow(ctx, `
		SELECT (now() AT TIME ZONE COALESCE(timezone, 'UTC'))::date
		FROM users WHERE id = $1`, userID).Scan(&d); err != nil {
		now := time.Now().UTC()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	}
	return d
}

func (a *app) activity(ctx context.Context, userID string) (heatmapData, error) {
	// The grid has to end on the viewer's today, not the server's: on a UTC
	// box an evening in New York is already tomorrow, so the last square would
	// be a day the user hasn't reached yet and today's minutes would land one
	// cell early.
	rows, err := a.db.Query(ctx, `
		WITH today AS (
			SELECT (now() AT TIME ZONE COALESCE(timezone, 'UTC'))::date AS d FROM users WHERE id = $1
		)
		SELECT g::date, COALESCE(a.focus_minutes, 0)
		FROM today, generate_series(today.d - INTERVAL '364 days', today.d, INTERVAL '1 day') g
		LEFT JOIN activity a ON a.user_id = $1 AND a.activity_date = g::date
		ORDER BY g`, userID)
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
	// Drop the channel once its last viewer goes: the map is keyed by room
	// code and by per-user channel, so leaving the empty inner map behind
	// meant every ephemeral room and every user who ever connected kept a
	// permanent entry in a process that runs for weeks.
	if len(h.rooms[code]) == 0 {
		delete(h.rooms, code)
	}
}

// broadcastTimeout bounds how long one connection may hold up a broadcast.
const broadcastTimeout = 2 * time.Second

func (h *hub) broadcast(code, msg string) {
	h.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(h.rooms[code]))
	for c := range h.rooms[code] {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	// One goroutine and one deadline per connection. Writing sequentially
	// under a single shared deadline meant a connection that had stopped
	// draining (a phone that slept mid-run) burned the entire budget, and
	// every remaining member then failed instantly against the dead context
	// — one stalled viewer silently cost the whole room its phase update.
	// Conn.Write is safe to call concurrently.
	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Add(1)
		go func(c *websocket.Conn) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), broadcastTimeout)
			defer cancel()
			_ = c.Write(ctx, websocket.MessageText, []byte(msg))
		}(c)
	}
	wg.Wait()
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

// randomCode formats a new room's join code. 6 random bytes (48 bits) keeps
// codes short enough to read aloud while putting guessing far out of reach of
// the join rate limits; codes minted at the old 4-byte length keep working.
func randomCode() string {
	return fmt.Sprintf("%s-%s", randomHex(3), randomHex(3))
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
