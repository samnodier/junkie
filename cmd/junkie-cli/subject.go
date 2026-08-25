package main

import (
	"fmt"
	"strings"
	"time"
)

// The desk shows one countdown at a time, and until now that countdown was
// always the private block: a room's run got a summary line and nothing
// more, so being in both at once meant watching the one you cared about
// least. The subject is which timer the pane is drawing — your own block,
// or one of your rooms — and tab moves between them.
//
// A subject is held as a room code rather than an index, because the desk
// re-reads its rooms every few seconds and a list that reorders would
// otherwise slide the selection onto a different room mid-block.

// lobbySeconds mirrors the window startRoomTimer opens. Only the progress
// bar under a lobby countdown depends on it, so a server that changed it
// would draw a bar that fills early, not a wrong deadline.
const lobbySeconds = 30

// soloSubject is the private block: the subject you start on, and the one
// guest mode never leaves, having no rooms to move to.
const soloSubject = ""

// face is one timer flattened to what the countdown draws. Solo and room
// runs are different tables, different rules and different endpoints on the
// server, but on screen they are the same handful of lines — so the pane
// takes this rather than one of each.
type face struct {
	// Title names the room a block belongs to. Empty means the private
	// block, which needs no naming: it is the only one that is yours alone.
	Title string
	Code  string

	Phase        string // focus · break · lobby
	Label        string // the word drawn above the digits
	SecondsLeft  int
	TotalSeconds int
	EndsAt       time.Time
	BreakPending bool
	BreakMinutes int

	// Note is the room detail the solo timer has no equivalent of: which
	// session of how many, and how many people are in it.
	Note string
}

// counting reports whether this face has a deadline running. A pending
// break is an offer with no clock on it, and a room sitting idle has
// nothing at all.
func (f *face) counting() bool {
	return f != nil && !f.BreakPending && f.Phase != ""
}

func soloFace(t *soloTimer) *face {
	if t == nil {
		return nil
	}
	return &face{
		Phase:        t.Phase,
		Label:        phaseLabel(t),
		SecondsLeft:  t.SecondsLeft,
		TotalSeconds: phaseSeconds(t),
		EndsAt:       t.EndsAt,
		BreakPending: t.BreakPending,
		BreakMinutes: t.BreakMinutes,
	}
}

// roomFace draws on the same reading of a room's state that the room list
// does, so the pane and the line below it never disagree about what a room
// is doing.
func roomFace(room deskRoom) *face {
	f := &face{Title: room.Name, Code: room.Code}
	t := room.Timer
	if t == nil {
		f.Label = "idle"
		f.Note = idleRoomNote(room)
		return f
	}
	word, _, _, _ := roomStateParts(room)
	f.Phase = t.Phase
	f.Label = word
	f.SecondsLeft = t.SecondsLeft
	f.EndsAt = t.EndsAt
	f.BreakPending = t.BreakPending
	f.BreakMinutes = t.BreakMinutes
	f.TotalSeconds = roomPhaseSeconds(t)
	f.Note = roomNote(t)
	return f
}

// idleRoomNote says what the room would do if someone started it, since an
// idle room has no state of its own to report.
func idleRoomNote(room deskRoom) string {
	if room.FocusMinutes <= 0 {
		return "nobody's working"
	}
	return fmt.Sprintf("nobody's working · %d min blocks", room.FocusMinutes)
}

func roomPhaseSeconds(t *roomTimer) int {
	switch {
	case t.Phase == "lobby":
		// The lobby is the fixed window the server opens (startRoomTimer),
		// not a phase of the block, so it measures itself.
		return lobbySeconds
	case t.Phase == "break":
		return t.BreakMinutes * 60
	default:
		return t.FocusMinutes * 60
	}
}

// roomNote is the line under a room's countdown: the two things a shared
// block has that a private one does not — where you are in the set, and
// who else is here.
func roomNote(t *roomTimer) string {
	var parts []string
	if t.TotalSessions > 1 {
		parts = append(parts, fmt.Sprintf("session %d/%d", t.CurrentSession, t.TotalSessions))
	}
	if n := len(t.Participants); n > 0 {
		parts = append(parts, fmt.Sprintf("%d here", n))
	}
	if t.Paused {
		parts = append(parts, "paused")
	}
	if !t.Participant {
		parts = append(parts, "you're out")
	}
	return strings.Join(parts, " · ")
}

// subjects is what tab moves through: your own block first, then the rooms
// in the order the desk lists them.
func (m *dashModel) subjects() []string {
	out := []string{soloSubject}
	for _, room := range m.desk.Rooms {
		out = append(out, room.Code)
	}
	return out
}

// currentRoom is the selected room, if the subject is one. A subject naming
// a room that is no longer on the desk — left, or deleted out from under us
// — reports nothing, and the caller falls back to the private block.
func (m *dashModel) currentRoom() (deskRoom, bool) {
	if m.subject == soloSubject {
		return deskRoom{}, false
	}
	for _, room := range m.desk.Rooms {
		if room.Code == m.subject {
			return room, true
		}
	}
	return deskRoom{}, false
}

// currentFace is the timer the pane draws.
func (m *dashModel) currentFace() *face {
	if room, ok := m.currentRoom(); ok {
		return roomFace(room)
	}
	return soloFace(m.desk.SoloTimer)
}

// cycleSubject moves the selection one step, wrapping. Rooms only exist on
// an account, so in guest mode there is nothing to move to and the key is
// left out of the footer rather than doing nothing visibly.
func (m *dashModel) cycleSubject(step int) {
	subjects := m.subjects()
	if len(subjects) < 2 {
		return
	}
	at := 0
	for i, s := range subjects {
		if s == m.subject {
			at = i
			break
		}
	}
	next := (at + step + len(subjects)) % len(subjects)
	m.selectSubject(subjects[next])
}

// selectSubject switches the pane and re-bases the countdown on the new
// timer, so the digits change with the same tick as the title rather than
// counting down the old block for half a second.
func (m *dashModel) selectSubject(code string) {
	m.subject = code
	m.anchor()
}

// settleSubject keeps the selection honest after a refresh: a room you have
// left stops being the subject rather than leaving the pane on a block the
// desk no longer knows about.
func (m *dashModel) settleSubject() {
	if m.subject == soloSubject {
		return
	}
	if _, ok := m.currentRoom(); !ok {
		m.subject = soloSubject
	}
}

// openingSubject is what the desk opens on. Sam's case: you entered a room,
// so running `junkie` should put you in front of that room's block rather
// than a private timer that is not running. A live room you are actually in
// wins; a live room you are not in is still better than nothing; and your
// own running block outranks both, because you started it deliberately.
func openingSubject(desk deskResponse, want string) string {
	if want != "" {
		for _, room := range desk.Rooms {
			if room.Code == want {
				return room.Code
			}
		}
	}
	if desk.SoloTimer != nil {
		return soloSubject
	}
	best := soloSubject
	for _, room := range desk.Rooms {
		if room.Timer == nil {
			continue
		}
		if room.Timer.Participant {
			return room.Code
		}
		if best == soloSubject {
			best = room.Code
		}
	}
	return best
}
