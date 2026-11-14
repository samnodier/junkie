package main

import (
	"strings"
	"testing"
)

// A stopwatch shows the time behind it, not the time left to its ceiling.
func TestStopwatchCountsUp(t *testing.T) {
	solo := &soloTimer{Phase: "focus", FocusMinutes: 180, SecondsLeft: 180*60 - 754, Stopwatch: true}
	f := soloFace(solo)
	if !f.CountUp || f.Label != "stopwatch" {
		t.Fatalf("face = %+v, want a count-up stopwatch", f)
	}
	if got := newWatchModel(f).countdownText(); got != "12:34" {
		t.Errorf("countdownText = %q, want 12:34 elapsed", got)
	}
	line := renderSoloLine(solo, 120)
	if !strings.Contains(line, "12:34 so far") || strings.Contains(line, "left") {
		t.Errorf("status line = %q, want elapsed time and no time left", line)
	}
}

// Once stopped, the same run is an ordinary pending break: nothing counts up.
func TestStoppedStopwatchIsAnOrdinaryBreak(t *testing.T) {
	f := soloFace(&soloTimer{Phase: "break", BreakPending: true, BreakMinutes: 5, FocusMinutes: 12, Stopwatch: true})
	if f.CountUp {
		t.Error("a stopwatch's break is counting up")
	}
}
