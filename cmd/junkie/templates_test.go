package main

import (
	"bytes"
	"net/http/httptest"
	"net/url"
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
		`name="focus_minutes" min="5" max="180" value="45"`,
		`Scroll ±1 · buttons ±5 · tap ring to start room`,
	} {
		if !strings.Contains(html, expected) {
			t.Errorf("dashboard output missing %q", expected)
		}
	}
	if strings.Contains(html, `value="45" readonly`) {
		t.Error("dashboard room focus input should be adjustable")
	}
}

func TestRequestedRoomFocusMinutes(t *testing.T) {
	tests := []struct {
		name     string
		values   url.Values
		fallback int
		want     int
		wantErr  bool
	}{
		{name: "saved room default", values: url.Values{}, fallback: 45, want: 45},
		{name: "focus minutes override", values: url.Values{"focus_minutes": {"75"}}, fallback: 45, want: 75},
		{name: "minutes alias", values: url.Values{"minutes": {"30"}}, fallback: 45, want: 30},
		{name: "minimum", values: url.Values{"focus_minutes": {"5"}}, fallback: 45, want: 5},
		{name: "maximum", values: url.Values{"focus_minutes": {"180"}}, fallback: 45, want: 180},
		{name: "below minimum", values: url.Values{"focus_minutes": {"4"}}, fallback: 45, wantErr: true},
		{name: "above maximum", values: url.Values{"focus_minutes": {"181"}}, fallback: 45, wantErr: true},
		{name: "not a number", values: url.Values{"focus_minutes": {"later"}}, fallback: 45, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/r/JUNK-IES/timer-start", strings.NewReader(tt.values.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			got, err := requestedRoomFocusMinutes(request, tt.fallback)
			if (err != nil) != tt.wantErr {
				t.Fatalf("requestedRoomFocusMinutes() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("requestedRoomFocusMinutes() = %d, want %d", got, tt.want)
			}
		})
	}
}
