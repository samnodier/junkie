package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// watchModel draws one timer at a size. It is not a program of its own —
// the desk hosts it as a pane, and `junkie watch` is the same desk with
// that pane filling the window. A run ending is a new state on the same
// screen, not a reason to quit.

type watchModel struct {
	timer *soloTimer

	countdown

	err    error
	width  int
	height int
	// chrome is the full-screen watch: help line, end time. The desk pane
	// drops them; the footer already names the keys.
	chrome bool
}

func newWatchModel(t *soloTimer) *watchModel {
	secs := 0
	if t != nil {
		secs = t.SecondsLeft
	}
	return &watchModel{
		timer:     t,
		countdown: newCountdown(secs),
		width:     80,
		height:    24,
		chrome:    true,
	}
}

// remaining is the countdown's single source of truth for the display. A
// pending break has no deadline — it is an offer — so it counts nothing.
func (m *watchModel) remaining() int {
	if m.timer == nil || m.timer.BreakPending {
		return 0
	}
	return m.countdown.remaining()
}

func (m *watchModel) View() string {
	if m.timer == nil {
		msg := styleFaint.Render(clip("no block running · f to start one", m.width))
		if m.chrome && m.height > 1 {
			return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
		}
		return msg + "\n"
	}
	tier := tierFor(m.width, m.height)
	var body []string
	switch tier {
	case tierFull:
		body = m.viewFull()
	case tierCompact:
		body = m.viewCompact()
	case tierMini:
		body = m.viewMini()
	default:
		// Micro places nothing and styles nothing: in a strip this narrow
		// every column is the countdown.
		return shortenCountdown(m.countdownText(), m.width)
	}
	card := lipgloss.JoinVertical(lipgloss.Center, body...)
	if m.chrome {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
	}
	// The desk hosts this as a pane: centre it horizontally, but do not
	// pad it to the budgeted height or the todos underneath starve.
	return lipgloss.Place(m.width, 0, lipgloss.Center, lipgloss.Top, card)
}

// countdownText is the number every tier is built around: the time left, or
// — for a break waiting to be started — the length being offered, since a
// pending break has no deadline to count down.
func (m *watchModel) countdownText() string {
	if m.timer == nil {
		return ""
	}
	if m.timer.BreakPending {
		return fmt.Sprintf("%02d:00", m.timer.BreakMinutes)
	}
	return formatDuration(m.remaining())
}

func (m *watchModel) phaseStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(phaseColor(m.timer.Phase))
}

// digits renders the countdown at the widest scale that fits, stepping down
// rather than overflowing: an hour-long block is a character wider than a
// minutes-only one, and would otherwise wrap and shear at the same terminal
// width where 25:00 sits comfortably.
func (m *watchModel) digits(scale int) []string {
	text := m.countdownText()
	for s := scale; s >= 1; s-- {
		if digitWidth(text, s) <= m.width {
			return bigDigits(text, s)
		}
	}
	return nil
}

func (m *watchModel) viewFull() []string {
	colour := m.phaseStyle()
	body := []string{colour.Bold(true).Render(strings.ToUpper(phaseLabel(m.timer))), ""}

	if rows := m.digits(2); rows != nil {
		body = append(body, colour.Render(strings.Join(rows, "\n")))
	} else {
		body = append(body, colour.Bold(true).Render(m.countdownText()))
	}
	body = append(body, "")

	if m.timer.BreakPending {
		body = append(body, styleFaint.Render(pendingHint(m.width)))
	} else {
		total := phaseSeconds(m.timer)
		barWidth := clamp(m.width-20, 10, 48)
		body = append(body,
			colour.Render(progressBar(elapsedFraction(m.remaining(), total), barWidth))+
				styleFaint.Render("  of "+formatDuration(total)))
		if m.chrome {
			body = append(body, "", styleFaint.Render("ends at "+m.timer.EndsAt.Local().Format("15:04")))
		}
	}

	if m.err != nil {
		body = append(body, "", styleDanger.Render(clip("offline: "+m.err.Error(), m.width)))
	}
	if m.chrome {
		body = append(body, "", styleFaint.Render(helpLine(m.width)))
	}
	return body
}

// viewCompact keeps the block digits but drops everything that only
// explains them — the end time, the help line — because at this size the
// number is the whole message.
func (m *watchModel) viewCompact() []string {
	colour := m.phaseStyle()
	body := []string{colour.Bold(true).Render(strings.ToUpper(phaseLabel(m.timer))), ""}

	if rows := m.digits(1); rows != nil {
		body = append(body, colour.Render(strings.Join(rows, "\n")))
	} else {
		body = append(body, colour.Bold(true).Render(m.countdownText()))
	}

	if m.timer.BreakPending {
		return append(body, "", styleFaint.Render("waiting"))
	}
	barWidth := clamp(m.width-6, 6, 24)
	return append(body, "",
		colour.Render(progressBar(elapsedFraction(m.remaining(), phaseSeconds(m.timer)), barWidth)))
}

// viewMini is two lines of plain text — no glyphs would fit, and a strip
// this narrow is being watched out of the corner of an eye anyway.
func (m *watchModel) viewMini() []string {
	colour := m.phaseStyle()
	return []string{
		colour.Render(clip(phaseLabel(m.timer), m.width)),
		colour.Bold(true).Render(shortenCountdown(m.countdownText(), m.width)),
	}
}

// shortenCountdown drops the seconds when the full clock will not fit,
// rather than truncating it into something unreadable: "2:02" tells you
// where you are, "2:02:0…" tells you nothing. Only a width this small can
// trigger it, and only an hour-plus block is wide enough to need it.
func shortenCountdown(text string, width int) string {
	if width <= 0 || len([]rune(text)) <= width {
		return text
	}
	if i := strings.LastIndex(text, ":"); i > 0 && i <= width {
		return text[:i]
	}
	return truncate(text, width)
}

// helpLine and pendingHint carry the same message at two lengths. Cutting
// the sentence short mid-word would be worse than saying less, so the short
// forms are written out rather than truncated.
func helpLine(width int) string {
	const full = "esc desk · q quit · the block keeps running either way"
	const short = "esc desk · q quit"
	if width >= len([]rune(full)) {
		return full
	}
	if width >= len([]rune(short)) {
		return short
	}
	return "q quit"
}

func pendingHint(width int) string {
	const full = "waiting to start · b to take it, s to skip"
	const short = "b take break · s skip"
	if width >= len([]rune(full)) {
		return full
	}
	if width >= len([]rune(short)) {
		return short
	}
	return "waiting"
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
