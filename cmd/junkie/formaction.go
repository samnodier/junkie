package main

import (
	"encoding/json"
	"net/http"
	"net/url"
)

// formActionHeader marks a POST made by the SPA's fetch rather than by a
// real form submission.
const formActionHeader = "X-Junkie-Action"

// formActionJSON answers the SPA's form posts with JSON instead of a
// redirect.
//
// The mutation endpoints were written for plain HTML forms: they do the work
// and redirect back to the page, carrying any rejection in ?error=. The SPA
// posts to them with fetch, which follows that redirect and downloads the
// whole page shell only to read the error out of its URL -- a second
// request, with its own session lookup, between every click and the screen
// updating. A fetch that sets the header gets the same outcome as a small
// JSON body and the redirect never happens. Requests without the header
// (old cached bundles, curl, real forms) behave exactly as before.
func formActionJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get(formActionHeader) != "1" {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(&formActionWriter{ResponseWriter: w}, r)
	})
}

type formActionWriter struct {
	http.ResponseWriter
	intercepted bool
}

func (w *formActionWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *formActionWriter) WriteHeader(status int) {
	h := w.Header()
	loc := h.Get("Location")
	if status < 300 || status > 399 || loc == "" {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.intercepted = true
	h.Del("Location")
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "no-store")

	out := map[string]any{"ok": true, "location": loc}
	code := http.StatusOK
	if u, err := url.Parse(loc); err == nil {
		if u.Path == "/login" {
			// requireAuth bounced the request: the session is gone.
			code = http.StatusUnauthorized
			out = map[string]any{"ok": false, "error": "Your session has ended — sign in again.", "location": loc}
		} else if msg := u.Query().Get("error"); msg != "" {
			out = map[string]any{"ok": false, "error": msg, "location": loc}
		}
	}
	w.ResponseWriter.WriteHeader(code)
	_ = json.NewEncoder(w.ResponseWriter).Encode(out)
}

// Write drops whatever body the handler meant to send with its redirect.
func (w *formActionWriter) Write(b []byte) (int, error) {
	if w.intercepted {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}
