package main

import (
	"errors"
	"strings"
	"testing"
	"time"
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
	m := newWatchModel(soloFace(focusTimer(1500)))
	if got := m.remaining(); got != 1500 {
		t.Errorf("remaining at t=0 is %d, want 1500", got)
	}
	m.countdown.fetchedAt = time.Now().Add(-90 * time.Second)
	if got := m.remaining(); got != 1410 {
		t.Errorf("remaining after 90s is %d, want 1410", got)
	}
	// Past the deadline it floors at zero rather than going negative.
	m.countdown.fetchedAt = time.Now().Add(-2000 * time.Second)
	if got := m.remaining(); got != 0 {
		t.Errorf("remaining past the end is %d, want 0", got)
	}
}

// A pending break is an offer with no deadline; counting it down would be
// inventing a clock the server does not have.
func TestWatchPendingBreakHasNoCountdown(t *testing.T) {
	m := newWatchModel(soloFace(&soloTimer{Phase: "break", BreakMinutes: 10, BreakPending: true}))
	m.countdown.fetchedAt = time.Now().Add(-time.Hour)
	if got := m.remaining(); got != 0 {
		t.Errorf("pending break remaining = %d, want 0", got)
	}
}

func TestWatchIdleInvitesAStart(t *testing.T) {
	m := newWatchModel(nil)
	if !strings.Contains(m.View(), "f to start one") {
		t.Errorf("an idle watch should say how to start:\n%s", m.View())
	}
}

func TestWatchViewShowsCountdown(t *testing.T) {
	m := newWatchModel(soloFace(focusTimer(1500)))
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

func TestWatchOfflineIsShown(t *testing.T) {
	m := newWatchModel(soloFace(focusTimer(300)))
	m.err = errors.New("network down")
	if !strings.Contains(m.View(), "offline") {
		t.Error("the view should show the offline state")
	}
}

func TestWatchRefreshAdoptsTheNewPhase(t *testing.T) {
	m := newWatchModel(soloFace(focusTimer(0)))
	m.timer = soloFace(&soloTimer{Phase: "break", BreakMinutes: 10, BreakPending: true})
	if !strings.Contains(m.View(), "BREAK READY") {
		t.Errorf("view should show the break offer:\n%s", m.View())
	}
}
