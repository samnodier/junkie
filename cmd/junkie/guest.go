package main

import (
	"embed"
	"net/http"
)

//go:embed static/guest.js
var guestAssets embed.FS

func (a *app) guestJS(w http.ResponseWriter, r *http.Request) {
	data, err := guestAssets.ReadFile("static/guest.js")
	if err != nil {
		http.Error(w, "guest script missing", http.StatusInternalServerError)
		return
	}
	serveStatic(w, r, "application/javascript; charset=utf-8", data)
}
