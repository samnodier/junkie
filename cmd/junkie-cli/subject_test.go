package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// idleDesk is the case Sam hit: a room you are in, and a private block, both
// on the same desk with only one countdown between them.
func bothRunningDesk() deskResponse {
	d := sampleDesk()
	d.Rooms = []deskRoom{{
		Code: "ABC-123", Name: "Deep work", FocusMinutes: 25, BreakMinutes: 5,
		Timer: &roomTimer{
			Phase: "focus", SecondsLeft: 724, FocusMinutes: 25, BreakMinutes: 5,
			CurrentSession: 1, TotalSessions: 4, Participant: true,
			Participants: []apiUser{{DisplayName: "Sam"}, {DisplayName: "Ada"}},
			EndsAt:       time.Now().Add(12 * time.Minute),
		},
	}}
	return d
}

func TestTabMovesBetweenTheBlocksAndWraps(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	if m.subject != soloSubject {
		t.Fatalf("a running private block should be the opening subject, got %q", m.subject)
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.subject != "ABC-123" {
		t.Errorf("tab should select the room, got %q", m.subject)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.subject != soloSubject {
		t.Errorf("tab should wrap back to the private block, got %q", m.subject)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.subject != "ABC-123" {
		t.Errorf("shift+tab should go the other way, got %q", m.subject)
	}
}

// The whole point of the switch: the room's own countdown, not the private
// one, with the room named so there is no doubt which block is on screen.
func TestSelectedRoomDrawsItsOwnCountdown(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})

	view := m.View()
	for _, want := range []string{"Deep work", "ABC-123", "of 25:00", "session 1/4", "2 here"} {
		if !strings.Contains(view, want) {
			t.Errorf("the room pane is missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "of 50:00") {
		t.Errorf("the private block's length should be gone from the pane:\n%s", view)
	}
}

// A countdown that kept the old block's seconds for half a second would read
// as the new block being further along than it is.
func TestSwitchingRebasesTheCountdown(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if got := m.countdown.remaining(); got != 724 {
		t.Errorf("countdown = %d, want the room's 724", got)
	}
}

func TestRoomKeysActOnTheRoom(t *testing.T) {
	for _, tc := range []struct {
		key  rune
		want string
	}{
		{'s', "/r/ABC-123/timer-skip-break"},
		{'x', "/r/ABC-123/timer-leave"},
	} {
		m := dashAt(100, 30)
		m.desk = bothRunningDesk()
		if tc.key == 's' {
			m.desk.Rooms[0].Timer.Phase = "break"
		}
		m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
		_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tc.key}})
		if cmd == nil {
			t.Fatalf("%c should have acted on the room", tc.key)
		}
		if msg, ok := cmd().(actionMsg); !ok {
			t.Errorf("%c did not post anything", tc.key)
		} else if msg.err == nil && !strings.Contains(msg.message, "ABC-123") {
			t.Errorf("%c reported %q, which does not name the room", tc.key, msg.message)
		}
	}
}

// Starting a block is f whichever timer is selected; only the endpoint moves.
func TestStartActsOnWhicheverBlockIsSelected(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	m.desk.SoloTimer = nil
	m.desk.Rooms[0].Timer = nil

	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if cmd == nil {
		t.Fatal("f should start the selected room's block")
	}
	if msg := cmd().(actionMsg); msg.err == nil && !strings.Contains(msg.message, "ABC-123") {
		t.Errorf("f started %q, not the room", msg.message)
	}
}

// An idle room has no countdown, and saying "no block running · f to start
// one" — the private timer's line — would be about the wrong thing entirely.
func TestIdleRoomSaysWhatItIs(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	m.desk.Rooms[0].Timer = nil
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})

	view := m.View()
	for _, want := range []string{"Deep work", "idle", "25 min blocks", "f starts a block here"} {
		if !strings.Contains(view, want) {
			t.Errorf("the idle room pane is missing %q:\n%s", want, view)
		}
	}
}

// Leaving a room while its block is the one on screen must not strand the
// pane on a room the desk no longer lists.
func TestSubjectFallsBackWhenTheRoomGoesAway(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})

	next := bothRunningDesk()
	next.Rooms = nil
	m.Update(deskMsg{desk: next})
	if m.subject != soloSubject {
		t.Errorf("subject = %q, want the private block", m.subject)
	}
}

// Sam's ask: having entered a room, `junkie` should put you in front of it
// rather than a private timer that is not running.
func TestOpeningSubjectPrefersTheRoomYoureIn(t *testing.T) {
	desk := bothRunningDesk()
	desk.SoloTimer = nil
	if got := openingSubject(desk, ""); got != "ABC-123" {
		t.Errorf("opening subject = %q, want the live room", got)
	}

	// Your own running block outranks it: you started that one deliberately.
	if got := openingSubject(bothRunningDesk(), ""); got != soloSubject {
		t.Errorf("opening subject = %q, want the private block", got)
	}

	// A room you are not in still beats an empty screen.
	desk.Rooms[0].Timer.Participant = false
	if got := openingSubject(desk, ""); got != "ABC-123" {
		t.Errorf("opening subject = %q, want the live room", got)
	}

	// And nothing running anywhere is the private block, as before.
	desk.Rooms[0].Timer = nil
	if got := openingSubject(desk, ""); got != soloSubject {
		t.Errorf("opening subject = %q, want the private block", got)
	}
}

// `junkie watch CODE` names a room explicitly, and that wins over the guess.
func TestOpeningSubjectHonoursAnAskedForRoom(t *testing.T) {
	desk := bothRunningDesk()
	if got := openingSubject(desk, "ABC-123"); got != "ABC-123" {
		t.Errorf("asked for ABC-123, got %q", got)
	}
	if got := openingSubject(desk, "NOPE-1"); got != soloSubject {
		t.Errorf("a room you're not in should not be selected, got %q", got)
	}
}

// A refresh must never move the pane off the block the user chose.
func TestARefreshDoesNotUndoTheUsersChoice(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	m.Update(deskMsg{desk: bothRunningDesk()})
	if m.subject != "ABC-123" {
		t.Errorf("subject = %q after a refresh, want the room the user picked", m.subject)
	}
}

func TestRoomCodeArgumentAcceptsAURL(t *testing.T) {
	got, err := optionalRoomCode([]string{"https://junkie.example/r/abc-123"}, "usage")
	if err != nil || got != "ABC-123" {
		t.Errorf("optionalRoomCode = %q, %v", got, err)
	}
	if _, err := optionalRoomCode([]string{"one", "two"}, "usage"); err == nil {
		t.Error("two arguments should be refused")
	}
}
