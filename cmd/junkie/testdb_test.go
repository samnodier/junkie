package main

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"testing"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Database-backed tests run against a throwaway `junkie_test` database, never
// the developer's own. They are skipped -- not failed -- when no server is
// reachable, so `go test ./...` still passes on a machine with no postgres.
//
// Set JUNKIE_TEST_DATABASE_URL to point somewhere else. The URL must NOT name
// a database anyone cares about: these tests truncate.
const defaultTestDatabaseURL = "postgres://junkie:junkie@localhost:5432/junkie_test?sslmode=disable"

var (
	testPoolOnce sync.Once
	testPool     *pgxpool.Pool
	testPoolErr  error
)

func testDatabaseURL() string {
	if v := os.Getenv("JUNKIE_TEST_DATABASE_URL"); v != "" {
		return v
	}
	return defaultTestDatabaseURL
}

// testDB returns a pool against the migrated test database, or skips the test.
// The pool and the migration run happen once for the whole package.
func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	testPoolOnce.Do(func() {
		ctx := context.Background()
		pool, err := pgxpool.New(ctx, testDatabaseURL())
		if err != nil {
			testPoolErr = err
			return
		}
		if err := pool.Ping(ctx); err != nil {
			testPoolErr = err
			return
		}
		if err := (&app{db: pool}).migrateFrom(ctx, "../../migrations"); err != nil {
			testPoolErr = fmt.Errorf("migrate: %w", err)
			return
		}
		testPool = pool
	})
	if testPoolErr != nil {
		t.Skipf("no test database (%v); start one with `docker compose up -d` and "+
			"`createdb junkie_test`, or set JUNKIE_TEST_DATABASE_URL", testPoolErr)
	}
	return testPool
}

// newTestApp builds an app wired to the test database. The signed-in user is
// set per-request by asUser; with none set, requests read as signed out.
func newTestApp(t *testing.T) *app {
	t.Helper()
	return &app{
		db:      testDB(t),
		hub:     &hub{rooms: map[string]map[*websocket.Conn]struct{}{}},
		limiter: newRateLimiter(),
	}
}

// asUser makes every request through a see this user as signed in.
func (a *app) asUser(u user) *app {
	a.currentUserOverride = func(*http.Request) (user, bool) {
		if u.ID == "" {
			return user{}, false
		}
		return u, true
	}
	return a
}

// makeUser inserts a user with a unique name and removes it when the test
// ends -- which cascades to their rooms, memberships, todos and activity, so
// tests leave the database as they found it.
func makeUser(t *testing.T, a *app, displayName string) user {
	t.Helper()
	ctx := context.Background()
	username := fmt.Sprintf("t%d%s", rand.Int63(), "u")
	var u user
	u.Username, u.DisplayName, u.Role = username, displayName, "user"
	if err := a.db.QueryRow(ctx,
		`INSERT INTO users (username, password_hash, display_name) VALUES ($1, 'x', $2) RETURNING id`,
		username, displayName).Scan(&u.ID); err != nil {
		t.Fatalf("make user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = a.db.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, u.ID)
	})
	return u
}

// makeRoom creates a room owned by creator, with creator already a member --
// the same pair of writes every real creation path performs.
func makeRoom(t *testing.T, a *app, creator user) room {
	t.Helper()
	ctx := context.Background()
	var rm room
	code := randomCode()
	if err := a.db.QueryRow(ctx,
		`INSERT INTO rooms (code, name, creator_id) VALUES ($1, 'Test room', $2)
		 RETURNING id, code, name, creator_id, focus_minutes, break_minutes, auto_sessions`,
		code, creator.ID).Scan(&rm.ID, &rm.Code, &rm.Name, &rm.CreatorID,
		&rm.FocusMinutes, &rm.BreakMinutes, &rm.AutoSessions); err != nil {
		t.Fatalf("make room: %v", err)
	}
	a.addRoomMember(ctx, rm.ID, creator.ID)
	return rm
}

// memberRole reads a member's stored role, or "" when they aren't a member.
func memberRole(t *testing.T, a *app, roomID, userID string) string {
	t.Helper()
	var role string
	_ = a.db.QueryRow(context.Background(),
		`SELECT role FROM room_members WHERE room_id = $1 AND user_id = $2`, roomID, userID).Scan(&role)
	return role
}
