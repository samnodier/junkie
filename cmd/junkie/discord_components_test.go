package main

import (
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
	tests := []struct {
		name  string
		timer *timerRun
		want  []string
	}{
		{name: "no run", timer: nil, want: []string{"Join"}},
		{name: "focus", timer: &timerRun{Phase: "focus"}, want: []string{"Join"}},
		{name: "running break", timer: &timerRun{Phase: "break"}, want: []string{"Join"}},
		{name: "pending break", timer: &timerRun{Phase: "break", PhaseStartedAt: now, PausedAt: &now}, want: []string{"Join", "Start break"}},
		{name: "paused mid-break", timer: &timerRun{Phase: "break", PhaseStartedAt: earlier, PausedAt: &now}, want: []string{"Join", "Resume break"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buttonLabels(timerComponents(tt.timer))
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
