package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// createKey runs the real create handler as u and returns the plaintext key.
func createKey(t *testing.T, a *app, u user, name, roomCode string) (int, map[string]any) {
	t.Helper()
	a.asUser(u)
	req := httptest.NewRequest(http.MethodPost, "/api/api-keys",
		strings.NewReader(url.Values{"name": {name}, "room": {roomCode}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.apiCreateAPIKey(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func mustKey(t *testing.T, a *app, u user, roomCode string) string {
	t.Helper()
	status, out := createKey(t, a, u, "Meeting notes", roomCode)
	if status != http.StatusOK {
		t.Fatalf("create key = %d (%v)", status, out)
	}
	return out["key"].(string)
}

// postTodos calls POST /api/todos the way an automation would: no session,
// just the bearer key.
func postTodos(t *testing.T, a *app, key, idemKey, body string) *httptest.ResponseRecorder {
	t.Helper()
	a.asUser(user{})
	req := httptest.NewRequest(http.MethodPost, "/api/todos", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	rec := httptest.NewRecorder()
	a.apiAddTodos(rec, req)
	return rec
}

func todoTexts(t *testing.T, a *app, userID string, roomID *string) []string {
	t.Helper()
	rows, err := a.db.Query(context.Background(),
		`SELECT text FROM todos WHERE user_id = $1 AND room_id IS NOT DISTINCT FROM $2 ORDER BY created_at, text`, userID, roomID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func TestAPIKeyAddsTodosToPrivateList(t *testing.T) {
	a := newTestApp(t)
	u := makeUser(t, a, "Meeting Person")
	key := mustKey(t, a, u, "")

	rec := postTodos(t, a, key, "", `{"todos":[{"text":"Acme: Send revised copy - Sep 26"},{"text":"Acme: Check GHL timezone - Sep 26"}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add = %d (%s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Added int
		List  string
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Added != 2 || out.List != "private" {
		t.Fatalf("response = %s", rec.Body.String())
	}
	if got := todoTexts(t, a, u.ID, nil); len(got) != 2 {
		t.Fatalf("private todos = %v, want 2", got)
	}
}

func TestAPIKeyForRoomAddsToThatRoomOnly(t *testing.T) {
	a := newTestApp(t)
	u := makeUser(t, a, "Room Person")
	rm := makeRoom(t, a, u)
	key := mustKey(t, a, u, rm.Code)

	if rec := postTodos(t, a, key, "", `{"todos":[{"text":"From the meeting"}]}`); rec.Code != http.StatusCreated {
		t.Fatalf("add = %d (%s)", rec.Code, rec.Body.String())
	}
	if got := todoTexts(t, a, u.ID, &rm.ID); len(got) != 1 {
		t.Fatalf("room todos = %v, want 1", got)
	}
	if got := todoTexts(t, a, u.ID, nil); len(got) != 0 {
		t.Fatalf("private todos = %v, want none", got)
	}

	// Leaving the room stops the key without deleting it.
	_, _ = a.db.Exec(context.Background(), `DELETE FROM room_members WHERE room_id = $1 AND user_id = $2`, rm.ID, u.ID)
	if rec := postTodos(t, a, key, "", `{"todos":[{"text":"After leaving"}]}`); rec.Code != http.StatusForbidden {
		t.Fatalf("add after leaving = %d, want 403", rec.Code)
	}
}

func TestAPIKeyCannotTargetSomeoneElsesRoom(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	outsider := makeUser(t, a, "Outsider")
	rm := makeRoom(t, a, owner)
	if status, _ := createKey(t, a, outsider, "Sneaky", rm.Code); status != http.StatusBadRequest {
		t.Fatalf("create key for a room you're not in = %d, want 400", status)
	}
}

func TestAPITodosRejectsBadKeysAndBodies(t *testing.T) {
	a := newTestApp(t)
	u := makeUser(t, a, "Validator")
	key := mustKey(t, a, u, "")

	cases := []struct {
		name, key, body string
		want            int
	}{
		{"no key", "", `{"todos":[{"text":"x"}]}`, http.StatusUnauthorized},
		{"wrong key", "jk_" + strings.Repeat("0", 64), `{"todos":[{"text":"x"}]}`, http.StatusUnauthorized},
		{"not json", key, `hello`, http.StatusBadRequest},
		{"empty list", key, `{"todos":[]}`, http.StatusBadRequest},
		{"blank text", key, `{"todos":[{"text":"   "}]}`, http.StatusBadRequest},
		{"too long", key, `{"todos":[{"text":"` + strings.Repeat("a", maxTodoTextLen+1) + `"}]}`, http.StatusBadRequest},
		{"too many", key, `{"todos":[` + strings.TrimSuffix(strings.Repeat(`{"text":"x"},`, maxAPITodosPerRequest+1), ",") + `]}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		if rec := postTodos(t, a, c.key, "", c.body); rec.Code != c.want {
			t.Errorf("%s: status = %d, want %d (%s)", c.name, rec.Code, c.want, rec.Body.String())
		}
	}
	// A rejected batch adds nothing, including the valid items in it.
	if got := todoTexts(t, a, u.ID, nil); len(got) != 0 {
		t.Fatalf("todos after rejected requests = %v, want none", got)
	}
}

func TestAPITodosFlattensToOneLine(t *testing.T) {
	a := newTestApp(t)
	u := makeUser(t, a, "Flattener")
	key := mustKey(t, a, u, "")
	rec := postTodos(t, a, key, "", `{"todos":[{"text":"  Acme:\nsend\u001b[31m copy\t- Sep 26 "}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add = %d (%s)", rec.Code, rec.Body.String())
	}
	got := todoTexts(t, a, u.ID, nil)
	if len(got) != 1 || got[0] != "Acme: send [31m copy - Sep 26" {
		t.Fatalf("stored = %q", got)
	}
}

// The reason idempotency exists: an automation that timed out retries, and
// the list must not end up with every todo twice.
func TestAPITodosIdempotencyKeyReplays(t *testing.T) {
	a := newTestApp(t)
	u := makeUser(t, a, "Retrier")
	key := mustKey(t, a, u, "")
	body := `{"todos":[{"text":"Once"},{"text":"Only once"}]}`

	first := postTodos(t, a, key, "zoom-123", body)
	if first.Code != http.StatusCreated {
		t.Fatalf("first = %d (%s)", first.Code, first.Body.String())
	}
	again := postTodos(t, a, key, "zoom-123", body)
	if again.Code != http.StatusCreated || again.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("retry = %d replayed=%q", again.Code, again.Header().Get("Idempotent-Replayed"))
	}
	// Stored as JSONB, so compare the content rather than the bytes.
	var r1, r2 map[string]any
	_ = json.Unmarshal(first.Body.Bytes(), &r1)
	_ = json.Unmarshal(again.Body.Bytes(), &r2)
	if r1["added"] != r2["added"] || len(r2["todos"].([]any)) != 2 {
		t.Fatalf("replayed body differs:\n%s\n%s", first.Body.String(), again.Body.String())
	}
	if got := todoTexts(t, a, u.ID, nil); len(got) != 2 {
		t.Fatalf("todos after retry = %v, want 2", got)
	}

	// Same key, different todos: a caller bug, refused.
	if rec := postTodos(t, a, key, "zoom-123", `{"todos":[{"text":"Something else"}]}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("reused key with new body = %d, want 422", rec.Code)
	}
	// A different key is a different call.
	if rec := postTodos(t, a, key, "zoom-456", body); rec.Code != http.StatusCreated || rec.Header().Get("Idempotent-Replayed") != "" {
		t.Fatalf("new idempotency key = %d", rec.Code)
	}
	if got := todoTexts(t, a, u.ID, nil); len(got) != 4 {
		t.Fatalf("todos = %v, want 4", got)
	}
}

func TestAPITodosRateLimitedPerKey(t *testing.T) {
	a := newTestApp(t)
	u := makeUser(t, a, "Spammer")
	key := mustKey(t, a, u, "")
	for i := 0; i < maxAPIRequestsPerMinute; i++ {
		if rec := postTodos(t, a, key, "", `{"todos":[{"text":"x"}]}`); rec.Code != http.StatusCreated {
			t.Fatalf("request %d = %d", i+1, rec.Code)
		}
	}
	rec := postTodos(t, a, key, "", `{"todos":[{"text":"x"}]}`)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("over the limit = %d retry-after=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestRevokedAPIKeyStopsWorking(t *testing.T) {
	a := newTestApp(t)
	u := makeUser(t, a, "Revoker")
	other := makeUser(t, a, "Other")
	status, out := createKey(t, a, u, "Zapier", "")
	if status != http.StatusOK {
		t.Fatal(out)
	}
	id, key := out["id"].(string), out["key"].(string)

	revoke := func(as user) int {
		a.asUser(as)
		req := httptest.NewRequest(http.MethodPost, "/api/api-keys/"+id+"/revoke", nil)
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		a.apiRevokeAPIKey(rec, req)
		return rec.Code
	}
	if code := revoke(other); code != http.StatusNotFound {
		t.Fatalf("someone else revoking = %d, want 404", code)
	}
	if rec := postTodos(t, a, key, "", `{"todos":[{"text":"still works"}]}`); rec.Code != http.StatusCreated {
		t.Fatalf("add before revoke = %d", rec.Code)
	}
	if code := revoke(u); code != http.StatusOK {
		t.Fatalf("revoke = %d", code)
	}
	if rec := postTodos(t, a, key, "", `{"todos":[{"text":"x"}]}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("add after revoke = %d, want 401", rec.Code)
	}
}

func TestAPIKeyListNeverShowsTheKey(t *testing.T) {
	a := newTestApp(t)
	u := makeUser(t, a, "Lister")
	key := mustKey(t, a, u, "")
	a.asUser(u)
	rec := httptest.NewRecorder()
	a.apiListAPIKeys(rec, httptest.NewRequest(http.MethodGet, "/api/api-keys", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), key) || strings.Contains(rec.Body.String(), hashToken(key)) {
		t.Fatalf("list leaks the key: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), key[:9]) {
		t.Fatalf("list lacks the prefix: %s", rec.Body.String())
	}
}
