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

	// cursor indexes the visible todos. editing holds the line being typed
	// when a todo is being added or edited; editingID is empty for a new
	// one and set when an existing one is being rewritten.
	cursor    int
	editing   *editor
	editingID string

	// undoID is the todo `d` last removed, so `u` can put it back. Removal
	// is reversible server-side (removed is a flag, not a delete), which is
	// what makes a one-key undo honest rather than a second guess.
	undoID string

	width, height int
}

// editor is a one-line text field. Hand-rolled rather than pulled from
// bubbles: a todo is a single line with no cursor movement, and the whole
// behaviour is runes in, backspace out.
type editor struct {
	label string
	text  []rune
}

func (e *editor) insert(runes []rune) { e.text = append(e.text, runes...) }

func (e *editor) backspace() {
	if len(e.text) > 0 {
		e.text = e.text[:len(e.text)-1]
	}
}

func (e *editor) clear() { e.text = nil }

func (e *editor) value() string { return strings.TrimSpace(string(e.text)) }

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

// visibleTodos is the working list: removed todos are a bin on the web, and
// listing them here would put the cursor on rows whose only action is
// restore.
func (m *dashModel) visibleTodos() []apiTodo {
	out := make([]apiTodo, 0, len(m.desk.Todos))
	for _, t := range m.desk.Todos {
		if !t.Removed {
			out = append(out, t)
		}
	}
	return out
}

// clampCursor keeps the selection on a real row after the list changes
// underneath it — a todo removed here, or added from the web.
func (m *dashModel) clampCursor() {
	n := len(m.visibleTodos())
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// selected is the highlighted todo, if there is one.
func (m *dashModel) selected() (apiTodo, bool) {
	todos := m.visibleTodos()
	if m.cursor < 0 || m.cursor >= len(todos) {
		return apiTodo{}, false
	}
	return todos[m.cursor], true
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
		m.clampCursor()
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
	// While a line is being typed, every key belongs to it — otherwise a
	// todo containing "q" would quit the program mid-word.
	if m.editing != nil {
		return m.handleEditKey(msg)
	}
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

	case "j", "down":
		m.cursor++
		m.clampCursor()
		return m, nil
	case "k", "up":
		m.cursor--
		m.clampCursor()
		return m, nil

	case "a":
		m.editing = &editor{label: "new todo"}
		m.editingID = ""
		return m, nil
	case "e":
		todo, ok := m.selected()
		if !ok {
			return m, nil
		}
		// Only active todos are editable server-side; completed and removed
		// ones keep the text they had when they changed state.
		if todo.Done {
			m.note("completed todos can't be edited")
			return m, nil
		}
		m.editing = &editor{label: "edit", text: []rune(todo.Text)}
		m.editingID = todo.ID
		return m, nil

	case " ", "enter":
		todo, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, m.do("/todo/"+todo.ID+"/toggle", nil, "")
	case "d":
		todo, ok := m.selected()
		if !ok {
			return m, nil
		}
		// Remembered so u can put it back: removal is a flag server-side,
		// not a delete, so the undo is real rather than a re-create.
		m.undoID = todo.ID
		return m, m.do("/todo/"+todo.ID+"/remove", nil, "removed · u to undo")
	case "u":
		if m.undoID == "" {
			m.note("nothing to undo")
			return m, nil
		}
		id := m.undoID
		m.undoID = ""
		return m, m.do("/todo/"+id+"/restore", nil, "restored")
	}
	return m, nil
}

// handleEditKey drives the one-line field for adding and editing todos.
func (m *dashModel) handleEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.editing = nil
		return m, nil
	case tea.KeyEnter:
		text, id := m.editing.value(), m.editingID
		m.editing = nil
		if text == "" {
			// An emptied todo is a no-op, not a delete — the same rule the
			// server applies to an edit that arrives blank.
			return m, nil
		}
		if id != "" {
			return m, m.do("/todo/"+id+"/edit", url.Values{"text": {text}}, "edited")
		}
		return m, m.do("/todos", url.Values{"text": {text}}, "added")
	case tea.KeyBackspace:
		m.editing.backspace()
		return m, nil
	case tea.KeyCtrlU:
		m.editing.clear()
		return m, nil
	case tea.KeySpace:
		m.editing.insert([]rune{' '})
		return m, nil
	case tea.KeyRunes:
		m.editing.insert(msg.Runes)
		return m, nil
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

	todos := m.visibleTodos()
	// The window scrolls to keep the selection visible rather than paging,
	// so the cursor never disappears off a short list.
	capacity := rows - 1
	if m.editing != nil && m.editingID == "" {
		capacity--
	}
	start := 0
	if capacity > 0 && m.cursor >= capacity {
		start = m.cursor - capacity + 1
	}
	shown := 0
	for i := start; i < len(todos) && shown < capacity; i++ {
		b.WriteString(m.todoLine(todos[i], i == m.cursor) + "\n")
		shown++
	}
	if m.editing != nil && m.editingID == "" {
		b.WriteString(m.editorLine() + "\n")
	} else if shown == 0 {
		b.WriteString(styleFaint.Render("  nothing on the list · a to add") + "\n")
	}
	return b.String()
}

func (m *dashModel) todoLine(todo apiTodo, selected bool) string {
	// The line being edited shows what is being typed, in place, so the
	// change is seen where it will land.
	if selected && m.editing != nil && m.editingID == todo.ID {
		return m.editorLine()
	}
	cursor := "  "
	if selected {
		cursor = styleAccent.Render("› ")
	}
	mark, text := styleAccent.Render("[ ]"), styleInk
	if todo.Done {
		mark, text = styleFaint.Render("[x]"), styleFaint
	}
	return cursor + mark + " " + text.Render(clip(todo.Text, m.width-6))
}

// editorLine draws the field being typed, with a block cursor at the end.
func (m *dashModel) editorLine() string {
	typed := string(m.editing.text)
	// Keep the tail visible: someone typing a long todo watches the end of
	// it, not the beginning.
	room := m.width - len([]rune(m.editing.label)) - 5
	if runes := []rune(typed); room > 0 && len(runes) > room {
		typed = "…" + string(runes[len(runes)-room+1:])
	}
	return styleAccent.Render("› ") + styleWarn.Render(m.editing.label+" ") +
		styleInk.Render(typed) + styleAccent.Render("▌")
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
	if m.editing != nil {
		return styleFaint.Render(clip(editKeys, m.width))
	}
	if m.status != "" && time.Now().Before(m.statusUntil) {
		return styleWarn.Render(clip(m.status, m.width))
	}
	return styleFaint.Render(clip(dashKeys(m.width), m.width))
}

// dashKeys names what the keys do, at whatever length fits. Like the watch
// screen's help, the short forms are written out rather than cut mid-word.
func dashKeys(width int) string {
	const full = "j/k move · space done · a add · e edit · d remove · f focus · b break · q quit"
	const medium = "j/k move · space done · a add · f focus · b break · q quit"
	const short = "j/k · space · a add · f focus · q quit"
	switch {
	case width >= len([]rune(full)):
		return full
	case width >= len([]rune(medium)):
		return medium
	case width >= len([]rune(short)):
		return short
	default:
		return "q quit"
	}
}

// editKeys replaces the hints while a line is being typed: none of the
// normal keys apply, and saying so is the whole job.
const editKeys = "enter save · esc cancel · ctrl+u clear"

// runDashboard opens the desk full-screen.
func runDashboard(c *client, user string) error {
	desk, err := c.desk()
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(newDashModel(c, user, desk), tea.WithAltScreen()).Run()
	return err
}
