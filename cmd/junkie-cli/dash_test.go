package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func sampleDesk() deskResponse {
	return deskResponse{
		SoloTimer: &soloTimer{
			Phase: "focus", FocusMinutes: 50, BreakMinutes: 10,
			SecondsLeft: 1500, EndsAt: time.Now().Add(25 * time.Minute),
		},
		Todos: []apiTodo{
			{ID: "1", Text: "ship the terminal client"},
			{ID: "2", Text: "write the README", Done: true},
			{ID: "3", Text: "old idea", Removed: true},
		},
		Rooms: []deskRoom{{Code: "ABC-123", Name: "Deep work", Timer: &roomTimer{
			Phase: "focus", SecondsLeft: 724, Participant: true,
			Participants: []apiUser{{DisplayName: "Sam"}},
		}}},
	}
}

func dashAt(width, height int) *dashModel {
	m := newDashModel(nil, identity{User: "sam", UserID: "me-id"}, sampleDesk())
	m.width, m.height = width, height
	return m
}

// Same invariant as the countdown: nothing may be drawn wider than the
// window, or it wraps and tears the layout apart.
func TestDashViewNeverExceedsItsWidth(t *testing.T) {
	for _, width := range []int{20, 30, 40, 46, 60, 80, 120} {
		for _, height := range []int{6, 10, 16, 24, 40} {
			m := dashAt(width, height)
			for i, line := range strings.Split(m.View(), "\n") {
				if got := lipgloss.Width(line); got > width {
					t.Errorf("%dx%d: line %d is %d wide\n%q", width, height, i, got, line)
				}
			}
		}
	}
}

func TestDashShowsTheDesk(t *testing.T) {
	view := dashAt(100, 30).View()
	for _, want := range []string{"junkie", "sam", "FOCUS", "of 50:00",
		"todos", "ship the terminal client", "[x]", "rooms", "ABC-123", "f focus"} {
		if !strings.Contains(view, want) {
			t.Errorf("desk missing %q:\n%s", want, view)
		}
	}
	// Removed todos are a bin, not a working list.
	if strings.Contains(view, "old idea") {
		t.Errorf("removed todo should not be listed:\n%s", view)
	}
}

// A short window drops sections from the bottom up, so what survives is the
// timer — the reason the screen is open at all.
func TestDashKeepsTheTimerWhenItIsShort(t *testing.T) {
	view := dashAt(80, 6).View()
	if !strings.Contains(view, "25:00") && !strings.Contains(strings.ToUpper(view), "FOCUS") {
		t.Errorf("the timer should survive a short window:\n%s", view)
	}
}

func TestDashNarrowDropsTheBar(t *testing.T) {
	wide := dashAt(80, 24).View()
	if !strings.Contains(wide, "of 50:00") {
		t.Errorf("a wide desk should show the bar:\n%s", wide)
	}
	narrow := dashAt(30, 24).View()
	if strings.Contains(narrow, "of 50:00") {
		t.Errorf("a narrow desk should drop the bar:\n%s", narrow)
	}
	if !strings.Contains(narrow, "FOCUS") && !strings.Contains(narrow, "█") {
		t.Errorf("the countdown should survive:\n%s", narrow)
	}
}

func TestDashKeys(t *testing.T) {
	if !strings.Contains(dashKeys(100, false), "d remove") {
		t.Error("a wide footer should name every key")
	}
	if got := dashKeys(60, false); strings.Contains(got, "d remove") || !strings.Contains(got, "a add") {
		t.Errorf("a medium footer should shorten, got %q", got)
	}
	if got := dashKeys(10, false); got != "q quit" {
		t.Errorf("a narrow footer = %q", got)
	}
	// Whatever the width, the footer must fit the window it is drawn in.
	for _, width := range []int{10, 20, 40, 60, 80, 100, 200} {
		if got := len([]rune(dashKeys(width, true))); got > width && width >= 10 {
			t.Errorf("width %d: hints are %d wide", width, got)
		}
	}
}

// Guarded client-side as well as server-side: /solo/start silently redirects
// when a run is already live, which would look like a start that did nothing.
func TestDashRefusesToStartOverARunningBlock(t *testing.T) {
	m := dashAt(80, 24)
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if cmd != nil {
		t.Error("expected no request while a block is running")
	}
	if !strings.Contains(m.View(), "already running") {
		t.Errorf("expected the refusal in the footer:\n%s", m.View())
	}
}

func TestDashRefusesBreakWithNoneWaiting(t *testing.T) {
	m := dashAt(80, 24)
	if _, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}}); cmd != nil {
		t.Error("expected no request when no break is waiting")
	}
	if !strings.Contains(m.View(), "no break is waiting") {
		t.Errorf("expected the refusal in the footer:\n%s", m.View())
	}
}

func TestDashStartsFocusWhenIdle(t *testing.T) {
	m := dashAt(80, 24)
	m.desk.SoloTimer = nil
	if _, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}}); cmd == nil {
		t.Error("expected a start request on an idle desk")
	}
	if !strings.Contains(m.View(), "f to start one") {
		t.Errorf("an idle desk should say how to start:\n%s", m.View())
	}
}

func TestDashQuits(t *testing.T) {
	m := dashAt(80, 24)
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", cmd())
	}
}

// A failed action shows in the footer and leaves the desk alone: what is on
// screen is still true, and the next keypress can retry.
func TestDashActionFailureIsShownNotFatal(t *testing.T) {
	m := dashAt(80, 24)
	_, cmd := m.Update(actionMsg{err: errors.New("could not reach the server")})
	if cmd == nil {
		t.Error("a failed action should still re-read the desk")
	}
	if !strings.Contains(m.View(), "could not reach") {
		t.Errorf("expected the failure in the footer:\n%s", m.View())
	}
	if m.desk.SoloTimer == nil {
		t.Error("the desk should survive a failed action")
	}
}

// A transient confirmation gives the footer back to the key hints once it
// has been read, rather than sitting there forever.
func TestDashStatusExpires(t *testing.T) {
	m := dashAt(80, 24)
	m.note("break started")
	if !strings.Contains(m.View(), "break started") {
		t.Error("expected the note in the footer")
	}
	m.statusUntil = time.Now().Add(-time.Second)
	if !strings.Contains(m.View(), "f focus") {
		t.Errorf("expected the keys back:\n%s", m.View())
	}
}

func TestDashRefreshAdoptsTheNewDesk(t *testing.T) {
	m := dashAt(80, 24)
	m.Update(deskMsg{desk: deskResponse{SoloTimer: &soloTimer{
		Phase: "break", BreakMinutes: 10, BreakPending: true,
	}}})
	view := m.View()
	if !strings.Contains(view, "BREAK READY") {
		t.Errorf("expected the pending break:\n%s", view)
	}
	if !strings.Contains(view, "b to take it") {
		t.Errorf("expected the break hint:\n%s", view)
	}
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func press(m *dashModel, keys ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = m.handleKey(key(k))
	}
	return cmd
}

// Removed todos are a bin, not a working list: putting the cursor on rows
// whose only action is restore would make j/k walk through the past.
func TestDashCursorSkipsRemovedTodos(t *testing.T) {
	m := dashAt(80, 24)
	// j and k scroll the focused pane; this one is about the todo list.
	onTodos(m)
	if got := len(m.visibleTodos()); got != 2 {
		t.Fatalf("visible todos = %d, want 2", got)
	}
	press(m, "j", "j", "j", "j")
	if m.cursor != 1 {
		t.Errorf("cursor ran past the end: %d", m.cursor)
	}
	press(m, "k", "k", "k")
	if m.cursor != 0 {
		t.Errorf("cursor ran past the start: %d", m.cursor)
	}
}

// A list that shrinks underneath the cursor — a todo removed here, or on
// the web — must not leave the selection pointing at nothing.
func TestDashCursorSurvivesAShrinkingList(t *testing.T) {
	m := dashAt(80, 24)
	press(m, "j")
	m.Update(deskMsg{desk: deskResponse{Todos: []apiTodo{{ID: "1", Text: "only one"}}}})
	if m.cursor != 0 {
		t.Errorf("cursor = %d after the list shrank, want 0", m.cursor)
	}
	if _, ok := m.selected(); !ok {
		t.Error("expected a valid selection")
	}
	// An empty list has no selection at all, and no key may act on one.
	m.Update(deskMsg{desk: deskResponse{}})
	if _, ok := m.selected(); ok {
		t.Error("an empty list should have no selection")
	}
	if cmd := press(m, "space"); cmd != nil {
		t.Error("space on an empty list should do nothing")
	}
}

func TestDashTogglesTheSelectedTodo(t *testing.T) {
	m := dashAt(80, 24)
	if cmd := press(m, "space"); cmd == nil {
		t.Fatal("space should toggle the selected todo")
	}
	view := m.View()
	if !strings.Contains(view, "›") {
		t.Errorf("the selection should be marked:\n%s", view)
	}
}

// Every key belongs to the field while a line is being typed — otherwise a
// todo containing "q" would quit the program mid-word.
func TestDashTypingDoesNotTriggerCommands(t *testing.T) {
	m := dashAt(80, 24)
	press(m, "a")
	if m.editing == nil {
		t.Fatal("a should open the field")
	}
	press(m, "q", "u", "i", "t", " ", "d", "b")
	if m.editing == nil {
		t.Fatal("typing q should not have quit")
	}
	if got := m.editing.value(); got != "quit db" {
		t.Errorf("typed %q", got)
	}
	if !strings.Contains(m.View(), "quit db") {
		t.Errorf("the typed text should show in place:\n%s", m.View())
	}
	if !strings.Contains(m.View(), "esc cancel") {
		t.Error("the footer should explain the field's keys")
	}
}

func TestDashAddSubmits(t *testing.T) {
	m := dashAt(80, 24)
	press(m, "a", "h", "i")
	cmd := press(m, "enter")
	if cmd == nil {
		t.Fatal("enter should submit")
	}
	if m.editing != nil {
		t.Error("the field should close on submit")
	}
}

// An emptied line is a no-op, not a delete — the same rule the server
// applies to an edit that arrives blank.
func TestDashEmptySubmitDoesNothing(t *testing.T) {
	m := dashAt(80, 24)
	press(m, "a")
	if cmd := press(m, "enter"); cmd != nil {
		t.Error("an empty submit should not post")
	}
	if m.editing != nil {
		t.Error("the field should still close")
	}
}

func TestDashEscapeAbandonsTheEdit(t *testing.T) {
	m := dashAt(80, 24)
	press(m, "e")
	if m.editing == nil || m.editingID == "" {
		t.Fatal("e should open the field on the selected todo")
	}
	if m.editing.value() != "ship the terminal client" {
		t.Errorf("the field should start from the existing text, got %q", m.editing.value())
	}
	press(m, "backspace", "backspace")
	if cmd := press(m, "esc"); cmd != nil {
		t.Error("esc should not post")
	}
	if m.editing != nil {
		t.Error("esc should close the field")
	}
}

// Completed todos keep the text they had when they changed state — the
// server refuses the edit, so the client says why rather than posting it.
func TestDashRefusesToEditACompletedTodo(t *testing.T) {
	m := dashAt(80, 24)
	// j and k scroll the focused pane; this one is about the todo list.
	onTodos(m)
	press(m, "j")
	if cmd := press(m, "e"); cmd != nil {
		t.Error("editing a completed todo should not post")
	}
	if !strings.Contains(m.View(), "can't be edited") {
		t.Errorf("expected the refusal in the footer:\n%s", m.View())
	}
}

// Removal is a flag server-side, not a delete, so a one-key undo is real
// rather than a re-create that would lose the todo's history.
func TestDashRemoveThenUndo(t *testing.T) {
	m := dashAt(80, 24)
	if cmd := press(m, "d"); cmd == nil {
		t.Fatal("d should remove")
	}
	if m.undoID != "1" {
		t.Errorf("undo target = %q, want the removed todo", m.undoID)
	}
	if cmd := press(m, "u"); cmd == nil {
		t.Fatal("u should restore")
	}
	// The undo is spent; a second u has nothing to put back.
	if cmd := press(m, "u"); cmd != nil {
		t.Error("a second undo should not post")
	}
	if !strings.Contains(m.View(), "nothing to undo") {
		t.Errorf("expected the refusal in the footer:\n%s", m.View())
	}
}

// A long list must keep the selection on screen rather than scrolling it
// off the bottom.
func TestDashScrollsToKeepTheCursorVisible(t *testing.T) {
	m := dashAt(80, 14)
	// j and k scroll the focused pane; this one is about the todo list.
	onTodos(m)
	var todos []apiTodo
	for i := 0; i < 40; i++ {
		todos = append(todos, apiTodo{ID: fmt.Sprint(i), Text: fmt.Sprintf("todo number %d", i)})
	}
	m.desk = deskResponse{Todos: todos}
	for i := 0; i < 30; i++ {
		press(m, "j")
	}
	if !strings.Contains(m.View(), "todo number 30") {
		t.Errorf("the selected row scrolled off:\n%s", m.View())
	}
}

func lobbySignal(starterID string, deadline time.Time) signal {
	return signal{room: "ABC-123", kind: "timer-lobby", event: map[string]any{
		"type": "timer-lobby", "roomCode": "ABC-123", "roomName": "Deep work",
		"starterName": "Ada", "starterUserId": starterID,
		"lobbyDeadline": deadline.UTC().Format(time.RFC3339Nano),
	}}
}

// The server opens a 30-second lobby when a run starts and broadcasts it.
// The terminal's job is to ask, and to take silence for an answer.
func TestDashShowsTheJoinPrompt(t *testing.T) {
	m := dashAt(80, 24)
	m.Update(lobbySignal("someone-else", time.Now().Add(22*time.Second)))
	if m.prompt == nil {
		t.Fatal("expected a join prompt")
	}
	view := m.View()
	for _, want := range []string{"Ada", "Deep work", "join?", "y/n"} {
		if !strings.Contains(view, want) {
			t.Errorf("banner missing %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "no answer means you sit this one out") {
		t.Errorf("the footer should explain what silence does:\n%s", view)
	}
	// The banner sits under the header rather than over the screen: half a
	// minute to answer is no reason to hide what you were doing.
	if !strings.Contains(view, "FOCUS") {
		t.Errorf("the desk should still be visible behind the prompt:\n%s", view)
	}
}

// Your own start is not an invitation; you are already in it.
func TestDashIgnoresYourOwnLobby(t *testing.T) {
	m := dashAt(80, 24)
	m.Update(lobbySignal("me-id", time.Now().Add(30*time.Second)))
	if m.prompt != nil {
		t.Error("your own lobby should not prompt you")
	}
}

func TestDashJoinPromptAnswers(t *testing.T) {
	m := dashAt(80, 24)
	m.Update(lobbySignal("someone-else", time.Now().Add(30*time.Second)))
	if cmd := press(m, "y"); cmd == nil {
		t.Fatal("y should join")
	}
	if m.prompt != nil {
		t.Error("answering should close the prompt")
	}

	m.Update(lobbySignal("someone-else", time.Now().Add(30*time.Second)))
	if cmd := press(m, "n"); cmd != nil {
		t.Error("declining should not post anything")
	}
	if m.prompt != nil {
		t.Error("declining should close the prompt")
	}
	if !strings.Contains(m.View(), "not joining") {
		t.Errorf("expected the decline in the footer:\n%s", m.View())
	}
}

// An unanswered lobby closes on its own, and that is the answer — the run
// started without you, exactly as it would have on the web.
func TestDashJoinPromptExpires(t *testing.T) {
	m := dashAt(80, 24)
	m.Update(lobbySignal("someone-else", time.Now().Add(-time.Second)))
	if m.prompt == nil {
		t.Fatal("expected a prompt before the tick")
	}
	m.Update(tickMsg(time.Now()))
	if m.prompt != nil {
		t.Error("an expired lobby should close")
	}
	if !strings.Contains(m.View(), "lobby closed") {
		t.Errorf("expected the expiry in the footer:\n%s", m.View())
	}
}

// The prompt must not overflow a narrow window any more than anything else.
func TestDashJoinPromptFitsNarrowWindows(t *testing.T) {
	for _, width := range []int{20, 30, 40, 60, 100} {
		m := dashAt(width, 20)
		m.Update(lobbySignal("someone-else", time.Now().Add(22*time.Second)))
		for i, line := range strings.Split(m.View(), "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Errorf("width %d: line %d is %d wide: %q", width, i, got, line)
			}
		}
	}
}

// Someone else finishing a todo is worth a line; your own echoed back at
// you is not.
func TestDashTodoDoneNotice(t *testing.T) {
	m := dashAt(80, 24)
	m.Update(signal{room: "ABC-123", kind: "todo-done", event: map[string]any{
		"type": "todo-done", "actorId": "someone-else", "actor": "Ada", "text": "read the diff",
	}})
	if !strings.Contains(m.View(), "Ada completed: read the diff") {
		t.Errorf("expected the notice:\n%s", m.View())
	}

	m = dashAt(80, 24)
	m.Update(signal{room: "ABC-123", kind: "todo-done", event: map[string]any{
		"type": "todo-done", "actorId": "me-id", "actor": "Sam", "text": "mine",
	}})
	if strings.Contains(m.View(), "completed") {
		t.Errorf("your own completion should not be announced back:\n%s", m.View())
	}
}

func TestDashCheckinKickNotice(t *testing.T) {
	m := dashAt(80, 24)
	m.Update(signal{room: "ABC-123", kind: "timer-checkin-kick", event: map[string]any{
		"type": "timer-checkin-kick", "userIds": []any{"someone-else", "me-id"},
	}})
	if !strings.Contains(m.View(), "didn't check in") {
		t.Errorf("expected the kick notice:\n%s", m.View())
	}

	m = dashAt(80, 24)
	m.Update(signal{room: "ABC-123", kind: "timer-checkin-kick", event: map[string]any{
		"type": "timer-checkin-kick", "userIds": []any{"someone-else"},
	}})
	if strings.Contains(m.View(), "didn't check in") {
		t.Errorf("someone else's kick is not your notice:\n%s", m.View())
	}
}

// Every other signal is a nudge to re-read, which is what the browser does
// with them too.
func TestDashPlainSignalsRefresh(t *testing.T) {
	for _, kind := range []string{"todos", "solo-timer", "timer-phase", "settings"} {
		m := dashAt(80, 24)
		if _, cmd := m.Update(signal{room: "ABC-123", kind: kind}); cmd == nil {
			t.Errorf("%q should trigger a re-read", kind)
		}
	}
}

func TestDashDoesNotQuitWhenTheRunEnds(t *testing.T) {
	m := dashAt(80, 24)
	_, cmd := m.Update(deskMsg{desk: deskResponse{SoloTimer: nil}})
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("ending a run should not quit the desk")
		}
	}
	if !strings.Contains(m.View(), "f to start one") {
		t.Errorf("expected the idle desk:\n%s", m.View())
	}
}

func TestDashZoomToggles(t *testing.T) {
	m := dashAt(80, 24)
	press(m, "w")
	if !m.zoom {
		t.Fatal("w should zoom the timer")
	}
	if strings.Contains(m.View(), "todos") {
		t.Errorf("the zoomed view should drop the lists:\n%s", m.View())
	}
	_, cmd := m.handleKey(key("esc"))
	if cmd != nil {
		t.Fatal("esc from zoom should return to the desk, not quit")
	}
	if m.zoom {
		t.Fatal("esc should unzoom")
	}
}

// A countdown sitting at zero ticks twice a second; without the in-flight
// guard it would stack a request every tick.
func TestDashDoesNotStackRefreshes(t *testing.T) {
	m := dashAt(80, 24)
	m.desk.SoloTimer.SecondsLeft = 0
	m.countdown = newCountdown(0)
	m.Update(tickMsg(time.Now()))
	before := m.lastRefresh
	m.Update(tickMsg(time.Now()))
	if m.lastRefresh != before {
		t.Error("a second tick started another refresh while one was in flight")
	}
}

// A running block does not need polling — its own clock is enough — until
// the safety-net interval passes and something started elsewhere might have
// changed underneath.
func TestDashDoesNotRefreshWhileTimeRemains(t *testing.T) {
	m := dashAt(80, 24)
	m.Update(tickMsg(time.Now()))
	if m.refreshing {
		t.Error("a running countdown should not refresh on every tick")
	}
	m.lastRefresh = time.Now().Add(-2 * dashRefreshInterval)
	m.Update(tickMsg(time.Now()))
	if !m.refreshing {
		t.Error("a stale desk should refresh")
	}
}

// A blip in connectivity should dim the display, not tear down a countdown
// the user is watching — the block is running on the server regardless.
func TestDashSurvivesARefreshFailure(t *testing.T) {
	m := dashAt(80, 24)
	m.refreshing = true
	if _, cmd := m.Update(deskMsg{err: errors.New("network down")}); cmd != nil {
		t.Error("a failed refresh should not quit or issue work")
	}
	if m.desk.SoloTimer == nil {
		t.Error("the desk should survive a failed refresh")
	}
	if m.err == nil {
		t.Error("the failure should be recorded for the display")
	}
	if m.refreshing {
		t.Error("the in-flight guard should clear so the next tick can retry")
	}
	if !strings.Contains(m.View(), "offline") {
		t.Error("the view should show the offline state")
	}
}

func TestDashGuestOpensLogin(t *testing.T) {
	t.Setenv("JUNKIE_CONFIG", t.TempDir()+"/config.json")
	m := dashAt(80, 24)
	m.identity.Guest = true
	m.user = "guest"
	press(m, "L")
	if m.login == nil {
		t.Fatal("L should open sign-in for a guest")
	}
	if !strings.Contains(m.View(), "sign in") {
		t.Errorf("expected the sign-in screen:\n%s", m.View())
	}
	press(m, "q", "u", "i", "t")
	if m.login == nil {
		t.Fatal("typing q on the sign-in screen should not quit")
	}
	if got := string(m.login.username); got != "quit" {
		t.Errorf("typed %q", got)
	}
	press(m, "esc")
	if m.login != nil {
		t.Fatal("esc should close sign-in")
	}
}

func TestDashRefreshesWhenTheClockRunsOut(t *testing.T) {
	m := dashAt(80, 24)
	m.desk.SoloTimer.SecondsLeft = 0
	m.countdown = newCountdown(0)
	if _, cmd := m.Update(tickMsg(time.Now())); cmd == nil {
		t.Fatal("expected a tick to schedule work")
	}
	if !m.refreshing {
		t.Error("an expired countdown should trigger a refresh")
	}
}
