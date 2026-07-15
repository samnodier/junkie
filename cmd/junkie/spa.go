package main

import (
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"net/http"
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
