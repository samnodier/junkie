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
	// timer is whichever block is being watched, flattened by face: the
	// pane draws a room's run and a private one the same way.
	timer *face

	countdown

	err    error
	width  int
	height int
	// chrome is the full-screen watch: help line, end time. The desk pane
	// drops them; the footer already names the keys.
	chrome bool
}

func newWatchModel(t *face) *watchModel {
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
	if !m.timer.counting() {
		return 0
	}
	return m.countdown.remaining()
}

func (m *watchModel) View() string {
	if m.timer == nil {
		msg := styleFaint.Render(clip("no block running · f to start one", m.width))
		if m.chrome && m.height > 1 {
			// With no timer there is nothing else on the screen, so the way
			// out has to be on it: an empty pane that does not say how to
			// leave is a dead end.
			body := msg + "\n\n" + styleFaint.Render(helpLine(m.width, false))
			return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
		}
		return msg + "\n"
	}
	if m.timer.Phase == "" {
		// An idle room: there is no countdown to draw, so the pane says
		// what the room is and what would start it.
		card := lipgloss.JoinVertical(lipgloss.Center, m.viewIdle()...)
		if m.chrome {
			return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
		}
		return lipgloss.Place(m.width, 0, lipgloss.Center, lipgloss.Top, card)
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
	if m.timer.Phase == "" {
		// An idle room has no number at all; the pane says so in words.
		return ""
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
	var body []string
	if title := m.titleLine(); title != "" {
		body = append(body, title)
	}
	body = append(body, colour.Bold(true).Render(strings.ToUpper(m.timer.Label)), "")

	if rows := m.digits(2); rows != nil {
		body = append(body, colour.Render(strings.Join(rows, "\n")))
	} else {
		body = append(body, colour.Bold(true).Render(m.countdownText()))
	}
	body = append(body, "")

	if m.timer.BreakPending {
		body = append(body, styleFaint.Render(pendingHint(m.width)))
	} else {
		total := m.timer.TotalSeconds
		barWidth := clamp(m.width-20, 10, 48)
		body = append(body,
			colour.Render(progressBar(elapsedFraction(m.remaining(), total), barWidth))+
				styleFaint.Render("  of "+formatDuration(total)))
		if m.chrome {
			body = append(body, "", styleFaint.Render("ends at "+m.timer.EndsAt.Local().Format("15:04")))
		}
	}
	if m.timer.Note != "" {
		body = append(body, "", styleFaint.Render(clip(m.timer.Note, m.width)))
	}

	if m.err != nil {
		body = append(body, "", styleDanger.Render(clip("offline: "+m.err.Error(), m.width)))
	}
	if m.chrome {
		body = append(body, "", styleFaint.Render(helpLine(m.width, m.timer.room())))
	}
	return body
}

// viewCompact keeps the block digits but drops everything that only
// explains them — the end time, the help line — because at this size the
// number is the whole message.
func (m *watchModel) viewCompact() []string {
	colour := m.phaseStyle()
	var body []string
	if title := m.titleLine(); title != "" {
		body = append(body, title)
	}
	body = append(body, colour.Bold(true).Render(strings.ToUpper(m.timer.Label)), "")

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
		colour.Render(progressBar(elapsedFraction(m.remaining(), m.timer.TotalSeconds), barWidth)))
}

// viewMini is two lines of plain text — no glyphs would fit, and a strip
// this narrow is being watched out of the corner of an eye anyway.
func (m *watchModel) viewMini() []string {
	colour := m.phaseStyle()
	return []string{
		colour.Render(clip(m.timer.Label, m.width)),
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
func helpLine(width int, room bool) string {
	// Leaving the pane and ending the block are different things, and the
	// pane named only the first: c was the way out of a block you no
	// longer wanted and appeared on no screen that was showing one.
	full := "esc or q desk — the block runs on · x ends it"
	short := "esc or q desk · x ends it"
	if room {
		// A room's block has somewhere else to be — the next room, or your
		// own timer — and two actions the private block has no version of.
		full = "esc or q desk · tab block · J/K rooms · i I'm in · x leave"
		short = "esc or q desk · J/K rooms"
	}
	if width >= len([]rune(full)) {
		return full
	}
	if width >= len([]rune(short)) {
		return short
	}
	return "q desk"
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

// titleLine names the room a block belongs to. The private block has no
// title: it is the only one that is nobody else's.
func (m *watchModel) titleLine() string {
	if m.timer == nil || m.timer.Title == "" {
		return ""
	}
	label := m.timer.Title
	if m.timer.Code != "" {
		label += " · " + m.timer.Code
	}
	return styleFaint.Render(clip(label, m.width))
}

// viewIdle is a room with nothing running. It is deliberately not the
// "no block running" line the private timer shows — that one is about you,
// and this one is about a room you can start.
func (m *watchModel) viewIdle() []string {
	body := []string{}
	if title := m.titleLine(); title != "" {
		body = append(body, title, "")
	}
	body = append(body, styleFaint.Render(clip("idle", m.width)))
	if m.timer.Note != "" {
		body = append(body, "", styleFaint.Render(clip(m.timer.Note, m.width)))
	}
	if m.height > 4 {
		body = append(body, "", styleFaint.Render(clip("f starts a block here", m.width)))
	}
	return body
}
