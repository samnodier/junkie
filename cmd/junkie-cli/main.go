// Command junkie is the terminal client for junkie, the shared focus app.
//
// Bare `junkie` opens a full-screen desk you stay in. Without an account it
// is a guest on this machine; sign in and it drives the same server the
// browser does — a block started here shows up on the web mid-countdown,
// and one started there can be finished here. The server owns the clock
// when signed in: this client only renders phase_ends_at minus now, and
// asks the server to transition when that hits zero. Guest blocks persist
// as an endsAt on disk, so closing the terminal mid-block loses nothing
// either way.
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
	// would hang a script rather than answer it.
	if len(args) == 0 {
		if terminalWidth() == 0 {
			return cmdStatus(nil)
		}
		return cmdDash(nil)
	}

	name, rest := args[0], args[1:]
	switch name {
	case "help", "--help", "-h":
		writeHelp(os.Stdout, rest)
		return nil
	case "version", "--version", "-v":
		fmt.Println(versionLine())
		return nil
	}

	cmd, ok := find(name)
	if !ok {
		// `junkie ABC-123` opens the desk on that room. Only an argument
		// shaped like a code is read this way — no command name contains a
		// dash or a slash — so a mistyped command is still a mistyped
		// command rather than a room nobody is in.
		if looksLikeRoomCode(name) && len(rest) == 0 {
			return cmdDash([]string{name})
		}
		writeOverview(os.Stderr)
		return fmt.Errorf("unknown command %q", name)
	}
	// `junkie room --help` should explain rooms, not attempt them. Commands
	// that take a subcommand of their own get first refusal, so
	// `junkie room join --help` can be theirs to answer later.
	if wantsHelp(rest) && len(rest) <= 1 {
		writeCommandHelp(os.Stdout, cmd)
		return nil
	}
	return cmd.run(rest)
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

// looksLikeRoomCode is deliberately about shape, not validity: whether this
// argument was meant as a room, not whether the room exists. Being wrong
// costs a clear "you're not in that room" from the desk command.
func looksLikeRoomCode(arg string) bool {
	if strings.Contains(arg, "/r/") {
		return true
	}
	if !strings.Contains(arg, "-") {
		return false
	}
	// Not a flag, and not something with spaces in it.
	return !strings.HasPrefix(arg, "-") && !strings.ContainsAny(arg, " \t")
}
