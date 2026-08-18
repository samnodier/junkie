package main

import (
	"strings"
	"testing"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		seconds int
		want    string
	}{
		{0, "00:00"},
		{9, "00:09"},
		{59, "00:59"},
		{60, "01:00"},
		{1500, "25:00"},
		{3599, "59:59"},
		{3600, "1:00:00"},
		{10800, "3:00:00"},
		// A countdown that overran is clamped: the server simply hasn't been
		// asked to transition yet, and "-00:03" would read as a fault.
		{-5, "00:00"},
	}
	for _, tc := range tests {
		if got := formatDuration(tc.seconds); got != tc.want {
			t.Errorf("formatDuration(%d) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
}

func TestBigDigits(t *testing.T) {
	for _, scale := range []int{1, 2} {
		rows := bigDigits("25:00", scale)
		if len(rows) != 5 {
			t.Fatalf("scale %d: expected 5 rows, got %d", scale, len(rows))
		}
		// Every row must be the same width or the digits shear apart. This
		// is the check that caught blanks not being widened alongside the
		// filled cells.
		width := len([]rune(rows[0]))
		for i, row := range rows {
			if got := len([]rune(row)); got != width {
				t.Errorf("scale %d: row %d is %d wide, row 0 is %d", scale, i, got, width)
			}
		}
		// digitWidth is what the tiers use to decide whether the countdown
		// fits, so it has to agree with what actually gets drawn.
		if got := digitWidth("25:00", scale); got != width {
			t.Errorf("scale %d: digitWidth says %d, rendered %d", scale, got, width)
		}
		if !strings.Contains(strings.Join(rows, "\n"), "█") {
			t.Errorf("scale %d: expected block glyphs", scale)
		}
	}
	// Doubling the scale doubles the cells but not the single-column gaps
	// between glyphs, so the widths are not simply 2x.
	if w1, w2 := digitWidth("25:00", 1), digitWidth("25:00", 2); w2 <= w1 {
		t.Errorf("scale 2 (%d) should be wider than scale 1 (%d)", w2, w1)
	}
	// A tier that draws no glyphs asks for scale 0 and must get nothing
	// back, not five blank rows that would push the layout around.
	if rows := bigDigits("25:00", 0); rows != nil {
		t.Errorf("scale 0 rendered %d rows, want none", len(rows))
	}
	if got := digitWidth("25:00", 0); got != 0 {
		t.Errorf("digitWidth at scale 0 = %d, want 0", got)
	}
	// An unmappable rune is skipped, not rendered, so it cannot widen a
	// single row and break the alignment checked above.
	if len([]rune(bigDigits("2x5", 2)[0])) != len([]rune(bigDigits("25", 2)[0])) {
		t.Error("unknown runes should be skipped, not rendered")
	}
}

func TestProgressBar(t *testing.T) {
	if got := progressBar(0, 10); got != strings.Repeat("░", 10) {
		t.Errorf("empty bar = %q", got)
	}
	if got := progressBar(1, 10); got != strings.Repeat("█", 10) {
		t.Errorf("full bar = %q", got)
	}
	if got := progressBar(0.5, 10); got != strings.Repeat("█", 5)+strings.Repeat("░", 5) {
		t.Errorf("half bar = %q", got)
	}
	// Clock skew between client and server can produce a fraction outside
	// [0,1]; strings.Repeat panics on a negative count, so it is clamped.
	for _, frac := range []float64{-0.5, 1.5} {
		if got := len([]rune(progressBar(frac, 10))); got != 10 {
			t.Errorf("progressBar(%v, 10) is %d wide, want 10", frac, got)
		}
	}
	if got := progressBar(0.5, 0); got != "" {
		t.Errorf("zero-width bar = %q", got)
	}
}

func TestElapsedFraction(t *testing.T) {
	if got := elapsedFraction(1500, 3000); got != 0.5 {
		t.Errorf("half elapsed = %v", got)
	}
	// A pending break has no phase length; dividing by it would panic.
	if got := elapsedFraction(0, 0); got != 0 {
		t.Errorf("zero total = %v, want 0", got)
	}
}

func TestPhaseLabel(t *testing.T) {
	tests := []struct {
		name  string
		timer *soloTimer
		want  string
	}{
		{"nothing", nil, "no timer running"},
		{"focus", &soloTimer{Phase: "focus"}, "focus"},
		{"break", &soloTimer{Phase: "break"}, "break"},
		// A pending break is phase "break" too, but it is an offer rather
		// than a countdown and has to read differently.
		{"pending", &soloTimer{Phase: "break", BreakPending: true}, "break ready"},
	}
	for _, tc := range tests {
		if got := phaseLabel(tc.timer); got != tc.want {
			t.Errorf("%s: phaseLabel = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestPhaseSeconds(t *testing.T) {
	if got := phaseSeconds(&soloTimer{Phase: "focus", FocusMinutes: 50, BreakMinutes: 10}); got != 3000 {
		t.Errorf("focus phase = %d, want 3000", got)
	}
	if got := phaseSeconds(&soloTimer{Phase: "break", FocusMinutes: 50, BreakMinutes: 10}); got != 600 {
		t.Errorf("break phase = %d, want 600", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("no truncation expected, got %q", got)
	}
	if got := truncate("a longer line", 6); got != "a lon…" {
		t.Errorf("truncate = %q", got)
	}
	if got := truncate("héllo wörld", 8); len([]rune(got)) != 8 {
		t.Errorf("multibyte truncate produced %d runes: %q", len([]rune(got)), got)
	}
}

func TestClamp(t *testing.T) {
	if got := clamp(5, 10, 48); got != 10 {
		t.Errorf("clamp below = %d", got)
	}
	if got := clamp(100, 10, 48); got != 48 {
		t.Errorf("clamp above = %d", got)
	}
	if got := clamp(30, 10, 48); got != 30 {
		t.Errorf("clamp inside = %d", got)
	}
}
