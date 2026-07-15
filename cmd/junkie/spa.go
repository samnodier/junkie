package main

import (
	"encoding/json"
	"log"
	"mime"
	"net/http"
	"path"
	"strings"
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
