package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

// buttonLabels flattens timerComponents output to the button labels, in order.
func buttonLabels(components []discordgo.MessageComponent) []string {
	var labels []string
	for _, c := range components {
		row, ok := c.(discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, b := range row.Components {
			if btn, ok := b.(discordgo.Button); ok {
				labels = append(labels, btn.Label)
			}
		}
	}
	return labels
}

func TestTimerComponents(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-2 * time.Minute)
	checkin := room{RequireCheckin: true}
	tests := []struct {
		name  string
		room  room
		timer *timerRun
		want  []string
	}{
		{name: "no run", timer: nil, want: []string{"Join"}},
		{name: "focus", timer: &timerRun{Phase: "focus"}, want: []string{"Join"}},
		{name: "running break", timer: &timerRun{Phase: "break"}, want: []string{"Join", "Pause break", "Skip break"}},
		{name: "pending break", timer: &timerRun{Phase: "break", PhaseStartedAt: now, PausedAt: &now}, want: []string{"Join", "Start break", "Skip break"}},
		{name: "paused mid-break", timer: &timerRun{Phase: "break", PhaseStartedAt: earlier, PausedAt: &now}, want: []string{"Join", "Resume break", "Skip break"}},
		{name: "check-in room hides skip", room: checkin, timer: &timerRun{Phase: "break"}, want: []string{"Join", "Pause break"}},
		{name: "check-in pending break hides skip", room: checkin, timer: &timerRun{Phase: "break", PhaseStartedAt: now, PausedAt: &now}, want: []string{"Join", "Start break"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buttonLabels(timerComponents(tt.room, tt.timer))
			if len(got) != len(tt.want) {
				t.Fatalf("timerComponents() buttons = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("timerComponents() buttons = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestSplitCheckedIn(t *testing.T) {
	// current_session 4: confirmed_session 5 means "claimed session 5"; 4 or
	// less means the seat was only ever claimed for a session already run.
	participants := []user{
		{ID: "a", DisplayName: "Ada", ConfirmedSession: 5},
		{ID: "b", DisplayName: "Bo", ConfirmedSession: 4},
		{ID: "c", DisplayName: "Cy", ConfirmedSession: 1},
	}
	in, pending := splitCheckedIn(participants, 4)
	if got := discordNameList(in); got != "Ada" {
		t.Errorf("in = %q, want %q", got, "Ada")
	}
	if got := discordNameList(pending); got != "Bo, Cy" {
		t.Errorf("pending = %q, want %q", got, "Bo, Cy")
	}
}

func TestDiscordStatusContentFocus(t *testing.T) {
	ends := time.Now().Add(80 * time.Minute)
	timer := &timerRun{Phase: "focus", FocusMinutes: 80, BreakMinutes: 20, TotalSessions: 2, CurrentSession: 1, PhaseEndsAt: ends}
	got := discordStatusContent(room{Name: "getting there"}, timer)
	// The countdown is rendered here, not left to Discord's <t:...:R>, which
	// never ticks on mobile; the absolute time rides along beside it.
	for _, want := range []string{"80 min", "in 1h 20m", fmt.Sprintf("at <t:%d:t>", ends.Unix())} {
		if !strings.Contains(got, want) {
			t.Errorf("content %q missing %q", got, want)
		}
	}
	if strings.Contains(got, ":R>") {
		t.Errorf("content %q still leans on Discord's relative timestamp", got)
	}
}

func TestDiscordRemaining(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{-time.Second, ""},
		{0, ""},
		{30 * time.Second, "under a minute"},
		// Rounded up, so a block never reads as done with time left on it.
		{90 * time.Second, "2 min"},
		{25 * time.Minute, "25 min"},
		{59*time.Minute + 30*time.Second, "1h 00m"},
		{80 * time.Minute, "1h 20m"},
		{2 * time.Hour, "2h 00m"},
	}
	for _, c := range cases {
		if got := discordRemaining(c.d); got != c.want {
			t.Errorf("discordRemaining(%s) = %q, want %q", c.d, got, c.want)
		}
	}
}

// The refresh loop edits only when the render actually changed, so a paused or
// otherwise static room costs nothing per tick.
func TestDiscordLiveUnchanged(t *testing.T) {
	a := &app{}
	if a.discordLiveUnchanged("room", "hello") {
		t.Error("unknown room reported unchanged; a cold cache must re-render")
	}
	timer := &timerRun{}
	a.rememberDiscordLive("room", timer, "hello")
	if !a.discordLiveUnchanged("room", "hello") {
		t.Error("identical content reported changed")
	}
	if a.discordLiveUnchanged("room", "hello · in 24 min") {
		t.Error("ticked countdown reported unchanged")
	}
	// A finished run drops its entry rather than pinning it in memory.
	a.rememberDiscordLive("room", nil, "run complete")
	if a.discordLiveUnchanged("room", "hello") {
		t.Error("entry survived the end of the run")
	}
}
