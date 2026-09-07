package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
)

func TestOpenPoolSetsDeliberateLimits(t *testing.T) {
	ctx := context.Background()
	// A URL that never connects is fine: openPool only parses and configures.
	const dsn = "postgres://u:p@127.0.0.1:1/db?sslmode=disable"

	pool, err := openPool(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	cfg := pool.Config()
	if cfg.MaxConns != defaultMaxConns {
		t.Errorf("MaxConns = %d, want %d", cfg.MaxConns, defaultMaxConns)
	}
	if cfg.MaxConnLifetime != poolMaxConnLifetime {
		t.Errorf("MaxConnLifetime = %v, want %v", cfg.MaxConnLifetime, poolMaxConnLifetime)
	}
	if cfg.MaxConnIdleTime != poolMaxConnIdleTime {
		t.Errorf("MaxConnIdleTime = %v, want %v", cfg.MaxConnIdleTime, poolMaxConnIdleTime)
	}

	t.Setenv("DATABASE_MAX_CONNS", "25")
	pool2, err := openPool(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool2.Close()
	if got := pool2.Config().MaxConns; got != 25 {
		t.Errorf("DATABASE_MAX_CONNS ignored: MaxConns = %d, want 25", got)
	}

	// A number already chosen in the URL wins over ours.
	pool3, err := openPool(ctx, dsn+"&pool_max_conns=7")
	if err != nil {
		t.Fatal(err)
	}
	defer pool3.Close()
	if got := pool3.Config().MaxConns; got != 7 {
		t.Errorf("pool_max_conns in the URL was overruled: MaxConns = %d, want 7", got)
	}
}

func TestIntFromEnvClampsAndFallsBack(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		want        int
	}{
		{"unset", "", 12},
		{"not a number", "lots", 12},
		{"below the floor", "0", 12},
		{"above the ceiling", "9999", 12},
		{"in range", "40", 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value == "" {
				os.Unsetenv("JUNKIE_TEST_INT")
			} else {
				t.Setenv("JUNKIE_TEST_INT", tc.value)
			}
			if got := intFromEnv("JUNKIE_TEST_INT", 12, 1, 200); got != tc.want {
				t.Errorf("intFromEnv(%q) = %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}

// The action ceiling has to stop a flood without tripping on normal use.
func TestRoomActionsAreRateLimited(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	rm := makeRoom(t, a, owner)
	a.asUser(owner)

	fields := url.Values{"text": {"a todo"}}
	for i := range maxRoomActionsPerMinute {
		if _, errMsg := postRoomAction(t, a, rm.Code, "todos", fields); errMsg != "" {
			t.Fatalf("call %d was refused early: %s", i+1, errMsg)
		}
	}
	if _, errMsg := postRoomAction(t, a, rm.Code, "todos", fields); errMsg == "" {
		t.Fatalf("call %d past the ceiling was allowed", maxRoomActionsPerMinute+1)
	}
	// The ceiling is per user: someone else in the same room is unaffected.
	other := makeUser(t, a, "Other")
	a.addRoomMember(context.Background(), rm.ID, other.ID)
	a.asUser(other)
	if _, errMsg := postRoomAction(t, a, rm.Code, "todos", fields); errMsg != "" {
		t.Errorf("another member was caught by someone else's limit: %s", errMsg)
	}
}

func TestRoomReadsAreRateLimited(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	rm := makeRoom(t, a, owner)
	a.asUser(owner)

	read := func() int {
		req := httptest.NewRequest(http.MethodGet, "/api/room/"+rm.Code, nil)
		req.SetPathValue("code", rm.Code)
		rec := httptest.NewRecorder()
		a.apiRoom(rec, req)
		return rec.Code
	}
	for i := range maxRoomReadsPerMinute {
		if got := read(); got == http.StatusTooManyRequests {
			t.Fatalf("read %d was refused early", i+1)
		}
	}
	if got := read(); got != http.StatusTooManyRequests {
		t.Errorf("read past the ceiling returned %d, want %d", got, http.StatusTooManyRequests)
	}
}
