package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// localStore is guest mode for the terminal: todos, a solo timer, and a
// work map, all on this machine. Nothing reaches the server. Signing in
// later does not copy this data into the account — the same rule as the
// browser guest desk.
//
// The timer is persisted as an endsAt, so closing the TUI mid-block loses
// nothing: the next open picks the countdown back up and credits the
// minutes when the deadline has passed.

const (
	guestTodosFile    = "todos.json"
	guestTimerFile    = "timer.json"
	guestActivityFile = "activity.json"
	guestDay          = 24 * time.Hour
)

type localStore struct {
	dir      string
	mu       sync.Mutex
	todos    []guestTodo
	timer    *guestTimer
	activity map[string]int
}

type guestTodo struct {
	ID         string     `json:"id"`
	Text       string     `json:"text"`
	Done       bool       `json:"done"`
	Removed    bool       `json:"removed"`
	InactiveAt *time.Time `json:"inactiveAt,omitempty"`
}

// guestTimer is the browser guest's three-phase run: focus, a break sitting
// as an offer (break_offer), then a running break. The desk maps offer to
// BreakPending so the same keys work signed-in or not.
type guestTimer struct {
	Phase         string     `json:"phase"`
	FocusMinutes  int        `json:"focusMinutes"`
	BreakMinutes  int        `json:"breakMinutes,omitempty"`
	EndsAt        *time.Time `json:"endsAt,omitempty"`
	FocusRecorded bool       `json:"focusRecorded,omitempty"`
}

func dataDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("JUNKIE_DATA")); dir != "" {
		return dir, nil
	}
	base := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home directory: %w", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "junkie"), nil
}

func openLocalStore() (*localStore, error) {
	dir, err := dataDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	s := &localStore{dir: dir, activity: map[string]int{}}
	if err := s.loadFiles(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *localStore) loadFiles() error {
	if err := readJSONFile(filepath.Join(s.dir, guestTodosFile), &s.todos); err != nil {
		return err
	}
	if err := readJSONFile(filepath.Join(s.dir, guestTimerFile), &s.timer); err != nil {
		return err
	}
	if err := readJSONFile(filepath.Join(s.dir, guestActivityFile), &s.activity); err != nil {
		return err
	}
	if s.activity == nil {
		s.activity = map[string]int{}
	}
	return nil
}

func (s *localStore) Load() (deskResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advanceLocked(time.Now())
	if err := s.persistLocked(); err != nil {
		return deskResponse{}, err
	}
	return s.deskLocked(), nil
}

func (s *localStore) deskLocked() deskResponse {
	todos := make([]apiTodo, len(s.todos))
	for i, t := range s.todos {
		todos[i] = apiTodo{ID: t.ID, Text: sanitize(t.Text), Done: t.Done, Removed: t.Removed}
	}
	return deskResponse{SoloTimer: s.timer.toSolo(time.Now()), Todos: todos}
}

func (s *localStore) Do(path string, form url.Values) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advanceLocked(time.Now())
	if err := s.mutateLocked(path, form); err != nil {
		return err
	}
	return s.persistLocked()
}

func (s *localStore) mutateLocked(path string, form url.Values) error {
	switch path {
	case "/solo/start":
		mins := 50
		if v := strings.TrimSpace(form.Get("focus_minutes")); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("%q is not a number of minutes", v)
			}
			mins = n
		}
		return s.startFocusLocked(mins)
	case "/solo/cancel":
		if s.timer == nil || s.timer.Phase != "focus" {
			return errors.New("only a running focus block can be cancelled — `junkie skip` ends a break")
		}
		s.timer = nil
		return nil
	case "/solo/break/start":
		if s.timer == nil || s.timer.Phase != "break_offer" {
			return errors.New("no break is waiting — `junkie focus` to start a block")
		}
		mins := s.timer.BreakMinutes
		if v := strings.TrimSpace(form.Get("minutes")); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("%q is not a number of minutes", v)
			}
			mins = n
		}
		if mins < minBreakMinutes || mins > maxBreakMinutes {
			return fmt.Errorf("a break is %d–%d minutes", minBreakMinutes, maxBreakMinutes)
		}
		end := time.Now().Add(time.Duration(mins) * time.Minute)
		s.timer.Phase = "break"
		s.timer.BreakMinutes = mins
		s.timer.EndsAt = &end
		return nil
	case "/solo/break/skip":
		if s.timer == nil || (s.timer.Phase != "break_offer" && s.timer.Phase != "break") {
			return errors.New("there is no break to skip")
		}
		mins := s.timer.FocusMinutes
		if mins == 0 {
			mins = 50
		}
		s.timer = nil
		return s.startFocusLocked(mins)
	case "/todos":
		text := strings.TrimSpace(form.Get("text"))
		if text == "" {
			return nil
		}
		s.todos = append([]guestTodo{{ID: newGuestID(), Text: text}}, s.todos...)
		return nil
	}
	id, action, ok := parseTodoPath(path)
	if !ok {
		return errNeedsAccount
	}
	return s.todoActionLocked(id, action, form)
}

func parseTodoPath(path string) (id, action string, ok bool) {
	rest, found := strings.CutPrefix(path, "/todo/")
	if !found {
		return "", "", false
	}
	id, action, found = strings.Cut(rest, "/")
	if !found || id == "" || action == "" {
		return "", "", false
	}
	return id, action, true
}

func (s *localStore) todoActionLocked(id, action string, form url.Values) error {
	i := s.todoIndex(id)
	if i < 0 {
		return errors.New("no such todo")
	}
	switch action {
	case "toggle":
		s.todos[i].Done = !s.todos[i].Done
		s.todos[i].stamp()
	case "remove":
		s.todos[i].Removed = true
		s.todos[i].stamp()
	case "restore":
		s.todos[i].Removed = false
		s.todos[i].stamp()
	case "edit":
		if s.todos[i].Done {
			return errors.New("completed todos can't be edited")
		}
		text := strings.TrimSpace(form.Get("text"))
		if text == "" {
			return nil
		}
		s.todos[i].Text = text
	default:
		return errNeedsAccount
	}
	return nil
}

func (s *localStore) todoIndex(id string) int {
	for i, t := range s.todos {
		if t.ID == id {
			return i
		}
	}
	return -1
}

func (s *localStore) startFocusLocked(minutes int) error {
	if minutes < minFocusMinutes || minutes > maxFocusMinutes {
		return fmt.Errorf("a focus block is %d–%d minutes", minFocusMinutes, maxFocusMinutes)
	}
	if s.timer != nil {
		if s.timer.Phase == "break_offer" {
			return errors.New("a break is waiting — `junkie break` to take it, `junkie skip` to go straight on")
		}
		return fmt.Errorf("a %s block is already running", s.timer.Phase)
	}
	end := time.Now().Add(time.Duration(minutes) * time.Minute)
	s.timer = &guestTimer{Phase: "focus", FocusMinutes: minutes, EndsAt: &end}
	return nil
}

func (t *guestTodo) stamp() {
	if t.Done || t.Removed {
		now := time.Now()
		t.InactiveAt = &now
		return
	}
	t.InactiveAt = nil
}

// advanceLocked flips expired phases the way the browser guest desk does:
// a finished focus records its minutes once and becomes a break offer; a
// finished break clears the run. A pending offer has no deadline.
func (s *localStore) advanceLocked(now time.Time) {
	s.pruneTodos(now)
	t := s.timer
	if t == nil {
		return
	}
	if t.Phase == "" {
		t.Phase = "focus"
	}
	switch t.Phase {
	case "focus":
		if t.EndsAt != nil && !now.Before(*t.EndsAt) {
			if !t.FocusRecorded {
				s.recordFocus(t.FocusMinutes, now)
				t.FocusRecorded = true
			}
			t.Phase = "break_offer"
			t.BreakMinutes = breakMinutesForFocus(t.FocusMinutes)
			t.EndsAt = nil
			s.timer = t
		}
	case "break_offer":
		return
	case "break":
		if t.EndsAt != nil && !now.Before(*t.EndsAt) {
			s.timer = nil
		}
	default:
		s.timer = nil
	}
}

func (s *localStore) pruneTodos(now time.Time) {
	kept := s.todos[:0]
	for i := range s.todos {
		t := s.todos[i]
		if !(t.Done || t.Removed) {
			kept = append(kept, t)
			continue
		}
		if t.InactiveAt == nil {
			stamp := now
			t.InactiveAt = &stamp
			kept = append(kept, t)
			continue
		}
		if now.Sub(*t.InactiveAt) > guestDay {
			continue
		}
		kept = append(kept, t)
	}
	s.todos = kept
}

func (s *localStore) recordFocus(minutes int, now time.Time) {
	if minutes <= 0 {
		return
	}
	key := now.Format("2006-01-02")
	s.activity[key] += minutes
}

func (s *localStore) persistLocked() error {
	if err := writeJSONFile(filepath.Join(s.dir, guestTodosFile), s.todos); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(s.dir, guestTimerFile), s.timer); err != nil {
		return err
	}
	return writeJSONFile(filepath.Join(s.dir, guestActivityFile), s.activity)
}

func (s *localStore) Profile() (profileResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advanceLocked(time.Now())
	if err := s.persistLocked(); err != nil {
		return profileResponse{}, err
	}
	return profileResponse{Heatmap: buildGuestHeatmap(s.activity, time.Now())}, nil
}

func (s *localStore) Events() <-chan signal { return nil }

func (s *localStore) Close() {}

func (t *guestTimer) toSolo(now time.Time) *soloTimer {
	if t == nil {
		return nil
	}
	out := &soloTimer{
		Phase:        "focus",
		FocusMinutes: t.FocusMinutes,
		BreakMinutes: t.BreakMinutes,
	}
	switch t.Phase {
	case "break_offer":
		out.Phase = "break"
		out.BreakPending = true
		if out.BreakMinutes == 0 {
			out.BreakMinutes = breakMinutesForFocus(t.FocusMinutes)
		}
	case "break":
		out.Phase = "break"
		fillEnds(out, t.EndsAt, now)
	default:
		out.Phase = "focus"
		fillEnds(out, t.EndsAt, now)
	}
	return out
}

func fillEnds(out *soloTimer, ends *time.Time, now time.Time) {
	if ends == nil {
		return
	}
	out.EndsAt = *ends
	left := int(ends.Sub(now).Seconds())
	if left < 0 {
		left = 0
	}
	out.SecondsLeft = left
}

func breakMinutesForFocus(focusMinutes int) int {
	switch {
	case focusMinutes < 30:
		return 5
	case focusMinutes < 120:
		return 10
	case focusMinutes < 180:
		return 20
	default:
		return 30
	}
}

func newGuestID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b[:])
}

func heatLevel(minutes int) int {
	switch {
	case minutes >= 180:
		return 4
	case minutes >= 90:
		return 3
	case minutes >= 30:
		return 2
	case minutes > 0:
		return 1
	default:
		return 0
	}
}

// buildGuestHeatmap is the terminal's copy of web/src/lib/guestActivity.js:
// a year of local days, Sunday-first, padded into a week grid.
func buildGuestHeatmap(activity map[string]int, now time.Time) apiHeatmap {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	first := today.AddDate(0, 0, -364)
	last := today

	dayByKey := map[string]heatCell{}
	total := 0
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		mins := activity[d.Format("2006-01-02")]
		total += mins
		dayByKey[d.Format("2006-01-02")] = heatCell{
			Date:    d.Format("Jan 2"),
			Minutes: mins,
			Level:   heatLevel(mins),
		}
	}

	gridStart := first.AddDate(0, 0, -int(first.Weekday()))
	totalDays := int(last.Sub(gridStart)/(24*time.Hour)) + 1
	weeks := (totalDays + 6) / 7
	cells := make([]heatCell, weeks*7)
	for i := range cells {
		cells[i] = heatCell{Empty: true}
	}
	for week := 0; week < weeks; week++ {
		for dow := 0; dow < 7; dow++ {
			d := gridStart.AddDate(0, 0, week*7+dow)
			if d.Before(first) || d.After(last) {
				continue
			}
			if day, ok := dayByKey[d.Format("2006-01-02")]; ok {
				cells[week*7+dow] = day
			}
		}
	}

	var months []heatMonth
	labeled := map[string]bool{}
	for week := 0; week < weeks; week++ {
		for dow := 0; dow < 7; dow++ {
			d := gridStart.AddDate(0, 0, week*7+dow)
			if d.Before(first) || d.After(last) {
				continue
			}
			if d.Day() != 1 {
				continue
			}
			key := d.Format("2006-01")
			if labeled[key] {
				break
			}
			months = append(months, heatMonth{Label: d.Format("Jan"), Col: week})
			labeled[key] = true
			break
		}
	}
	return apiHeatmap{Cells: cells, Months: months, Weeks: weeks, TotalMinutes: total}
}

func readJSONFile(path string, dest any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("save %s: %w", path, err)
	}
	return nil
}
