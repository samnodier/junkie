package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestTierFor(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		want          sizeTier
	}{
		{"full screen", 120, 40, tierFull},
		{"exactly full", fullMinWidth, fullMinHeight, tierFull},
		// One column short of full is compact, not a squeezed full.
		{"one column short of full", fullMinWidth - 1, fullMinHeight, tierCompact},
		{"one row short of full", fullMinWidth, fullMinHeight - 1, tierCompact},
		{"exactly compact", compactMinWidth, compactMinHeight, tierCompact},
		{"one column short of compact", compactMinWidth - 1, compactMinHeight, tierMini},
		{"exactly mini", miniMinWidth, miniMinHeight, tierMini},
		{"one column short of mini", miniMinWidth - 1, miniMinHeight, tierMicro},
		{"a sliver", 6, 2, tierMicro},
		// A window pinned down the side of a screen: narrow but tall. Width
		// is the binding constraint, and it must not be treated as full
		// just because it has rows to spare.
		{"side strip", 30, 50, tierCompact},
		{"thin side strip", 16, 50, tierMini},
		// A short wide window — a split pane above a shell — has the columns
		// but not the rows.
		{"short and wide", 200, 5, tierMini},
		{"zero", 0, 0, tierMicro},
	}
	for _, tc := range tests {
		if got := tierFor(tc.width, tc.height); got != tc.want {
			t.Errorf("%s (%dx%d): tier %d, want %d", tc.name, tc.width, tc.height, got, tc.want)
		}
	}
}

func TestDigitScale(t *testing.T) {
	if got := tierFull.digitScale(); got != 2 {
		t.Errorf("full scale = %d, want 2", got)
	}
	if got := tierCompact.digitScale(); got != 1 {
		t.Errorf("compact scale = %d, want 1", got)
	}
	// The text tiers draw no glyphs, and say so as zero rather than as a
	// scale that would render something.
	if got := tierMini.digitScale(); got != 0 {
		t.Errorf("mini scale = %d, want 0", got)
	}
	if got := tierMicro.digitScale(); got != 0 {
		t.Errorf("micro scale = %d, want 0", got)
	}
}

// The whole point of the tiers: whatever the terminal's size, nothing the
// countdown draws may be wider than it. A single overflowing row wraps and
// tears the display apart, which is exactly what happens in a narrow strip.
func TestWatchViewNeverExceedsItsWidth(t *testing.T) {
	for _, width := range []int{6, 10, 14, 20, 24, 30, 39, 40, 60, 100, 200} {
		for _, height := range []int{2, 3, 5, 9, 13, 14, 24, 50} {
			for _, timer := range []*soloTimer{
				focusTimer(1500),
				focusTimer(7325), // over an hour: one glyph wider
				{Phase: "break", BreakMinutes: 10, BreakPending: true},
			} {
				m := newWatchModel(timer)
				m.width, m.height = width, height
				for i, line := range strings.Split(m.View(), "\n") {
					if got := lipgloss.Width(line); got > width {
						t.Errorf("%dx%d %s: line %d is %d wide\n%q",
							width, height, phaseLabel(timer), i, got, line)
					}
				}
			}
		}
	}
}

// An hour-long block is a glyph wider than a minutes-only one. At a width
// where 25:00 fits at scale 2 but 2:02:05 does not, the display steps the
// scale down instead of overflowing.
func TestWatchDigitsStepDownRatherThanOverflow(t *testing.T) {
	m := newWatchModel(focusTimer(7325))
	m.width, m.height = 44, 20
	rows := m.digits(2)
	if rows == nil {
		t.Fatal("expected digits at this size")
	}
	if got := lipgloss.Width(rows[0]); got > m.width {
		t.Errorf("digits are %d wide in a %d-column terminal", got, m.width)
	}
	// Narrow enough that even scale 1 cannot fit: no glyphs at all, and the
	// view falls back to plain text rather than drawing a torn number.
	m.width = 8
	if rows := m.digits(2); rows != nil {
		t.Errorf("expected no glyphs at width 8, got %d wide", lipgloss.Width(rows[0]))
	}
}

func TestWatchTiersShowTheirEssentials(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		want          []string
		omit          []string
	}{
		{
			name:  "full shows the chrome",
			width: 100, height: 30,
			want: []string{"FOCUS", "of 50:00", "ends at", "q quit"},
		},
		{
			// At this size the number is the whole message.
			name:  "compact drops the chrome",
			width: 30, height: 12,
			want: []string{"FOCUS", "█"},
			omit: []string{"q quit", "ends at", "of 50:00"},
		},
		{
			name:  "mini is two lines of text",
			width: 16, height: 4,
			want: []string{"focus", "25:00"},
			omit: []string{"█", "q quit"},
		},
		{
			name:  "micro is the digits alone",
			width: 8, height: 2,
			want: []string{"25:00"},
			omit: []string{"focus", "█"},
		},
	}
	for _, tc := range tests {
		m := newWatchModel(focusTimer(1500))
		m.width, m.height = tc.width, tc.height
		view := m.View()
		for _, want := range tc.want {
			if !strings.Contains(view, want) {
				t.Errorf("%s: missing %q in\n%s", tc.name, want, view)
			}
		}
		for _, omit := range tc.omit {
			if strings.Contains(view, omit) {
				t.Errorf("%s: should not contain %q in\n%s", tc.name, omit, view)
			}
		}
	}
}

// Output going to a pipe belongs to a script: clipping a room name it was
// about to parse would be worse than a long line.
func TestFitLeavesPipedOutputAlone(t *testing.T) {
	long := strings.Repeat("x", 200)
	if got := fit(long, 0); got != long {
		t.Errorf("width 0 should not truncate, got %d runes", len([]rune(got)))
	}
	if got := fit(long, 10); len([]rune(got)) != 10 {
		t.Errorf("fit to 10 produced %d runes", len([]rune(got)))
	}
}
