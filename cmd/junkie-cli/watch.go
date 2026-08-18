package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// watchTimer draws the running block full-screen until the user leaves.
//
// The countdown is never authoritative: the server owns phase_ends_at and
// credits the minutes, and this only renders the remainder and pokes the
// server when it runs out. Quitting the watch, or closing the terminal
// outright, therefore costs nothing — the block keeps running and is
// credited whenever anything next reads it.
func watchTimer(c *client) error {
	desk, err := c.desk()
	if err != nil {
		return err
	}
	if desk.SoloTimer == nil {
		fmt.Print(renderSoloLine(nil))
		return nil
	}
	m := newWatchModel(c, desk.SoloTimer)
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// refreshInterval is the safety net: the countdown itself needs no polling,
// but a phase started or cancelled elsewhere (the web, another terminal)
// should show up here within a reasonable time. The room WebSocket makes
// this exact, and will replace it.
const refreshInterval = 30 * time.Second

type watchModel struct {
	client *client
	timer  *soloTimer

	// baseSeconds and fetchedAt anchor the countdown on the server's own
	// reading rather than on its wall-clock deadline: secondsLeft was
	// computed server-side, and time.Since uses a monotonic clock, so
	// neither clock skew between the two machines nor an NTP correction
	// mid-block can shift what this displays.
	baseSeconds int
	fetchedAt   time.Time

	lastRefresh time.Time
	refreshing  bool
	err         error
	width       int
	height      int
}

func newWatchModel(c *client, t *soloTimer) *watchModel {
	return &watchModel{
		client:      c,
		timer:       t,
		baseSeconds: t.SecondsLeft,
		fetchedAt:   time.Now(),
		lastRefresh: time.Now(),
		width:       80,
		height:      24,
	}
}

type tickMsg time.Time

// deskMsg carries a completed refresh. The error rides along rather than
// failing the program: a blip in connectivity should dim the display, not
// tear down a countdown the user is watching.
type deskMsg struct {
	desk deskResponse
	err  error
}

func tick() tea.Cmd {
	return tea.Tick(time.Second/2, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *watchModel) refresh() tea.Cmd {
	return func() tea.Msg {
		desk, err := m.client.desk()
		return deskMsg{desk: desk, err: err}
	}
}

func (m *watchModel) Init() tea.Cmd { return tick() }

func (m *watchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "r":
			if !m.refreshing {
				m.refreshing = true
				return m, m.refresh()
			}
		}
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{tick()}
		// Zero on the clock is the server's cue, not ours: ask it to read
		// the run, which is what performs the focus->break flip and credits
		// the minutes. One in-flight refresh at a time, so a countdown
		// sitting at zero doesn't stack requests every half second.
		expired := m.timer != nil && !m.timer.BreakPending && m.remaining() <= 0
		stale := time.Since(m.lastRefresh) >= refreshInterval
		if !m.refreshing && (expired || stale) {
			m.refreshing = true
			m.lastRefresh = time.Now()
			cmds = append(cmds, m.refresh())
		}
		return m, tea.Batch(cmds...)

	case deskMsg:
		m.refreshing = false
		m.lastRefresh = time.Now()
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		// A run that ended — cancelled here or elsewhere, or a break that
		// went stale — leaves nothing to watch.
		if msg.desk.SoloTimer == nil {
			m.timer = nil
			return m, tea.Quit
		}
		m.timer = msg.desk.SoloTimer
		m.baseSeconds = msg.desk.SoloTimer.SecondsLeft
		m.fetchedAt = time.Now()
		return m, nil
	}
	return m, nil
}

// remaining is the countdown's single source of truth for the display.
func (m *watchModel) remaining() int {
	if m.timer == nil || m.timer.BreakPending {
		return 0
	}
	left := m.baseSeconds - int(time.Since(m.fetchedAt).Seconds())
	if left < 0 {
		return 0
	}
	return left
}

func (m *watchModel) View() string {
	if m.timer == nil {
		return ""
	}
	colour := lipgloss.NewStyle().Foreground(phaseColor(m.timer.Phase))
	var body []string

	body = append(body, colour.Bold(true).Render(strings.ToUpper(phaseLabel(m.timer))))
	body = append(body, "")

	if m.timer.BreakPending {
		// A pending break has no deadline to count down — it is an offer.
		body = append(body, colour.Render(strings.Join(bigDigits(fmt.Sprintf("%02d:00", m.timer.BreakMinutes)), "\n")))
		body = append(body, "")
		body = append(body, styleFaint.Render("waiting to start · `junkie break` to take it, `junkie skip` to go on"))
	} else {
		left := m.remaining()
		body = append(body, colour.Render(strings.Join(bigDigits(formatDuration(left)), "\n")))
		body = append(body, "")
		total := phaseSeconds(m.timer)
		barWidth := clamp(m.width-20, 10, 48)
		body = append(body,
			colour.Render(progressBar(elapsedFraction(left, total), barWidth))+
				styleFaint.Render("  of "+formatDuration(total)))
		body = append(body, "")
		body = append(body, styleFaint.Render("ends at "+m.timer.EndsAt.Local().Format("15:04")))
	}

	if m.err != nil {
		body = append(body, "", styleDanger.Render("offline: "+m.err.Error()))
	}
	body = append(body, "", styleFaint.Render("q quit · r refresh · the block keeps running either way"))

	card := lipgloss.JoinVertical(lipgloss.Center, body...)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
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
