package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

// Password reset never involves email — junkie doesn't collect any. A reset
// link reaches its owner through one of two verified channels: the bot DMs it
// to the account's linked Discord user (/junkie reset-password), or the owner
// generates it in the admin space and hands it over out of band.
const passwordResetTTL = 30 * time.Minute

// resetTokenExecer is the slice of pgx both *pgxpool.Pool and pgx.Tx satisfy,
// so a reset token can be minted standalone (the Discord DM path) or inside a
// transaction (the audited admin path).
type resetTokenExecer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// createPasswordResetToken mints a single-use reset link for the user. Only
// the hash is stored, mirroring sessions and discord_link_tokens; it is also
// returned so a caller whose delivery fails can burn the token again.
func createPasswordResetToken(ctx context.Context, db resetTokenExecer, userID string, adminIssued bool) (link, tokenHash string, err error) {
	token := randomHex(32)
	tokenHash = hashToken(token)
	if _, err = db.Exec(ctx, `DELETE FROM password_reset_tokens WHERE user_id = $1`, userID); err != nil {
		return "", "", err
	}
	_, err = db.Exec(ctx, `
		INSERT INTO password_reset_tokens (token_hash, user_id, admin_issued, expires_at)
		VALUES ($1, $2, $3, $4)`,
		tokenHash, userID, adminIssued, time.Now().Add(passwordResetTTL))
	if err != nil {
		return "", "", err
	}
	link = fmt.Sprintf("%s/reset-password/%s", strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/"), url.PathEscape(token))
	return link, tokenHash, nil
}

// apiPasswordResetContext peeks (never consumes) a reset token so the page
// can show which account it unlocks, exactly like apiDiscordLinkContext.
func (a *app) apiPasswordResetContext(w http.ResponseWriter, r *http.Request) {
	var username string
	err := a.db.QueryRow(r.Context(), `
		SELECT users.username FROM password_reset_tokens
		JOIN users ON users.id = password_reset_tokens.user_id
		WHERE token_hash = $1 AND expires_at > now()`, hashToken(r.PathValue("token"))).Scan(&username)
	if err != nil {
		writeJSON(w, map[string]bool{"invalid": true})
		return
	}
	writeJSON(w, map[string]string{"username": username})
}

// apiPasswordReset redeems a reset token: validates the new password first so
// a typo doesn't burn the single-use token, then consumes it in a transaction
// (a racing second redeem loses) and signs out every session the account had.
func (a *app) apiPasswordReset(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !a.limiter.allow("pwreset-redeem:"+clientIP(r), 10, 15*time.Minute) {
		writeJSONError(w, http.StatusTooManyRequests, "Too many attempts. Try again in a few minutes.")
		return
	}
	tokenHash := hashToken(r.PathValue("token"))
	var userID, username string
	if err := a.db.QueryRow(ctx, `
		SELECT user_id, users.username FROM password_reset_tokens
		JOIN users ON users.id = password_reset_tokens.user_id
		WHERE token_hash = $1 AND expires_at > now()`, tokenHash).Scan(&userID, &username); err != nil {
		writeJSONError(w, http.StatusBadRequest, "That reset link is invalid or has expired. Request a new one.")
		return
	}
	newPassword := r.FormValue("new_password")
	if len([]rune(newPassword)) < minPasswordLength {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Passwords must be at least %d characters.", minPasswordLength))
		return
	}
	if len(newPassword) > maxPasswordBytes {
		writeJSONError(w, http.StatusBadRequest, "That password is too long.")
		return
	}
	if strings.EqualFold(newPassword, username) {
		writeJSONError(w, http.StatusBadRequest, "Your password can't be your username.")
		return
	}
	if newPassword != r.FormValue("confirm_password") {
		writeJSONError(w, http.StatusBadRequest, "The passwords do not match.")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not update the password.")
		return
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not update the password.")
		return
	}
	defer tx.Rollback(ctx)
	if err := tx.QueryRow(ctx, `
		DELETE FROM password_reset_tokens
		WHERE token_hash = $1 AND expires_at > now()
		RETURNING user_id`, tokenHash).Scan(&userID); err != nil {
		writeJSONError(w, http.StatusBadRequest, "That reset link is invalid or has expired. Request a new one.")
		return
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2`, string(hash), userID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not update the password.")
		return
	}
	if _, err := tx.Exec(ctx, `DELETE FROM password_reset_tokens WHERE user_id = $1`, userID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not update the password.")
		return
	}
	// Whoever forgot the password wasn't signed in — every existing session
	// belongs to old devices (or whoever else knew the old password).
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not update the password.")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not update the password.")
		return
	}
	writeJSON(w, map[string]string{"next": "/login?notice=" + url.QueryEscape("Password updated. Sign in with your new password.")})
}

// adminCreateResetLink serves POST /admin/users/{id}/reset-link: the owner
// mints a reset link for a user who can't self-serve through Discord and
// hands it over manually. Owner-only — a reset link is account takeover, so
// it takes the same bar as changing roles.
func (a *app) adminCreateResetLink(w http.ResponseWriter, r *http.Request) {
	actor, _ := a.currentUser(r)
	if actor.Role != roleOwner {
		writeJSONError(w, http.StatusForbidden, "Only the owner can issue reset links.")
		return
	}
	targetID := r.PathValue("id")
	var targetUsername string
	if err := a.db.QueryRow(r.Context(), `SELECT username FROM users WHERE id = $1`, targetID).Scan(&targetUsername); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "No such user.")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "Could not load that user.")
		return
	}
	// Mint and audit in one transaction, matching adminChangeRole and
	// adminDeleteRoom: a reset link is an account-takeover credential, so it
	// must never exist without its audit row.
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not create a reset link.")
		return
	}
	defer tx.Rollback(r.Context())
	link, _, err := createPasswordResetToken(r.Context(), tx, targetID, true)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not create a reset link.")
		return
	}
	metadata, _ := json.Marshal(map[string]string{"username": targetUsername})
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO admin_audit_log (actor_user_id, action, target_type, target_id, metadata)
		VALUES ($1, 'user.reset_link_issued', 'user', $2, $3::jsonb)`, actor.ID, targetID, metadata); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not audit the reset link.")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not create a reset link.")
		return
	}
	writeJSON(w, map[string]any{
		"url":            link,
		"username":       targetUsername,
		"expiresMinutes": int(passwordResetTTL.Minutes()),
	})
}
