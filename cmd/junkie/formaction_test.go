package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func formActionServe(t *testing.T, header bool, method string, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/r/ABC/todos", strings.NewReader("text=x"))
	if header {
		req.Header.Set(formActionHeader, "1")
	}
	rec := httptest.NewRecorder()
	formActionJSON(h).ServeHTTP(rec, req)
	return rec
}

func redirectTo(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}

func TestFormActionJSON(t *testing.T) {
	decode := func(rec *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("body %q is not JSON: %v", rec.Body.String(), err)
		}
		return out
	}

	t.Run("success redirect becomes ok", func(t *testing.T) {
		rec := formActionServe(t, true, http.MethodPost, redirectTo("/r/ABC"))
		if rec.Code != http.StatusOK || rec.Header().Get("Location") != "" {
			t.Fatalf("got %d Location=%q, want 200 and no Location", rec.Code, rec.Header().Get("Location"))
		}
		if out := decode(rec); out["ok"] != true || out["location"] != "/r/ABC" {
			t.Fatalf("unexpected body %v", out)
		}
	})

	t.Run("error redirect carries the message", func(t *testing.T) {
		rec := formActionServe(t, true, http.MethodPost, redirectTo("/r/ABC?error=Timer+settings+can%27t+change"))
		out := decode(rec)
		if rec.Code != http.StatusOK || out["ok"] != false || out["error"] != "Timer settings can't change" {
			t.Fatalf("got %d %v", rec.Code, out)
		}
	})

	t.Run("login bounce is a 401", func(t *testing.T) {
		rec := formActionServe(t, true, http.MethodPost, redirectTo("/login?next=%2Fr%2FABC%2Ftodos"))
		if out := decode(rec); rec.Code != http.StatusUnauthorized || out["ok"] != false {
			t.Fatalf("got %d %v", rec.Code, out)
		}
	})

	t.Run("without the header the redirect is untouched", func(t *testing.T) {
		rec := formActionServe(t, false, http.MethodPost, redirectTo("/r/ABC"))
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/r/ABC" {
			t.Fatalf("got %d Location=%q", rec.Code, rec.Header().Get("Location"))
		}
	})

	t.Run("non-redirect responses pass through", func(t *testing.T) {
		rec := formActionServe(t, true, http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "not found") {
			t.Fatalf("got %d %q", rec.Code, rec.Body.String())
		}
	})
}
