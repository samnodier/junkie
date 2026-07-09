package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
	"nhooyr.io/websocket"
)

type app struct {
	db        *pgxpool.Pool
	templates *template.Template
	hub       *hub
}

type user struct {
	ID          string
	Username    string
	DisplayName string
}

type room struct {
	ID           string
	Code         string
	Name         string
	CreatorID    string
	FocusMinutes int
	BreakMinutes int
	AutoSessions int
}

type todo struct {
	ID          string
	Text        string
	Done        bool
	Removed     bool
	DisplayName string
	CreatedAt   time.Time
}

type timerRun struct {
	ID             string
	Phase          string
	FocusMinutes   int
	BreakMinutes   int
	TotalSessions  int
	CurrentSession int
	PhaseStartedAt time.Time
	PhaseEndsAt    time.Time
	Participant    bool
	Participants   []string
}

type pageData struct {
	Title                string
	User                 user
	Error                string
	Rooms                []room
	Room                 room
	PersonalTodos        []todo
	RoomTodos            []todo
	Timer                *timerRun
	SoloTimer            *timerRun
	Activity             []activityDay
	ActivityMonths       []activityMonth
	ActivityWeeks        int
	ActivityTotalMinutes int
	FocusMode            bool
	GuestMode            bool
	Next                 string
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
	}
	if err := a.migrate(ctx); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /assets/app.css", a.css)
	mux.HandleFunc("GET /assets/guest.js", a.serveStaticAsset("guest.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("GET /assets/notifications.js", a.serveStaticAsset("notifications.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("GET /assets/icon.svg", a.serveStaticAsset("icon.svg", "image/svg+xml"))
	mux.HandleFunc("GET /", a.home)
	mux.HandleFunc("GET /signup", a.signupForm)
	mux.HandleFunc("POST /signup", a.signup)
	mux.HandleFunc("GET /login", a.loginForm)
	mux.HandleFunc("POST /login", a.login)
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("GET /dashboard", a.home)
	mux.HandleFunc("GET /profile", a.profilePage)
	mux.HandleFunc("POST /todos", a.requireAuth(a.createPersonalTodo))
	mux.HandleFunc("POST /solo/start", a.requireAuth(a.startSoloTimer))
	mux.HandleFunc("POST /solo/cancel", a.requireAuth(a.cancelSoloTimer))
	mux.HandleFunc("POST /todo/", a.requireAuth(a.todoAction))
	mux.HandleFunc("POST /rooms", a.requireAuth(a.createRoom))
	mux.HandleFunc("GET /r/", a.requireAuth(a.roomPage))
	mux.HandleFunc("POST /r/", a.requireAuth(a.roomAction))
	mux.HandleFunc("GET /ws/r/", a.requireAuth(a.roomWS))

	addr := getenv("ADDR", ":8080")
	log.Printf("junkie listening on http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
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
	a.render(w, "signup", a.authPageData(r, "Sign up", safeNext(r.URL.Query().Get("next")), ""))
}

func (a *app) signup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	username := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	password := r.FormValue("password")
	next := safeNext(r.FormValue("next"))
	if username == "" || password == "" {
		a.render(w, "signup", a.authPageData(r, "Sign up", next, "Username and password are required."))
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
		a.render(w, "signup", a.authPageData(r, "Sign up", next, "That username is already taken."))
		return
	}
	a.createSession(w, r, id)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (a *app) loginForm(w http.ResponseWriter, r *http.Request) {
	a.render(w, "login", a.authPageData(r, "Sign in", safeNext(r.URL.Query().Get("next")), ""))
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	username := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	password := r.FormValue("password")
	next := safeNext(r.FormValue("next"))
	var id, hash string
	err := a.db.QueryRow(ctx, `SELECT id, password_hash FROM users WHERE username = $1`, username).Scan(&id, &hash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		a.render(w, "login", a.authPageData(r, "Sign in", next, "Username or password is incorrect."))
		return
	}
	a.createSession(w, r, id)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (a *app) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("junkie_session"); err == nil {
		_, _ = a.db.Exec(r.Context(), `DELETE FROM sessions WHERE token = $1`, cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "junkie_session", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
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
	a.render(w, "profile", pageData{
		Title:                "Profile",
		User:                 u,
		Rooms:                rooms,
		Activity:             heatmap.Cells,
		ActivityMonths:       heatmap.Months,
		ActivityWeeks:        heatmap.Weeks,
		ActivityTotalMinutes: heatmap.TotalMinutes,
	})
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
	a.render(w, "dashboard", pageData{
		Title:         "Dashboard",
		User:          u,
		PersonalTodos: todos,
		Rooms:         rooms,
		SoloTimer:     soloTimer,
	})
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
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (a *app) cancelSoloTimer(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	_, _ = a.db.Exec(r.Context(), `UPDATE timer_runs SET phase = 'ended', ended_at = now() WHERE user_id = $1 AND room_id IS NULL AND ended_at IS NULL`, u.ID)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (a *app) createPersonalTodo(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	text := strings.TrimSpace(r.FormValue("text"))
	if text != "" {
		_, _ = a.db.Exec(r.Context(), `INSERT INTO todos (user_id, text) VALUES ($1, $2)`, u.ID, text)
	}
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
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
	var roomCode string
	_ = a.db.QueryRow(r.Context(), `SELECT COALESCE(r.code, '') FROM todos t LEFT JOIN rooms r ON r.id = t.room_id WHERE t.id = $1`, id).Scan(&roomCode)
	switch action {
	case "toggle":
		_, _ = a.db.Exec(r.Context(), `UPDATE todos SET done = NOT done, updated_at = now() WHERE id = $1 AND (user_id = $2 OR room_id IS NOT NULL)`, id, u.ID)
	case "remove":
		_, _ = a.db.Exec(r.Context(), `UPDATE todos SET removed = true, updated_at = now() WHERE id = $1 AND (user_id = $2 OR room_id IS NOT NULL)`, id, u.ID)
	case "delete":
		_, _ = a.db.Exec(r.Context(), `DELETE FROM todos WHERE id = $1 AND (user_id = $2 OR room_id IS NOT NULL)`, id, u.ID)
	default:
		http.NotFound(w, r)
		return
	}
	if roomCode != "" {
		a.hub.broadcast(roomCode, "todos")
		http.Redirect(w, r, "/r/"+roomCode, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (a *app) createRoom(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = u.DisplayName + "'s focus room"
	}
	code := randomCode()
	_, err := a.db.Exec(r.Context(), `INSERT INTO rooms (code, name, creator_id) VALUES ($1, $2, $3)`, code, name, u.ID)
	if err != nil {
		http.Error(w, "could not create room", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/r/"+code, http.StatusSeeOther)
}

func (a *app) roomPage(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	code := strings.TrimPrefix(r.URL.Path, "/r/")
	rm, ok := a.findRoom(r.Context(), code)
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = a.db.Exec(r.Context(), `INSERT INTO room_members (room_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, rm.ID, u.ID)
	timer, _ := a.normalizeTimer(r.Context(), rm.ID, u.ID)
	todos, _ := a.roomTodos(r.Context(), rm.ID)
	focusMode := timer != nil && timer.Phase == "focus" && timer.Participant
	a.render(w, "room", pageData{Title: rm.Name, User: u, Room: rm, RoomTodos: todos, Timer: timer, FocusMode: focusMode})
}

func (a *app) roomAction(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/r/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	code, action := parts[0], parts[1]
	rm, ok := a.findRoom(r.Context(), code)
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = a.db.Exec(r.Context(), `INSERT INTO room_members (room_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, rm.ID, u.ID)
	switch action {
	case "rename":
		name := strings.TrimSpace(r.FormValue("name"))
		if name != "" {
			_, _ = a.db.Exec(r.Context(), `UPDATE rooms SET name = $1, updated_at = now() WHERE id = $2`, name, rm.ID)
		}
	case "settings":
		focus := clampInt(r.FormValue("focus_minutes"), 5, 180, rm.FocusMinutes)
		breaks := clampInt(r.FormValue("break_minutes"), 1, 60, rm.BreakMinutes)
		sessions := clampInt(r.FormValue("auto_sessions"), 1, 12, rm.AutoSessions)
		_, _ = a.db.Exec(r.Context(), `UPDATE rooms SET focus_minutes = $1, break_minutes = $2, auto_sessions = $3, updated_at = now() WHERE id = $4`, focus, breaks, sessions, rm.ID)
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
		text := strings.TrimSpace(r.FormValue("text"))
		if text != "" {
			_, _ = a.db.Exec(r.Context(), `INSERT INTO todos (user_id, room_id, text) VALUES ($1, $2, $3)`, u.ID, rm.ID, text)
		}
	case "timer-start":
		if active, _ := a.activeTimer(r.Context(), rm.ID, u.ID); active == nil || active.Phase == "ended" {
			ends := time.Now().Add(time.Duration(rm.FocusMinutes) * time.Minute)
			var runID string
			err := a.db.QueryRow(r.Context(), `INSERT INTO timer_runs (room_id, host_user_id, phase, focus_minutes, break_minutes, total_sessions, phase_ends_at) VALUES ($1, $2, 'focus', $3, $4, $5, $6) RETURNING id`, rm.ID, u.ID, rm.FocusMinutes, rm.BreakMinutes, rm.AutoSessions, ends).Scan(&runID)
			if err == nil {
				_, _ = a.db.Exec(r.Context(), `INSERT INTO timer_participants (timer_run_id, user_id) SELECT $1, user_id FROM room_members WHERE room_id = $2 ON CONFLICT DO NOTHING`, runID, rm.ID)
			}
		}
	case "timer-join":
		timer, _ := a.normalizeTimer(r.Context(), rm.ID, u.ID)
		if timer != nil && timer.Phase == "break" {
			_, _ = a.db.Exec(r.Context(), `INSERT INTO timer_participants (timer_run_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, timer.ID, u.ID)
		}
	case "timer-leave":
		timer, _ := a.normalizeTimer(r.Context(), rm.ID, u.ID)
		if timer != nil && timer.Participant {
			_, _ = a.db.Exec(r.Context(), `DELETE FROM timer_participants WHERE timer_run_id = $1 AND user_id = $2`, timer.ID, u.ID)
		}
	default:
		http.NotFound(w, r)
		return
	}
	a.hub.broadcast(code, action)
	http.Redirect(w, r, "/r/"+code, http.StatusSeeOther)
}

func (a *app) roomWS(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimPrefix(r.URL.Path, "/ws/r/")
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
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

func (a *app) normalizeTimer(ctx context.Context, roomID, userID string) (*timerRun, error) {
	timer, err := a.activeTimer(ctx, roomID, userID)
	if err != nil || timer == nil {
		return timer, err
	}
	now := time.Now()
	for timer.Phase != "ended" && now.After(timer.PhaseEndsAt) {
		if timer.Phase == "focus" {
			_, _ = a.db.Exec(ctx, `
				INSERT INTO activity (user_id, activity_date, focus_minutes)
				SELECT user_id, CURRENT_DATE, $1 FROM timer_participants WHERE timer_run_id = $2
				ON CONFLICT (user_id, activity_date)
				DO UPDATE SET focus_minutes = activity.focus_minutes + EXCLUDED.focus_minutes`, timer.FocusMinutes, timer.ID)
			if timer.CurrentSession >= timer.TotalSessions {
				_, _ = a.db.Exec(ctx, `UPDATE timer_runs SET phase = 'ended', ended_at = now() WHERE id = $1`, timer.ID)
				break
			}
			timer.Phase = "break"
			timer.PhaseStartedAt = now
			timer.PhaseEndsAt = now.Add(time.Duration(timer.BreakMinutes) * time.Minute)
			_, _ = a.db.Exec(ctx, `UPDATE timer_runs SET phase = 'break', phase_started_at = $1, phase_ends_at = $2 WHERE id = $3`, timer.PhaseStartedAt, timer.PhaseEndsAt, timer.ID)
		} else if timer.Phase == "break" {
			timer.Phase = "focus"
			timer.CurrentSession++
			timer.PhaseStartedAt = now
			timer.PhaseEndsAt = now.Add(time.Duration(timer.FocusMinutes) * time.Minute)
			_, _ = a.db.Exec(ctx, `UPDATE timer_runs SET phase = 'focus', current_session = $1, phase_started_at = $2, phase_ends_at = $3 WHERE id = $4`, timer.CurrentSession, timer.PhaseStartedAt, timer.PhaseEndsAt, timer.ID)
		}
	}
	if timer.Phase == "ended" {
		return nil, nil
	}
	timer.Participants, _ = a.timerParticipants(ctx, timer.ID)
	return timer, nil
}

func (a *app) normalizeSoloTimer(ctx context.Context, userID string) (*timerRun, error) {
	timer, err := a.activeSoloTimer(ctx, userID)
	if err != nil || timer == nil {
		return timer, err
	}
	if time.Now().After(timer.PhaseEndsAt) {
		_, _ = a.db.Exec(ctx, `
			INSERT INTO activity (user_id, activity_date, focus_minutes)
			VALUES ($1, CURRENT_DATE, $2)
			ON CONFLICT (user_id, activity_date)
			DO UPDATE SET focus_minutes = activity.focus_minutes + EXCLUDED.focus_minutes`, userID, timer.FocusMinutes)
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
			EXISTS (SELECT 1 FROM timer_participants tp WHERE tp.timer_run_id = tr.id AND tp.user_id = $2)
		FROM timer_runs tr
		WHERE tr.room_id = $1 AND tr.ended_at IS NULL
		ORDER BY tr.created_at DESC
		LIMIT 1`, roomID, userID).Scan(&t.ID, &t.Phase, &t.FocusMinutes, &t.BreakMinutes, &t.TotalSessions, &t.CurrentSession, &t.PhaseStartedAt, &t.PhaseEndsAt, &t.Participant)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.Participants, _ = a.timerParticipants(ctx, t.ID)
	return &t, nil
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

func (a *app) authPageData(r *http.Request, title, next, errMsg string) pageData {
	data := pageData{Title: title, Next: next, Error: errMsg}
	if u, ok := a.currentUser(r); ok {
		data.User = u
	}
	return data
}

func (a *app) currentUser(r *http.Request) (user, bool) {
	cookie, err := r.Cookie("junkie_session")
	if err != nil {
		return user{}, false
	}
	var u user
	err = a.db.QueryRow(r.Context(), `
		SELECT u.id, u.username, u.display_name
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = $1 AND s.expires_at > now()`, cookie.Value).Scan(&u.ID, &u.Username, &u.DisplayName)
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
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "/"
	}
	return raw
}

func (a *app) createSession(w http.ResponseWriter, r *http.Request, userID string) {
	token := randomHex(32)
	expires := time.Now().Add(30 * 24 * time.Hour)
	_, _ = a.db.Exec(r.Context(), `INSERT INTO sessions (token, user_id, expires_at) VALUES ($1, $2, $3)`, token, userID, expires)
	http.SetCookie(w, &http.Cookie{Name: "junkie_session", Value: token, Path: "/", Expires: expires, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func (a *app) findRoom(ctx context.Context, code string) (room, bool) {
	var rm room
	err := a.db.QueryRow(ctx, `SELECT id, code, name, creator_id, focus_minutes, break_minutes, auto_sessions FROM rooms WHERE code = $1`, code).Scan(&rm.ID, &rm.Code, &rm.Name, &rm.CreatorID, &rm.FocusMinutes, &rm.BreakMinutes, &rm.AutoSessions)
	return rm, err == nil
}

func (a *app) roomsForUser(ctx context.Context, userID string) ([]room, error) {
	rows, err := a.db.Query(ctx, `SELECT r.id, r.code, r.name, r.creator_id, r.focus_minutes, r.break_minutes, r.auto_sessions FROM room_members rm JOIN rooms r ON r.id = rm.room_id WHERE rm.user_id = $1 ORDER BY r.updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rooms []room
	for rows.Next() {
		var rm room
		if rows.Scan(&rm.ID, &rm.Code, &rm.Name, &rm.CreatorID, &rm.FocusMinutes, &rm.BreakMinutes, &rm.AutoSessions) == nil {
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
	rows, err := a.db.Query(ctx, `SELECT t.id, t.text, t.done, t.removed, u.display_name, t.created_at FROM todos t JOIN users u ON u.id = t.user_id WHERE t.room_id = $1 ORDER BY t.removed, t.done, t.created_at DESC`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var todos []todo
	for rows.Next() {
		var t todo
		if rows.Scan(&t.ID, &t.Text, &t.Done, &t.Removed, &t.DisplayName, &t.CreatedAt) == nil {
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.templates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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

func randomCode() string {
	return fmt.Sprintf("%s-%s", randomHex(2), randomHex(2))
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
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
