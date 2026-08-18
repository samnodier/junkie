package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func sampleDesk() deskResponse {
	return deskResponse{
		SoloTimer: &soloTimer{
			Phase: "focus", FocusMinutes: 50, BreakMinutes: 10,
			SecondsLeft: 1500, EndsAt: time.Now().Add(25 * time.Minute),
		},
		Todos: []apiTodo{
			{ID: "1", Text: "ship the terminal client"},
			{ID: "2", Text: "write the README", Done: true},
			{ID: "3", Text: "old idea", Removed: true},
		},
		Rooms: []deskRoom{{Code: "ABC-123", Name: "Deep work", Timer: &roomTimer{
			Phase: "focus", SecondsLeft: 724, Participant: true,
			Participants: []apiUser{{DisplayName: "Sam"}},
		}}},
	}
}

func dashAt(width, height int) *dashModel {
	m := newDashModel(nil, "sam", sampleDesk())
	m.width, m.height = width, height
	return m
}

// Same invariant as the countdown: nothing may be drawn wider than the
// window, or it wraps and tears the layout apart.
func TestDashViewNeverExceedsItsWidth(t *testing.T) {
	for _, width := range []int{20, 30, 40, 46, 60, 80, 120} {
		for _, height := range []int{6, 10, 16, 24, 40} {
			m := dashAt(width, height)
			for i, line := range strings.Split(m.View(), "\n") {
				if got := lipgloss.Width(line); got > width {
					t.Errorf("%dx%d: line %d is %d wide\n%q", width, height, i, got, line)
				}
			}
		}
	}
}

func TestDashShowsTheDesk(t *testing.T) {
	view := dashAt(100, 30).View()
	for _, want := range []string{"junkie", "sam", "FOCUS", "25:00", "of 50:00",
		"todos", "ship the terminal client", "[x]", "rooms", "ABC-123", "f focus"} {
		if !strings.Contains(view, want) {
			t.Errorf("desk missing %q:\n%s", want, view)
		}
	}
	// Removed todos are a bin, not a working list.
	if strings.Contains(view, "old idea") {
		t.Errorf("removed todo should not be listed:\n%s", view)
	}
}

// A short window drops sections from the bottom up, so what survives is the
// timer — the reason the screen is open at all.
func TestDashKeepsTheTimerWhenItIsShort(t *testing.T) {
	view := dashAt(80, 6).View()
	if !strings.Contains(view, "FOCUS") {
		t.Errorf("the timer should survive a short window:\n%s", view)
	}
}

func TestDashNarrowDropsTheBar(t *testing.T) {
	wide := dashAt(80, 24).View()
	if !strings.Contains(wide, "of 50:00") {
		t.Errorf("a wide desk should show the bar:\n%s", wide)
	}
	narrow := dashAt(30, 24).View()
	if strings.Contains(narrow, "of 50:00") {
		t.Errorf("a narrow desk should drop the bar:\n%s", narrow)
	}
	// The countdown itself never goes.
	if !strings.Contains(narrow, "25:00") {
		t.Errorf("the countdown should survive:\n%s", narrow)
	}
}

func TestDashKeys(t *testing.T) {
	if !strings.Contains(dashKeys(80), "c cancel") {
		t.Error("a wide footer should name every key")
	}
	if got := dashKeys(40); strings.Contains(got, "c cancel") || !strings.Contains(got, "f focus") {
		t.Errorf("a medium footer should shorten, got %q", got)
	}
	if got := dashKeys(10); got != "q quit" {
		t.Errorf("a narrow footer = %q", got)
	}
}

// Guarded client-side as well as server-side: /solo/start silently redirects
// when a run is already live, which would look like a start that did nothing.
func TestDashRefusesToStartOverARunningBlock(t *testing.T) {
	m := dashAt(80, 24)
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if cmd != nil {
		t.Error("expected no request while a block is running")
	}
	if !strings.Contains(m.View(), "already running") {
		t.Errorf("expected the refusal in the footer:\n%s", m.View())
	}
}

func TestDashRefusesBreakWithNoneWaiting(t *testing.T) {
	m := dashAt(80, 24)
	if _, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}}); cmd != nil {
		t.Error("expected no request when no break is waiting")
	}
	if !strings.Contains(m.View(), "no break is waiting") {
		t.Errorf("expected the refusal in the footer:\n%s", m.View())
	}
}

func TestDashStartsFocusWhenIdle(t *testing.T) {
	m := dashAt(80, 24)
	m.desk.SoloTimer = nil
	if _, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}}); cmd == nil {
		t.Error("expected a start request on an idle desk")
	}
	if !strings.Contains(m.View(), "f to start one") {
		t.Errorf("an idle desk should say how to start:\n%s", m.View())
	}
}

func TestDashQuits(t *testing.T) {
	m := dashAt(80, 24)
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", cmd())
	}
}

// A failed action shows in the footer and leaves the desk alone: what is on
// screen is still true, and the next keypress can retry.
func TestDashActionFailureIsShownNotFatal(t *testing.T) {
	m := dashAt(80, 24)
	_, cmd := m.Update(actionMsg{err: errors.New("could not reach the server")})
	if cmd == nil {
		t.Error("a failed action should still re-read the desk")
	}
	if !strings.Contains(m.View(), "could not reach") {
		t.Errorf("expected the failure in the footer:\n%s", m.View())
	}
	if m.desk.SoloTimer == nil {
		t.Error("the desk should survive a failed action")
	}
}

// A transient confirmation gives the footer back to the key hints once it
// has been read, rather than sitting there forever.
func TestDashStatusExpires(t *testing.T) {
	m := dashAt(80, 24)
	m.note("break started")
	if !strings.Contains(m.View(), "break started") {
		t.Error("expected the note in the footer")
	}
	m.statusUntil = time.Now().Add(-time.Second)
	if !strings.Contains(m.View(), "f focus") {
		t.Errorf("expected the keys back:\n%s", m.View())
	}
}

func TestDashRefreshAdoptsTheNewDesk(t *testing.T) {
	m := dashAt(80, 24)
	m.Update(deskMsg{desk: deskResponse{SoloTimer: &soloTimer{
		Phase: "break", BreakMinutes: 10, BreakPending: true,
	}}})
	view := m.View()
	if !strings.Contains(view, "BREAK READY") {
		t.Errorf("expected the pending break:\n%s", view)
	}
	if !strings.Contains(view, "b to take it") {
		t.Errorf("expected the break hint:\n%s", view)
	}
}
