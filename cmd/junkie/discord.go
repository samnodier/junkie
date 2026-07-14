package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// discordBot runs one Discord gateway connection for the whole junkie
// instance. A guild registers a single junkie room (see discord_guilds in
// migrations/010_discord.sql); every guild that invites the bot shares this
// same connection and command set.
type discordBot struct {
	app     *app
	session *discordgo.Session
	appID   string

	// scheduled dedupes the phase-end wakeups (scheduleNext) so overlapping
	// notify calls don't stack timers for the same room+phase.
	mu        sync.Mutex
	scheduled map[string]string
}

const discordJoinButtonID = "junkie:join"

var discordCommands = []*discordgo.ApplicationCommand{
	{
		Name:        "junkie",
		Description: "Run a junkie focus room from Discord",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "register",
				Description: "Register this server's junkie room, posting to this channel",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "code",
						Description: "Code of an existing room to connect; omit to create a new room",
						Required:    false,
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "deregister",
				Description: "Disconnect this server from its junkie room",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "config",
				Description: "Set the timer: focus-minutes/break-minutes/sessions, e.g. 30/5/3",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "shorthand",
						Description: "focus-minutes/break-minutes/sessions, e.g. 30/5/3",
						Required:    true,
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "start",
				Description: "Start a focus run in this server's room",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "join",
				Description: "Join the active run",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "leave",
				Description: "Leave the active run",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "status",
				Description: "See where the room's timer is right now",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "stats",
				Description: "Your focus stats: today, this week, total, and streak",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "link",
				Description: "Link your Discord account to your junkie account",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "help",
				Description: "List junkie commands",
			},
		},
	},
}

// newDiscordBot opens the gateway connection and registers slash commands
// when DISCORD_BOT_TOKEN is set. It returns (nil, nil) when the bot isn't
// configured, so main can skip it without treating that as a startup error.
func newDiscordBot(a *app) (*discordBot, error) {
	token := os.Getenv("DISCORD_BOT_TOKEN")
	if token == "" {
		return nil, nil
	}
	appID := os.Getenv("DISCORD_APPLICATION_ID")
	if appID == "" {
		return nil, fmt.Errorf("DISCORD_APPLICATION_ID must be set alongside DISCORD_BOT_TOKEN")
	}

	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("create discord session: %w", err)
	}
	bot := &discordBot{app: a, session: session, appID: appID, scheduled: map[string]string{}}
	session.AddHandler(bot.onInteraction)

	if err := session.Open(); err != nil {
		return nil, fmt.Errorf("open discord gateway: %w", err)
	}
	// Registered globally (guildID "") so the command shows up in any server
	// the bot is invited to without a per-guild registration step. Discord
	// can take up to ~1 hour to propagate a *new* global command to clients;
	// updates to an already-registered command apply immediately.
	for _, cmd := range discordCommands {
		if _, err := session.ApplicationCommandCreate(appID, "", cmd); err != nil {
			log.Printf("discord: register command %s: %v", cmd.Name, err)
		}
	}
	return bot, nil
}

func (b *discordBot) Close() error {
	return b.session.Close()
}

func (b *discordBot) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		b.handleCommand(s, i)
	case discordgo.InteractionMessageComponent:
		if i.MessageComponentData().CustomID == discordJoinButtonID {
			b.handleJoin(s, i)
		}
	}
}

func (b *discordBot) handleCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	if len(data.Options) == 0 {
		return
	}
	sub := data.Options[0]
	switch sub.Name {
	case "register":
		code := ""
		if len(sub.Options) > 0 {
			code, _ = sub.Options[0].Value.(string)
		}
		b.handleRegister(s, i, code)
	case "deregister":
		b.handleDeregister(s, i)
	case "config":
		shorthand := ""
		if len(sub.Options) > 0 {
			shorthand, _ = sub.Options[0].Value.(string)
		}
		b.handleConfig(s, i, shorthand)
	case "start":
		b.handleStart(s, i)
	case "join":
		b.handleJoin(s, i)
	case "leave":
		b.handleLeave(s, i)
	case "status":
		b.handleStatus(s, i)
	case "stats":
		b.handleStats(s, i)
	case "link":
		b.handleLink(s, i)
	case "help":
		b.handleHelp(s, i)
	}
}

// interactionUserID returns the invoking Discord user's id, whether the
// interaction came from a guild channel (Member set) or a DM (User set).
func interactionUserID(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

func discordTimestamp(t time.Time) string {
	return fmt.Sprintf("<t:%d:R>", t.Unix())
}

func joinComponents() []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "Join", Style: discordgo.PrimaryButton, CustomID: discordJoinButtonID},
		}},
	}
}

func (b *discordBot) reply(s *discordgo.Session, i *discordgo.InteractionCreate, content string, components []discordgo.MessageComponent) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: content, Components: components},
	})
	if err != nil {
		log.Printf("discord: reply: %v", err)
	}
}

func (b *discordBot) ephemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: content, Flags: discordgo.MessageFlagsEphemeral},
	})
	if err != nil {
		log.Printf("discord: ephemeral reply: %v", err)
	}
}

func (b *discordBot) replyNotRegistered(s *discordgo.Session, i *discordgo.InteractionCreate) {
	b.ephemeral(s, i, "This server doesn't have a junkie room yet. An account-linked member can run `/junkie register` in the channel you want it to post in.")
}

// replyLinkRequired asks the caller to link before continuing, since every
// junkie room/timer table row is keyed to a real users.id and this codebase
// has no guest-user concept.
func (b *discordBot) replyLinkRequired(s *discordgo.Session, i *discordgo.InteractionCreate) {
	b.ephemeral(s, i, "Link your Discord account to junkie first — run `/junkie link` and open the link it gives you.")
}

// discordLinkForUser reports the Discord username linked to a junkie
// account, for the profile page's connected indicator.
func (a *app) discordLinkForUser(ctx context.Context, userID string) (string, bool) {
	var username string
	err := a.db.QueryRow(ctx, `SELECT discord_username FROM discord_links WHERE user_id = $1`, userID).Scan(&username)
	return username, err == nil
}

// discordUnlink removes the viewer's Discord link; the bot treats them as
// unlinked from the next interaction on.
func (a *app) discordUnlink(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	_, _ = a.db.Exec(r.Context(), `DELETE FROM discord_links WHERE user_id = $1`, u.ID)
	http.Redirect(w, r, "/profile?notice="+url.QueryEscape("Discord account disconnected."), http.StatusSeeOther)
}

// discordLinkedUser resolves a Discord user id to the junkie account it's
// linked to, if any.
func (a *app) discordLinkedUser(ctx context.Context, discordUserID string) (user, bool) {
	var u user
	err := a.db.QueryRow(ctx, `
		SELECT users.id, users.username, users.display_name, users.role
		FROM discord_links
		JOIN users ON users.id = discord_links.user_id
		WHERE discord_links.discord_user_id = $1`, discordUserID).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role)
	return u, err == nil
}

// discordRoom resolves a guild id to its registered room and the channel the
// bot should post announcements to.
func (a *app) discordRoom(ctx context.Context, guildID string) (room, string, bool) {
	var rm room
	var channelID string
	err := a.db.QueryRow(ctx, `
		SELECT rooms.id, rooms.code, rooms.name, rooms.creator_id, rooms.focus_minutes, rooms.break_minutes, rooms.auto_sessions, rooms.auto_roll, discord_guilds.channel_id
		FROM discord_guilds
		JOIN rooms ON rooms.id = discord_guilds.room_id
		WHERE discord_guilds.guild_id = $1`, guildID).Scan(
		&rm.ID, &rm.Code, &rm.Name, &rm.CreatorID, &rm.FocusMinutes, &rm.BreakMinutes, &rm.AutoSessions, &rm.AutoRoll, &channelID)
	return rm, channelID, err == nil
}

// discordStatusContent renders the one live status message for a room's
// current timer state. Discord's <t:...:R> timestamps tick down client-side,
// so the message only needs an edit per phase change, not per second.
func discordStatusContent(rm room, timer *timerRun) string {
	switch {
	case timer == nil:
		return fmt.Sprintf("**%s** — run complete. Nice work! `/junkie start` when you're ready for another.", rm.Name)
	case timer.Phase == "lobby":
		return fmt.Sprintf("**%s** — focus run starting %s. Tap Join to be in from the first session!", rm.Name, discordTimestamp(timer.PhaseEndsAt))
	case timer.Phase == "focus":
		return fmt.Sprintf("**%s** — focus · session %d of %d. Break %s. Tap Join to hop in at the break.", rm.Name, timer.CurrentSession, timer.TotalSessions, discordTimestamp(timer.PhaseEndsAt))
	case timer.BreakPending():
		return fmt.Sprintf("**%s** — break ready, waiting for someone to start it. Tap Join to be in the next session.", rm.Name)
	case timer.PausedAt != nil:
		return fmt.Sprintf("**%s** — break paused. Tap Join to be in the next session.", rm.Name)
	default:
		return fmt.Sprintf("**%s** — break · focus resumes %s. Tap Join to be in the next session!", rm.Name, discordTimestamp(timer.PhaseEndsAt))
	}
}

// notifyDiscord keeps the room's live status message in the linked channel
// current: a lobby posts a fresh message (one per run), every later phase
// change edits it in place, and timer == nil marks the run complete. It also
// arms the phase-end wakeup so Discord-only rooms advance without a web
// viewer polling. No-op when the bot isn't running or the room isn't linked.
func (a *app) notifyDiscord(rm room, timer *timerRun) {
	if a.discord == nil {
		return
	}
	ctx := context.Background()
	var channelID, messageID string
	if err := a.db.QueryRow(ctx, `SELECT channel_id, live_message_id FROM discord_guilds WHERE room_id = $1`, rm.ID).Scan(&channelID, &messageID); err != nil {
		return
	}
	content := discordStatusContent(rm, timer)
	components := joinComponents()
	if timer == nil {
		components = nil
	}
	fresh := timer != nil && timer.Phase == "lobby"
	if !fresh && messageID != "" {
		edit := &discordgo.MessageEdit{Channel: channelID, ID: messageID, Content: &content, Components: &components}
		if _, err := a.discord.session.ChannelMessageEditComplex(edit); err == nil {
			a.discord.scheduleNext(rm, timer)
			return
		}
		// The tracked message was deleted or is unreachable; fall through and
		// post a replacement.
	}
	msg, err := a.discord.session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{Content: content, Components: components})
	if err != nil {
		log.Printf("discord: notify %s: %v", rm.Code, err)
		return
	}
	_, _ = a.db.Exec(ctx, `UPDATE discord_guilds SET live_message_id = $1 WHERE room_id = $2`, msg.ID, rm.ID)
	a.discord.scheduleNext(rm, timer)
}

// scheduleNext arms a wakeup just past the timer's phase end that advances
// the state machine and re-notifies, so a room whose members are all on
// Discord still transitions on time. normalizeTimer stays the single source
// of truth; this only pokes it. Deduped per room+run+phase so overlapping
// notify calls (web viewers polling plus this chain) don't stack timers.
func (b *discordBot) scheduleNext(rm room, timer *timerRun) {
	if timer == nil || timer.PausedAt != nil {
		return
	}
	key := timer.ID + ":" + timer.Phase + ":" + timer.PhaseEndsAt.UTC().Format(time.RFC3339Nano)
	b.mu.Lock()
	if b.scheduled[rm.ID] == key {
		b.mu.Unlock()
		return
	}
	b.scheduled[rm.ID] = key
	b.mu.Unlock()
	delay := max(time.Until(timer.PhaseEndsAt), 0) + time.Second
	time.AfterFunc(delay, func() {
		b.mu.Lock()
		delete(b.scheduled, rm.ID)
		b.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// The user id only shapes the Participant flag, which nothing in the
		// broadcast path reads; the creator is a stable stand-in.
		next, transitioned, err := b.app.normalizeTimer(ctx, rm.ID, rm.CreatorID)
		if err != nil {
			log.Printf("discord: phase wakeup %s: %v", rm.Code, err)
			return
		}
		if transitioned {
			b.app.broadcastTimerPhase(rm, next)
		} else if next != nil {
			// Deadline moved (pause, break-length change) — track the new one.
			b.app.notifyDiscord(rm, next)
		}
	})
}

// handleRegister connects the guild to a room: an existing one when a room
// code is given (the caller must be — or becomes — a member, same as the web
// join-by-code flow), or a newly created one when the code is omitted. The
// discord_guilds constraints keep both directions 1:1 — a second guild
// linking the same room, or the same guild linking twice, fails cleanly.
func (b *discordBot) handleRegister(s *discordgo.Session, i *discordgo.InteractionCreate, roomCode string) {
	a := b.app
	ctx := context.Background()
	u, linked := a.discordLinkedUser(ctx, interactionUserID(i))
	if !linked {
		b.replyLinkRequired(s, i)
		return
	}
	if _, _, ok := a.discordRoom(ctx, i.GuildID); ok {
		b.ephemeral(s, i, "This server already has a junkie room. `/junkie deregister` first to connect a different one.")
		return
	}

	var roomID, code string
	if roomCode = normalizeRoomCode(roomCode); roomCode != "" {
		rm, ok := a.findRoom(ctx, roomCode)
		if !ok {
			b.ephemeral(s, i, "No room found with that code.")
			return
		}
		roomID, code = rm.ID, rm.Code
	} else {
		name := "Discord room"
		if g, err := s.Guild(i.GuildID); err == nil && g.Name != "" {
			name = g.Name + " focus room"
		}
		var err error
		for range 5 {
			code = randomCode()
			err = a.db.QueryRow(ctx, `INSERT INTO rooms (code, name, creator_id) VALUES ($1, $2, $3) RETURNING id`, code, name, u.ID).Scan(&roomID)
			if err == nil || !isUniqueViolation(err) {
				break
			}
		}
		if err != nil {
			log.Printf("discord: register room: %v", err)
			b.ephemeral(s, i, "Couldn't create a room for this server — try again.")
			return
		}
	}
	a.addRoomMember(ctx, roomID, u.ID)
	if _, err := a.db.Exec(ctx, `INSERT INTO discord_guilds (guild_id, room_id, channel_id, linked_by) VALUES ($1, $2, $3, $4)`,
		i.GuildID, roomID, i.ChannelID, u.ID); err != nil {
		if isUniqueViolation(err) {
			b.ephemeral(s, i, "That room is already connected to another Discord server.")
			return
		}
		log.Printf("discord: register guild: %v", err)
		b.ephemeral(s, i, "Couldn't link this channel — try again.")
		return
	}
	b.reply(s, i, fmt.Sprintf("Registered! This channel now runs room `%s`. Configure it with `/junkie config focus/break/sessions`, e.g. `/junkie config 30/5/3`.", code), nil)
}

// handleDeregister disconnects the guild from its room. The room itself and
// its todos/history survive on the web — only the Discord linkage row goes,
// so registering again later starts a fresh room. Allowed for the member who
// registered it or anyone with Manage Server permission.
func (b *discordBot) handleDeregister(s *discordgo.Session, i *discordgo.InteractionCreate) {
	a := b.app
	ctx := context.Background()
	rm, _, ok := a.discordRoom(ctx, i.GuildID)
	if !ok {
		b.replyNotRegistered(s, i)
		return
	}
	canManage := i.Member != nil && i.Member.Permissions&discordgo.PermissionManageGuild != 0
	if !canManage {
		var registeredBy string
		_ = a.db.QueryRow(ctx, `SELECT COALESCE(linked_by::text, '') FROM discord_guilds WHERE guild_id = $1`, i.GuildID).Scan(&registeredBy)
		u, linked := a.discordLinkedUser(ctx, interactionUserID(i))
		if !linked || u.ID != registeredBy {
			b.ephemeral(s, i, "Only the member who registered this room or someone with Manage Server permission can deregister it.")
			return
		}
	}
	if _, err := a.db.Exec(ctx, `DELETE FROM discord_guilds WHERE guild_id = $1`, i.GuildID); err != nil {
		log.Printf("discord: deregister guild: %v", err)
		b.ephemeral(s, i, "Couldn't deregister — try again.")
		return
	}
	b.reply(s, i, fmt.Sprintf("Deregistered. Room `%s` still exists on the web, but this server is no longer connected to it. Run `/junkie register` to connect a new room.", rm.Code), nil)
}

func (b *discordBot) handleConfig(s *discordgo.Session, i *discordgo.InteractionCreate, shorthand string) {
	a := b.app
	ctx := context.Background()
	rm, _, ok := a.discordRoom(ctx, i.GuildID)
	if !ok {
		b.replyNotRegistered(s, i)
		return
	}
	parts := strings.Split(strings.TrimSpace(shorthand), "/")
	if len(parts) != 3 {
		b.ephemeral(s, i, "Use the form `focus-minutes/break-minutes/sessions`, e.g. `30/5/3`.")
		return
	}
	focus := clampInt(parts[0], 5, 180, rm.FocusMinutes)
	breaks := clampInt(parts[1], 1, 60, rm.BreakMinutes)
	sessions := clampInt(parts[2], 1, 12, rm.AutoSessions)
	if err := a.applyRoomSettings(ctx, rm.ID, focus, breaks, sessions, rm.AutoRoll); err != nil {
		log.Printf("discord: config room %s: %v", rm.Code, err)
		b.ephemeral(s, i, "Couldn't save that configuration — try again.")
		return
	}
	b.reply(s, i, fmt.Sprintf("Configured **%s**: %d min focus, %d min break, %d session(s).", rm.Name, focus, breaks, sessions), nil)
}

func (b *discordBot) handleStart(s *discordgo.Session, i *discordgo.InteractionCreate) {
	a := b.app
	ctx := context.Background()
	u, linked := a.discordLinkedUser(ctx, interactionUserID(i))
	if !linked {
		b.replyLinkRequired(s, i)
		return
	}
	rm, _, ok := a.discordRoom(ctx, i.GuildID)
	if !ok {
		b.replyNotRegistered(s, i)
		return
	}
	if !a.isRoomMember(ctx, rm.ID, u.ID) {
		a.addRoomMember(ctx, rm.ID, u.ID)
	}
	_, created, err := a.startRoomTimerAndSchedule(ctx, rm, u.ID, rm.FocusMinutes, u.DisplayName)
	if err != nil {
		log.Printf("discord: start timer %s: %v", rm.Code, err)
		b.ephemeral(s, i, "Couldn't start the timer — try again.")
		return
	}
	if !created {
		b.ephemeral(s, i, "A run is already active in this room.")
		return
	}
	// The shared start path posts the live countdown message to the channel;
	// this reply just closes the interaction for the starter.
	b.ephemeral(s, i, "Run started — countdown posted below. You're in.")
}

func (b *discordBot) handleJoin(s *discordgo.Session, i *discordgo.InteractionCreate) {
	a := b.app
	ctx := context.Background()
	u, linked := a.discordLinkedUser(ctx, interactionUserID(i))
	if !linked {
		b.replyLinkRequired(s, i)
		return
	}
	rm, _, ok := a.discordRoom(ctx, i.GuildID)
	if !ok {
		b.replyNotRegistered(s, i)
		return
	}
	if !a.isRoomMember(ctx, rm.ID, u.ID) {
		a.addRoomMember(ctx, rm.ID, u.ID)
	}
	_, outcome, err := a.joinTimer(ctx, rm, u.ID)
	if err != nil {
		log.Printf("discord: join timer %s: %v", rm.Code, err)
		b.ephemeral(s, i, "Couldn't join the run — try again.")
		return
	}
	a.hub.broadcast(rm.Code, "timer-phase")
	switch outcome {
	case joinedQueuedStart:
		b.ephemeral(s, i, "You're in — you'll join automatically when the next run starts. `/junkie leave` cancels.")
	case joinedQueuedBreak:
		b.ephemeral(s, i, "A focus session is in progress — you'll join automatically when the break starts. `/junkie leave` cancels.")
	case joinedAlready:
		b.ephemeral(s, i, "You're already in this run.")
	default:
		b.ephemeral(s, i, "You're in for this run.")
	}
}

func (b *discordBot) handleLeave(s *discordgo.Session, i *discordgo.InteractionCreate) {
	a := b.app
	ctx := context.Background()
	u, linked := a.discordLinkedUser(ctx, interactionUserID(i))
	if !linked {
		b.replyLinkRequired(s, i)
		return
	}
	rm, _, ok := a.discordRoom(ctx, i.GuildID)
	if !ok {
		b.replyNotRegistered(s, i)
		return
	}
	ended, err := a.leaveTimer(ctx, rm, u.ID)
	if err != nil {
		log.Printf("discord: leave timer %s: %v", rm.Code, err)
		b.ephemeral(s, i, "Couldn't leave the run — try again.")
		return
	}
	if ended {
		a.hub.broadcast(rm.Code, "timer-end")
		a.notifyDiscord(rm, nil)
	} else {
		a.hub.broadcast(rm.Code, "timer-leave")
	}
	b.ephemeral(s, i, "You've left the run.")
}

// handleStatus privately shows where the room's timer is right now — focus
// with time remaining, break with a Join button, or idle — for someone who
// walked in late. Linked accounts only; the nudge to link is ephemeral so
// unlinked users cause no channel noise.
func (b *discordBot) handleStatus(s *discordgo.Session, i *discordgo.InteractionCreate) {
	a := b.app
	ctx := context.Background()
	u, linked := a.discordLinkedUser(ctx, interactionUserID(i))
	if !linked {
		b.replyLinkRequired(s, i)
		return
	}
	rm, _, ok := a.discordRoom(ctx, i.GuildID)
	if !ok {
		b.replyNotRegistered(s, i)
		return
	}
	timer, transitioned, err := a.normalizeTimer(ctx, rm.ID, u.ID)
	if err != nil {
		log.Printf("discord: status %s: %v", rm.Code, err)
		b.ephemeral(s, i, "Couldn't read the timer — try again.")
		return
	}
	if transitioned {
		a.broadcastTimerPhase(rm, timer)
	}
	content := discordStatusContent(rm, timer)
	if timer == nil {
		content = fmt.Sprintf("**%s** — no run active. `/junkie start` to begin, or tap Join to be in automatically when someone starts.", rm.Name)
	} else if timer.Participant {
		content += "\nYou're in this run."
	}
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: content, Flags: discordgo.MessageFlagsEphemeral, Components: joinComponents()},
	})
	if err != nil {
		log.Printf("discord: status reply: %v", err)
	}
}

// formatFocusMinutes renders a minute count the way people say it: "45 min"
// under an hour, "3h 20m" above.
func formatFocusMinutes(minutes int) string {
	if minutes < 60 {
		return fmt.Sprintf("%d min", minutes)
	}
	if minutes%60 == 0 {
		return fmt.Sprintf("%dh", minutes/60)
	}
	return fmt.Sprintf("%dh %dm", minutes/60, minutes%60)
}

// handleStats replies (privately) with the caller's focus numbers from the
// same activity table the web profile reads: today, the trailing 7 days, the
// all-time total, the current daily streak, and rooms joined.
func (b *discordBot) handleStats(s *discordgo.Session, i *discordgo.InteractionCreate) {
	a := b.app
	ctx := context.Background()
	u, linked := a.discordLinkedUser(ctx, interactionUserID(i))
	if !linked {
		b.replyLinkRequired(s, i)
		return
	}
	var today, week, total, activeDays int
	_ = a.db.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(focus_minutes) FILTER (WHERE activity_date = CURRENT_DATE), 0),
			COALESCE(SUM(focus_minutes) FILTER (WHERE activity_date > CURRENT_DATE - 7), 0),
			COALESCE(SUM(focus_minutes), 0),
			COUNT(*) FILTER (WHERE focus_minutes > 0)
		FROM activity WHERE user_id = $1`, u.ID).Scan(&today, &week, &total, &activeDays)
	var rooms int
	_ = a.db.QueryRow(ctx, `SELECT COUNT(*) FROM room_members WHERE user_id = $1`, u.ID).Scan(&rooms)

	// Streak: consecutive days with focus, counting back from today — or
	// from yesterday, so a streak isn't "broken" before today's first block.
	streak := 0
	if rows, err := a.db.Query(ctx, `
		SELECT activity_date FROM activity
		WHERE user_id = $1 AND focus_minutes > 0 AND activity_date > CURRENT_DATE - 366
		ORDER BY activity_date DESC`, u.ID); err == nil {
		expect := time.Now()
		first := true
		for rows.Next() {
			var day time.Time
			if rows.Scan(&day) != nil {
				break
			}
			if first && day.Format("2006-01-02") != expect.Format("2006-01-02") {
				expect = expect.AddDate(0, 0, -1)
			}
			first = false
			if day.Format("2006-01-02") != expect.Format("2006-01-02") {
				break
			}
			streak++
			expect = expect.AddDate(0, 0, -1)
		}
		rows.Close()
	}

	reply := fmt.Sprintf("**%s** — focus stats\nToday: %s · Last 7 days: %s\nAll time: %s across %d day(s)",
		u.DisplayName, formatFocusMinutes(today), formatFocusMinutes(week), formatFocusMinutes(total), activeDays)
	if streak > 0 {
		reply += fmt.Sprintf("\nStreak: %d day(s)", streak)
	}
	reply += fmt.Sprintf("\nRooms joined: %d", rooms)
	b.ephemeral(s, i, reply)
}

// handleLink mints a single-use link token (mirroring createConnectLink,
// main.go) and DMs the URL that completes the link once the recipient signs
// in on the web.
func (b *discordBot) handleLink(s *discordgo.Session, i *discordgo.InteractionCreate) {
	a := b.app
	discordUserID := interactionUserID(i)
	if !a.limiter.allow("discordlink:"+discordUserID, 10, time.Hour) {
		b.ephemeral(s, i, "Too many link attempts — try again later.")
		return
	}
	discordUsername := ""
	if i.Member != nil && i.Member.User != nil {
		discordUsername = i.Member.User.Username
	} else if i.User != nil {
		discordUsername = i.User.Username
	}
	token := randomHex(32)
	expires := time.Now().Add(24 * time.Hour)
	if _, err := a.db.Exec(context.Background(), `
		INSERT INTO discord_link_tokens (token_hash, discord_user_id, discord_username, expires_at) VALUES ($1, $2, $3, $4)`,
		hashToken(token), discordUserID, discordUsername, expires); err != nil {
		log.Printf("discord: create link token: %v", err)
		b.ephemeral(s, i, "Couldn't create a link right now — try again.")
		return
	}
	linkURL := fmt.Sprintf("%s/discord/link/%s", strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/"), url.PathEscape(token))
	b.ephemeral(s, i, "Open this link (valid 24h) signed in to your junkie account to connect it: "+linkURL)
}

// discordLinkConfirm serves GET /discord/link/{token}: the signed-in visitor
// redeems the single-use token minted by /junkie link, tying their junkie
// account to the Discord user the token was issued for. Mirrors
// consumeConnectToken's delete-first redemption so a token can't be replayed.
func (a *app) discordLinkConfirm(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	var discordUserID, discordUsername string
	err := a.db.QueryRow(r.Context(), `
		DELETE FROM discord_link_tokens
		WHERE token_hash = $1 AND expires_at > now()
		RETURNING discord_user_id, discord_username`, hashToken(r.PathValue("token"))).Scan(&discordUserID, &discordUsername)
	if err != nil {
		http.Redirect(w, r, "/profile?error="+url.QueryEscape("That Discord link is invalid or has expired — run /junkie link again."), http.StatusSeeOther)
		return
	}
	// One row per Discord user and per junkie account (both uniquely keyed):
	// re-linking either side replaces the old pairing.
	if _, err := a.db.Exec(r.Context(), `
		INSERT INTO discord_links (discord_user_id, user_id, discord_username) VALUES ($1, $2, $3)
		ON CONFLICT (discord_user_id) DO UPDATE SET user_id = EXCLUDED.user_id, discord_username = EXCLUDED.discord_username, linked_at = now()`,
		discordUserID, u.ID, discordUsername); err != nil {
		if isUniqueViolation(err) {
			http.Redirect(w, r, "/profile?error="+url.QueryEscape("This junkie account is already linked to a different Discord account."), http.StatusSeeOther)
			return
		}
		log.Printf("discord: confirm link: %v", err)
		http.Redirect(w, r, "/profile?error="+url.QueryEscape("Could not complete the Discord link — try again."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/profile?notice="+url.QueryEscape("Discord account linked! You can now use /junkie commands."), http.StatusSeeOther)
}

func (b *discordBot) handleHelp(s *discordgo.Session, i *discordgo.InteractionCreate) {
	b.ephemeral(s, i, "**junkie commands**\n"+
		"`/junkie link` — connect your Discord account to your junkie account\n"+
		"`/junkie register [code]` — connect an existing room by code, or create a new one (posts to this channel)\n"+
		"`/junkie deregister` — disconnect this server from its room\n"+
		"`/junkie config focus/break/sessions` — set the timer, e.g. `30/5/3`\n"+
		"`/junkie start` — start a focus run\n"+
		"`/junkie join` — join now, or be queued in for the next break/run\n"+
		"`/junkie leave` — leave the run (or cancel a queued join)\n"+
		"`/junkie status` — where the timer is right now\n"+
		"`/junkie stats` — your focus stats")
}
