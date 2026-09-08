package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// idleDesk is the case Sam hit: a room you are in, and a private block, both
// on the same desk with only one countdown between them.
func bothRunningDesk() deskResponse {
	d := sampleDesk()
	d.Rooms = []deskRoom{{
		Code: "ABC-123", Name: "Deep work", FocusMinutes: 25, BreakMinutes: 5,
		Timer: &roomTimer{
			Phase: "focus", SecondsLeft: 724, FocusMinutes: 25, BreakMinutes: 5,
			CurrentSession: 1, TotalSessions: 4, Participant: true,
			Participants: []apiUser{{DisplayName: "Sam"}, {DisplayName: "Ada"}},
			EndsAt:       time.Now().Add(12 * time.Minute),
		},
	}}
	return d
}

func TestTabMovesBetweenTheBlocksAndWraps(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	if m.subject != soloSubject {
		t.Fatalf("a running private block should be the opening subject, got %q", m.subject)
	}

	// The block pane's list is the blocks, and j walks it.
	m.focus = paneBlock
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.subject != "ABC-123" {
		t.Errorf("j should select the room, got %q", m.subject)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.subject != soloSubject {
		t.Errorf("j should wrap back to the private block, got %q", m.subject)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.subject != "ABC-123" {
		t.Errorf("k should go the other way, got %q", m.subject)
	}
}

// The whole point of the switch: the room's own countdown, not the private
// one, with the room named so there is no doubt which block is on screen.
func TestSelectedRoomDrawsItsOwnCountdown(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	onFirstRoom(m)

	view := m.View()
	for _, want := range []string{"Deep work", "ABC-123", "of 25:00", "session 1/4", "2 here"} {
		if !strings.Contains(view, want) {
			t.Errorf("the room pane is missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "of 50:00") {
		t.Errorf("the private block's length should be gone from the pane:\n%s", view)
	}
}

// A countdown that kept the old block's seconds for half a second would read
// as the new block being further along than it is.
func TestSwitchingRebasesTheCountdown(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	onFirstRoom(m)
	if got := m.countdown.remaining(); got != 724 {
		t.Errorf("countdown = %d, want the room's 724", got)
	}
}

func TestRoomKeysActOnTheRoom(t *testing.T) {
	for _, tc := range []struct {
		key  rune
		want string
	}{
		{'s', "/r/ABC-123/timer-skip-break"},
		{'x', "/r/ABC-123/timer-leave"},
	} {
		m := dashAt(100, 30)
		m.desk = bothRunningDesk()
		if tc.key == 's' {
			m.desk.Rooms[0].Timer.Phase = "break"
		}
		onFirstRoom(m)
		_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tc.key}})
		// Leaving asks first, so the key that acts on the room is the y
		// that answers it.
		if cmd == nil && m.confirm != nil {
			_, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
		}
		if cmd == nil {
			t.Fatalf("%c should have acted on the room", tc.key)
		}
		if msg, ok := cmd().(actionMsg); !ok {
			t.Errorf("%c did not post anything", tc.key)
		} else if msg.err == nil && !strings.Contains(msg.message, "ABC-123") {
			t.Errorf("%c reported %q, which does not name the room", tc.key, msg.message)
		}
	}
}

// x is one key from i and s and cannot be undone, so it asks before it acts.
func TestLeavingABlockAsksFirst(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	onFirstRoom(m)

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd != nil {
		t.Fatal("x left the block without asking")
	}
	if m.confirm == nil {
		t.Fatal("x asked nothing")
	}
	if !strings.Contains(m.footer(), "leave the block") {
		t.Errorf("the question is not on screen:\n%s", m.footer())
	}

	// Anything that is not y is a no, and it leaves the block alone.
	_, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd != nil {
		t.Error("declining still posted something")
	}
	if m.confirm != nil {
		t.Error("the question stayed up after being answered")
	}
}

// Starting a block is f whichever timer is selected; only the endpoint moves.
func TestStartActsOnWhicheverBlockIsSelected(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	m.desk.SoloTimer = nil
	m.desk.Rooms[0].Timer = nil

	onFirstRoom(m)
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if cmd == nil {
		t.Fatal("f should start the selected room's block")
	}
	if msg := cmd().(actionMsg); msg.err == nil && !strings.Contains(msg.message, "ABC-123") {
		t.Errorf("f started %q, not the room", msg.message)
	}
}

// An idle room has no countdown, and saying "no block running · f to start
// one" — the private timer's line — would be about the wrong thing entirely.
func TestIdleRoomSaysWhatItIs(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	m.desk.Rooms[0].Timer = nil
	onFirstRoom(m)

	view := m.View()
	for _, want := range []string{"Deep work", "idle", "25 min blocks", "f starts a block here"} {
		if !strings.Contains(view, want) {
			t.Errorf("the idle room pane is missing %q:\n%s", want, view)
		}
	}
}

// Leaving a room while its block is the one on screen must not strand the
// pane on a room the desk no longer lists.
func TestSubjectFallsBackWhenTheRoomGoesAway(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	onFirstRoom(m)

	next := bothRunningDesk()
	next.Rooms = nil
	m.Update(deskMsg{desk: next})
	if m.subject != soloSubject {
		t.Errorf("subject = %q, want the private block", m.subject)
	}
}

// Sam's ask: having entered a room, `junkie` should put you in front of it
// rather than a private timer that is not running.
func TestOpeningSubjectPrefersTheRoomYoureIn(t *testing.T) {
	desk := bothRunningDesk()
	desk.SoloTimer = nil
	if got := openingSubject(desk, ""); got != "ABC-123" {
		t.Errorf("opening subject = %q, want the live room", got)
	}

	// Your own running block outranks it: you started that one deliberately.
	if got := openingSubject(bothRunningDesk(), ""); got != soloSubject {
		t.Errorf("opening subject = %q, want the private block", got)
	}

	// A room you are not in still beats an empty screen.
	desk.Rooms[0].Timer.Participant = false
	if got := openingSubject(desk, ""); got != "ABC-123" {
		t.Errorf("opening subject = %q, want the live room", got)
	}

	// And nothing running anywhere is the private block, as before.
	desk.Rooms[0].Timer = nil
	if got := openingSubject(desk, ""); got != soloSubject {
		t.Errorf("opening subject = %q, want the private block", got)
	}
}

// `junkie watch CODE` names a room explicitly, and that wins over the guess.
func TestOpeningSubjectHonoursAnAskedForRoom(t *testing.T) {
	desk := bothRunningDesk()
	if got := openingSubject(desk, "ABC-123"); got != "ABC-123" {
		t.Errorf("asked for ABC-123, got %q", got)
	}
	if got := openingSubject(desk, "NOPE-1"); got != soloSubject {
		t.Errorf("a room you're not in should not be selected, got %q", got)
	}
}

// A refresh must never move the pane off the block the user chose.
func TestARefreshDoesNotUndoTheUsersChoice(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	onFirstRoom(m)
	m.Update(deskMsg{desk: bothRunningDesk()})
	if m.subject != "ABC-123" {
		t.Errorf("subject = %q after a refresh, want the room the user picked", m.subject)
	}
}

func TestRoomCodeArgumentAcceptsAURL(t *testing.T) {
	got, err := optionalRoomCode([]string{"https://junkie.example/r/abc-123"}, "usage")
	if err != nil || got != "ABC-123" {
		t.Errorf("optionalRoomCode = %q, %v", got, err)
	}
	if _, err := optionalRoomCode([]string{"one", "two"}, "usage"); err == nil {
		t.Error("two arguments should be refused")
	}
}

// An untitled countdown beside a room list reads as that room's block. The
// private block says whose it is whenever there is a room to confuse it with.
func TestPrivateBlockIsNamedWhenThereAreRooms(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	if f := m.currentFace(); f == nil || f.Title != "your block" {
		t.Fatalf("private face title = %q, want \"your block\"", f.Title)
	}
	if view := m.View(); !strings.Contains(view, "your block") {
		t.Errorf("the pane does not say whose block it is:\n%s", view)
	}

	// With no room on the desk there is nothing to mistake it for.
	m.desk.Rooms = nil
	if f := m.currentFace(); f == nil || f.Title != "" {
		t.Errorf("private face title = %q, want it unnamed with no rooms", f.Title)
	}
}

// c cancels the private block. With a room on screen that block is not the
// one being drawn, so c must not quietly end it.
func TestCancelDoesNotReachThePrivateBlockFromARoom(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	onFirstRoom(m)
	if _, ok := m.currentRoom(); !ok {
		t.Fatal("tab did not put a room on screen")
	}
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if cmd != nil {
		t.Fatal("c posted something while a room was on screen")
	}
	if !strings.Contains(m.footer(), "can't be cancelled here") {
		t.Errorf("c said nothing about why:\n%s", m.footer())
	}
}

// A shared block whose todos nobody can see is just the same clock running
// alone: with a room on screen the pane shows that room's list, everyone's.
func TestRoomTodosAppearWithTheRoom(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	m.desk.Todos = []apiTodo{{ID: "p1", Text: "private thing"}}
	m.desk.Rooms[0].Mine = []apiTodo{{ID: "r1", Text: "my room thing"}}
	m.desk.Rooms[0].Others = []apiTodo{{ID: "r2", Text: "their thing", DisplayName: "Pal"}}

	if got := m.visibleTodos(); len(got) != 1 || got[0].ID != "p1" {
		t.Fatalf("your own block should show your private list, got %+v", got)
	}
	onFirstRoom(m)

	got := m.visibleTodos()
	if len(got) != 2 || got[0].ID != "r1" || got[1].ID != "r2" {
		t.Fatalf("room todos = %+v, want yours then theirs", got)
	}
	if !got[1].ReadOnly {
		t.Error("someone else's todo is not marked read-only")
	}
	view := m.View()
	for _, want := range []string{"room todos", "my room thing", "their thing", "Pal"} {
		if !strings.Contains(view, want) {
			t.Errorf("the pane is missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "private thing") {
		t.Error("the private list is still on screen with a room selected")
	}
}

// Someone else's todo is read, not worked.
func TestOthersTodosCannotBeChanged(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	m.desk.Rooms[0].Others = []apiTodo{{ID: "r2", Text: "their thing", DisplayName: "Pal"}}
	onFirstRoom(m)

	for _, key := range []rune{' ', 'd', 'e'} {
		m.cursor = 0
		_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if cmd != nil {
			t.Errorf("%q acted on someone else's todo", key)
		}
		if !strings.Contains(m.footer(), "Pal's") {
			t.Errorf("%q did not say whose todo it was: %s", key, m.footer())
		}
	}
}

// g and G are the whole of the vim jump: the list is too short to earn more.
func TestGoToEndsOfTheList(t *testing.T) {
	m := dashAt(100, 30)
	// j and k scroll the focused pane; this one is about the todo list.
	onTodos(m)
	m.desk.Todos = []apiTodo{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if m.cursor != 2 {
		t.Errorf("G left the cursor at %d, want the last row", m.cursor)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if m.cursor != 0 {
		t.Errorf("g left the cursor at %d, want the first row", m.cursor)
	}
}

// Four seconds is right for a note you did not expect and far too long for
// one you already know, so the next key clears it.
func TestANoteClearsOnTheNextKey(t *testing.T) {
	m := dashAt(100, 30)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if !strings.Contains(m.footer(), "no break is waiting") {
		t.Fatalf("b said nothing: %s", m.footer())
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.status != "" {
		t.Errorf("the note outlived the next key: %q", m.status)
	}
	if strings.Contains(m.footer(), "no break is waiting") {
		t.Errorf("the note is still on screen:\n%s", m.footer())
	}
}

// The zoomed pane is a screen you are inside. q leaves that before it
// leaves the program, so a full-window countdown is never a dead end.
func TestQuitLeavesTheZoomedPaneFirst(t *testing.T) {
	m := dashAt(100, 30)
	m.zoom = true

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd != nil {
		t.Fatal("q quit the program from the zoomed pane")
	}
	if m.zoom {
		t.Fatal("q did not leave the zoomed pane")
	}

	// From the desk there is nothing left to back out of.
	if _, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}); cmd == nil {
		t.Error("q on the desk did not quit")
	}
}

// An empty pane that does not say how to leave it is a dead end.
func TestTheEmptyZoomedPaneSaysHowToLeave(t *testing.T) {
	m := dashAt(100, 30)
	m.zoom = true
	m.desk.SoloTimer = nil
	if view := m.View(); !strings.Contains(view, "esc or q") {
		t.Errorf("no way out on screen:\n%s", view)
	}
}

// The zoomed pane offered i and x over a private block once the private
// block had a title, because a title was how it recognised a room. Only a
// room carries a code.
func TestZoomedPrivateBlockDoesNotOfferRoomKeys(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	m.zoom = true

	f := m.currentFace()
	if f.Title != "your block" {
		t.Fatalf("expected the named private block, got %q", f.Title)
	}
	if f.room() {
		t.Fatal("the private block claims to be a room")
	}
	view := m.View()
	for _, absent := range []string{"i I'm in", "x leave", "tab next"} {
		if strings.Contains(view, absent) {
			t.Errorf("private block offers %q, which does nothing here:\n%s", absent, view)
		}
	}

	// A room's block still gets them.
	onFirstRoom(m)
	if !m.currentFace().room() {
		t.Fatal("the room's face does not report as a room")
	}
	if view := m.View(); !strings.Contains(view, "x leave") {
		t.Errorf("a room's block lost its keys:\n%s", view)
	}
}

// Ending a block and leaving the pane showing it are different things, and
// the pane named only the second.
func TestZoomedPrivatePaneSaysHowToEndTheBlock(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	m.zoom = true
	if view := m.View(); !strings.Contains(view, "x ends it") {
		t.Errorf("no way to end the block on screen:\n%s", view)
	}
	// And x reaches it from inside the pane, asking first.
	if _, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}); cmd != nil {
		t.Error("x ended the block without asking")
	}
	if m.confirm == nil {
		t.Fatal("x did nothing in the zoomed pane")
	}
	if _, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}); cmd == nil {
		t.Error("answering yes ended nothing")
	}
}

// x is the leave key wherever you are: it ends your own block and leaves a
// room's, so there is one thing to remember rather than two.
func TestXEndsWhicheverBlockIsOnScreen(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()

	// Over your own block it ends it.
	m.selectSubject(soloSubject)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if m.confirm == nil || !strings.Contains(m.confirm.question, "end your block") {
		t.Fatalf("x over your own block asked %v", m.confirm)
	}
	m.confirm = nil

	// Over a room's it leaves that.
	onFirstRoom(m)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if m.confirm == nil || !strings.Contains(m.confirm.question, "leave the block") {
		t.Fatalf("x over a room asked %v", m.confirm)
	}
}

// i and x belong to a room. Over your own block they did nothing and said
// nothing, which reads as dropped keystrokes.
func TestRoomKeysExplainThemselvesOverYourOwnBlock(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	for _, key := range []rune{'i'} {
		m.selectSubject(soloSubject)
		_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if cmd != nil {
			t.Errorf("%q acted on something over the private block", key)
		}
		if !strings.Contains(m.footer(), "for a room's block") {
			t.Errorf("%q said nothing: %s", key, m.footer())
		}
	}
}

// Joining a room was the one thing you had to leave the desk to do, and it
// wanted the code typed before the command rather than pasted when asked.
func TestJoinARoomFromTheDesk(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	if m.editing == nil || !m.joining {
		t.Fatal("A did not open a field for a room code")
	}
	if !strings.Contains(m.footer(), "enter join") {
		t.Errorf("the field does not say it joins: %s", m.footer())
	}

	// A pasted link works as well as a typed code.
	for _, r := range "http://x/r/ABC-123" {
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter joined nothing")
	}
	if m.joining || m.editing != nil {
		t.Error("the field stayed open after joining")
	}

	// A code nobody owns is reported rather than assumed to have worked.
	model, _ := m.Update(joinedMsg{})
	if !strings.Contains(model.(*dashModel).footer(), "no room with that code") {
		t.Errorf("a bad code passed silently: %s", m.footer())
	}
}

// The desk is three panes and tab moves between them; j and k scroll
// whichever one has the focus, so there is one pair of keys for moving
// about rather than a different pair per thing that moves.
func TestTabMovesPanesAndJKScrollsTheFocusedOne(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	m.desk.Todos = []apiTodo{{ID: "p1"}, {ID: "p2"}}
	m.desk.Rooms[0].Mine = []apiTodo{{ID: "r1"}, {ID: "r2"}}

	if m.focus != paneBlock {
		t.Fatalf("focus starts on %v, want the block", m.focus)
	}

	// The block pane's list is the blocks themselves.
	before := m.subject
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.subject == before {
		t.Error("j on the block pane did not change the block")
	}
	if m.cursor != 0 {
		t.Errorf("j on the block pane moved the todo cursor to %d", m.cursor)
	}

	// tab to the todos, where j is the cursor and the block sits still.
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != paneTodos {
		t.Fatalf("tab landed on %v, want the todos", m.focus)
	}
	onRoom := m.subject
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.cursor != 1 {
		t.Errorf("j on the todos left the cursor at %d", m.cursor)
	}
	if m.subject != onRoom {
		t.Error("j on the todos moved the block")
	}

	// tab again to the rooms, where j walks the list and the countdown
	// follows the row it lands on.
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != paneRooms {
		t.Fatalf("tab landed on %v, want the rooms", m.focus)
	}
	m.scrollTo(0)
	if _, ok := m.currentRoom(); !ok {
		t.Error("the rooms pane did not put a room on screen")
	}

	// and back round to the block.
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != paneBlock {
		t.Errorf("tab wrapped to %v, want the block", m.focus)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.focus != paneRooms {
		t.Errorf("shift+tab went to %v, want back to the rooms", m.focus)
	}
}

// tab that appears to do nothing is worse than no tab: the focus has to be
// on the screen.
func TestTheFocusedPaneIsMarked(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()

	for _, tc := range []struct {
		focus pane
		mark  string
	}{
		{paneTodos, "todos ‹"},
		{paneRooms, "rooms ‹"},
	} {
		m.focus = tc.focus
		if view := m.View(); !strings.Contains(view, tc.mark) {
			t.Errorf("%v is focused but %q is not on screen:\n%s", tc.focus, tc.mark, view)
		}
	}

	// The block pane has no label, so the mark goes on what it is drawing.
	m.focus = paneBlock
	if view := m.View(); !strings.Contains(view, "‹") {
		t.Errorf("the block pane is focused and nothing says so:\n%s", view)
	}
}

// A pane with nothing in it is a stop on the way to nowhere.
func TestTheRoomsPaneIsSkippedWithNoRooms(t *testing.T) {
	m := dashAt(100, 30)
	m.desk = bothRunningDesk()
	m.desk.Rooms = nil

	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus == paneRooms {
		t.Error("tab stopped on a rooms pane with no rooms in it")
	}
}

// onFirstRoom puts the first room on screen the way the desk does it: the
// block pane focused, and j walking off your own block onto the next one.
func onFirstRoom(m *dashModel) {
	m.focus = paneBlock
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
}

// onTodos points j and k at the todo list, which is what most of the older
// tests meant when they pressed them.
func onTodos(m *dashModel) { m.focus = paneTodos }
