package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestDashboardRendersIndependentSoloAndRoomTimers(t *testing.T) {
	now := time.Now()
	rm := room{
		ID:           "room-id",
		Code:         "JUNK-IES",
		Name:         "junkies",
		FocusMinutes: 50,
		BreakMinutes: 10,
		AutoSessions: 3,
	}
	data := pageData{
		Title: "Dashboard",
		User:  user{ID: "user-id", DisplayName: "Sam"},
		Rooms: []room{rm},
		SoloTimer: &timerRun{
			ID:             "solo-id",
			Phase:          "focus",
			FocusMinutes:   25,
			TotalSessions:  1,
			CurrentSession: 1,
			PhaseEndsAt:    now.Add(25 * time.Minute),
		},
		DeskRoomTodos: []roomTodosGroup{{
			Room: rm,
			Timer: &timerRun{
				ID:             "room-timer-id",
				Phase:          "break",
				FocusMinutes:   50,
				BreakMinutes:   10,
				TotalSessions:  3,
				CurrentSession: 2,
				PhaseEndsAt:    now.Add(10 * time.Minute),
				Participant:    false,
				Participants:   []string{"Alex"},
			},
		}},
	}

	var output bytes.Buffer
	if err := parseTemplates().ExecuteTemplate(&output, "dashboard", data); err != nil {
		t.Fatalf("render dashboard: %v", err)
	}
	html := output.String()
	for _, expected := range []string{
		`class="desk-timer-view" data-mode="private"`,
		`data-mode="room" data-room="JUNK-IES"`,
		`Break · session 2 of 3`,
		`Watching · 1 focusing`,
		`action="/r/JUNK-IES/timer-join"`,
	} {
		if !strings.Contains(html, expected) {
			t.Errorf("dashboard output missing %q", expected)
		}
	}
}

func TestDashboardRendersIdleRoomStart(t *testing.T) {
	rm := room{Code: "JUNK-IES", Name: "junkies", FocusMinutes: 45, BreakMinutes: 15, AutoSessions: 4}
	data := pageData{
		Title:         "Dashboard",
		User:          user{ID: "user-id", DisplayName: "Sam"},
		Rooms:         []room{rm},
		DeskRoomTodos: []roomTodosGroup{{Room: rm}},
	}

	var output bytes.Buffer
	if err := parseTemplates().ExecuteTemplate(&output, "dashboard", data); err != nil {
		t.Fatalf("render dashboard: %v", err)
	}
	html := output.String()
	for _, expected := range []string{
		`action="/r/JUNK-IES/timer-start"`,
		`value="45" readonly`,
		`Room focus · junkies · 4 × 45/15`,
	} {
		if !strings.Contains(html, expected) {
			t.Errorf("dashboard output missing %q", expected)
		}
	}
}
