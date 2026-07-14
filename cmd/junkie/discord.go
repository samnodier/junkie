package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
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
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "config",
				Description: "Set the timer: sessions/focus-minutes/break-minutes, e.g. 3/30/5",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "shorthand",
						Description: "sessions/focus-minutes/break-minutes, e.g. 3/30/5",
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
	bot := &discordBot{app: a, session: session, appID: appID}
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
		b.handleRegister(s, i)
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

// discordChannelForRoom is the reverse lookup used by notifyDiscord, which
// only has the web-side room, not the guild id.
func (a *app) discordChannelForRoom(ctx context.Context, roomID string) (string, bool) {
	var channelID string
	err := a.db.QueryRow(ctx, `SELECT channel_id FROM discord_guilds WHERE room_id = $1`, roomID).Scan(&channelID)
	return channelID, err == nil
}

// notifyDiscord posts a Join-able announcement to a room's linked Discord
// channel when its lobby countdown or break starts from the *web* side. It's
// a no-op when the bot isn't running or the room has no linked guild.
// Discord-triggered starts skip this and reply directly instead (see
// handleStart), so a single start doesn't double-post.
func (a *app) notifyDiscord(rm room, event string, timer *timerRun) {
	if a.discord == nil || timer == nil {
		return
	}
	channelID, ok := a.discordChannelForRoom(context.Background(), rm.ID)
	if !ok {
		return
	}
	var content string
	switch event {
	case "lobby":
		content = fmt.Sprintf("Focus run starting %s in **%s** — tap Join before it begins!", discordTimestamp(timer.PhaseEndsAt), rm.Name)
	case "break":
		content = fmt.Sprintf("Break started in **%s** — back to focus %s. Tap Join if you weren't in the last block.", rm.Name, discordTimestamp(timer.PhaseEndsAt))
	default:
		return
	}
	_, err := a.discord.session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{Content: content, Components: joinComponents()})
	if err != nil {
		log.Printf("discord: notify %s: %v", rm.Code, err)
	}
}

func (b *discordBot) handleRegister(s *discordgo.Session, i *discordgo.InteractionCreate) {
	a := b.app
	ctx := context.Background()
	u, linked := a.discordLinkedUser(ctx, interactionUserID(i))
	if !linked {
		b.replyLinkRequired(s, i)
		return
	}
	if _, _, ok := a.discordRoom(ctx, i.GuildID); ok {
		b.ephemeral(s, i, "This server already has a junkie room. Use `/junkie config` to change its timer settings.")
		return
	}

	name := "Discord room"
	if g, err := s.Guild(i.GuildID); err == nil && g.Name != "" {
		name = g.Name + " focus room"
	}
	var roomID, code string
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
	a.addRoomMember(ctx, roomID, u.ID)
	if _, err := a.db.Exec(ctx, `INSERT INTO discord_guilds (guild_id, room_id, channel_id, linked_by) VALUES ($1, $2, $3, $4)`,
		i.GuildID, roomID, i.ChannelID, u.ID); err != nil {
		log.Printf("discord: register guild: %v", err)
		b.ephemeral(s, i, "Room created but couldn't link this channel — try again.")
		return
	}
	b.reply(s, i, fmt.Sprintf("Registered! This channel now runs room `%s`. Configure it with `/junkie config sessions/focus/break`, e.g. `/junkie config 3/30/5`.", code), nil)
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
		b.ephemeral(s, i, "Use the form `sessions/focus-minutes/break-minutes`, e.g. `3/30/5`.")
		return
	}
	sessions := clampInt(parts[0], 1, 12, rm.AutoSessions)
	focus := clampInt(parts[1], 5, 180, rm.FocusMinutes)
	breaks := clampInt(parts[2], 1, 60, rm.BreakMinutes)
	if err := a.applyRoomSettings(ctx, rm.ID, focus, breaks, sessions, rm.AutoRoll); err != nil {
		log.Printf("discord: config room %s: %v", rm.Code, err)
		b.ephemeral(s, i, "Couldn't save that configuration — try again.")
		return
	}
	b.reply(s, i, fmt.Sprintf("Configured `%s`: %d session(s), %d min focus, %d min break.", rm.Code, sessions, focus, breaks), nil)
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
	timer, created, err := a.startRoomTimerAndSchedule(ctx, rm, u.ID, rm.FocusMinutes, u.DisplayName)
	if err != nil {
		log.Printf("discord: start timer %s: %v", rm.Code, err)
		b.ephemeral(s, i, "Couldn't start the timer — try again.")
		return
	}
	if !created {
		b.ephemeral(s, i, "A run is already active in this room.")
		return
	}
	if timer == nil {
		b.ephemeral(s, i, "Started, but couldn't confirm the lobby countdown.")
		return
	}
	b.reply(s, i, fmt.Sprintf("%s started a focus run in **%s** — starting %s. Tap Join before it begins!",
		u.DisplayName, rm.Name, discordTimestamp(timer.PhaseEndsAt)), joinComponents())
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
	timer, err := a.joinTimer(ctx, rm, u.ID)
	if err != nil {
		log.Printf("discord: join timer %s: %v", rm.Code, err)
		b.ephemeral(s, i, "Couldn't join the run — try again.")
		return
	}
	if timer == nil {
		b.ephemeral(s, i, "No active run to join right now — use `/junkie start`.")
		return
	}
	a.hub.broadcast(rm.Code, "timer-phase")
	b.ephemeral(s, i, "You're in for this run.")
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
	} else {
		a.hub.broadcast(rm.Code, "timer-leave")
	}
	b.ephemeral(s, i, "You've left the run.")
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
	token := randomHex(32)
	expires := time.Now().Add(24 * time.Hour)
	if _, err := a.db.Exec(context.Background(), `
		INSERT INTO discord_link_tokens (token_hash, discord_user_id, expires_at) VALUES ($1, $2, $3)`,
		hashToken(token), discordUserID, expires); err != nil {
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
	var discordUserID string
	err := a.db.QueryRow(r.Context(), `
		DELETE FROM discord_link_tokens
		WHERE token_hash = $1 AND expires_at > now()
		RETURNING discord_user_id`, hashToken(r.PathValue("token"))).Scan(&discordUserID)
	if err != nil {
		http.Redirect(w, r, "/?error="+url.QueryEscape("That Discord link is invalid or has expired — run /junkie link again."), http.StatusSeeOther)
		return
	}
	// One row per Discord user and per junkie account (both uniquely keyed):
	// re-linking either side replaces the old pairing.
	if _, err := a.db.Exec(r.Context(), `
		INSERT INTO discord_links (discord_user_id, user_id) VALUES ($1, $2)
		ON CONFLICT (discord_user_id) DO UPDATE SET user_id = EXCLUDED.user_id, linked_at = now()`,
		discordUserID, u.ID); err != nil {
		if isUniqueViolation(err) {
			http.Redirect(w, r, "/?error="+url.QueryEscape("This junkie account is already linked to a different Discord account."), http.StatusSeeOther)
			return
		}
		log.Printf("discord: confirm link: %v", err)
		http.Redirect(w, r, "/?error="+url.QueryEscape("Could not complete the Discord link — try again."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/?notice="+url.QueryEscape("Discord account linked! You can now use /junkie commands."), http.StatusSeeOther)
}

func (b *discordBot) handleHelp(s *discordgo.Session, i *discordgo.InteractionCreate) {
	b.ephemeral(s, i, "**junkie commands**\n"+
		"`/junkie link` — connect your Discord account to your junkie account\n"+
		"`/junkie register` — register this server's junkie room (posts to this channel)\n"+
		"`/junkie config sessions/focus/break` — set the timer, e.g. `3/30/5`\n"+
		"`/junkie start` — start a focus run\n"+
		"`/junkie join` — join the active run\n"+
		"`/junkie leave` — leave the active run")
}
