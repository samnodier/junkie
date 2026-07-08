package main

import (
	"embed"
	"net/http"
)

//go:embed static/*
var staticAssets embed.FS

func (a *app) serveStaticAsset(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := staticAssets.ReadFile("static/" + name)
		if err != nil {
			http.Error(w, "asset missing", http.StatusNotFound)
			return
		}
		serveStatic(w, r, contentType, data)
	}
}
