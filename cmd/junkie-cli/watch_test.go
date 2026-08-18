package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func focusTimer(secondsLeft int) *soloTimer {
	return &soloTimer{
		Phase: "focus", FocusMinutes: 50, BreakMinutes: 10,
		SecondsLeft: secondsLeft, EndsAt: time.Now().Add(time.Duration(secondsLeft) * time.Second),
	}
}

// The countdown is anchored on the server's own secondsLeft plus elapsed
// monotonic time, not on the deadline in wall-clock terms: that is what
// makes it immune to clock skew between this machine and the server.
func TestWatchRemainingCountsDownFromServerReading(t *testing.T) {
	m := newWatchModel(nil, focusTimer(1500))
	if got := m.remaining(); got != 1500 {
		t.Errorf("remaining at t=0 is %d, want 1500", got)
	}
	m.fetchedAt = time.Now().Add(-90 * time.Second)
	if got := m.remaining(); got != 1410 {
		t.Errorf("remaining after 90s is %d, want 1410", got)
	}
	// Past the deadline it floors at zero rather than going negative.
	m.fetchedAt = time.Now().Add(-2000 * time.Second)
	if got := m.remaining(); got != 0 {
		t.Errorf("remaining past the end is %d, want 0", got)
	}
}

// A pending break is an offer with no deadline; counting it down would be
// inventing a clock the server does not have.
func TestWatchPendingBreakHasNoCountdown(t *testing.T) {
	m := newWatchModel(nil, &soloTimer{Phase: "break", BreakMinutes: 10, BreakPending: true})
	m.fetchedAt = time.Now().Add(-time.Hour)
	if got := m.remaining(); got != 0 {
		t.Errorf("pending break remaining = %d, want 0", got)
	}
}

// Zero on the clock is the server's cue: the client asks it to read the run,
// which is what performs the flip and credits the minutes.
func TestWatchRefreshesWhenTheClockRunsOut(t *testing.T) {
	m := newWatchModel(nil, focusTimer(0))
	if _, cmd := m.Update(tickMsg(time.Now())); cmd == nil {
		t.Fatal("expected a tick to schedule work")
	}
	if !m.refreshing {
		t.Error("an expired countdown should trigger a refresh")
	}
}

// A countdown sitting at zero ticks twice a second; without the guard it
// would stack a request every tick.
func TestWatchDoesNotStackRefreshes(t *testing.T) {
	m := newWatchModel(nil, focusTimer(0))
	m.Update(tickMsg(time.Now()))
	before := m.lastRefresh
	m.Update(tickMsg(time.Now()))
	if m.lastRefresh != before {
		t.Error("a second tick started another refresh while one was in flight")
	}
}

// A running block does not need polling — its own clock is enough — until
// the safety-net interval passes and something started elsewhere might have
// changed underneath.
func TestWatchDoesNotRefreshWhileTimeRemains(t *testing.T) {
	m := newWatchModel(nil, focusTimer(1500))
	m.Update(tickMsg(time.Now()))
	if m.refreshing {
		t.Error("a running countdown should not refresh on every tick")
	}
	m.lastRefresh = time.Now().Add(-2 * refreshInterval)
	m.Update(tickMsg(time.Now()))
	if !m.refreshing {
		t.Error("a stale view should refresh")
	}
}

// The run ending — cancelled here, on the web, or gone stale — leaves
// nothing to watch.
func TestWatchQuitsWhenTheRunEnds(t *testing.T) {
	m := newWatchModel(nil, focusTimer(30))
	_, cmd := m.Update(deskMsg{desk: deskResponse{SoloTimer: nil}})
	if cmd == nil {
		t.Fatal("expected the program to quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", cmd())
	}
}

// A blip in connectivity should dim the display, not tear down a countdown
// the user is watching — the block is running on the server regardless.
func TestWatchSurvivesARefreshFailure(t *testing.T) {
	m := newWatchModel(nil, focusTimer(300))
	m.refreshing = true
	if _, cmd := m.Update(deskMsg{err: errors.New("network down")}); cmd != nil {
		t.Error("a failed refresh should not quit or issue work")
	}
	if m.timer == nil {
		t.Error("the timer should survive a failed refresh")
	}
	if m.err == nil {
		t.Error("the failure should be recorded for the display")
	}
	if m.refreshing {
		t.Error("the in-flight guard should clear so the next tick can retry")
	}
	if !strings.Contains(m.View(), "offline") {
		t.Error("the view should show the offline state")
	}
}

func TestWatchRefreshAdoptsTheNewPhase(t *testing.T) {
	m := newWatchModel(nil, focusTimer(0))
	m.Update(deskMsg{desk: deskResponse{SoloTimer: &soloTimer{
		Phase: "break", BreakMinutes: 10, BreakPending: true,
	}}})
	if m.timer == nil || !m.timer.BreakPending {
		t.Fatalf("expected the pending break, got %+v", m.timer)
	}
	if !strings.Contains(m.View(), "BREAK READY") {
		t.Errorf("view should show the break offer:\n%s", m.View())
	}
}

func TestWatchViewShowsCountdown(t *testing.T) {
	m := newWatchModel(nil, focusTimer(1500))
	view := m.View()
	if !strings.Contains(view, "FOCUS") {
		t.Errorf("view missing the phase:\n%s", view)
	}
	if !strings.Contains(view, "█") {
		t.Errorf("view missing the block digits:\n%s", view)
	}
	if !strings.Contains(view, "of 50:00") {
		t.Errorf("view missing the phase length:\n%s", view)
	}
}
