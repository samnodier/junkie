package main

import "time"

// The payload structs mirror what the SPA consumes, field for field. Only
// what the terminal renders is decoded — avatars, roles and versions are
// deliberately absent, and unknown fields decode away harmlessly, so the
// server can grow without breaking an older installed CLI.

type meResponse struct {
	User     *apiUser `json:"user"`
	Timezone string   `json:"timezone"`
}

type apiUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	CheckedIn   bool   `json:"checkedIn"`
}

type deskResponse struct {
	SoloTimer *soloTimer `json:"soloTimer"`
	Todos     []apiTodo  `json:"todos"`
	Rooms     []deskRoom `json:"rooms"`
}

// soloTimer is the private focus run. Phase is "focus" or "break";
// BreakPending marks the one state with no deadline running — the break
// waiting to be started, which the ring on the web shows as adjustable.
type soloTimer struct {
	Phase        string    `json:"phase"`
	FocusMinutes int       `json:"focusMinutes"`
	BreakMinutes int       `json:"breakMinutes"`
	EndsAt       time.Time `json:"endsAt"`
	SecondsLeft  int       `json:"secondsLeft"`
	BreakPending bool      `json:"breakPending"`
}

type apiTodo struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Done    bool   `json:"done"`
	Removed bool   `json:"removed"`
	// Only room todos carry an author; a private todo has none, so these
	// stay out of --json output rather than showing as empty strings.
	UserID      string `json:"userId,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	ReadOnly    bool   `json:"readOnly,omitempty"`
}

// roomResponse is one room's own page. It carries what the desk list does
// not: whether you are queued to join, which is a different state from being
// in the block and from being out of it.
type roomResponse struct {
	Room struct {
		Code string `json:"code"`
		Name string `json:"name"`
	} `json:"room"`
	Timer       *roomTimer `json:"timer"`
	Waiting     bool       `json:"waiting"`
	MemberCount int        `json:"memberCount"`
}

type deskRoom struct {
	Code           string     `json:"code"`
	Name           string     `json:"name"`
	FocusMinutes   int        `json:"focusMinutes"`
	BreakMinutes   int        `json:"breakMinutes"`
	AutoSessions   int        `json:"autoSessions"`
	RequireCheckin bool       `json:"requireCheckin"`
	Timer          *roomTimer `json:"timer"`
	Mine           []apiTodo  `json:"mine"`
	Others         []apiTodo  `json:"others"`
}

// roomTimer is a shared run. Phase adds "lobby" — the 30-second window
// after someone hits start, during which members can still join.
type roomTimer struct {
	RunID          string    `json:"runId"`
	Phase          string    `json:"phase"`
	EndsAt         time.Time `json:"endsAt"`
	SecondsLeft    int       `json:"secondsLeft"`
	FocusMinutes   int       `json:"focusMinutes"`
	BreakMinutes   int       `json:"breakMinutes"`
	CurrentSession int       `json:"currentSession"`
	TotalSessions  int       `json:"totalSessions"`
	Participant    bool      `json:"participant"`
	CheckedIn      bool      `json:"checkedIn"`
	Participants   []apiUser `json:"participants"`
	Paused         bool      `json:"paused"`
	BreakPending   bool      `json:"breakPending"`
}

// active reports whether a solo run is counting down right now, as opposed
// to sitting on a pending break with no deadline.
func (t *soloTimer) active() bool {
	return t != nil && !t.BreakPending
}
