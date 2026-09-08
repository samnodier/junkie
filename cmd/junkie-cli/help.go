package main

import (
	"fmt"
	"io"
	"runtime"
	"sort"
	"strings"
)

// Every command is described once, here, and both the overview and the
// per-command help are generated from it — so `junkie help focus` and the
// list you get from `junkie help` cannot drift apart.

type command struct {
	name    string
	aliases []string
	args    string
	group   string
	summary string
	// detail is what `junkie help <name>` prints under the usage line: what
	// the command actually does, and anything about it that would otherwise
	// be a surprise.
	detail string
	run    func([]string) error
}

// Groups, in the order they are printed.
const (
	groupAccount = "Account"
	groupRooms   = "Rooms"
	groupLooking = "Looking at things"
)

var groupOrder = []string{groupAccount, groupRooms, groupLooking}

func commands() []command {
	return []command{
		{
			name: "login", group: groupAccount, args: "[USERNAME]",
			summary: "sign in and remember the session",
			run:     cmdLogin,
			detail: `Asks for your username and password — the same ones you use on the web —
and stores the session in ~/.config/junkie/config.json (mode 0600).

In a terminal it then opens the desk, signed in. Piped, it prints the
confirmation and stops, so a script can sign in without hanging in a
full-screen program.

You stay signed in across terminals and reboots for 30 days from the moment
you sign in. The session does not renew as you use it, so about once a month
you will be asked again. Changing your password anywhere ends it early,
because that ends every other session on your account.

  --url URL    sign in to a different server (default: the hosted one)`,
		},
		{
			name: "logout", group: groupAccount,
			summary: "sign out and forget the session",
			run:     cmdLogout,
			detail: `Revokes the session on the server and deletes the stored copy.

If the server can't be reached, the local copy is deleted anyway — someone
who typed logout should not be left holding a live session on disk.`,
		},
		{
			name: "whoami", group: groupAccount,
			summary: "show who this terminal is signed in as",
			run:     cmdWhoami,
			detail:  `Prints your display name, username, and which server this terminal is talking to.`,
		},
		{
			name: "room", group: groupRooms, args: "<command>",
			summary: "create, join and run shared rooms",
			run:     cmdRoom,
			detail: `  junkie room new NAME          create a room and print its code
  junkie room join CODE         join someone else's room
  junkie room start [CODE] [M]  start a block, opening the 30-second lobby
  junkie room enter [CODE]      get into the block that's running
  junkie room leave [CODE]      leave the block
  junkie room checkin [CODE]    confirm you're staying for the next block
  junkie room skip [CODE]       skip the room's break

CODE can be left out when you're in exactly one room. Pasting a room's URL
works as well as typing its code.

A block already in focus can't be joined mid-way: 'room enter' puts you in
the queue and you're in at the next break. During the lobby or a break, it
puts you in straight away.`,
		},
		{
			name: "status", aliases: []string{"st"}, group: groupLooking,
			summary: "timer, todo counts and room activity",
			run:     cmdStatus,
			detail:  `One glance at everything. This is also what bare ` + "`junkie`" + ` prints when its output is piped somewhere.`,
		},
		{
			name: "todos", aliases: []string{"todo"}, group: groupLooking,
			summary: "list your private todos",
			run:     cmdTodos,
			detail:  `Lists them. To add, complete or remove one, open the desk with ` + "`junkie`" + ` — the list is worked from there.`,
		},
		{
			name: "stats", aliases: []string{"map"}, group: groupLooking,
			summary: "the work map: a year of focused days",
			run:     cmdStats,
			detail: `A year of focused days as a grid, plus your total, your current streak and
your best day.

A day with no focus on it yet doesn't break the streak until the day is over.`,
		},
	}
}

// find resolves a name or alias.
func find(name string) (command, bool) {
	for _, c := range commands() {
		if c.name == name {
			return c, true
		}
		for _, alias := range c.aliases {
			if alias == name {
				return c, true
			}
		}
	}
	return command{}, false
}

// wantsHelp spots --help anywhere in a command's arguments, so
// `junkie room --help` works as well as `junkie help room`.
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "--help" || a == "-h" || a == "help" {
			return true
		}
	}
	return false
}

func writeHelp(w io.Writer, args []string) {
	if len(args) > 0 {
		if c, ok := find(args[0]); ok {
			writeCommandHelp(w, c)
			return
		}
		fmt.Fprintf(w, "junkie: no command called %q\n\n", args[0])
	}
	writeOverview(w)
}

func writeCommandHelp(w io.Writer, c command) {
	usage := "junkie " + c.name
	if c.args != "" {
		usage += " " + c.args
	}
	fmt.Fprintf(w, "\n  %s — %s\n\n", usage, c.summary)
	if len(c.aliases) > 0 {
		fmt.Fprintf(w, "  also: junkie %s\n\n", strings.Join(c.aliases, ", junkie "))
	}
	for _, line := range strings.Split(c.detail, "\n") {
		// A blank line stays blank rather than becoming two spaces.
		if strings.TrimSpace(line) == "" {
			fmt.Fprintln(w)
			continue
		}
		fmt.Fprintf(w, "  %s\n", line)
	}
	if readOnly(c.name) {
		fmt.Fprint(w, "\n  --json    print the raw data instead\n")
	}
	fmt.Fprintln(w)
}

// readOnly names the commands that take --json, so the flag is documented
// exactly where it works.
func readOnly(name string) bool {
	switch name {
	case "status", "todos", "rooms", "stats", "whoami":
		return true
	}
	return false
}

func writeOverview(w io.Writer) {
	// The desk is the program; the commands are the way in and the way to
	// script it. Listing twenty of them first made a small tool read like a
	// large one, so what almost everyone needs comes first and the rest is
	// named as the rest.
	fmt.Fprint(w, `
  junkie — shared focus, in the terminal

  junkie                        open the desk, and stay in it
  junkie CODE                   open the desk on that room's block

  No account needed: without one the desk is yours alone on this machine.
  Sign in with `+"`junkie login`"+`, or L inside the desk, to share rooms with
  the web and keep your work map across machines.

  Inside the desk — everything happens here

    Your block   f  start   b  break   s  skip break   x  end it
    A room's     f  start   b  break   s  skip break   i  I'm in   x  leave
    Todos        a  add   space  done   e  edit   d  remove   u  undo

    Moving       tab  next pane (block · todos · rooms)   shift+tab  back
                 j/k  scroll the focused pane   g/G  its ends

    Rooms        A  join one by code   L  sign in   y/n  answer a join offer
    Screen       w  zoom the timer   esc  back   r  refresh
                 q  quit — from the zoomed pane it goes back to the desk first

  The pane j and k are pointed at is marked with a ‹. With a room on screen
  the block keys act on that room, and the todo list is the room's — yours to
  work, everyone else's to read.

  Everything below is for reading from a shell. `+"`junkie help <command>`"+` explains one.

`)
	byGroup := map[string][]command{}
	for _, c := range commands() {
		byGroup[c.group] = append(byGroup[c.group], c)
	}
	for _, group := range groupOrder {
		list := byGroup[group]
		sort.Slice(list, func(i, j int) bool { return list[i].name < list[j].name })
		fmt.Fprintf(w, "  %s\n", group)
		for _, c := range list {
			usage := c.name
			if c.args != "" {
				usage += " " + c.args
			}
			fmt.Fprintf(w, "    %-26s %s\n", usage, c.summary)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprint(w, `  Environment
    JUNKIE_URL                 talk to a different server for one command
    JUNKIE_CONFIG              use a different config file
    JUNKIE_DATA                keep guest todos and timer somewhere else

`)
}

// versionLine names the build. A release carries its tag; anything built
// from source says so rather than claiming a version it does not have.
func versionLine() string {
	return fmt.Sprintf("junkie %s (%s/%s, %s)", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
}
