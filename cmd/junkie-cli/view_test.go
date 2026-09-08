package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestCountTodos(t *testing.T) {
	todos := []apiTodo{
		{Text: "open"},
		{Text: "also open"},
		{Text: "finished", Done: true},
		{Text: "binned", Removed: true},
		// Removed wins over done: the web files these under removed, and
		// counting one row twice would make the summary lie.
		{Text: "binned after finishing", Done: true, Removed: true},
	}
	open, done, removed := countTodos(todos)
	if open != 2 || done != 1 || removed != 2 {
		t.Errorf("open=%d done=%d removed=%d, want 2/1/2", open, done, removed)
	}
}

func TestRenderStatusEmptyDesk(t *testing.T) {
	out := renderStatus(deskResponse{}, 0)
	for _, want := range []string{"nothing running", "press f", "0 open", "none yet"} {
		if !strings.Contains(out, want) {
			t.Errorf("status missing %q:\n%s", want, out)
		}
	}
}

func TestRenderStatusRunningFocus(t *testing.T) {
	out := renderStatus(deskResponse{
		SoloTimer: &soloTimer{
			Phase: "focus", FocusMinutes: 50, BreakMinutes: 10,
			SecondsLeft: 1500, EndsAt: time.Now().Add(25 * time.Minute),
		},
		Todos: []apiTodo{{Text: "ship it"}, {Text: "done thing", Done: true}},
	}, 0)
	for _, want := range []string{"focus", "25:00 left", "of 50:00", "1 open", "1 done"} {
		if !strings.Contains(out, want) {
			t.Errorf("status missing %q:\n%s", want, out)
		}
	}
}

// A pending break has no deadline, so it must not claim one — and it should
// name both ways out, which is the whole decision at that moment.
func TestRenderStatusPendingBreak(t *testing.T) {
	out := renderStatus(deskResponse{
		SoloTimer: &soloTimer{Phase: "break", FocusMinutes: 50, BreakMinutes: 10, BreakPending: true},
	}, 0)
	if !strings.Contains(out, "break ready") {
		t.Errorf("status missing the pending break:\n%s", out)
	}
	if !strings.Contains(out, "10 min offered") {
		t.Errorf("status missing the offered length:\n%s", out)
	}
	if strings.Contains(out, "left") {
		t.Errorf("a pending break has no countdown, but status shows one:\n%s", out)
	}
}

func TestRoomLineStates(t *testing.T) {
	tests := []struct {
		name string
		room deskRoom
		want []string
		omit []string
	}{
		{
			name: "idle",
			room: deskRoom{Code: "ABCD", Name: "Deep work"},
			want: []string{"ABCD", "Deep work", "idle"},
		},
		{
			name: "lobby",
			room: deskRoom{Code: "ABCD", Name: "Deep work", Timer: &roomTimer{
				Phase: "lobby", SecondsLeft: 22, Participant: true,
				Participants: []apiUser{{DisplayName: "Sam"}},
			}},
			want: []string{"starting", "00:22 to join", "1 here"},
			omit: []string{"you're out"},
		},
		{
			name: "focus you are not in",
			room: deskRoom{Code: "ABCD", Name: "Deep work", Timer: &roomTimer{
				Phase: "focus", SecondsLeft: 724, CurrentSession: 2, TotalSessions: 4,
				Participants: []apiUser{{DisplayName: "Sam"}, {DisplayName: "Ada"}},
			}},
			want: []string{"focus", "12:04 left", "session 2/4", "2 here", "you're out"},
		},
		{
			name: "paused break",
			room: deskRoom{Code: "ABCD", Name: "Deep work", Timer: &roomTimer{
				Phase: "break", SecondsLeft: 240, Paused: true, Participant: true,
			}},
			want: []string{"break paused", "04:00 left"},
		},
	}
	for _, tc := range tests {
		got := roomLine(tc.room, 0)
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s: line %q missing %q", tc.name, got, want)
			}
		}
		for _, omit := range tc.omit {
			if strings.Contains(got, omit) {
				t.Errorf("%s: line %q should not contain %q", tc.name, got, omit)
			}
		}
	}
}

func TestRenderTodos(t *testing.T) {
	out := renderTodos([]apiTodo{
		{Text: "open one"},
		{Text: "closed one", Done: true},
		{Text: "binned one", Removed: true},
	}, 0)
	if !strings.Contains(out, "[ ] open one") {
		t.Errorf("missing the open todo:\n%s", out)
	}
	if !strings.Contains(out, "[x] closed one") {
		t.Errorf("missing the completed todo:\n%s", out)
	}
	// Removed todos are a bin, not a working list: counted, never listed.
	if strings.Contains(out, "binned one") {
		t.Errorf("removed todo should not be listed:\n%s", out)
	}
	if !strings.Contains(out, "1 removed") {
		t.Errorf("missing the removed count:\n%s", out)
	}
}

func TestRenderTodosEmpty(t *testing.T) {
	if out := renderTodos(nil, 0); !strings.Contains(out, "nothing on the list") {
		t.Errorf("empty list reads as %q", out)
	}
}

// The room list lives inside status now, so an account with no rooms has to
// read as that there rather than nowhere.
func TestRenderStatusWithNoRooms(t *testing.T) {
	if out := renderStatus(deskResponse{}, 0); !strings.Contains(out, "none yet") {
		t.Errorf("empty rooms reads as %q", out)
	}
}

// A narrow terminal drops room detail in order of what you came for, and
// never overflows: the code and the phase are the last things standing.
func TestRoomLineDegradesWithWidth(t *testing.T) {
	room := deskRoom{Code: "ABC-123", Name: "Deep work sessions", Timer: &roomTimer{
		Phase: "focus", SecondsLeft: 724, CurrentSession: 2, TotalSessions: 4,
		Participant: true, Participants: []apiUser{{DisplayName: "Sam"}, {DisplayName: "Ada"}},
	}}
	for _, width := range []int{8, 12, 16, 20, 28, 40, 60, 100} {
		line := roomLine(room, width)
		if got := lipgloss.Width(line); got > width {
			t.Errorf("width %d: line is %d wide: %q", width, got, line)
		}
		// The code identifies the room, so it survives as far as it can.
		if width >= 8 && !strings.Contains(line, "ABC") {
			t.Errorf("width %d: lost the room code: %q", width, line)
		}
	}
	// Given room, everything shows.
	full := roomLine(room, 100)
	for _, want := range []string{"ABC-123", "Deep work sessions", "focus", "12:04 left", "session 2/4", "2 here"} {
		if !strings.Contains(full, want) {
			t.Errorf("full line missing %q: %q", want, full)
		}
	}
	// Squeezed, the extras go before the phase does.
	tight := roomLine(room, 20)
	if strings.Contains(tight, "session 2/4") || strings.Contains(tight, "2 here") {
		t.Errorf("a 20-column line should have dropped the extras: %q", tight)
	}
	if !strings.Contains(tight, "focus") {
		t.Errorf("a 20-column line should keep the phase: %q", tight)
	}
}

// Piped output belongs to a script; it is never clipped.
func TestRoomLineUnlimitedWidth(t *testing.T) {
	room := deskRoom{Code: "ABC-123", Name: strings.Repeat("long ", 40)}
	if got := roomLine(room, 0); !strings.Contains(got, strings.Repeat("long ", 40)) {
		t.Error("width 0 should not clip the name")
	}
}
