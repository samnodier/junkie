package main

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The dashboard is the whole desk in one screen: what the timer is doing,
// what is on the list, and which rooms are live — the terminal's answer to
// leaving a browser tab open all day.
//
// It owns no state of its own. Every key that changes something posts to the
// server and re-reads, so two terminals and a browser can all be open on the
// same desk without any of them holding a stale idea of it.

// dashRefreshInterval is the safety net until the room WebSockets land: the
// countdown needs no polling, but a block started elsewhere should appear
// here without the user reaching for r.
const dashRefreshInterval = 20 * time.Second

// statusHold is how long a one-line confirmation stays up before the footer
// goes back to the key hints.
const statusHold = 4 * time.Second

type dashModel struct {
	client *client
	user   string

	desk deskResponse
	countdown

	refreshing  bool
	lastRefresh time.Time
	err         error

	// status is a transient line — "break started", "could not reach the
	// server" — shown in place of the key hints until statusUntil passes.
	status      string
	statusUntil time.Time

	width, height int
}

func newDashModel(c *client, user string, desk deskResponse) *dashModel {
	m := &dashModel{
		client:      c,
		user:        user,
		desk:        desk,
		lastRefresh: time.Now(),
		width:       80,
		height:      24,
	}
	m.anchor()
	return m
}

// anchor re-bases the countdown whenever a fresh desk arrives.
func (m *dashModel) anchor() {
	if m.desk.SoloTimer != nil {
		m.countdown.reset(m.desk.SoloTimer.SecondsLeft)
	}
}

func (m *dashModel) Init() tea.Cmd { return tick() }

// actionMsg reports a completed mutation. Failure is shown in the footer
// rather than ending the program: the desk on screen is still true, and the
// next keypress can retry.
type actionMsg struct {
	message string
	err     error
}

// do posts a mutation and then re-reads, so the screen only ever shows what
// the server agreed to.
func (m *dashModel) do(path string, form url.Values, message string) tea.Cmd {
	return func() tea.Msg {
		if err := m.client.post(path, form); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{message: message}
	}
}

func (m *dashModel) refresh() tea.Cmd {
	return func() tea.Msg {
		desk, err := m.client.desk()
		return deskMsg{desk: desk, err: err}
	}
}

func (m *dashModel) note(text string) {
	m.status = text
	m.statusUntil = time.Now().Add(statusHold)
}

func (m *dashModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tickMsg:
		cmds := []tea.Cmd{tick()}
		t := m.desk.SoloTimer
		expired := t != nil && !t.BreakPending && m.countdown.remaining() <= 0
		stale := time.Since(m.lastRefresh) >= dashRefreshInterval
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
		m.desk = msg.desk
		m.anchor()
		return m, nil

	case actionMsg:
		if msg.err != nil {
			m.note(msg.err.Error())
		} else if msg.message != "" {
			m.note(msg.message)
		}
		m.refreshing = true
		m.lastRefresh = time.Now()
		return m, m.refresh()
	}
	return m, nil
}

func (m *dashModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	t := m.desk.SoloTimer
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "r":
		if m.refreshing {
			return m, nil
		}
		m.refreshing = true
		return m, m.refresh()
	case "f":
		// Guarded here as well as server-side: /solo/start silently
		// redirects when a run is already live, which would look like a
		// start that did nothing.
		if t != nil {
			m.note("a block is already running")
			return m, nil
		}
		return m, m.do("/solo/start", nil, "focus block started")
	case "b":
		if t == nil || !t.BreakPending {
			m.note("no break is waiting")
			return m, nil
		}
		return m, m.do("/solo/break/start", nil, "break started")
	case "s":
		if t == nil || t.Phase != "break" {
			m.note("there is no break to skip")
			return m, nil
		}
		return m, m.do("/solo/break/skip", nil, "break skipped")
	case "c":
		if t == nil || t.Phase != "focus" {
			m.note("no running block to cancel")
			return m, nil
		}
		return m, m.do("/solo/cancel", nil, "block cancelled — nothing banked")
	}
	return m, nil
}

func (m *dashModel) View() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n")
	b.WriteString(m.timerBlock())
	b.WriteString("\n")

	// Sections are dropped from the bottom up when the window is short, so
	// what survives is always the timer — the reason the screen is open.
	rows := m.height - lipgloss.Height(m.header()) - lipgloss.Height(m.timerBlock()) - 4
	todos, rooms := m.sectionBudget(rows)
	if todos > 0 {
		b.WriteString(m.todoBlock(todos))
		b.WriteString("\n")
	}
	if rooms > 0 {
		b.WriteString(m.roomBlock(rooms))
		b.WriteString("\n")
	}
	return b.String() + m.footer()
}

// sectionBudget splits the rows left over after the timer between the two
// lists, giving the todos the larger share — a room list is a glance, a todo
// list is worked from.
func (m *dashModel) sectionBudget(rows int) (todos, rooms int) {
	if rows <= 2 {
		return 0, 0
	}
	roomCount := len(m.desk.Rooms)
	if roomCount > 4 {
		roomCount = 4
	}
	// Each list costs a heading plus its rows.
	rooms = roomCount
	if rooms > 0 {
		rooms++
	}
	todos = rows - rooms - 1
	if todos < 0 {
		// No room for both: the todos go, the rooms stay, because a live
		// room is time-sensitive in a way a todo is not.
		todos = 0
		if rooms > rows {
			rooms = rows
		}
	}
	return todos, rooms
}

func (m *dashModel) header() string {
	left := styleAccent.Render(clip("junkie", m.width))
	if m.user != "" {
		left += styleFaint.Render(clip(" · "+m.user, m.width-6))
	}
	if m.width < 30 {
		return left + "\n"
	}
	right := styleFaint.Render(time.Now().Format("15:04"))
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return fit(left, m.width) + "\n"
	}
	return left + strings.Repeat(" ", gap) + right + "\n"
}

func (m *dashModel) timerBlock() string {
	t := m.desk.SoloTimer
	if t == nil {
		return styleFaint.Render(clip("no block running · f to start one", m.width)) + "\n"
	}
	colour := lipgloss.NewStyle().Foreground(phaseColor(t.Phase))
	if t.BreakPending {
		head := "BREAK READY"
		return colour.Bold(true).Render(head) +
			styleFaint.Render(clip(fmt.Sprintf(" · %d min · b to take it, s to skip", t.BreakMinutes),
				m.width-len(head))) + "\n"
	}
	left := m.countdown.remaining()
	head := colour.Bold(true).Render(strings.ToUpper(phaseLabel(t))) + "  " +
		colour.Render(formatDuration(left))
	// The bar only earns its columns once the text beside it has room.
	if m.width >= 46 {
		total := phaseSeconds(t)
		barWidth := clamp(m.width-lipgloss.Width(head)-14, 8, 30)
		head += "  " + colour.Render(progressBar(elapsedFraction(left, total), barWidth)) +
			styleFaint.Render(" of "+formatDuration(total))
	}
	return head + "\n"
}

func (m *dashModel) todoBlock(rows int) string {
	var b strings.Builder
	open, done, _ := countTodos(m.desk.Todos)
	b.WriteString(styleLabel.Render("todos") +
		styleFaint.Render(clip(fmt.Sprintf("  %d open · %d done", open, done), m.width-5)) + "\n")

	shown := 0
	for _, todo := range m.desk.Todos {
		if todo.Removed || shown >= rows-1 {
			continue
		}
		b.WriteString("  " + m.todoLine(todo) + "\n")
		shown++
	}
	if shown == 0 {
		b.WriteString(styleFaint.Render("  nothing on the list") + "\n")
	}
	return b.String()
}

func (m *dashModel) todoLine(todo apiTodo) string {
	mark, text := styleAccent.Render("[ ]"), styleInk
	if todo.Done {
		mark, text = styleFaint.Render("[x]"), styleFaint
	}
	return mark + " " + text.Render(clip(todo.Text, m.width-6))
}

func (m *dashModel) roomBlock(rows int) string {
	var b strings.Builder
	b.WriteString(styleLabel.Render("rooms") + "\n")
	shown := 0
	for _, room := range m.desk.Rooms {
		if shown >= rows-1 {
			break
		}
		b.WriteString("  " + roomLine(room, m.width-2) + "\n")
		shown++
	}
	if shown == 0 {
		b.WriteString(styleFaint.Render("  none yet") + "\n")
	}
	return b.String()
}

func (m *dashModel) footer() string {
	if m.err != nil {
		return styleDanger.Render(clip("offline: "+m.err.Error(), m.width))
	}
	if m.status != "" && time.Now().Before(m.statusUntil) {
		return styleWarn.Render(clip(m.status, m.width))
	}
	return styleFaint.Render(clip(dashKeys(m.width), m.width))
}

// dashKeys names what the keys do, at whatever length fits. Like the watch
// screen's help, the short forms are written out rather than cut mid-word.
func dashKeys(width int) string {
	const full = "f focus · b break · s skip · c cancel · r refresh · q quit"
	const short = "f focus · b break · s skip · q quit"
	switch {
	case width >= len([]rune(full)):
		return full
	case width >= len([]rune(short)):
		return short
	default:
		return "q quit"
	}
}

// runDashboard opens the desk full-screen.
func runDashboard(c *client, user string) error {
	desk, err := c.desk()
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(newDashModel(c, user, desk), tea.WithAltScreen()).Run()
	return err
}
