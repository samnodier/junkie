package main

import (
	"mime"
	"net/http"
	"path"
	"strings"
)

// Phase-end notification sounds.
//
// The catalogue is static/sounds/sounds.json — {id, name, file} entries the
// SPA fetches to fill its picker. Playback needs only the file, so adding a
// sound is dropping a file in that directory and adding one line to the
// manifest; nothing in Go or the SPA has to change.
//
// Which sound plays (and whether one plays at all) is a per-browser choice
// kept in localStorage, so none of this is per-room or per-account state.
func (a *app) soundAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	// Flat directory: no separators, no traversal, no dotfiles.
	if name == "" || strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") {
		http.NotFound(w, r)
		return
	}
	data, err := staticAssets.ReadFile("static/sounds/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	serveStatic(w, r, contentType, data)
}
