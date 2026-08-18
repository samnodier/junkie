package main

import (
	"net/http"
	"strings"
	"testing"
)

// Codes are uppercase server-side, and pasting a room's URL should work as
// well as typing its code — the same normalisation normalizeRoomCode does.
func TestNormalizeCode(t *testing.T) {
	tests := map[string]string{
		"abc-123":     "ABC-123",
		"  ABC-123  ": "ABC-123",
		"https://junkie-blin.onrender.com/r/abc-123": "ABC-123",
		"/r/abc-123/members":                         "ABC-123",
		"abc-123?error=nope":                         "ABC-123",
	}
	for in, want := range tests {
		if got := normalizeCode(in); got != want {
			t.Errorf("normalizeCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveRoomNeedsAMembership(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"rooms":[]}`))
	})
	_, err := resolveRoom(c, nil)
	if err == nil || !strings.Contains(err.Error(), "not in any rooms") {
		t.Errorf("error = %v", err)
	}
}

// One room is no guess at all; several is, and naming the choices beats
// picking one and being wrong about which room you just started.
func TestResolveRoomPicksTheOnlyOne(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"rooms":[{"code":"ABC-123","name":"Deep work"}]}`))
	})
	room, err := resolveRoom(c, nil)
	if err != nil {
		t.Fatalf("resolveRoom: %v", err)
	}
	if room.Code != "ABC-123" {
		t.Errorf("resolved %q", room.Code)
	}
}

func TestResolveRoomAsksWhenAmbiguous(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"rooms":[{"code":"ABC-123","name":"A"},{"code":"DEF-456","name":"B"}]}`))
	})
	_, err := resolveRoom(c, nil)
	if err == nil || !strings.Contains(err.Error(), "ABC-123") || !strings.Contains(err.Error(), "DEF-456") {
		t.Errorf("error should name the choices, got %v", err)
	}
	// Given a code, it resolves — and normalises on the way.
	room, err := resolveRoom(c, []string{"def-456"})
	if err != nil || room.Code != "DEF-456" {
		t.Errorf("room = %+v, err = %v", room, err)
	}
	// A room you are not in is a different error from an ambiguous one.
	if _, err := resolveRoom(c, []string{"XYZ-999"}); err == nil ||
		!strings.Contains(err.Error(), "room join") {
		t.Errorf("error should suggest joining, got %v", err)
	}
}

func TestRoomSubcommandUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"frobnicate"}, {"new"}, {"join"}, {"join", "A", "B"}} {
		if err := cmdRoom(args); err == nil {
			t.Errorf("expected usage error for %v", args)
		}
	}
}
