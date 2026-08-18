package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// buildHeat lays out weeks of days the way the server does — column-major,
// week*7+weekday — with minutes given per day in date order.
func buildHeat(weeks int, minutesByDay map[int]int, leadingEmpty int) apiHeatmap {
	cells := make([]heatCell, weeks*7)
	day := 0
	for week := 0; week < weeks; week++ {
		for dow := 0; dow < 7; dow++ {
			idx := week*7 + dow
			if idx < leadingEmpty {
				cells[idx] = heatCell{Empty: true}
				continue
			}
			minutes := minutesByDay[day]
			level := 0
			if minutes > 0 {
				level = 1 + minutes/60
				if level > 4 {
					level = 4
				}
			}
			cells[idx] = heatCell{Date: fmt.Sprintf("Day %d", day), Minutes: minutes, Level: level}
			day++
		}
	}
	total := 0
	for _, m := range minutesByDay {
		total += m
	}
	return apiHeatmap{Cells: cells, Weeks: weeks, TotalMinutes: total,
		Months: []heatMonth{{Label: "Jan", Col: 0}, {Label: "Feb", Col: 4}}}
}

func TestRenderHeatmapShape(t *testing.T) {
	h := buildHeat(8, map[int]int{0: 30, 1: 90, 3: 200}, 0)
	out := renderHeatmap(h, 0)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// One month row plus seven weekday rows.
	if len(lines) != 8 {
		t.Fatalf("expected 8 lines, got %d:\n%s", len(lines), out)
	}
	for _, want := range []string{"Mon", "Wed", "Fri"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing the %s label:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "■") {
		t.Errorf("no cells drawn:\n%s", out)
	}
}

// A year is 53 columns wide. A narrower terminal keeps the most recent
// weeks, because this month is what anyone reads first.
func TestRenderHeatmapTrimsToWidth(t *testing.T) {
	h := buildHeat(53, map[int]int{}, 0)
	for _, width := range []int{20, 30, 40, 60, 80} {
		out := renderHeatmap(h, width)
		for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Errorf("width %d: line %d is %d wide", width, i, got)
			}
		}
	}
	// Given room, the whole year is drawn.
	wide := strings.Split(renderHeatmap(h, 0), "\n")[1]
	if got := lipgloss.Width(wide); got != labelColumn+53 {
		t.Errorf("unlimited width drew %d columns, want %d", got, labelColumn+53)
	}
}

func TestRenderHeatmapEmpty(t *testing.T) {
	if out := renderHeatmap(apiHeatmap{}, 80); !strings.Contains(out, "no focus recorded yet") {
		t.Errorf("empty heatmap reads as %q", out)
	}
}

func TestStreaks(t *testing.T) {
	// Days 0-2 worked, 3 blank, 4-8 worked, ending on a worked day.
	h := buildHeat(2, map[int]int{0: 30, 1: 30, 2: 30, 4: 30, 5: 30, 6: 30, 7: 30, 8: 30}, 0)
	// Trim to exactly nine days so the last one is the end of the record.
	h.Cells = h.Cells[:9]
	current, longest, best := streaks(h.Cells)
	if longest != 5 {
		t.Errorf("longest = %d, want 5", longest)
	}
	if current != 5 {
		t.Errorf("current = %d, want 5", current)
	}
	if best.Minutes != 30 {
		t.Errorf("best day = %+v", best)
	}
}

// Today with nothing on it yet is not a broken streak — the day is not over.
func TestStreakSurvivesAnUnworkedToday(t *testing.T) {
	h := buildHeat(1, map[int]int{0: 30, 1: 30, 2: 30}, 0)
	h.Cells = h.Cells[:4] // three worked days, then today, still blank
	if current, _, _ := streaks(h.Cells); current != 3 {
		t.Errorf("current = %d, want 3 — today is not over", current)
	}
	// A blank day before the last one does break it.
	h.Cells = h.Cells[:5]
	h.Cells[4] = heatCell{Date: "Day 4", Minutes: 60, Level: 1}
	if current, _, _ := streaks(h.Cells); current != 1 {
		t.Errorf("current = %d, want 1 — the gap breaks the run", current)
	}
}

// Empty cells are padding outside the year, not gaps in it, so they neither
// extend nor break a run.
func TestStreaksIgnorePadding(t *testing.T) {
	h := buildHeat(2, map[int]int{0: 30, 1: 30}, 3)
	h.Cells = h.Cells[:5]
	if current, longest, _ := streaks(h.Cells); current != 2 || longest != 2 {
		t.Errorf("current = %d, longest = %d, want 2/2", current, longest)
	}
}

func TestFormatMinutes(t *testing.T) {
	tests := map[int]string{
		0:    "no focus yet",
		1:    "1 minute",
		45:   "45 minutes",
		60:   "1 hour",
		120:  "2 hours",
		90:   "1 hour 30 minutes",
		1501: "25 hours 1 minute",
	}
	for minutes, want := range tests {
		if got := formatMinutes(minutes); got != want {
			t.Errorf("formatMinutes(%d) = %q, want %q", minutes, got, want)
		}
	}
}

func TestRenderStats(t *testing.T) {
	h := buildHeat(6, map[int]int{0: 120, 1: 60}, 0)
	out := renderStats(profileResponse{RoomsCount: 2, Heatmap: h}, 80)
	for _, want := range []string{"total", "3 hours", "streak", "best day", "rooms", "2 rooms", "less", "more"} {
		if !strings.Contains(out, want) {
			t.Errorf("stats missing %q:\n%s", want, out)
		}
	}
}

// A level outside the palette must not index past its end.
func TestHeatStyleClampsLevel(t *testing.T) {
	for _, level := range []int{-3, 0, 4, 9} {
		if got := heatStyle(level).Render("■"); got == "" {
			t.Errorf("level %d rendered nothing", level)
		}
	}
}
