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
func renderStatus(desk deskResponse) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(renderSoloLine(desk.SoloTimer))

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
			b.WriteString(labeled(label, roomLine(room)))
		}
	}
	b.WriteString("\n")
	return b.String()
}

// renderSoloLine is the timer's two lines — state and, when something is
// actually counting down, a bar showing how far through the phase it is.
func renderSoloLine(t *soloTimer) string {
	if t == nil {
		return labeled("timer", styleFaint.Render("nothing running")+
			styleFaint.Render(" · `junkie focus` to start a block")) + "\n"
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

// roomLine states what one room is doing in a single line.
func roomLine(room deskRoom) string {
	head := styleInk.Render(room.Code) + styleFaint.Render(" "+room.Name)
	t := room.Timer
	if t == nil {
		return head + styleFaint.Render(" · idle")
	}
	style := lipgloss.NewStyle().Foreground(phaseColor(t.Phase))
	var state string
	switch {
	case t.Phase == "lobby":
		state = style.Render("starting") + fmt.Sprintf(" · %s to join", formatDuration(t.SecondsLeft))
	case t.BreakPending:
		state = style.Render("break ready")
	case t.Paused:
		state = style.Render("break paused") + fmt.Sprintf(" · %s left", formatDuration(t.SecondsLeft))
	default:
		state = style.Render(t.Phase) + fmt.Sprintf(" · %s left", formatDuration(t.SecondsLeft))
	}
	if t.TotalSessions > 1 {
		state += styleFaint.Render(fmt.Sprintf(" · session %d/%d", t.CurrentSession, t.TotalSessions))
	}
	if n := len(t.Participants); n > 0 {
		state += styleFaint.Render(fmt.Sprintf(" · %d here", n))
	}
	if !t.Participant {
		state += styleFaint.Render(" · you're out")
	}
	return head + " · " + state
}

// renderTodos lists the private todos, open first. Removed ones are counted
// but not listed: they are a recycle bin on the web, not a working list.
func renderTodos(todos []apiTodo) string {
	var b strings.Builder
	b.WriteString("\n")
	shown := 0
	for _, t := range todos {
		if t.Removed {
			continue
		}
		mark := styleAccent.Render("[ ]")
		text := styleInk.Render(t.Text)
		if t.Done {
			mark = styleFaint.Render("[x]")
			text = styleFaint.Render(t.Text)
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

func renderRooms(rooms []deskRoom) string {
	var b strings.Builder
	b.WriteString("\n")
	if len(rooms) == 0 {
		b.WriteString(styleFaint.Render("  you're not in any rooms yet\n\n"))
		return b.String()
	}
	for _, room := range rooms {
		b.WriteString("  " + roomLine(room) + "\n")
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
