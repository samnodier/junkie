package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The dashboard is the whole desk in one screen: what the timer is doing,
// what is on the list, and which rooms are live — the terminal's answer to
// leaving a browser tab open all day. `junkie watch` is this same program
// with the timer pane filling the window, not a second one that quits when
// the block ends.
//
// It owns no state of its own. Every key that changes something goes to the
// store and re-reads, so two terminals and a browser can all be open on the
// same desk without any of them holding a stale idea of it. Guest mode is
// the same screen against files on this machine.

// dashRefreshInterval is the safety net until the room WebSockets land: the
// countdown needs no polling, but a block started elsewhere should appear
// here without the user reaching for r. Guest has no elsewhere, but the
// interval still advances an expired local phase.
const dashRefreshInterval = 20 * time.Second

// statusHold is how long a one-line confirmation stays up before the footer
// goes back to the key hints.
const statusHold = 4 * time.Second

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

type dashModel struct {
	store    store
	identity identity

	user   string
	userID string

	// prompt is the "X is starting a block — join?" offer. The server opens
	// a 30-second lobby when a run starts (startRoomTimer) and broadcasts
	// it; answering nothing is a real answer, and the offer simply expires.
	prompt *joinPrompt

	// joining marks the one-line field as a room code rather than a todo:
	// the two share the editor, and only the key that opened it knows
	// which was meant.
	joining bool

	// confirm is a yes/no asked before something this desk cannot undo --
	// leaving a block. It takes the footer until it is answered.
	confirm *confirmPrompt

	desk deskResponse
	countdown

	// subject is which timer the pane draws — the private block, or a room
	// by its code. See subject.go: being in a room and in your own block at
	// once used to mean only ever seeing the latter.
	subject string
	// picked records that the user chose the subject themselves, so a
	// refresh never moves the pane out from under them.
	picked bool

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

	// zoom is `junkie watch`: the timer pane takes the window. Esc returns
	// to the desk; a run ending does not.
	zoom bool

	login *loginForm

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

// joinPrompt is one room's lobby, waiting on an answer.
type joinPrompt struct {
	code     string
	roomName string
	starter  string
	deadline time.Time
}

func (p *joinPrompt) secondsLeft() int {
	left := int(time.Until(p.deadline).Seconds())
	if left < 0 {
		return 0
	}
	return left
}

func newDashModel(s store, id identity, desk deskResponse) *dashModel {
	m := &dashModel{
		store:       s,
		identity:    id,
		user:        id.User,
		userID:      id.UserID,
		desk:        desk,
		lastRefresh: time.Now(),
		width:       80,
		height:      24,
	}
	m.subject = openingSubject(desk, "")
	m.anchor()
	return m
}

// openOn puts the desk in front of one room, for `junkie watch CODE` and
// `junkie dash CODE`. An unknown code is refused by the caller before this
// is reached, so a subject set here is one the desk knows about.
func (m *dashModel) openOn(code string) {
	if code == "" {
		return
	}
	m.picked = true
	m.selectSubject(code)
}

func deskRoomCodes(desk deskResponse) []string {
	codes := make([]string, 0, len(desk.Rooms))
	for _, room := range desk.Rooms {
		codes = append(codes, room.Code)
	}
	return codes
}

// sameCodes reports whether the room set is unchanged, so the desk only
// tears its sockets down when it actually has different rooms to watch.
func sameCodes(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// anchor re-bases the countdown whenever a fresh desk arrives, or the
// subject changes — the digits belong to whichever block the pane is on.
func (m *dashModel) anchor() {
	if f := m.currentFace(); f.counting() {
		m.countdown.reset(f.SecondsLeft)
	}
}

// visibleTodos is the working list: removed todos are a bin on the web, and
// listing them here would put the cursor on rows whose only action is
// restore.
func (m *dashModel) visibleTodos() []apiTodo {
	all := m.subjectTodos()
	out := make([]apiTodo, 0, len(all))
	for _, t := range all {
		if !t.Removed {
			out = append(out, t)
		}
	}
	return out
}

// subjectTodos is the list the pane works on: your private todos with your
// own block on screen, and the room's -- yours first, then everyone else's
// -- with a room on screen. A shared block whose todos nobody can see is
// just the same clock running alone.
func (m *dashModel) subjectTodos() []apiTodo {
	room, ok := m.currentRoom()
	if !ok {
		return m.desk.Todos
	}
	out := make([]apiTodo, 0, len(room.Mine)+len(room.Others))
	out = append(out, room.Mine...)
	// Marked here rather than trusted from the payload: every key that acts
	// on a todo reads this to decide whether it may.
	for _, t := range room.Others {
		t.ReadOnly = true
		out = append(out, t)
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

func (m *dashModel) Init() tea.Cmd {
	return tea.Batch(tick(), listenFor(m.store))
}

// listenFor waits on the next signal. Bubble Tea drives one command at a
// time, so each delivered signal re-issues this to wait for the next.
func listenFor(s store) tea.Cmd {
	if s == nil {
		return nil
	}
	events := s.Events()
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		sig, ok := <-events
		if !ok {
			return nil
		}
		return sig
	}
}

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
		if m.store == nil {
			return actionMsg{message: message}
		}
		if err := m.store.Do(path, form); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{message: message}
	}
}

// joinedMsg reports a join attempt. /rooms/join redirects without
// complaint for a code nobody owns, so the desk is re-read and the room
// looked for rather than the join being taken on trust.
type joinedMsg struct {
	code string
	name string
	err  error
}

func (m *dashModel) joinRoom(code string) tea.Cmd {
	return func() tea.Msg {
		if m.store == nil {
			return joinedMsg{err: errors.New("no server to join through")}
		}
		if err := m.store.Do("/rooms/join", url.Values{"code": {code}}); err != nil {
			return joinedMsg{err: err}
		}
		desk, err := m.store.Load()
		if err != nil {
			return joinedMsg{err: err}
		}
		for _, room := range desk.Rooms {
			if room.Code == code {
				return joinedMsg{code: code, name: room.Name}
			}
		}
		return joinedMsg{}
	}
}

func (m *dashModel) refresh() tea.Cmd {
	return func() tea.Msg {
		if m.store == nil {
			return deskMsg{desk: m.desk}
		}
		desk, err := m.store.Load()
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
		// An unanswered lobby closes on its own. That is the answer: the
		// run started without you, exactly as it would have on the web.
		if m.prompt != nil && m.prompt.secondsLeft() <= 0 {
			m.prompt = nil
			m.note("the lobby closed — you're not in that block")
		}
		expired := m.currentFace().counting() && m.countdown.remaining() <= 0
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
		if !m.picked {
			m.subject = openingSubject(m.desk, m.subject)
		}
		m.settleSubject()
		m.anchor()
		m.clampCursor()
		return m, nil

	case joinedMsg:
		switch {
		case msg.err != nil:
			m.note(msg.err.Error())
		case msg.code == "":
			m.note("no room with that code")
		default:
			// You just asked for this room, so it becomes the one on screen.
			m.picked = true
			m.selectSubject(msg.code)
			m.note("joined " + msg.name)
		}
		return m, m.refresh()

	case signal:
		return m.handleSignal(msg)

	case loginResultMsg:
		return m.handleLogin(msg)

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

// handleSignal turns a pushed message into whatever the desk should do
// about it. Most signals only mean "something changed, read it again" —
// the payload-carrying ones are the invitations.
func (m *dashModel) handleSignal(sig signal) (tea.Model, tea.Cmd) {
	next := listenFor(m.store)
	switch sig.kind {
	case "timer-lobby":
		// Your own start needs no invitation; you are already in it.
		if sig.str("starterUserId") == m.userID {
			break
		}
		prompt := &joinPrompt{
			code:     firstNonEmpty(sig.str("roomCode"), sig.room),
			roomName: firstNonEmpty(sig.str("roomName"), sig.room),
			starter:  firstNonEmpty(sig.str("starterName"), "Someone"),
			deadline: time.Now().Add(30 * time.Second),
		}
		if at, ok := sig.at("lobbyDeadline"); ok {
			prompt.deadline = at
		}
		m.prompt = prompt
		// The whole point is being told while looking at something else.
		return m, tea.Batch(next, bell(), m.refresh())

	case "timer-break-invite":
		m.note(firstNonEmpty(sig.str("roomName"), sig.room) + " is on a break — room to join before the next block")
		return m, tea.Batch(next, bell(), m.refresh())

	case "timer-checkin-kick":
		if ids, ok := sig.event["userIds"].([]any); ok {
			for _, id := range ids {
				if s, _ := id.(string); s == m.userID {
					m.note("dropped from " + sig.room + " — you didn't check in during the break")
				}
			}
		}

	case "todo-done":
		if sig.str("actorId") != m.userID {
			m.note(firstNonEmpty(sig.str("actor"), "Someone") + " completed: " + sig.str("text"))
		}

	case "deleted":
		m.note(sig.room + " was deleted")
	}
	// Everything else — todos, solo-timer, timer-phase, settings — is a
	// nudge to re-read, which is the same thing the browser does with them.
	return m, tea.Batch(next, m.refresh())
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// bell rings the terminal once. A prompt with a 30-second fuse is no use if
// it arrives silently in a window nobody is looking at.
func bell() tea.Cmd {
	return func() tea.Msg {
		fmt.Fprint(os.Stderr, "\a")
		return nil
	}
}

// confirmPrompt is a question the desk asks itself, not one the server
// sent. Unlike the lobby offer it has no deadline: it waits.
type confirmPrompt struct {
	question string
	act      func() tea.Cmd
}

func (m *dashModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.login != nil {
		return m.handleLoginKey(msg)
	}
	// A note has said its piece the moment you reach for anything else.
	// Four seconds is right for reading one you did not expect and far too
	// long for one you already know, so any key clears it -- and still does
	// whatever it does.
	if m.status != "" {
		m.status = ""
		m.statusUntil = time.Time{}
	}
	// While a line is being typed, every key belongs to it — otherwise a
	// todo containing "q" would quit the program mid-word.
	if m.editing != nil {
		return m.handleEditKey(msg)
	}
	// An unanswered confirmation swallows every other key. The whole point
	// is that the next keystroke should not be able to do something else
	// while the question is still on screen.
	if m.confirm != nil {
		switch msg.String() {
		case "y", "Y":
			act := m.confirm.act
			m.confirm = nil
			return m, act()
		case "ctrl+c":
			return m, tea.Quit
		default:
			m.confirm = nil
			m.note("nothing changed")
			return m, nil
		}
	}
	// A lobby is a question with a deadline, so it gets first claim on the
	// keys that answer it. Everything else still works: ignoring the offer
	// is a valid way to decline it.
	if m.prompt != nil {
		switch msg.String() {
		case "y", "Y":
			code := m.prompt.code
			m.prompt = nil
			// You said yes to that block, so it becomes the one on screen.
			m.picked = true
			m.selectSubject(code)
			return m, m.do("/r/"+code+"/timer-join", nil, "joined "+code)
		case "n", "N":
			m.prompt = nil
			m.note("not joining")
			return m, nil
		}
	}
	if room, ok := m.currentRoom(); ok {
		if handled, model, cmd := m.handleRoomKey(msg, room); handled {
			return model, cmd
		}
	}
	t := m.desk.SoloTimer
	switch msg.String() {
	case "q":
		// The zoomed pane is a screen you are inside, so q leaves that
		// first. From the desk itself there is nothing left to back out
		// of, and it quits.
		if m.zoom {
			m.zoom = false
			return m, nil
		}
		return m, tea.Quit
	case "ctrl+c":
		return m, tea.Quit
	// J and K move between blocks the way j and k move down a list: the
	// shifted pair, because the plain one belongs to the todo cursor. tab
	// still works, but reaching for it is a different hand position.
	case "tab", "J":
		m.picked = true
		m.cycleSubject(1)
		return m, nil
	case "shift+tab", "K":
		m.picked = true
		m.cycleSubject(-1)
		return m, nil
	case "esc":
		if m.zoom {
			m.zoom = false
			return m, nil
		}
		return m, tea.Quit
	case "w":
		m.zoom = !m.zoom
		return m, nil
	case "A":
		// Adding a room to the desk is the one thing here that needs a code
		// typed, and it happens once per room -- so it takes the shifted a,
		// beside the a that adds a todo, and leaves J to the moving about
		// that happens constantly.
		if m.identity.Guest {
			m.note("rooms need an account — L to sign in")
			return m, nil
		}
		m.editing = &editor{label: "room code"}
		m.editingID = ""
		m.joining = true
		return m, nil
	case "L":
		if !m.identity.Guest {
			m.note("already signed in")
			return m, nil
		}
		cfg, _ := loadConfig()
		m.login = newLoginForm(firstNonEmpty(m.identity.BaseURL, cfg.BaseURL), cfg.Username)
		return m, nil
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
	// x is the leave key wherever you are: it ends your own block here and
	// leaves a room's block over there. c stays as it was, so a hand that
	// learned it keeps working.
	case "x", "c":
		if t == nil || t.Phase != "focus" {
			m.note("no running block to end")
			return m, nil
		}
		// Ending a block banks none of its minutes, which is worth one
		// keystroke of warning -- and it is the same question x asks over a
		// room's block.
		m.confirm = &confirmPrompt{
			question: "end your block? none of its minutes are banked",
			act: func() tea.Cmd {
				return m.do("/solo/cancel", nil, "block ended — nothing banked")
			},
		}
		return m, nil

	case "j", "down":
		m.cursor++
		m.clampCursor()
		return m, nil
	case "k", "up":
		m.cursor--
		m.clampCursor()
		return m, nil
	// The list is short enough that vim's counts and searches would be
	// ceremony, but jumping to either end of it is worth the two keys.
	case "g", "home":
		m.cursor = 0
		m.clampCursor()
		return m, nil
	case "G", "end":
		m.cursor = len(m.visibleTodos()) - 1
		m.clampCursor()
		return m, nil

	// i is a room's key. Pressed over your own block it used to do nothing
	// at all, which reads as the program having missed it.
	case "i":
		if len(m.desk.Rooms) == 0 {
			m.note("that one is for a room's block — A to join a room")
			return m, nil
		}
		m.note("that one is for a room's block — tab to a room first")
		return m, nil

	case "a":
		label := "new todo"
		if room, ok := m.currentRoom(); ok {
			label = "new todo in " + room.Code
		}
		m.editing = &editor{label: label}
		m.editingID = ""
		return m, nil
	case "e":
		todo, ok := m.selected()
		if !ok {
			return m, nil
		}
		if todo.ReadOnly {
			m.note("that one is " + todoOwner(todo) + "'s — you can't edit it")
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
		if todo.ReadOnly {
			m.note("that one is " + todoOwner(todo) + "'s — you can't complete it")
			return m, nil
		}
		return m, m.do("/todo/"+todo.ID+"/toggle", nil, "")
	case "d":
		todo, ok := m.selected()
		if !ok {
			return m, nil
		}
		if todo.ReadOnly {
			m.note("that one is " + todoOwner(todo) + "'s — you can't remove it")
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

// handleRoomKey is what the timer keys mean while a room is the subject.
// They are the same keys as the private block's, pointed at the room's
// endpoints — the ones that have no private equivalent (i, x) are the two
// a shared block adds: getting in, and getting out.
//
// It reports whether it took the key, so everything it does not claim —
// the todo list, tab, quit — still works with a room on screen.
func (m *dashModel) handleRoomKey(msg tea.KeyMsg, room deskRoom) (bool, tea.Model, tea.Cmd) {
	t := room.Timer
	base := "/r/" + room.Code + "/"
	switch msg.String() {
	case "f":
		if t != nil {
			m.note(room.Code + " already has a block running")
			return true, m, nil
		}
		return true, m, m.do(base+"timer-start", nil, "block started in "+room.Code+" — 30 seconds to join")
	case "b":
		if t == nil || !t.BreakPending {
			m.note("no break is waiting in " + room.Code)
			return true, m, nil
		}
		// No minutes: the server falls back to the room's own break
		// length, which is the setting the room agreed on.
		return true, m, m.do(base+"timer-break-length", nil, "break started in "+room.Code)
	case "s":
		if t == nil || t.Phase != "break" {
			m.note("there is no break to skip in " + room.Code)
			return true, m, nil
		}
		return true, m, m.do(base+"timer-skip-break", nil, "break skipped in "+room.Code)
	case "i":
		if t == nil {
			// Not a no-op: with nothing running, joining queues you for
			// whenever someone does start one.
			return true, m, m.do(base+"timer-join", nil, "waiting in "+room.Code+" — you're in when it starts")
		}
		// Joining during a break is also the check-in, but only for
		// someone not already in the block; a participant confirming they
		// are staying has its own endpoint.
		if t.Participant {
			if t.Phase != "break" && !t.BreakPending {
				m.note("you're already in this block")
				return true, m, nil
			}
			return true, m, m.do(base+"timer-checkin", nil, "checked in for the next block")
		}
		return true, m, m.do(base+"timer-join", nil, "joining "+room.Code)
	case "c":
		// Claimed rather than passed through: the solo handler's c would
		// cancel your private block, which is not the one on screen. A
		// room's run is ended from the web, not from here.
		m.note("a room's block can't be cancelled here — x leaves it, or end it on the web")
		return true, m, nil
	case "x":
		if t == nil || !t.Participant {
			m.note("you're not in a block in " + room.Code)
			return true, m, nil
		}
		// x sits one key from i and s, and there is no undo: a block that
		// has moved past its lobby will not always take you back.
		code := room.Code
		m.confirm = &confirmPrompt{
			question: "leave the block in " + code + "?",
			act: func() tea.Cmd {
				return m.do(base+"timer-leave", nil, "left the block in "+code)
			},
		}
		return true, m, nil
	}
	return false, m, nil
}

// handleEditKey drives the one-line field for adding and editing todos.
func (m *dashModel) handleEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.editing = nil
		m.joining = false
		return m, nil
	case tea.KeyEnter:
		text, id := m.editing.value(), m.editingID
		joining := m.joining
		m.editing = nil
		m.joining = false
		if joining {
			// The same normalising the room commands do, so a pasted link
			// works as well as a typed code.
			code := normalizeCode(text)
			if code == "" {
				return m, nil
			}
			return m, m.joinRoom(code)
		}
		if text == "" {
			// An emptied todo is a no-op, not a delete — the same rule the
			// server applies to an edit that arrives blank.
			return m, nil
		}
		if id != "" {
			return m, m.do("/todo/"+id+"/edit", url.Values{"text": {text}}, "edited")
		}
		// A todo added while looking at a room belongs to that room: the
		// list on screen is the one it should join.
		if room, ok := m.currentRoom(); ok {
			return m, m.do("/r/"+room.Code+"/todos", url.Values{"text": {text}}, "added to "+room.Code)
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

func (m *dashModel) handleLoginKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.login.busy {
		return m, nil
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.login = nil
		return m, nil
	case "enter":
		return m, m.login.submit()
	case "tab", "down":
		m.login.field = (m.login.field + 1) % 2
		return m, nil
	case "shift+tab", "up":
		m.login.field = (m.login.field + 1) % 2
		return m, nil
	}
	switch msg.Type {
	case tea.KeyBackspace:
		m.login.backspace()
	case tea.KeySpace:
		m.login.insert([]rune{' '})
	case tea.KeyRunes:
		m.login.insert(msg.Runes)
	}
	return m, nil
}

func (m *dashModel) handleLogin(msg loginResultMsg) (tea.Model, tea.Cmd) {
	if m.login != nil {
		m.login.busy = false
	}
	if msg.err != nil {
		if m.login != nil {
			m.login.err = msg.err.Error()
		}
		return m, nil
	}
	if m.store != nil {
		m.store.Close()
	}
	m.store = msg.store
	m.identity = msg.id
	m.user = msg.id.User
	m.userID = msg.id.UserID
	m.login = nil
	m.note("signed in as " + msg.id.User)
	m.refreshing = true
	m.lastRefresh = time.Now()
	return m, tea.Batch(m.refresh(), listenFor(m.store))
}

func (m *dashModel) View() string {
	if m.login != nil {
		return m.login.View(m.width, m.height)
	}
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n")
	if banner := m.promptBanner(); banner != "" {
		b.WriteString(banner)
		b.WriteString("\n")
	}
	b.WriteString(m.timerBlock())
	if m.zoom {
		return b.String() + m.footer()
	}
	b.WriteString("\n")

	// Sections are dropped from the bottom up when the window is short, so
	// what survives is always the timer — the reason the screen is open.
	// The footer is measured rather than assumed: it is three rows of
	// grouped hints when there is room for them and one row when not.
	foot := m.footer()
	rows := m.height - lipgloss.Height(m.header()) - lipgloss.Height(m.timerBlock()) - 3 - lipgloss.Height(foot)
	todos, rooms := m.sectionBudget(rows)
	if todos > 0 {
		b.WriteString(m.todoBlock(todos))
		b.WriteString("\n")
	}
	if rooms > 0 {
		b.WriteString(m.roomBlock(rooms))
		b.WriteString("\n")
	}
	return b.String() + foot
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
	who := m.user
	if m.identity.Guest {
		who = "guest"
	}
	left := styleAccent.Render(clip("junkie", m.width))
	if who != "" {
		left += styleFaint.Render(clip(" · "+who, m.width-6))
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

// promptBanner is the lobby offer, with the seconds left on it. It sits
// directly under the header rather than over the screen: a countdown you
// have half a minute to answer should not hide what you were doing.
func (m *dashModel) promptBanner() string {
	if m.prompt == nil {
		return ""
	}
	left := formatDuration(m.prompt.secondsLeft())
	full := fmt.Sprintf("%s is starting %s — join? y/n · %s",
		m.prompt.starter, m.prompt.roomName, left)
	short := fmt.Sprintf("join %s? y/n · %s", m.prompt.roomName, left)
	text := full
	if len([]rune(full)) > m.width {
		text = short
	}
	return styleWarn.Render(clip(text, m.width)) + "\n"
}

func (m *dashModel) timerHeight() int {
	if m.zoom {
		// Header, the blank line under it, and the footer row that carries
		// errors and notes.
		used := lipgloss.Height(m.header()) + 2
		if m.prompt != nil {
			used += 2
		}
		h := m.height - used
		if h < 1 {
			return 1
		}
		return h
	}
	if m.currentFace() == nil {
		return 1
	}
	switch {
	case m.height >= 22:
		return 14
	case m.height >= 14:
		return 9
	case m.height >= 8:
		return 3
	default:
		return 1
	}
}

func (m *dashModel) timerFace() *watchModel {
	return &watchModel{
		timer:     m.currentFace(),
		countdown: m.countdown,
		width:     m.width,
		height:    m.timerHeight(),
		chrome:    m.zoom,
	}
}

func (m *dashModel) timerBlock() string {
	return m.timerFace().View()
}

func (m *dashModel) todoBlock(rows int) string {
	var b strings.Builder
	open, done, _ := countTodos(m.subjectTodos())
	label := "todos"
	if _, ok := m.currentRoom(); ok {
		label = "room todos"
	}
	b.WriteString(styleLabel.Render(label) +
		styleFaint.Render(clip(fmt.Sprintf("  %d open · %d done", open, done), m.width-len(label))) + "\n")

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
		empty := "  nothing on the list · a to add"
		short := "  nothing on the list"
		if m.width < len([]rune(empty)) {
			empty = short
		}
		b.WriteString(styleFaint.Render(clip(empty, m.width)) + "\n")
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
	// Someone else's todo is read, not worked: it says whose it is, and the
	// whole line is faint so the list reads as yours-then-theirs.
	if todo.ReadOnly {
		who := todo.DisplayName
		if who == "" {
			who = "someone"
		}
		body := clip(todo.Text+" · "+who, m.width-6)
		return cursor + styleFaint.Render("[ ] ") + styleFaint.Render(body)
	}
	return cursor + mark + " " + text.Render(clip(todo.Text, m.width-6))
}

// todoOwner names whoever a todo belongs to, for the message that says why
// a key did nothing.
func todoOwner(todo apiTodo) string {
	if todo.DisplayName == "" {
		return "someone else"
	}
	return todo.DisplayName
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
		// The same cursor the todo list uses, for the same reason: it says
		// which line the countdown above belongs to.
		cursor := "  "
		if room.Code == m.subject {
			cursor = styleAccent.Render("› ")
		}
		b.WriteString(cursor + roomLine(room, m.width-2) + "\n")
		shown++
	}
	if shown == 0 {
		b.WriteString(styleFaint.Render("  none yet") + "\n")
	}
	return b.String()
}

func (m *dashModel) footer() string {
	if m.err != nil && m.login == nil {
		return styleDanger.Render(clip("offline: "+m.err.Error(), m.width))
	}
	if m.confirm != nil {
		return styleWarn.Render(clip(m.confirm.question+"  y yes · any other key no", m.width))
	}
	if m.editing != nil {
		if m.joining {
			return styleFaint.Render(clip(joinKeys, m.width))
		}
		return styleFaint.Render(clip(editKeys, m.width))
	}
	if m.prompt != nil {
		return styleFaint.Render(clip("y join · n decline · no answer means you sit this one out", m.width))
	}
	if m.status != "" && time.Now().Before(m.statusUntil) {
		return styleWarn.Render(clip(m.status, m.width))
	}
	if m.zoom {
		// The pane draws its own key line at this size; repeating it here
		// would be two footers arguing. Errors and notes are handled above.
		return ""
	}
	if grouped := m.groupedKeys(); grouped != "" {
		return styleFaint.Render(grouped)
	}
	if m.identity.Guest {
		return styleFaint.Render(clip(guestDashKeys(m.width), m.width))
	}
	if _, ok := m.currentRoom(); ok {
		return styleFaint.Render(clip(roomDashKeys(m.width), m.width))
	}
	return styleFaint.Render(clip(dashKeys(m.width, len(m.desk.Rooms) > 0), m.width))
}

// keyLine is one labelled row of the footer. Items are dropped from the end
// as the window narrows, so what survives is what was listed first.
type keyLine struct {
	label string
	items []string
}

// groupedKeys is the footer as three labelled rows rather than one long
// line: moving around the todo list, running a block, and working the desk
// are three different jobs, and nine hints in a row read as noise. It
// returns "" when the window cannot spare the rows or the width, and the
// single-line forms below answer instead.
func (m *dashModel) groupedKeys() string {
	if m.height < 16 || m.width < 34 {
		return ""
	}
	todos := keyLine{"todos", []string{"j/k move", "g/G ends", "space done", "a add", "e edit", "d remove", "u undo"}}

	block := keyLine{"block", []string{"f focus", "b break", "s skip", "x end it"}}
	if _, ok := m.currentRoom(); ok {
		// A room's block is joined and left, and cancelling is not on
		// offer: c would end your own private block, not the room's.
		block = keyLine{"block", []string{"f start", "i I'm in", "b break", "s skip", "x leave"}}
	}

	// "tab room" said which key without saying what it did. J and K move the
	// countdown between the blocks you have running -- yours, then each
	// room -- so it is named for that.
	desk := keyLine{"desk", []string{"q quit", "J/K switch block", "A join a room", "w zoom", "r refresh"}}
	if m.identity.Guest {
		desk = keyLine{"desk", []string{"q quit", "L sign in", "w zoom"}}
	} else if len(m.desk.Rooms) == 0 {
		desk = keyLine{"desk", []string{"q quit", "A join a room", "w zoom", "r refresh"}}
	}

	rows := make([]string, 0, 3)
	for _, line := range []keyLine{todos, block, desk} {
		rows = append(rows, renderKeyLine(line, m.width))
	}
	return strings.Join(rows, "\n")
}

// renderKeyLine fits one row to the window by dropping hints off the end.
// The label is padded to a common width so the three rows line up.
func renderKeyLine(line keyLine, width int) string {
	items := line.items
	for {
		text := fmt.Sprintf("%-6s %s", line.label, strings.Join(items, " · "))
		if len([]rune(text)) <= width || len(items) == 1 {
			return clip(text, width)
		}
		items = items[:len(items)-1]
	}
}

func guestDashKeys(width int) string {
	const full = "j/k move · space done · a add · f focus · L sign in · q quit"
	const medium = "j/k move · a add · f focus · L sign in · q quit"
	const short = "f focus · L sign in · q quit"
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

// dashKeys names what the keys do, at whatever length fits. The short forms
// are written out rather than cut mid-word.
// dashKeys is the footer over the private block. Tab is only named when
// there is a room to move to — an account with none has nowhere to go, and
// a key that does nothing is worse than a key nobody was told about.
func dashKeys(width int, rooms bool) string {
	full := "j/k move · space done · a add · e edit · d remove · f focus · b break · x end it · q quit"
	medium := "j/k move · space done · a add · f focus · b break · q quit"
	short := "j/k · space · a add · f focus · q quit"
	if rooms {
		full = "j/k move · space done · a add · e edit · d remove · f focus · b break · J/K block · q quit"
		medium = "j/k move · space done · a add · f focus · b break · J/K block · q quit"
		short = "j/k · space · a add · f focus · tab · q quit"
	}
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

// The same field takes a room code, and saving one is not "saving".
const joinKeys = "enter join · esc cancel · ctrl+u clear"

// runDashboard opens the desk full-screen. zoom is `junkie watch`: the
// timer pane takes the window, but it is the same program and a run ending
// does not quit it.
func runDashboard(zoom bool, want string) error {
	s, id, err := openDeskSession()
	if errors.Is(err, errSessionExpired) {
		s, err = openLocalStore()
		if err != nil {
			return err
		}
		cfg, _ := loadConfig()
		id = guestIdentity(cfg.BaseURL)
	} else if err != nil {
		return err
	}
	defer s.Close()
	desk, err := s.Load()
	if err != nil {
		return err
	}
	if want != "" && !hasRoom(desk, want) {
		return fmt.Errorf("you're not in %s — `junkie rooms` lists the ones you are", want)
	}
	m := newDashModel(s, id, desk)
	m.openOn(want)
	m.zoom = zoom
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// roomDashKeys is the footer while a room is the subject. The timer keys
// are the same letters as the private block's — they act on the room
// instead — plus the two only a shared block has.
func roomDashKeys(width int) string {
	const full = "J/K block · f start · i I'm in · b break · s skip · x leave · a add · q quit"
	const medium = "J/K block · f start · i I'm in · s skip · x leave · q quit"
	const short = "tab · f start · i in · x leave · q quit"
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

// hasRoom reports whether a code names a room on this desk. Opening
// straight onto a room you are not in would show an empty pane and no way
// to tell why, so the command says so instead.
func hasRoom(desk deskResponse, code string) bool {
	for _, room := range desk.Rooms {
		if room.Code == code {
			return true
		}
	}
	return false
}
