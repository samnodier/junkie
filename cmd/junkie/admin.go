package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	roleUser  = "user"
	roleAdmin = "admin"
	roleOwner = "owner"
)

type adminOverview struct {
	Users             int
	Rooms             int
	ActiveRoomTimers  int
	TotalFocusMinutes int
}

type adminUser struct {
	ID             string
	Username       string
	Role           string
	JoinedAt       time.Time
	RoomsCount     int
	FocusMinutes   int
	LastActivityAt *time.Time
}

type adminRoom struct {
	ID           string
	Name         string
	Code         string
	Creator      string
	MembersCount int
	Active       bool
	CreatedAt    time.Time
}

type adminPageData struct {
	Overview adminOverview
	Users    []adminUser
	Rooms    []adminRoom
	IsOwner  bool
}

func validRole(role string) bool {
	return role == roleUser || role == roleAdmin || role == roleOwner
}

func canAccessAdmin(role string) bool {
	return role == roleAdmin || role == roleOwner
}

func canChangeRole(actor, target, requested string) bool {
	if actor != roleOwner || target == roleOwner {
		return false
	}
	return (target == roleUser && requested == roleAdmin) ||
		(target == roleAdmin && requested == roleUser)
}

func (a *app) bootstrapOwner(ctx context.Context, username string) error {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return nil
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin owner bootstrap: %w", err)
	}
	defer tx.Rollback(ctx)
	var targetID string
	err = tx.QueryRow(ctx, `
		UPDATE users
		SET role = 'owner'
		WHERE username = $1 AND role <> 'owner'
		RETURNING id`, username).Scan(&targetID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("bootstrap owner: %w", err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE username = $1)`, username).Scan(&exists); err != nil {
			return fmt.Errorf("check bootstrap owner: %w", err)
		}
		if !exists {
			log.Printf("owner bootstrap skipped: configured username was not found")
			return nil
		}
	} else {
		metadata, _ := json.Marshal(map[string]string{"method": "environment"})
		if _, err := tx.Exec(ctx, `
			INSERT INTO admin_audit_log (actor_user_id, action, target_type, target_id, metadata)
			VALUES (NULL, 'owner.bootstrapped', 'user', $1, $2::jsonb)`, targetID, metadata); err != nil {
			return fmt.Errorf("audit bootstrap owner: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit owner bootstrap: %w", err)
	}
	log.Printf("owner bootstrap confirmed for configured account")
	return nil
}

func (a *app) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := a.currentUser(r)
		if !ok {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		if !canAccessAdmin(u.Role) {
			a.renderStatus(w, http.StatusForbidden, "forbidden", pageData{
				Title:            "Access denied",
				User:             u,
				ForbiddenMessage: "This space is limited to platform administrators.",
			})
			return
		}
		next(w, r)
	}
}

func (a *app) requireAdminMutation(next http.HandlerFunc) http.HandlerFunc {
	return a.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			http.Error(w, "cross-site request rejected", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

func sameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	return err == nil && strings.EqualFold(parsed.Host, r.Host) &&
		(parsed.Scheme == "http" || parsed.Scheme == "https")
}

func (a *app) adminPage(w http.ResponseWriter, r *http.Request) {
	actor, _ := a.currentUser(r)
	data, err := a.loadAdminPage(r.Context(), actor.Role == roleOwner)
	if err != nil {
		log.Printf("load admin page: %v", err)
		http.Error(w, "could not load admin space", http.StatusInternalServerError)
		return
	}
	a.render(w, "admin", pageData{
		Title: "Admin",
		User:  actor,
		Admin: data,
		Error: r.URL.Query().Get("error"),
	})
}

func (a *app) loadAdminPage(ctx context.Context, includeFocusSummaries bool) (adminPageData, error) {
	var data adminPageData
	data.IsOwner = includeFocusSummaries
	if err := a.db.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM users),
			(SELECT COUNT(*) FROM rooms),
			(SELECT COUNT(*) FROM timer_runs WHERE room_id IS NOT NULL AND ended_at IS NULL AND phase <> 'ended'),
			(SELECT COALESCE(SUM(focus_minutes), 0) FROM activity)
	`).Scan(&data.Overview.Users, &data.Overview.Rooms, &data.Overview.ActiveRoomTimers, &data.Overview.TotalFocusMinutes); err != nil {
		return data, err
	}

	userQuery := `
		SELECT u.id, u.username, u.role, u.created_at, COUNT(DISTINCT rm.room_id)
		FROM users u
		LEFT JOIN room_members rm ON rm.user_id = u.id
		GROUP BY u.id
		ORDER BY u.created_at DESC, u.username`
	if includeFocusSummaries {
		userQuery = `
			SELECT u.id, u.username, u.role, u.created_at, COUNT(DISTINCT rm.room_id),
			       COALESCE((SELECT SUM(a.focus_minutes) FROM activity a WHERE a.user_id = u.id), 0),
			       (SELECT MAX(a.activity_date)::timestamptz FROM activity a WHERE a.user_id = u.id)
			FROM users u
			LEFT JOIN room_members rm ON rm.user_id = u.id
			GROUP BY u.id
			ORDER BY u.created_at DESC, u.username`
	}
	rows, err := a.db.Query(ctx, userQuery)
	if err != nil {
		return data, err
	}
	for rows.Next() {
		var item adminUser
		if includeFocusSummaries {
			var last sql.NullTime
			err = rows.Scan(&item.ID, &item.Username, &item.Role, &item.JoinedAt, &item.RoomsCount, &item.FocusMinutes, &last)
			if last.Valid {
				item.LastActivityAt = &last.Time
			}
		} else {
			err = rows.Scan(&item.ID, &item.Username, &item.Role, &item.JoinedAt, &item.RoomsCount)
		}
		if err != nil {
			rows.Close()
			return data, err
		}
		data.Users = append(data.Users, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return data, err
	}
	rows.Close()

	rows, err = a.db.Query(ctx, `
		SELECT r.id, r.name, r.code, creator.username, COUNT(DISTINCT rm.user_id),
		       EXISTS (
		           SELECT 1 FROM timer_runs tr
		           WHERE tr.room_id = r.id AND tr.ended_at IS NULL AND tr.phase <> 'ended'
		       ),
		       r.created_at
		FROM rooms r
		JOIN users creator ON creator.id = r.creator_id
		LEFT JOIN room_members rm ON rm.room_id = r.id
		GROUP BY r.id, creator.username
		ORDER BY r.created_at DESC, r.name`)
	if err != nil {
		return data, err
	}
	defer rows.Close()
	for rows.Next() {
		var item adminRoom
		if err := rows.Scan(&item.ID, &item.Name, &item.Code, &item.Creator, &item.MembersCount, &item.Active, &item.CreatedAt); err != nil {
			return data, err
		}
		data.Rooms = append(data.Rooms, item)
	}
	return data, rows.Err()
}

func (a *app) adminChangeRole(w http.ResponseWriter, r *http.Request) {
	actor, _ := a.currentUser(r)
	targetID := r.PathValue("id")
	requested := strings.TrimSpace(r.FormValue("role"))
	if requested != roleUser && requested != roleAdmin {
		http.Error(w, "invalid role", http.StatusBadRequest)
		return
	}

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "could not update role", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	var actorRole, targetRole, targetUsername string
	if err = tx.QueryRow(r.Context(), `SELECT role FROM users WHERE id = $1 FOR UPDATE`, actor.ID).Scan(&actorRole); err != nil {
		http.Error(w, "could not verify actor", http.StatusForbidden)
		return
	}
	if err = tx.QueryRow(r.Context(), `SELECT role, username FROM users WHERE id = $1 FOR UPDATE`, targetID).Scan(&targetRole, &targetUsername); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load target", http.StatusInternalServerError)
		return
	}
	if !canChangeRole(actorRole, targetRole, requested) {
		http.Error(w, "role change is not permitted", http.StatusForbidden)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE users SET role = $1 WHERE id = $2`, requested, targetID); err != nil {
		http.Error(w, "could not update role", http.StatusInternalServerError)
		return
	}
	metadata, _ := json.Marshal(map[string]string{
		"from":     targetRole,
		"to":       requested,
		"username": targetUsername,
	})
	if _, err = tx.Exec(r.Context(), `
		INSERT INTO admin_audit_log (actor_user_id, action, target_type, target_id, metadata)
		VALUES ($1, 'user.role_changed', 'user', $2, $3::jsonb)`, actor.ID, targetID, metadata); err != nil {
		http.Error(w, "could not audit role update", http.StatusInternalServerError)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		http.Error(w, "could not update role", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (a *app) adminDeleteRoom(w http.ResponseWriter, r *http.Request) {
	actor, _ := a.currentUser(r)
	roomID := r.PathValue("id")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "could not delete room", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	var actorRole, name, code string
	if err = tx.QueryRow(r.Context(), `SELECT role FROM users WHERE id = $1 FOR UPDATE`, actor.ID).Scan(&actorRole); err != nil || !canAccessAdmin(actorRole) {
		http.Error(w, "room deletion is not permitted", http.StatusForbidden)
		return
	}
	if err = tx.QueryRow(r.Context(), `SELECT name, code FROM rooms WHERE id = $1 FOR UPDATE`, roomID).Scan(&name, &code); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load room", http.StatusInternalServerError)
		return
	}
	metadata, _ := json.Marshal(map[string]string{"name": name, "code": code})
	if _, err = tx.Exec(r.Context(), `
		INSERT INTO admin_audit_log (actor_user_id, action, target_type, target_id, metadata)
		VALUES ($1, 'room.deleted', 'room', $2, $3::jsonb)`, actor.ID, roomID, metadata); err != nil {
		http.Error(w, "could not audit room deletion", http.StatusInternalServerError)
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM rooms WHERE id = $1`, roomID); err != nil {
		http.Error(w, "could not delete room", http.StatusInternalServerError)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		http.Error(w, "could not delete room", http.StatusInternalServerError)
		return
	}
	a.hub.broadcast(code, "deleted")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}
