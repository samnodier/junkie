package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The read commands share one visual grammar: a dim lowercase label in the
// left column, the thing itself in the right. It is the terminal's version
// of the web's small-caps labels above each panel.
const labelWidth = 8

func labeled(label, body string) string {
	return styleLabel.Render(fmt.Sprintf("%-*s", labelWidth, label)) + body + "\n"
}

// renderStatus is the desk in a dozen lines: what the timer is doing, how
// the todos stand, and which rooms are live.
func renderStatus(desk deskResponse, width int) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(renderSoloLine(desk.SoloTimer, width))

	open, done, removed := countTodos(desk.Todos)
	summary := fmt.Sprintf("%d open", open)
	if done > 0 {
		summary += fmt.Sprintf(", %d done", done)
	}
	if removed > 0 {
		summary += styleFaint.Render(fmt.Sprintf(", %d removed", removed))
	}
	b.WriteString(labeled("todos", summary))

	if len(desk.Rooms) == 0 {
		b.WriteString(labeled("rooms", styleFaint.Render("none yet")))
	} else {
		for i, room := range desk.Rooms {
			label := "rooms"
			if i > 0 {
				label = ""
			}
			b.WriteString(labeled(label, roomLine(room, width-labelWidth)))
		}
	}
	b.WriteString("\n")
	return b.String()
}

// renderSoloLine is the timer's two lines — state and, when something is
// actually counting down, a bar showing how far through the phase it is.
func renderSoloLine(t *soloTimer, width int) string {
	if t == nil {
		return labeled("timer", styleFaint.Render(
			fit("nothing running · `junkie focus` to start a block", width-labelWidth))) + "\n"
	}
	style := lipgloss.NewStyle().Foreground(phaseColor(t.Phase)).Bold(true)
	if t.BreakPending {
		return labeled("timer", style.Render("break ready")+
			fmt.Sprintf(" · %d min offered", t.BreakMinutes)+
			styleFaint.Render(" · `junkie break` or `junkie skip`")) + "\n"
	}
	total := phaseSeconds(t)
	line := labeled("timer", style.Render(phaseLabel(t))+
		fmt.Sprintf(" · %s left", formatDuration(t.SecondsLeft))+
		styleFaint.Render(" · ends "+t.EndsAt.Local().Format("15:04")))
	bar := lipgloss.NewStyle().Foreground(phaseColor(t.Phase)).
		Render(progressBar(elapsedFraction(t.SecondsLeft, total), 24))
	return line + labeled("", bar+styleFaint.Render(" of "+formatDuration(total))) + "\n"
}

// roomLine states what one room is doing in a single line, fitted to width
// (0 for no limit).
//
// Narrow terminals drop detail in order of what you came for: the extras
// (who is here, which session) go first, then the time left, and the name is
// clipped to whatever room is left over. The code and the phase are the last
// things standing, because a room you cannot identify or whose state you
// cannot read is not worth a line at all.
func roomLine(room deskRoom, width int) string {
	word, rest, tail, style := roomStateParts(room)

	for _, level := range []struct{ withRest, withTail bool }{
		{true, true}, {true, false}, {false, false},
	} {
		plain := room.Code + " · " + word
		if level.withRest {
			plain += rest
		}
		if level.withTail {
			plain += tail
		}
		if width > 0 && len([]rune(plain)) > width {
			continue
		}

		name := room.Name
		if width > 0 {
			// One column for the space that would precede it.
			name = clip(name, width-len([]rune(plain))-1)
		}
		out := styleInk.Render(room.Code)
		if name != "" {
			out += styleFaint.Render(" " + name)
		}
		out += styleFaint.Render(" · ") + style.Render(word)
		if level.withRest {
			out += styleFaint.Render(rest)
		}
		if level.withTail {
			out += styleFaint.Render(tail)
		}
		return out
	}
	// Not even the code and the phase fit; show as much of them as does.
	return styleInk.Render(clip(room.Code+" · "+word, width))
}

// roomStateParts breaks what a room's timer is doing into the word for the
// phase, the time beside it, and the extras — so roomLine can decide how
// many of them the terminal has room for. They come back as plain text with
// the phase's colour separately, since measuring a styled string means
// counting escape sequences.
func roomStateParts(room deskRoom) (word, rest, tail string, style lipgloss.Style) {
	t := room.Timer
	if t == nil {
		return "idle", "", "", lipgloss.NewStyle().Foreground(colFaint)
	}
	style = lipgloss.NewStyle().Foreground(phaseColor(t.Phase))
	switch {
	case t.Phase == "lobby":
		word, rest = "starting", fmt.Sprintf(" · %s to join", formatDuration(t.SecondsLeft))
	case t.BreakPending:
		word = "break ready"
	case t.Paused:
		word, rest = "break paused", fmt.Sprintf(" · %s left", formatDuration(t.SecondsLeft))
	default:
		word, rest = t.Phase, fmt.Sprintf(" · %s left", formatDuration(t.SecondsLeft))
	}
	if t.TotalSessions > 1 {
		tail += fmt.Sprintf(" · session %d/%d", t.CurrentSession, t.TotalSessions)
	}
	if n := len(t.Participants); n > 0 {
		tail += fmt.Sprintf(" · %d here", n)
	}
	if !t.Participant {
		tail += " · you're out"
	}
	return word, rest, tail, style
}

// renderTodos lists the private todos, open first. Removed ones are counted
// but not listed: they are a recycle bin on the web, not a working list.
func renderTodos(todos []apiTodo, width int) string {
	var b strings.Builder
	b.WriteString("\n")
	shown := 0
	for _, t := range todos {
		if t.Removed {
			continue
		}
		body := fit(t.Text, width-6)
		mark, text := styleAccent.Render("[ ]"), styleInk.Render(body)
		if t.Done {
			mark, text = styleFaint.Render("[x]"), styleFaint.Render(body)
		}
		b.WriteString("  " + mark + " " + text + "\n")
		shown++
	}
	if shown == 0 {
		b.WriteString(styleFaint.Render("  nothing on the list\n"))
	}
	if _, _, removed := countTodos(todos); removed > 0 {
		b.WriteString(styleFaint.Render(fmt.Sprintf("\n  %d removed\n", removed)))
	}
	b.WriteString("\n")
	return b.String()
}

func renderRooms(rooms []deskRoom, width int) string {
	var b strings.Builder
	b.WriteString("\n")
	if len(rooms) == 0 {
		b.WriteString(styleFaint.Render("  you're not in any rooms yet\n\n"))
		return b.String()
	}
	for _, room := range rooms {
		b.WriteString("  " + roomLine(room, width-2) + "\n")
		if open, _, _ := countTodos(room.Mine); open > 0 {
			b.WriteString(styleFaint.Render(fmt.Sprintf("    %d of your todos here\n", open)))
		}
	}
	b.WriteString("\n")
	return b.String()
}

func countTodos(todos []apiTodo) (open, done, removed int) {
	for _, t := range todos {
		switch {
		case t.Removed:
			removed++
		case t.Done:
			done++
		default:
			open++
		}
	}
	return open, done, removed
}

// phaseSeconds is the length of the phase currently running, which is what
// the progress bar measures against.
func phaseSeconds(t *soloTimer) int {
	if t == nil {
		return 0
	}
	if t.Phase == "break" {
		return t.BreakMinutes * 60
	}
	return t.FocusMinutes * 60
}

// elapsedFraction converts "seconds left of total" into how much of the
// phase is behind you. A total of zero has no meaningful fraction — that is
// a pending break, which draws no bar — so it reports empty rather than
// dividing by zero.
func elapsedFraction(secondsLeft, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(total-secondsLeft) / float64(total)
}
