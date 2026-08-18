// Command junkie is the terminal client for junkie, the shared focus app.
//
// It drives the same server the browser does — the /api/* reads the SPA
// consumes and the form-encoded mutations its forms post — so a block
// started here shows up on the web mid-countdown, and one started there can
// be finished here. The server owns the clock in both: this client only
// renders phase_ends_at minus now, and asks the server to transition when
// that hits zero. Focus minutes are credited server-side, so closing the
// terminal mid-block loses nothing.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// version is stamped by the release build (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		// A dead session is the one error with an obvious next step, so it
		// gets said plainly rather than wrapped in "error:".
		if errors.Is(err, errNotLoggedIn) || errors.Is(err, errSessionExpired) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	// Bare `junkie` opens the desk rather than printing usage: the common
	// case is wanting to see what's running, not wanting to read the manual.
	// Piped, it prints the one-shot status instead — a full-screen program
	// is no use to a script, and would hang it.
	if len(args) == 0 {
		if terminalWidth() == 0 {
			return cmdStatus(nil)
		}
		return cmdDash(nil)
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "login":
		return cmdLogin(rest)
	case "logout":
		return cmdLogout(rest)
	case "whoami":
		return cmdWhoami(rest)
	case "dash", "desk":
		return cmdDash(rest)
	case "status", "st":
		return cmdStatus(rest)
	case "todos", "todo":
		return cmdTodos(rest)
	case "rooms":
		return cmdRooms(rest)
	case "focus", "start":
		return cmdFocus(rest)
	case "break":
		return cmdBreak(rest)
	case "skip":
		return cmdSkip(rest)
	case "cancel", "stop":
		return cmdCancel(rest)
	case "watch":
		return cmdWatch(rest)
	case "version", "--version", "-v":
		fmt.Println("junkie " + version)
		return nil
	case "help", "--help", "-h":
		usage(os.Stdout)
		return nil
	default:
		usage(os.Stderr)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// authed loads the stored session and hands back a ready client. Every
// command except login and help starts here.
func authed() (*client, config, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, cfg, err
	}
	return newClient(cfg), cfg, nil
}

// hasFlag pulls a boolean flag out of args, returning the rest. Hand-rolled
// because the whole flag surface is three booleans and an optional number —
// a flag.FlagSet per subcommand would be more ceremony than the thing it
// parses.
func hasFlag(args []string, name string) ([]string, bool) {
	out := make([]string, 0, len(args))
	found := false
	for _, a := range args {
		if a == "--"+name {
			found = true
			continue
		}
		out = append(out, a)
	}
	return out, found
}

// flagValue pulls "--name value" or "--name=value" out of args.
func flagValue(args []string, name string) ([]string, string) {
	out := make([]string, 0, len(args))
	value := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--"+name && i+1 < len(args):
			value = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--"+name+"="):
			value = strings.TrimPrefix(args[i], "--"+name+"=")
		default:
			out = append(out, args[i])
		}
	}
	return out, value
}

func usage(w *os.File) {
	fmt.Fprint(w, `junkie — shared focus, in the terminal

Usage:
  junkie                      show the desk (same as `+"`junkie status`"+`)
  junkie login [--url URL]    sign in and store the session
  junkie logout               end the session and forget it
  junkie whoami               show who this terminal is signed in as

  junkie status [--json]      solo timer, todo counts, room activity
  junkie todos [--json]       private todos
  junkie rooms [--json]       your rooms and what their timers are doing

  junkie focus [MINUTES]      start a private focus block (5–180, default 50)
  junkie break [MINUTES]      start the offered break (1–60)
  junkie skip                 skip the break, start the next block
  junkie cancel               end the current block early
  junkie watch                full-screen countdown for the running block

Keys on the desk:
  f focus · b break · s skip · c cancel · r refresh · q quit

Flags:
  --watch                     on focus/break, stay and draw the countdown
  --json                      machine-readable output for read commands

Environment:
  JUNKIE_URL                  server to talk to (overrides the stored one)
  JUNKIE_CONFIG               path to the config file
`)
}
