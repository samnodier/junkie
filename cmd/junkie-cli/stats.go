package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The work map is the one part of junkie a terminal draws better than a
// browser: a year of focused days is a grid of coloured cells, and a
// terminal is already a grid of coloured cells.
//
// The server sends the grid pre-laid-out — the same cells the web renders,
// indexed week*7+weekday — so this only has to colour them and decide how
// many weeks the window has room for.

type profileResponse struct {
	RoomsCount int        `json:"roomsCount"`
	Heatmap    apiHeatmap `json:"heatmap"`
}

type apiHeatmap struct {
	Cells        []heatCell  `json:"cells"`
	Months       []heatMonth `json:"months"`
	Weeks        int         `json:"weeks"`
	TotalMinutes int         `json:"totalMinutes"`
}

// Empty marks a cell outside the year — the padding at either end of the
// first and last weeks — as opposed to a day with no focus on it, which is
// a real day at level 0.
type heatCell struct {
	Empty   bool   `json:"empty"`
	Date    string `json:"date"`
	Minutes int    `json:"minutes"`
	Level   int    `json:"level"`
}

type heatMonth struct {
	Label string `json:"label"`
	Col   int    `json:"col"`
}

// heatColours are junkie's own five levels, from web/src/assets/app.css.
var heatColours = []lipgloss.AdaptiveColor{
	{Light: "#e7e5d8", Dark: "#202920"},
	{Light: "#bfdcc6", Dark: "#274c36"},
	{Light: "#8cc3a0", Dark: "#2f6b47"},
	{Light: "#55a278", Dark: "#3f8f60"},
	{Light: "#2c7a52", Dark: "#62bc85"},
}

func heatStyle(level int) lipgloss.Style {
	if level < 0 {
		level = 0
	}
	if level >= len(heatColours) {
		level = len(heatColours) - 1
	}
	return lipgloss.NewStyle().Foreground(heatColours[level])
}

// dayLabels are the rows, Sunday first — the order the server builds the
// grid in. Only three are printed, as the web does, because seven labels
// beside a grid of squares is more ink than information.
var dayLabels = [7]string{"", "Mon", "", "Wed", "", "Fri", ""}

const labelColumn = 4

// renderHeatmap draws the year. width 0 means no limit (piped output);
// otherwise the grid is trimmed from the left, keeping the most recent
// weeks, because this month is what anyone reads first.
func renderHeatmap(h apiHeatmap, width int) string {
	if h.Weeks == 0 || len(h.Cells) == 0 {
		return styleFaint.Render("  no focus recorded yet") + "\n"
	}
	first := 0
	if width > 0 {
		if room := width - labelColumn - 1; room < h.Weeks {
			first = h.Weeks - room
		}
	}
	if first < 0 {
		first = 0
	}

	var b strings.Builder
	b.WriteString(monthRow(h, first))
	for dow := 0; dow < 7; dow++ {
		b.WriteString(fmt.Sprintf("%-*s", labelColumn, dayLabels[dow]))
		for week := first; week < h.Weeks; week++ {
			idx := week*7 + dow
			if idx >= len(h.Cells) {
				break
			}
			cell := h.Cells[idx]
			if cell.Empty {
				b.WriteString(" ")
				continue
			}
			b.WriteString(heatStyle(cell.Level).Render("■"))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// monthRow places each month's label over the week it starts in.
func monthRow(h apiHeatmap, first int) string {
	row := make([]rune, 0, h.Weeks-first)
	for _, month := range h.Months {
		col := month.Col - first
		if col < 0 || col >= h.Weeks-first {
			continue
		}
		// Pad out to the label's column, then write it. A label that would
		// collide with the previous one is skipped rather than overlapping.
		if col < len(row) {
			continue
		}
		for len(row) < col {
			row = append(row, ' ')
		}
		row = append(row, []rune(month.Label)...)
	}
	return strings.Repeat(" ", labelColumn) + styleFaint.Render(string(row)) + "\n"
}

// heatSummary is what the grid does not say out loud: the total, how long
// the current run of days is, and the best one.
func heatSummary(h apiHeatmap) string {
	current, longest, best := streaks(h.Cells)
	var b strings.Builder
	b.WriteString(labeled("total", styleInk.Render(formatMinutes(h.TotalMinutes))))
	b.WriteString(labeled("streak", styleInk.Render(plural(current, "day"))+
		styleFaint.Render(fmt.Sprintf(" · longest %s", plural(longest, "day")))))
	if best.Minutes > 0 {
		b.WriteString(labeled("best day", styleInk.Render(best.Date)+
			styleFaint.Render(" · "+formatMinutes(best.Minutes))))
	}
	return b.String()
}

// streaks walks the grid in date order. Empty cells are padding outside the
// year, not gaps in it, so they neither extend nor break a run.
//
// The current streak is counted from the end backwards, and a today with no
// focus on it yet does not break it — the day is not over.
func streaks(cells []heatCell) (current, longest int, best heatCell) {
	days := make([]heatCell, 0, len(cells))
	// The grid is column-major (week*7+weekday), so date order means
	// walking weekdays within each week.
	for week := 0; week*7 < len(cells); week++ {
		for dow := 0; dow < 7; dow++ {
			idx := week*7 + dow
			if idx >= len(cells) {
				break
			}
			if !cells[idx].Empty {
				days = append(days, cells[idx])
			}
		}
	}

	run := 0
	for _, day := range days {
		if day.Minutes > 0 {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
		if day.Minutes > best.Minutes {
			best = day
		}
	}

	for i := len(days) - 1; i >= 0; i-- {
		if days[i].Minutes > 0 {
			current++
			continue
		}
		// Today with nothing on it yet is not a broken streak: there is
		// still time. Any earlier blank day is.
		if i == len(days)-1 {
			continue
		}
		break
	}
	return current, longest, best
}

// formatMinutes says hours and minutes the way a person would.
func formatMinutes(minutes int) string {
	if minutes <= 0 {
		return "no focus yet"
	}
	if minutes < 60 {
		return plural(minutes, "minute")
	}
	h, m := minutes/60, minutes%60
	if m == 0 {
		return plural(h, "hour")
	}
	return fmt.Sprintf("%s %s", plural(h, "hour"), plural(m, "minute"))
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// legend is the level scale, so a colour can be read as an amount.
func legend() string {
	var b strings.Builder
	b.WriteString(styleFaint.Render("less "))
	for level := range heatColours {
		b.WriteString(heatStyle(level).Render("■"))
	}
	b.WriteString(styleFaint.Render(" more"))
	return b.String()
}

func renderStats(p profileResponse, width int) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(renderHeatmap(p.Heatmap, width))
	b.WriteString("\n")
	b.WriteString(heatSummary(p.Heatmap))
	b.WriteString(labeled("rooms", styleInk.Render(plural(p.RoomsCount, "room"))))
	b.WriteString("\n" + strings.Repeat(" ", labelColumn) + legend() + "\n\n")
	return b.String()
}
