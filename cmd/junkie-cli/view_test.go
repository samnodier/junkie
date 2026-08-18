package main

import (
	"strings"
	"testing"
	"time"
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
	out := renderStatus(deskResponse{})
	for _, want := range []string{"nothing running", "junkie focus", "0 open", "none yet"} {
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
	})
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
	})
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
		got := roomLine(tc.room)
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
	})
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
	if out := renderTodos(nil); !strings.Contains(out, "nothing on the list") {
		t.Errorf("empty list reads as %q", out)
	}
}

func TestRenderRoomsEmpty(t *testing.T) {
	if out := renderRooms(nil); !strings.Contains(out, "not in any rooms") {
		t.Errorf("empty rooms reads as %q", out)
	}
}
