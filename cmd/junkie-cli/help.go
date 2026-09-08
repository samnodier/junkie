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
	groupFocus   = "Your own focus"
	groupRooms   = "Rooms"
	groupLooking = "Looking at things"
)

var groupOrder = []string{groupAccount, groupFocus, groupRooms, groupLooking}

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
			name: "focus", aliases: []string{"start"}, group: groupFocus, args: "[MINUTES]",
			summary: "start a private focus block",
			run:     cmdFocus,
			detail: `Starts a private block of MINUTES minutes (5–180, default 50).

Without an account this runs on this machine only. Signed in, the server
owns the clock, so the block keeps running whether or not this terminal
stays open, and the minutes are credited when it completes. A block started
here shows up on the web mid-countdown, and vice versa.

  --watch    stay in the desk, timer pane filling the window`,
		},
		{
			name: "break", group: groupFocus, args: "[MINUTES]",
			summary: "take the break you were offered",
			run:     cmdBreak,
			detail: `When a focus block ends, junkie offers a break rather than starting one.
This takes it, for MINUTES minutes (1–60; the default is derived from the
block you just finished).

An offered break left untouched for an hour is treated as walked away from:
the run ends and the next visit starts fresh. The focus minutes were already
banked at the end of the block, so nothing is lost.

  --watch    stay in the desk, timer pane filling the window`,
		},
		{
			name: "skip", group: groupFocus,
			summary: "skip the break and start the next block",
			run:     cmdSkip,
			detail:  `Ends the break and immediately starts another focus block the same length as the last one.`,
		},
		{
			name: "cancel", aliases: []string{"stop"}, group: groupFocus,
			summary: "end the running block early",
			run:     cmdCancel,
			detail: `Ends the focus block now.

Minutes are only credited when a block completes, so a cancelled block banks
nothing. Use it when you're abandoning the session, not when you're done.`,
		},
		{
			name: "watch", group: groupFocus,
			summary: "the desk, timer pane filling the window",
			run:     cmdWatch,
			detail: `The same program as bare ` + "`junkie`" + `, with the countdown taking the
window. Sized to the terminal, so a strip parked down the side of a screen
works: it steps down to smaller digits, then to a line of text, then to the
numbers alone.

  junkie watch          your own block
  junkie watch CODE     that room's block

Tab moves between them without leaving the window. No block running is fine
— f starts one. Esc or q returns to the full desk, and q again quits from
there. The block keeps running either way.`,
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
			name: "dash", aliases: []string{"desk"}, group: groupLooking,
			summary: "open the desk full-screen (same as bare `junkie`)",
			run:     cmdDash,
			detail: `The timer, your todos and your rooms on one screen, kept live. No account
is required: without one this is a guest desk on this machine. L signs in
to sync with the same account the web uses.

  tab        show the next block: your own, then each room
  f b s      start focus · take the break · skip it
  x          end the block on screen — yours, or leave a room's
  J          join a room by code, without leaving the desk
  j k        move down and up the list
  g G        jump to the top and the bottom of it
  space      complete or un-complete
  a e d u    add · edit · remove · undo the last remove
  w          timer pane fills the window (` + "`junkie watch`" + `)
  L          sign in (guest)
  y n        answer a room's join prompt
  r q        refresh · quit

The countdown shows one block at a time and tab moves between them — your
private block first, then each room you are in. The room the pane is
showing is the one marked in the list below it.

With a room on screen the timer keys act on that room, and two more apply:

  f b s      start a block · take the break · skip it
  i          I'm in — join the block, or check in for the next one
  x          leave the block — the same key that ends your own

The todo list is the room's while a room is on screen: yours to work, and
everyone else's to read. A todo added there joins the room's list rather
than your private one.

` + "`junkie CODE`" + ` opens the desk on a room directly, and ` + "`junkie watch CODE`" + ` opens it
with that room's countdown filling the window.

When someone starts a block in one of your rooms, the desk asks whether you
want in and counts down the 30 seconds you have to answer. Not answering is
an answer. This only reaches you while the desk is open. Saying yes also
puts that room's block on screen.`,
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
			name: "rooms", group: groupLooking,
			summary: "your rooms and what their timers are doing",
			run:     cmdRooms,
			detail:  `Every room you're in, with its code and whether a block is running in it.`,
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
  junkie login                  sign in to share rooms with the web

  Inside the desk

    f  start a focus block          i  I'm in — join a room's block
    b  take the break               x  end it — your block, or a room's
    s  skip the break               tab  show the next block
    J  join a room by code          w  timer fills the window

    a  add a todo                   j/k  move · g/G  ends
    space  done · e  edit · d  remove · u  undo

    q  quit · from the zoomed pane it goes back to the desk first

  With a room on screen the block keys act on the room, and the todo list
  is the room's — yours to work, everyone else's to read.

  Everything else is a command. `+"`junkie help <command>`"+` explains one.

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
