package main

import (
	"errors"
	"net/url"
	"testing"
	"time"
)

func testLocal(t *testing.T) *localStore {
	t.Helper()
	t.Setenv("JUNKIE_DATA", t.TempDir())
	s, err := openLocalStore()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLocalStoreStartsAndCountsDown(t *testing.T) {
	s := testLocal(t)
	if err := s.Do("/solo/start", url.Values{"focus_minutes": {"25"}}); err != nil {
		t.Fatal(err)
	}
	desk, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if desk.SoloTimer == nil || desk.SoloTimer.Phase != "focus" {
		t.Fatalf("expected a focus block, got %+v", desk.SoloTimer)
	}
	if desk.SoloTimer.FocusMinutes != 25 {
		t.Errorf("focus minutes = %d", desk.SoloTimer.FocusMinutes)
	}
	if desk.SoloTimer.SecondsLeft < 24*60 || desk.SoloTimer.SecondsLeft > 25*60 {
		t.Errorf("seconds left = %d", desk.SoloTimer.SecondsLeft)
	}
}

func TestLocalStoreRefusesASecondStart(t *testing.T) {
	s := testLocal(t)
	if err := s.Do("/solo/start", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Do("/solo/start", nil); err == nil {
		t.Fatal("expected a second start to fail")
	}
}

func TestLocalStoreFocusBecomesABreakOffer(t *testing.T) {
	s := testLocal(t)
	if err := s.Do("/solo/start", url.Values{"focus_minutes": {"25"}}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	past := time.Now().Add(-time.Second)
	s.timer.EndsAt = &past
	s.advanceLocked(time.Now())
	s.mu.Unlock()

	desk, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if desk.SoloTimer == nil || !desk.SoloTimer.BreakPending {
		t.Fatalf("expected a break offer, got %+v", desk.SoloTimer)
	}
	if desk.SoloTimer.BreakMinutes != 5 {
		t.Errorf("break minutes = %d, want 5 for a 25-minute block", desk.SoloTimer.BreakMinutes)
	}
	s.mu.Lock()
	got := s.activity[time.Now().Format("2006-01-02")]
	s.mu.Unlock()
	if got != 25 {
		t.Errorf("credited %d minutes, want 25", got)
	}
}

func TestLocalStoreSkipStartsTheNextBlock(t *testing.T) {
	s := testLocal(t)
	if err := s.Do("/solo/start", url.Values{"focus_minutes": {"25"}}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	past := time.Now().Add(-time.Second)
	s.timer.EndsAt = &past
	s.advanceLocked(time.Now())
	s.mu.Unlock()
	if err := s.Do("/solo/break/skip", nil); err != nil {
		t.Fatal(err)
	}
	desk, _ := s.Load()
	if desk.SoloTimer == nil || desk.SoloTimer.Phase != "focus" || desk.SoloTimer.BreakPending {
		t.Fatalf("expected a new focus block, got %+v", desk.SoloTimer)
	}
	if desk.SoloTimer.FocusMinutes != 25 {
		t.Errorf("next block should reuse 25 minutes, got %d", desk.SoloTimer.FocusMinutes)
	}
}

func TestLocalStoreCancelBanksNothing(t *testing.T) {
	s := testLocal(t)
	if err := s.Do("/solo/start", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Do("/solo/cancel", nil); err != nil {
		t.Fatal(err)
	}
	desk, _ := s.Load()
	if desk.SoloTimer != nil {
		t.Fatalf("expected no timer, got %+v", desk.SoloTimer)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.activity) != 0 {
		t.Errorf("cancelled block credited %v", s.activity)
	}
}

func TestLocalStoreTodos(t *testing.T) {
	s := testLocal(t)
	if err := s.Do("/todos", url.Values{"text": {"ship it"}}); err != nil {
		t.Fatal(err)
	}
	desk, _ := s.Load()
	if len(desk.Todos) != 1 || desk.Todos[0].Text != "ship it" {
		t.Fatalf("todos = %+v", desk.Todos)
	}
	id := desk.Todos[0].ID
	if err := s.Do("/todo/"+id+"/toggle", nil); err != nil {
		t.Fatal(err)
	}
	desk, _ = s.Load()
	if !desk.Todos[0].Done {
		t.Fatal("expected the todo to be done")
	}
	if err := s.Do("/todo/"+id+"/remove", nil); err != nil {
		t.Fatal(err)
	}
	desk, _ = s.Load()
	if !desk.Todos[0].Removed {
		t.Fatal("expected the todo to be removed")
	}
	if err := s.Do("/todo/"+id+"/restore", nil); err != nil {
		t.Fatal(err)
	}
	desk, _ = s.Load()
	if desk.Todos[0].Removed {
		t.Fatal("expected the todo to come back")
	}
}

func TestLocalStorePrunesOldInactiveTodos(t *testing.T) {
	s := testLocal(t)
	old := time.Now().Add(-25 * time.Hour)
	s.mu.Lock()
	s.todos = []guestTodo{{ID: "1", Text: "stale", Done: true, InactiveAt: &old}}
	s.pruneTodos(time.Now())
	s.mu.Unlock()
	if len(s.todos) != 0 {
		t.Fatalf("stale todo survived: %+v", s.todos)
	}
}

func TestLocalStorePersistsAcrossOpen(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("JUNKIE_DATA", dir)
	s, err := openLocalStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Do("/todos", url.Values{"text": {"remember me"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Do("/solo/start", url.Values{"focus_minutes": {"50"}}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := openLocalStore()
	if err != nil {
		t.Fatal(err)
	}
	desk, err := s2.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(desk.Todos) != 1 || desk.Todos[0].Text != "remember me" {
		t.Errorf("todos did not persist: %+v", desk.Todos)
	}
	if desk.SoloTimer == nil || desk.SoloTimer.Phase != "focus" {
		t.Errorf("timer did not persist: %+v", desk.SoloTimer)
	}
}

func TestLocalStoreRoomsNeedAnAccount(t *testing.T) {
	s := testLocal(t)
	if err := s.Do("/rooms", url.Values{"name": {"nope"}}); !errors.Is(err, errNeedsAccount) {
		t.Errorf("error = %v, want errNeedsAccount", err)
	}
}

func TestBreakMinutesForFocus(t *testing.T) {
	tests := map[int]int{25: 5, 50: 10, 119: 10, 120: 20, 180: 30}
	for in, want := range tests {
		if got := breakMinutesForFocus(in); got != want {
			t.Errorf("breakMinutesForFocus(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestBuildGuestHeatmapCreditsToday(t *testing.T) {
	now := time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC)
	h := buildGuestHeatmap(map[string]int{"2026-08-18": 50}, now)
	if h.TotalMinutes != 50 {
		t.Errorf("total = %d", h.TotalMinutes)
	}
	found := false
	for _, c := range h.Cells {
		if !c.Empty && c.Minutes == 50 {
			found = true
			if c.Level != 2 {
				t.Errorf("level = %d, want 2", c.Level)
			}
		}
	}
	if !found {
		t.Error("today's minutes missing from the grid")
	}
	if h.Weeks < 52 || h.Weeks > 54 {
		t.Errorf("weeks = %d", h.Weeks)
	}
}
