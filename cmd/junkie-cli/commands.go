package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

// Solo timer bounds, mirrored from the server (startSoloTimer and
// startSoloBreak both clamp with clampInt). Checked here too so a typo is a
// clear message rather than a silently clamped block of the wrong length.
const (
	minFocusMinutes = 5
	maxFocusMinutes = 180
	minBreakMinutes = 1
	maxBreakMinutes = 60
)

func cmdLogin(args []string) error {
	args, urlFlag := flagValue(args, "url")
	if len(args) > 1 {
		return errors.New("usage: junkie login [--url URL] [USERNAME]")
	}

	// A stored login is the starting point, so `junkie login` against the
	// same server doesn't need --url repeated.
	cfg, err := loadConfig()
	if err != nil && !errors.Is(err, errNotLoggedIn) {
		return err
	}
	if urlFlag != "" {
		cfg.BaseURL = strings.TrimSuffix(strings.TrimSpace(urlFlag), "/")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = baseURLFromEnv(defaultBaseURL)
	}

	username := ""
	if len(args) == 1 {
		username = args[0]
	}
	if username == "" {
		if username, err = prompt("username: "); err != nil {
			return err
		}
	}
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return errors.New("a username is required")
	}
	password, err := promptPassword("password: ")
	if err != nil {
		return err
	}

	if warning := insecureURLWarning(cfg.BaseURL); warning != "" {
		fmt.Fprintln(os.Stderr, styleWarn.Render(warning))
	}
	fmt.Fprintf(os.Stderr, "signing in to %s…\n", cfg.BaseURL)
	c := newClient(config{BaseURL: cfg.BaseURL})
	token, err := c.login(username, password)
	if err != nil {
		return err
	}

	cfg.Token = token
	cfg.Username = username
	if err := saveConfig(cfg); err != nil {
		return err
	}
	// Record the machine's zone so focus minutes land on the day this user
	// actually had. Best-effort: the server rejects zones Postgres doesn't
	// know, and a declined timezone is no reason to fail a good login.
	_ = newClient(cfg).setTimezone(time.Local.String())

	path, _ := configPath()
	fmt.Printf("Signed in as %s. Session stored in %s\n", username, path)
	if terminalWidth() == 0 {
		return nil
	}
	return runDashboard(false, "")
}

// insecureURLWarning flags a plaintext server. The password goes over that
// connection and the session cookie comes back over it, so anyone on the
// path has both. Localhost is exempt: that is the development server, and
// the traffic never leaves the machine.
func insecureURLWarning(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "http" {
		return ""
	}
	// Hostname strips the port and an IPv6 address's brackets, which hand
	// -rolled splitting on ":" gets wrong.
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return ""
	}
	return "Warning: " + baseURL + " is not HTTPS. Your password and session " +
		"will cross the network in the clear."
}

func cmdLogout(args []string) error {
	if len(args) > 0 {
		return errors.New("usage: junkie logout")
	}
	c, _, err := authed()
	if errors.Is(err, errNotLoggedIn) {
		fmt.Println("Already signed out.")
		return nil
	}
	if err != nil {
		return err
	}
	// Revoke server-side first, then forget locally. If the network is down
	// the local file still goes, because a user who typed logout should not
	// be left holding a live token on disk.
	serverErr := c.logout()
	if err := clearConfig(); err != nil {
		return err
	}
	if serverErr != nil {
		fmt.Printf("Signed out locally. The server could not be reached to revoke the session: %v\n", serverErr)
		return nil
	}
	fmt.Println("Signed out.")
	return nil
}

func cmdWhoami(args []string) error {
	args, asJSON := hasFlag(args, "json")
	if len(args) > 0 {
		return errors.New("usage: junkie whoami [--json]")
	}
	c, cfg, err := authed()
	if err != nil {
		return err
	}
	me, err := c.me()
	if err != nil {
		return err
	}
	if me.User == nil {
		return errSessionExpired
	}
	if asJSON {
		return printJSON(map[string]any{
			"username": me.User.Username, "displayName": me.User.DisplayName,
			"timezone": me.Timezone, "server": cfg.BaseURL,
		})
	}
	fmt.Printf("%s (%s) on %s\n", me.User.DisplayName, me.User.Username, cfg.BaseURL)
	return nil
}

func cmdDash(args []string) error {
	code, err := optionalRoomCode(args, "usage: junkie dash [CODE]")
	if err != nil {
		return err
	}
	return runDashboard(false, code)
}

func cmdStatus(args []string) error {
	args, asJSON := hasFlag(args, "json")
	if len(args) > 0 {
		return errors.New("usage: junkie status [--json]")
	}
	s, _, err := openDeskSession()
	if err != nil {
		return err
	}
	defer s.Close()
	desk, err := s.Load()
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(desk)
	}
	fmt.Print(renderStatus(desk, terminalWidth()))
	return nil
}

func cmdTodos(args []string) error {
	args, asJSON := hasFlag(args, "json")
	if len(args) > 0 {
		return errors.New("usage: junkie todos [--json]\n(adding and completing todos is done from the desk: `junkie`)")
	}
	s, _, err := openDeskSession()
	if err != nil {
		return err
	}
	defer s.Close()
	desk, err := s.Load()
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(desk.Todos)
	}
	fmt.Print(renderTodos(desk.Todos, terminalWidth()))
	return nil
}

func cmdRooms(args []string) error {
	args, asJSON := hasFlag(args, "json")
	if len(args) > 0 {
		return errors.New("usage: junkie rooms [--json]")
	}
	c, _, err := authed()
	if err != nil {
		return err
	}
	desk, err := c.desk()
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(desk.Rooms)
	}
	fmt.Print(renderRooms(desk.Rooms, terminalWidth()))
	return nil
}

func cmdStats(args []string) error {
	args, asJSON := hasFlag(args, "json")
	if len(args) > 0 {
		return errors.New("usage: junkie stats [--json]")
	}
	s, _, err := openDeskSession()
	if err != nil {
		return err
	}
	defer s.Close()
	profile, err := s.Profile()
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(profile)
	}
	fmt.Print(renderStats(profile, terminalWidth()))
	return nil
}

func cmdFocus(args []string) error {
	args, watch := hasFlag(args, "watch")
	minutes, err := optionalMinutes(args, "junkie focus [MINUTES] [--watch]")
	if err != nil {
		return err
	}
	if minutes != 0 && (minutes < minFocusMinutes || minutes > maxFocusMinutes) {
		return fmt.Errorf("a focus block is %d–%d minutes", minFocusMinutes, maxFocusMinutes)
	}
	s, _, err := openDeskSession()
	if err != nil {
		return err
	}
	desk, err := s.Load()
	if err != nil {
		s.Close()
		return err
	}
	if desk.SoloTimer != nil {
		s.Close()
		if desk.SoloTimer.BreakPending {
			return errors.New("a break is waiting — `junkie break` to take it, `junkie skip` to go straight on")
		}
		return fmt.Errorf("a %s block is already running (%s left)",
			phaseLabel(desk.SoloTimer), formatDuration(desk.SoloTimer.SecondsLeft))
	}
	form := url.Values{}
	if minutes != 0 {
		form.Set("focus_minutes", strconv.Itoa(minutes))
	}
	if err := s.Do("/solo/start", form); err != nil {
		s.Close()
		return err
	}
	s.Close()
	return afterStart(watch, "Focus block started")
}

func cmdBreak(args []string) error {
	args, watch := hasFlag(args, "watch")
	minutes, err := optionalMinutes(args, "junkie break [MINUTES] [--watch]")
	if err != nil {
		return err
	}
	if minutes != 0 && (minutes < minBreakMinutes || minutes > maxBreakMinutes) {
		return fmt.Errorf("a break is %d–%d minutes", minBreakMinutes, maxBreakMinutes)
	}
	s, _, err := openDeskSession()
	if err != nil {
		return err
	}
	desk, err := s.Load()
	if err != nil {
		s.Close()
		return err
	}
	if desk.SoloTimer == nil {
		s.Close()
		return errors.New("no break is waiting — `junkie focus` to start a block")
	}
	if !desk.SoloTimer.BreakPending {
		s.Close()
		return fmt.Errorf("no break is waiting (a %s block is running)", phaseLabel(desk.SoloTimer))
	}
	form := url.Values{}
	if minutes != 0 {
		form.Set("minutes", strconv.Itoa(minutes))
	}
	if err := s.Do("/solo/break/start", form); err != nil {
		s.Close()
		return err
	}
	s.Close()
	return afterStart(watch, "Break started")
}

func cmdSkip(args []string) error {
	if len(args) > 0 {
		return errors.New("usage: junkie skip")
	}
	s, _, err := openDeskSession()
	if err != nil {
		return err
	}
	desk, err := s.Load()
	if err != nil {
		s.Close()
		return err
	}
	if desk.SoloTimer == nil || desk.SoloTimer.Phase != "break" {
		s.Close()
		return errors.New("there is no break to skip")
	}
	if err := s.Do("/solo/break/skip", nil); err != nil {
		s.Close()
		return err
	}
	s.Close()
	return afterStart(false, "Break skipped — next block started")
}

func cmdCancel(args []string) error {
	if len(args) > 0 {
		return errors.New("usage: junkie cancel")
	}
	s, _, err := openDeskSession()
	if err != nil {
		return err
	}
	defer s.Close()
	desk, err := s.Load()
	if err != nil {
		return err
	}
	if desk.SoloTimer == nil {
		return errors.New("no timer is running")
	}
	if desk.SoloTimer.Phase != "focus" {
		return errors.New("only a running focus block can be cancelled — `junkie skip` ends a break")
	}
	if err := s.Do("/solo/cancel", nil); err != nil {
		return err
	}
	// Ending early keeps nothing: minutes are credited at the focus->break
	// flip, which cancelling never reaches. Say so, so it isn't a surprise
	// when the day's total doesn't move.
	fmt.Println("Block cancelled. Minutes are only credited when a block completes, so nothing was banked.")
	return nil
}

// afterStart re-reads the desk so the confirmation quotes the length the
// server actually stored (it clamps), then hands over to the countdown when
// --watch asked for it.
func afterStart(watch bool, message string) error {
	if watch {
		return runDashboard(true, "")
	}
	s, _, err := openDeskSession()
	if err != nil {
		return err
	}
	defer s.Close()
	desk, err := s.Load()
	if err != nil {
		return err
	}
	if desk.SoloTimer == nil {
		return errors.New("the server did not start a timer — try `junkie status`")
	}
	fmt.Printf("%s · %s · ends at %s\n", message,
		formatDuration(desk.SoloTimer.SecondsLeft),
		desk.SoloTimer.EndsAt.Local().Format("15:04"))
	fmt.Println(styleFaint.Render("`junkie watch` to follow it, `junkie` to open the desk."))
	return nil
}

func cmdWatch(args []string) error {
	code, err := optionalRoomCode(args, "usage: junkie watch [CODE]")
	if err != nil {
		return err
	}
	return runDashboard(true, code)
}

// optionalRoomCode reads the room a desk command should open on. Codes are
// normalized the way `junkie room` normalizes them, so pasting a room's URL
// works here too.
func optionalRoomCode(args []string, usage string) (string, error) {
	switch len(args) {
	case 0:
		return "", nil
	case 1:
		code := normalizeCode(args[0])
		if code == "" {
			return "", errors.New(usage)
		}
		return code, nil
	default:
		return "", errors.New(usage)
	}
}

// optionalMinutes reads the one positional argument these commands take.
func optionalMinutes(args []string, usage string) (int, error) {
	if len(args) == 0 {
		return 0, nil
	}
	if len(args) > 1 {
		return 0, errors.New("usage: " + usage)
	}
	n, err := strconv.Atoi(args[0])
	if err != nil {
		return 0, fmt.Errorf("%q is not a number of minutes", args[0])
	}
	return n, nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func prompt(label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// promptPassword reads without echoing when stdin is a terminal, and falls
// back to a plain line when it isn't — so `echo pw | junkie login sam` works
// in a script without the terminal call failing on a pipe.
func promptPassword(label string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return prompt(label)
	}
	fmt.Fprint(os.Stderr, label)
	pw, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(pw), nil
}
