package main

import (
	"os"

	"golang.org/x/term"
)

// A terminal is not one size. junkie's countdown has to read equally well
// full-screen and in the narrow strip people park down the side of a screen
// so it doesn't cover anything — so the display picks a tier from the space
// it actually has, the way the web's CSS picks a breakpoint.
//
// Tiers drop one thing at a time rather than scaling one design down:
//
//	full     doubled block digits, progress bar, phase, end time, help
//	compact  single-width block digits and a short bar
//	mini     one line of plain text: phase and the countdown
//	micro    the digits alone — for a strip a few columns wide
type sizeTier int

const (
	tierMicro sizeTier = iota
	tierMini
	tierCompact
	tierFull
)

// The thresholds are what each tier actually needs, not round numbers.
// A doubled "00:00" is five glyphs of six columns plus four gaps — 34 —
// and the chrome around it wants a little air, hence 40. Its rows are the
// label, the five digit rows, the bar, the end time, the help line and the
// blanks between them: 13, so 14. Single-width digits need 19 columns, and
// their trimmed chrome needs 9 rows.
const (
	fullMinWidth     = 40
	fullMinHeight    = 14
	compactMinWidth  = 24
	compactMinHeight = 9
	miniMinWidth     = 14
	miniMinHeight    = 3
)

func tierFor(width, height int) sizeTier {
	switch {
	case width >= fullMinWidth && height >= fullMinHeight:
		return tierFull
	case width >= compactMinWidth && height >= compactMinHeight:
		return tierCompact
	case width >= miniMinWidth && height >= miniMinHeight:
		return tierMini
	default:
		return tierMicro
	}
}

// digitScale is how wide each glyph cell is drawn at this tier. Only the two
// block-digit tiers draw glyphs at all; the rest report zero, which reads at
// the call site as "no block digits here".
func (t sizeTier) digitScale() int {
	switch t {
	case tierFull:
		return 2
	case tierCompact:
		return 1
	default:
		return 0
	}
}

// terminalWidth reports how wide stdout is, or 0 when stdout is not a
// terminal. Zero means "don't wrap or truncate": output going to a pipe or a
// file belongs to a script, and silently clipping a room name it was about
// to parse would be worse than a long line.
func terminalWidth() int {
	fd := int(os.Stdout.Fd())
	if !term.IsTerminal(fd) {
		return 0
	}
	w, _, err := term.GetSize(fd)
	if err != nil || w <= 0 {
		return 0
	}
	return w
}

// fit truncates text to width, and leaves it alone when width is 0.
func fit(text string, width int) string {
	if width <= 0 {
		return text
	}
	return truncate(text, width)
}
