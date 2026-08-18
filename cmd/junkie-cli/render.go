package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The palette is junkie's own, lifted from web/src/assets/app.css so the
// terminal and the browser are recognisably the same product. Light and dark
// values are the two themes the stylesheet already defines; lipgloss picks
// per the terminal's background.
var (
	colAccent = lipgloss.AdaptiveColor{Light: "#1e5c3c", Dark: "#7cc79a"}
	colWarn   = lipgloss.AdaptiveColor{Light: "#b3801f", Dark: "#d8a84e"}
	colInk    = lipgloss.AdaptiveColor{Light: "#1d241f", Dark: "#ecebe0"}
	colMuted  = lipgloss.AdaptiveColor{Light: "#6f766a", Dark: "#99a193"}
	colFaint  = lipgloss.AdaptiveColor{Light: "#8a9083", Dark: "#6e7568"}
	colDanger = lipgloss.AdaptiveColor{Light: "#a8452f", Dark: "#c86a55"}

	styleLabel  = lipgloss.NewStyle().Foreground(colMuted)
	styleFaint  = lipgloss.NewStyle().Foreground(colFaint)
	styleInk    = lipgloss.NewStyle().Foreground(colInk)
	styleAccent = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleWarn   = lipgloss.NewStyle().Foreground(colWarn).Bold(true)
	styleDanger = lipgloss.NewStyle().Foreground(colDanger)
)

// phaseColor is the one place phase drives colour: focus is junkie's green,
// a break is its amber, and everything else stays ink.
func phaseColor(phase string) lipgloss.TerminalColor {
	switch phase {
	case "focus":
		return colAccent
	case "break":
		return colWarn
	case "lobby":
		return colWarn
	default:
		return colInk
	}
}

// formatDuration renders a countdown the way a clock does: MM:SS below an
// hour, H:MM:SS above it. Negative durations clamp to zero — an overrun
// countdown means the server hasn't been asked to transition yet, and
// showing "-00:03" would suggest a problem the user can't act on.
func formatDuration(seconds int) string {
	if seconds < 0 {
		seconds = 0
	}
	h, m, s := seconds/3600, (seconds%3600)/60, seconds%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// glyphs is a 3x5 block font, doubled horizontally at render time so the
// digits come out roughly square in a terminal cell's 1:2 aspect ratio.
// Hand-rolled rather than pulled from a figlet dependency: ten digits and a
// colon is the whole alphabet a countdown needs.
var glyphs = map[rune][5]string{
	'0': {"███", "█ █", "█ █", "█ █", "███"},
	'1': {"  █", "  █", "  █", "  █", "  █"},
	'2': {"███", "  █", "███", "█  ", "███"},
	'3': {"███", "  █", "███", "  █", "███"},
	'4': {"█ █", "█ █", "███", "  █", "  █"},
	'5': {"███", "█  ", "███", "  █", "███"},
	'6': {"███", "█  ", "███", "█ █", "███"},
	'7': {"███", "  █", "  █", "  █", "  █"},
	'8': {"███", "█ █", "███", "█ █", "███"},
	'9': {"███", "█ █", "███", "  █", "███"},
	':': {"   ", " █ ", "   ", " █ ", "   "},
	' ': {"   ", "   ", "   ", "   ", "   "},
}

// bigDigits renders text as five rows of block glyphs. Unknown runes are
// skipped rather than substituted, so a caller can only ever widen the
// output by passing something odd, never break the row alignment.
func bigDigits(text string) []string {
	rows := make([]string, 5)
	first := true
	for _, r := range text {
		g, ok := glyphs[r]
		if !ok {
			continue
		}
		for i := 0; i < 5; i++ {
			if !first {
				rows[i] += " "
			}
			// Double every cell horizontally — blanks included, or a row
			// with gaps comes out narrower than a solid one and the digits
			// shear apart. A 3-wide glyph five rows tall reads as a thin
			// sliver at single width.
			for _, cell := range g[i] {
				rows[i] += string(cell) + string(cell)
			}
		}
		first = false
	}
	return rows
}

// progressBar draws elapsed-vs-remaining as a filled track. frac is clamped
// rather than trusted: a clock skew between server and client can hand this
// a fraction slightly outside [0,1], and a negative repeat count panics.
func progressBar(frac float64, width int) string {
	if width < 1 {
		return ""
	}
	if frac < 0 {
		frac = 0
	} else if frac > 1 {
		frac = 1
	}
	filled := int(frac*float64(width) + 0.5)
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// phaseLabel names what the timer is doing in words, matching the wording
// the web screens use so the two don't drift into different vocabularies.
func phaseLabel(t *soloTimer) string {
	switch {
	case t == nil:
		return "no timer running"
	case t.BreakPending:
		return "break ready"
	case t.Phase == "break":
		return "break"
	default:
		return "focus"
	}
}

// truncate shortens text to width, ending in an ellipsis so a clipped todo
// is visibly clipped rather than silently wrong.
func truncate(text string, width int) string {
	runes := []rune(text)
	if width <= 0 || len(runes) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}
