package main

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Rooms were left out of the first cut, which made the terminal a private-
// focus tool: you could see that a room was live but not start one, join it,
// or leave. These are the room actions the web page has, as subcommands.
//
// Every one of them posts to the same /r/{code}/{action} endpoints the web
// forms do, and the server applies the same rules — membership, the
// per-user cap on live rooms, and settings that refuse to change mid-run.

func cmdRoom(args []string) error {
	if len(args) == 0 {
		return errors.New(roomUsage)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "new", "create":
		return cmdRoomNew(rest)
	case "join":
		return cmdRoomJoin(rest)
	case "start":
		return cmdRoomStart(rest)
	case "enter":
		return cmdRoomEnter(rest)
	case "leave":
		return cmdRoomAction(rest, "timer-leave", "left the block in %s")
	case "checkin":
		return cmdRoomAction(rest, "timer-checkin", "checked in for the next block in %s")
	case "skip":
		return cmdRoomAction(rest, "timer-skip-break", "skipped the break in %s")
	default:
		return errors.New(roomUsage)
	}
}

const roomUsage = `usage: junkie room <command>

  junkie room new NAME          create a room
  junkie room join CODE         join a room by its code
  junkie room start [CODE] [M]  start a block (opens a 30-second lobby)
  junkie room enter [CODE]      join the block that's running
  junkie room leave [CODE]      leave the block
  junkie room checkin [CODE]    confirm you're staying for the next block
  junkie room skip [CODE]       skip the break and start the next block

CODE may be omitted when you're in exactly one room.`

func cmdRoomNew(args []string) error {
	name := strings.TrimSpace(strings.Join(args, " "))
	if name == "" {
		return errors.New("usage: junkie room new NAME")
	}
	c, _, err := authed()
	if err != nil {
		return err
	}
	before, err := c.desk()
	if err != nil {
		return err
	}
	if err := c.post("/rooms", url.Values{"name": {name}}); err != nil {
		return err
	}
	// The create endpoint redirects to the new room rather than answering
	// with its code, so the code is found by diffing the room list — which
	// is also how we confirm it was really created.
	after, err := c.desk()
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, room := range before.Rooms {
		known[room.Code] = true
	}
	for _, room := range after.Rooms {
		if !known[room.Code] {
			fmt.Printf("Created %s · code %s\n", room.Name, room.Code)
			fmt.Println(styleFaint.Render("Share the code, or `junkie room start` to open a block."))
			return nil
		}
	}
	return errors.New("the room was not created — try `junkie rooms`")
}

func cmdRoomJoin(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: junkie room join CODE")
	}
	code := normalizeCode(args[0])
	c, _, err := authed()
	if err != nil {
		return err
	}
	if err := c.post("/rooms/join", url.Values{"code": {code}}); err != nil {
		return err
	}
	// Joining a code that does not exist redirects without complaint, so
	// membership is confirmed rather than assumed.
	desk, err := c.desk()
	if err != nil {
		return err
	}
	for _, room := range desk.Rooms {
		if room.Code == code {
			fmt.Printf("Joined %s · %s\n", room.Name, room.Code)
			return nil
		}
	}
	return fmt.Errorf("no room joined — is %s the right code?", code)
}

func cmdRoomStart(args []string) error {
	var minutes int
	// A trailing number is the block length; anything else is the code.
	if len(args) > 0 {
		if n, err := strconv.Atoi(args[len(args)-1]); err == nil {
			minutes = n
			args = args[:len(args)-1]
		}
	}
	if minutes != 0 && (minutes < minFocusMinutes || minutes > maxFocusMinutes) {
		return fmt.Errorf("a focus block is %d–%d minutes", minFocusMinutes, maxFocusMinutes)
	}
	c, _, err := authed()
	if err != nil {
		return err
	}
	room, err := resolveRoom(c, args)
	if err != nil {
		return err
	}
	if room.Timer != nil {
		return fmt.Errorf("%s already has a block running (%s)", room.Code, room.Timer.Phase)
	}
	form := url.Values{}
	if minutes != 0 {
		form.Set("focus_minutes", strconv.Itoa(minutes))
	}
	if err := c.post("/r/"+room.Code+"/timer-start", form); err != nil {
		return err
	}
	fmt.Printf("Started a block in %s.\n", room.Name)
	fmt.Println(styleFaint.Render("The room has 30 seconds to join before focus begins."))
	return nil
}

// cmdRoomEnter joins the block, and says which of the three things actually
// happened. The server does not join you to a focus block already under way
// — joinTimer queues you instead, and you are in at the next break — so
// reporting "joined" for every accepted request would be a lie two thirds of
// the time.
func cmdRoomEnter(args []string) error {
	c, _, err := authed()
	if err != nil {
		return err
	}
	room, err := resolveRoom(c, args)
	if err != nil {
		return err
	}
	if err := c.post("/r/"+room.Code+"/timer-join", nil); err != nil {
		return err
	}
	state, err := c.room(room.Code)
	if err != nil {
		return err
	}
	switch {
	case state.Timer != nil && state.Timer.Participant:
		fmt.Printf("You're in the block in %s · %s left\n",
			room.Name, formatDuration(state.Timer.SecondsLeft))
	case state.Waiting && state.Timer != nil:
		fmt.Printf("%s is mid-block. You're in the queue — you'll join at the next break.\n", room.Name)
	case state.Waiting:
		fmt.Printf("Waiting in %s. You'll be in when someone starts a block.\n", room.Name)
	default:
		fmt.Printf("Nothing to join in %s yet — `junkie room start` opens one.\n", room.Name)
	}
	return nil
}

func cmdRoomAction(args []string, action, success string) error {
	c, _, err := authed()
	if err != nil {
		return err
	}
	room, err := resolveRoom(c, args)
	if err != nil {
		return err
	}
	if err := c.post("/r/"+room.Code+"/"+action, nil); err != nil {
		return err
	}
	fmt.Printf(success+"\n", room.Name)
	return nil
}

// resolveRoom finds the room a command means. With no code given it will
// only guess when there is nothing to guess between — naming the choices
// beats picking one and being wrong about which room you just started.
func resolveRoom(c *client, args []string) (deskRoom, error) {
	if len(args) > 1 {
		return deskRoom{}, errors.New("give one room code")
	}
	desk, err := c.desk()
	if err != nil {
		return deskRoom{}, err
	}
	if len(desk.Rooms) == 0 {
		return deskRoom{}, errors.New("you're not in any rooms — `junkie room new NAME` or `junkie room join CODE`")
	}
	if len(args) == 0 {
		if len(desk.Rooms) == 1 {
			return desk.Rooms[0], nil
		}
		var codes []string
		for _, room := range desk.Rooms {
			codes = append(codes, room.Code)
		}
		return deskRoom{}, fmt.Errorf("which room? %s", strings.Join(codes, ", "))
	}
	code := normalizeCode(args[0])
	for _, room := range desk.Rooms {
		if room.Code == code {
			return room, nil
		}
	}
	return deskRoom{}, fmt.Errorf("you're not in %s — `junkie room join %s` first", code, code)
}

// normalizeCode matches the server's own normalizeRoomCode: codes are
// uppercase, and pasting a room's URL should work as well as typing its code.
func normalizeCode(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "/r/"); i >= 0 {
		raw = raw[i+3:]
	}
	if i := strings.IndexAny(raw, "/?#"); i >= 0 {
		raw = raw[:i]
	}
	return strings.ToUpper(raw)
}
