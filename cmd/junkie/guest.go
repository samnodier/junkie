package main

import (
	"embed"
	"net/http"
)

//go:embed static/guest.js
var guestAssets embed.FS

func (a *app) guestJS(w http.ResponseWriter, _ *http.Request) {
	data, err := guestAssets.ReadFile("static/guest.js")
	if err != nil {
		http.Error(w, "guest script missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	_, _ = w.Write(data)
}
