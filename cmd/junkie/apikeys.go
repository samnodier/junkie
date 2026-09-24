package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
)

// API keys let something that isn't a browser add todos: the case that
// prompted them is a meeting-notes automation that reads a transcript and
// files what you said you'd do. See migrations/024_api_keys.sql for what a
// key can and can't reach.
//
// There is one endpoint, POST /api/todos, and deliberately no read, edit, or
// delete. A credential that sits in a third-party automation should be able
// to do the one thing that automation needs and nothing else.
const (
	apiKeyPrefix = "jk_"
	// maxAPIKeysPerUser keeps the profile list readable. One per automation
	// is the expected shape; ten is generous.
	maxAPIKeysPerUser = 10
	maxAPIKeyNameLen  = 60
	// maxAPITodosPerRequest caps one call. A meeting yields a handful of
	// commitments; a batch of hundreds means the extraction went wrong, and
	// the list should not be what pays for that.
	maxAPITodosPerRequest = 20
	// Per-key ceilings. A handful of meetings a day is the real load, so both
	// sit far above it and far below what would bury a todo list.
	maxAPIRequestsPerMinute = 10
	maxAPIRequestsPerDay    = 100
	// Unauthenticated attempts are capped per IP before the key is even
	// looked up, so guessing costs the guesser and not the database.
	maxAPIAttemptsPerIPMinute = 60
	maxIdempotencyKeyLen      = 255
	idempotencyRetention      = 24 * time.Hour
)

type apiKey struct {
	ID       string
	UserID   string
	RoomID   string // empty for the private list
	RoomCode string
}

// newAPIKey returns the plaintext key and the short prefix shown on the
// profile page. The prefix is not secret: it's there so a person with three
// keys can tell which one they pasted where.
func newAPIKey() (plain, prefix string) {
	plain = apiKeyPrefix + randomHex(32)
	return plain, plain[:len(apiKeyPrefix)+6]
}

// bearerToken extracts the credential from an Authorization header.
func bearerToken(r *http.Request) string {
	scheme, token, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

func (a *app) apiKeyFromRequest(ctx context.Context, r *http.Request) (apiKey, bool) {
	token := bearerToken(r)
	if !strings.HasPrefix(token, apiKeyPrefix) {
		return apiKey{}, false
	}
	var k apiKey
	var roomID, roomCode *string
	err := a.db.QueryRow(ctx, `
		SELECT k.id, k.user_id, k.room_id::text, r.code
		FROM api_keys k LEFT JOIN rooms r ON r.id = k.room_id
		WHERE k.key_hash = $1`, hashToken(token)).Scan(&k.ID, &k.UserID, &roomID, &roomCode)
	if err != nil {
		return apiKey{}, false
	}
	if roomID != nil {
		k.RoomID = *roomID
	}
	if roomCode != nil {
		k.RoomCode = *roomCode
	}
	return k, true
}

// cleanAPITodoText flattens what an automation sends into what the todo
// input would have produced: one line, no control characters. A newline in a
// title would break every list that renders it, and an escape sequence would
// reach the terminal client verbatim.
func cleanAPITodoText(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// apiAddTodos is POST /api/todos: add up to maxAPITodosPerRequest todos to
// the list the key was made for, all or none.
//
// Retries are expected -- an automation that times out can't know whether
// the call landed -- so a caller that sends an Idempotency-Key gets the
// original response back on a repeat instead of a second copy of every todo.
func (a *app) apiAddTodos(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !a.limiter.allow("apiip:"+clientIP(r), maxAPIAttemptsPerIPMinute, time.Minute) {
		writeAPIRateLimited(w, time.Minute)
		return
	}
	k, ok := a.apiKeyFromRequest(ctx, r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="junkie"`)
		writeJSONError(w, http.StatusUnauthorized, "Missing or invalid API key.")
		return
	}
	if !a.limiter.allow("apikey-min:"+k.ID, maxAPIRequestsPerMinute, time.Minute) {
		writeAPIRateLimited(w, time.Minute)
		return
	}
	if !a.limiter.allow("apikey-day:"+k.ID, maxAPIRequestsPerDay, 24*time.Hour) {
		writeAPIRateLimited(w, time.Hour)
		return
	}

	var body struct {
		Todos []struct {
			Text string `json:"text"`
		} `json:"todos"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, `Body must be JSON like {"todos": [{"text": "..."}]}.`)
		return
	}
	if len(body.Todos) == 0 {
		writeJSONError(w, http.StatusBadRequest, "Send at least one todo.")
		return
	}
	if len(body.Todos) > maxAPITodosPerRequest {
		writeJSONError(w, http.StatusBadRequest, "At most "+strconv.Itoa(maxAPITodosPerRequest)+" todos per request.")
		return
	}
	texts := make([]string, 0, len(body.Todos))
	for i, t := range body.Todos {
		text := cleanAPITodoText(t.Text)
		if text == "" {
			writeJSONError(w, http.StatusBadRequest, "Todo "+strconv.Itoa(i+1)+" has no text.")
			return
		}
		// Refused rather than cut: the UI truncates what you can see as you
		// type, but an automation would never learn its todo lost its end.
		if len([]rune(text)) > maxTodoTextLen {
			writeJSONError(w, http.StatusBadRequest, "Todo "+strconv.Itoa(i+1)+" is longer than "+strconv.Itoa(maxTodoTextLen)+" characters.")
			return
		}
		texts = append(texts, text)
	}

	idemKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(idemKey) > maxIdempotencyKeyLen {
		writeJSONError(w, http.StatusBadRequest, "Idempotency-Key is longer than "+strconv.Itoa(maxIdempotencyKeyLen)+" characters.")
		return
	}
	// Hashed over the cleaned texts, not the raw bytes, so a retry that
	// re-serialises the same todos differently still counts as the same call.
	blob, _ := json.Marshal(texts)
	sum := sha256.Sum256(blob)
	requestHash := hex.EncodeToString(sum[:])

	if idemKey != "" {
		_, _ = a.db.Exec(ctx, `DELETE FROM api_idempotency WHERE created_at < $1`, time.Now().Add(-idempotencyRetention))
		if a.replayIdempotent(ctx, w, k.ID, idemKey, requestHash) {
			return
		}
	}

	// A room key only works while its owner is still in the room. The key
	// itself survives leaving (rejoining brings it back); it just stops
	// adding todos to a list its owner can no longer see.
	if k.RoomID != "" && !a.isRoomMember(ctx, k.RoomID, k.UserID) {
		writeJSONError(w, http.StatusForbidden, "This key's room is one you're no longer a member of.")
		return
	}

	type addedTodo struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	added := make([]addedTodo, 0, len(texts))
	tx, err := a.db.Begin(ctx)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not add todos. Try again.")
		return
	}
	defer tx.Rollback(ctx)
	var roomArg any
	if k.RoomID != "" {
		roomArg = k.RoomID
	}
	for _, text := range texts {
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO todos (user_id, room_id, text) VALUES ($1, $2, $3) RETURNING id`,
			k.UserID, roomArg, text).Scan(&id); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "Could not add todos. Try again.")
			return
		}
		added = append(added, addedTodo{ID: id, Text: text})
	}
	resp := map[string]any{"added": len(added), "todos": added, "list": "private"}
	if k.RoomID != "" {
		resp["list"] = "room"
		resp["room"] = k.RoomCode
	}
	if idemKey != "" {
		stored, _ := json.Marshal(resp)
		_, err := tx.Exec(ctx, `
			INSERT INTO api_idempotency (api_key_id, idem_key, request_hash, status, response)
			VALUES ($1, $2, $3, $4, $5)`, k.ID, idemKey, requestHash, http.StatusCreated, stored)
		if isUniqueViolation(err) {
			// A concurrent call with the same key committed first. Ours rolls
			// back, todos and all, and answers with what that one stored.
			_ = tx.Rollback(ctx)
			if !a.replayIdempotent(ctx, w, k.ID, idemKey, requestHash) {
				writeJSONError(w, http.StatusConflict, "A request with this Idempotency-Key is already in progress.")
			}
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "Could not add todos. Try again.")
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not add todos. Try again.")
		return
	}
	_, _ = a.db.Exec(ctx, `UPDATE api_keys SET last_used_at = now() WHERE id = $1`, k.ID)

	if k.RoomID != "" {
		a.hub.broadcast(k.RoomCode, "todos")
	} else {
		a.hub.broadcast(userChannel(k.UserID), "todos")
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// replayIdempotent answers from a stored response when this key has been
// used before, and reports whether it did.
func (a *app) replayIdempotent(ctx context.Context, w http.ResponseWriter, keyID, idemKey, requestHash string) bool {
	var storedHash string
	var status int
	var response []byte
	err := a.db.QueryRow(ctx, `
		SELECT request_hash, status, response FROM api_idempotency
		WHERE api_key_id = $1 AND idem_key = $2 AND created_at >= $3`,
		keyID, idemKey, time.Now().Add(-idempotencyRetention)).Scan(&storedHash, &status, &response)
	if errors.Is(err, pgx.ErrNoRows) || err != nil {
		return false
	}
	if storedHash != requestHash {
		writeJSONError(w, http.StatusUnprocessableEntity, "This Idempotency-Key was already used with different todos.")
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Idempotent-Replayed", "true")
	w.WriteHeader(status)
	_, _ = w.Write(response)
	return true
}

func writeAPIRateLimited(w http.ResponseWriter, retryAfter time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
	writeJSONError(w, http.StatusTooManyRequests, "Too many requests. Slow down and try again later.")
}

// apiListAPIKeys feeds the profile page's key list. Hashes never leave the
// server; the prefix is all anyone sees of a key after it's made.
func (a *app) apiListAPIKeys(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	rows, err := a.db.Query(r.Context(), `
		SELECT k.id, k.name, k.prefix, COALESCE(r.code, ''), COALESCE(r.name, ''),
			k.created_at, k.last_used_at
		FROM api_keys k LEFT JOIN rooms r ON r.id = k.room_id
		WHERE k.user_id = $1 ORDER BY k.created_at`, u.ID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not load API keys.")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, prefix, roomCode, roomName string
		var created time.Time
		var lastUsed *time.Time
		if err := rows.Scan(&id, &name, &prefix, &roomCode, &roomName, &created, &lastUsed); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "Could not load API keys.")
			return
		}
		k := map[string]any{
			"id": id, "name": name, "prefix": prefix,
			"roomCode": roomCode, "roomName": roomName,
			"createdAt": created.UTC().Format(time.RFC3339),
		}
		if lastUsed != nil {
			k["lastUsedAt"] = lastUsed.UTC().Format(time.RFC3339)
		}
		out = append(out, k)
	}
	writeJSON(w, map[string]any{"keys": out, "max": maxAPIKeysPerUser})
}

// apiCreateAPIKey makes a key aimed at the private list (room empty) or at
// one of the caller's rooms, and returns the plaintext -- the only time it is
// ever available.
func (a *app) apiCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	ctx := r.Context()
	if !a.limiter.allow("apikeycreate:"+u.ID, 10, time.Hour) {
		writeJSONError(w, http.StatusTooManyRequests, "You're making keys too quickly — try again later.")
		return
	}
	name := limitRunes(cleanAPITodoText(r.FormValue("name")), maxAPIKeyNameLen)
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "Give the key a name, so you know later what uses it.")
		return
	}
	var roomArg any
	roomCode := ""
	if code := strings.TrimSpace(r.FormValue("room")); code != "" {
		rm, ok := a.findRoom(ctx, code)
		if !ok || !a.isRoomMember(ctx, rm.ID, u.ID) {
			writeJSONError(w, http.StatusBadRequest, "That room isn't one you're in.")
			return
		}
		// A temporary room is gone when its run ends, and its key with it.
		if rm.Ephemeral {
			writeJSONError(w, http.StatusBadRequest, "Temporary rooms can't have API keys.")
			return
		}
		roomArg, roomCode = rm.ID, rm.Code
	}
	var count int
	_ = a.db.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys WHERE user_id = $1`, u.ID).Scan(&count)
	if count >= maxAPIKeysPerUser {
		writeJSONError(w, http.StatusBadRequest, "You already have "+strconv.Itoa(maxAPIKeysPerUser)+" keys. Revoke one first.")
		return
	}
	plain, prefix := newAPIKey()
	var id string
	if err := a.db.QueryRow(ctx, `
		INSERT INTO api_keys (user_id, room_id, name, key_hash, prefix)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		u.ID, roomArg, name, hashToken(plain), prefix).Scan(&id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not create the key. Try again.")
		return
	}
	a.logEvent(ctx, u.ID, eventAPIKeyCreated, "user", u.ID, "", map[string]string{"name": name, "room": roomCode})
	writeJSON(w, map[string]any{"id": id, "key": plain, "prefix": prefix, "name": name, "roomCode": roomCode})
}

func (a *app) apiRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	var name string
	err := a.db.QueryRow(r.Context(), `DELETE FROM api_keys WHERE id = $1 AND user_id = $2 RETURNING name`,
		r.PathValue("id"), u.ID).Scan(&name)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "No such key.")
		return
	}
	a.logEvent(r.Context(), u.ID, eventAPIKeyRevoked, "user", u.ID, "", map[string]string{"name": name})
	writeJSON(w, map[string]any{"ok": true})
}
