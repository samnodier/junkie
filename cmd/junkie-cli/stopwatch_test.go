package main

import (
	"strings"
	"testing"
)

// A stopwatch shows the time behind it, not the time left to its ceiling.
func TestStopwatchCountsUp(t *testing.T) {
	solo := &soloTimer{Phase: "focus", FocusMinutes: 360, SecondsLeft: 360*60 - 754, Stopwatch: true}
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

// A stopwatch pauses itself at its ceiling. Paused, it stands at what it has
// counted and offers continue or stop -- not the break a pending break would.
func TestPausedStopwatchHoldsItsCount(t *testing.T) {
	solo := &soloTimer{Phase: "break", BreakPending: true, FocusMinutes: 360, Stopwatch: true,
		StopwatchPaused: true, StopwatchBaseSeconds: 360 * 60}
	f := soloFace(solo)
	if !f.Held || f.CountUp || f.Label != "stopwatch paused" {
		t.Fatalf("face = %+v, want a held stopwatch", f)
	}
	if got := newWatchModel(f).countdownText(); got != "6:00:00" {
		t.Errorf("countdownText = %q, want 6:00:00 held", got)
	}
	line := renderSoloLine(solo, 160)
	if !strings.Contains(line, "6:00:00 saved") || !strings.Contains(line, "t continues") || strings.Contains(line, "break ready") {
		t.Errorf("status line = %q, want the paused stopwatch and its two ways on", line)
	}
}

// Continued, the clock carries on from what was already counted.
func TestContinuedStopwatchCarriesOn(t *testing.T) {
	solo := &soloTimer{Phase: "focus", FocusMinutes: 360, SecondsLeft: 360*60 - 61, Stopwatch: true,
		StopwatchBaseSeconds: 360 * 60}
	if got := newWatchModel(soloFace(solo)).countdownText(); got != "6:01:01" {
		t.Errorf("countdownText = %q, want 6:01:01", got)
	}
}
