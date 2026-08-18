package main

import "strings"

// Everything the server sends may have been typed by somebody else. A room's
// name is set by whoever created it, a todo's text by any member of that
// room, and both are rendered straight into this terminal — where a browser
// would have shown them as text, a terminal executes them.
//
// The server stores these verbatim (limitRunes caps the length and trims the
// ends, nothing more), which is right for HTML and wrong for a TTY: an
// escape sequence in a room name could move the cursor, repaint the screen,
// retitle the window, or — with OSC 52 — write the user's clipboard. So the
// client cleans what it receives, at the boundary where it receives it,
// rather than trusting every future call site to remember.

// sanitize strips what a terminal would act on rather than print.
//
// C0 and C1 controls and DEL cover the escape sequences themselves; ESC in
// particular is the one that starts them. Tabs and newlines become spaces
// instead of vanishing, so words either side of them stay apart in a display
// that is single-line anyway. The bidirectional overrides go too: they
// cannot execute anything, but they can reorder a line into reading as
// something it is not.
func sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteRune(' ')
		case r < 0x20 || r == 0x7f:
			// C0 controls and DEL, ESC among them.
		case r >= 0x80 && r <= 0x9f:
			// C1 controls, which some terminals honour as escapes.
		case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
			// Bidirectional overrides and isolates.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sanitizeDesk cleans a decoded desk in place. Every string here is either
// user-typed (room names, todo text, display names) or server-generated and
// harmless — cleaning both is cheaper than auditing which is which.
func sanitizeDesk(desk *deskResponse) {
	for i := range desk.Todos {
		sanitizeTodo(&desk.Todos[i])
	}
	for i := range desk.Rooms {
		room := &desk.Rooms[i]
		room.Code = sanitize(room.Code)
		room.Name = sanitize(room.Name)
		for j := range room.Mine {
			sanitizeTodo(&room.Mine[j])
		}
		for j := range room.Others {
			sanitizeTodo(&room.Others[j])
		}
		if room.Timer != nil {
			for j := range room.Timer.Participants {
				sanitizeUser(&room.Timer.Participants[j])
			}
		}
	}
}

func sanitizeTodo(t *apiTodo) {
	t.Text = sanitize(t.Text)
	t.DisplayName = sanitize(t.DisplayName)
}

func sanitizeUser(u *apiUser) {
	u.Username = sanitize(u.Username)
	u.DisplayName = sanitize(u.DisplayName)
}
