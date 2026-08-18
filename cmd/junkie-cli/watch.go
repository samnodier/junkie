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
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
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
		body = append(body, "", styleFaint.Render("ends at "+m.timer.EndsAt.Local().Format("15:04")))
	}

	if m.err != nil {
		body = append(body, "", styleDanger.Render(fit("offline: "+m.err.Error(), m.width)))
	}
	return append(body, "", styleFaint.Render(helpLine(m.width)))
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
		colour.Render(fit(phaseLabel(m.timer), m.width)),
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
	const full = "q quit · r refresh · the block keeps running either way"
	const short = "q quit · r refresh"
	if width >= len([]rune(full)) {
		return full
	}
	if width >= len([]rune(short)) {
		return short
	}
	return "q quit"
}

func pendingHint(width int) string {
	const full = "waiting to start · `junkie break` to take it, `junkie skip` to go on"
	const short = "`junkie break` or `junkie skip`"
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
