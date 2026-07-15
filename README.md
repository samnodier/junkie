# junkie

`junkie` is a small shared focus app for study groups. Open it in a browser, track what you need to finish, run solo or shared focus timers, and see progress on a GitHub-inspired work map of focused minutes per day.


| Light | Dark |
| ----- | ---- |
| ![The junkie desk in light mode: a focus ring beside a private todo list](assets/desk-light.png) | ![The junkie desk in dark mode: a focus ring beside a private todo list](assets/desk-dark.png) |




## How to use junkie



### Guest mode (no account)

You can use junkie without signing up. Everything stays in **browser** `localStorage` **on this device only** — it is not stored on the server:

- **Private todos** — add, complete, remove, and restore tasks on the desk. Completed and removed todos clean themselves up after 24 hours.
- **Solo timer** — run private focus sessions with optional breaks.
- **Work map** — a heatmap of focus minutes per day, stored locally.

Guest data is tied to one browser profile on one device. Clearing site data or switching browsers starts you over. Guest data is **not** copied into an account when you sign up later.

On the desk, the solo timer ring is adjustable before you start:

- Scroll the ring or use arrow keys to change by **±1 minute**.
- Use the **±5** buttons for larger steps.
- Tap the ring or press Enter/Space to start.

When a focus block ends, junkie offers a break. You can take it, adjust the break length on the ring, or **Skip break & continue** to start the next focus session immediately with the same duration.

### Accounts

Create an account when you want shared rooms or data that follows you across devices.

- **Sign up / sign in** — username and password. Usernames are lowercase letters, numbers, dots, dashes, and underscores (2–32 characters).
- **Username** — change it from Profile → Account.
- **Password change** — Profile → Security. Updating your password **signs out all other devices**.
- **Account deletion** — Profile → Danger zone. Permanently deletes your account, rooms you created, todos, and activity history. Requires your current password.
- **Profile picture** — upload or remove from Profile → Preferences.



### Solo focus (signed in)

Logged-in solo focus works like guest mode, but timer runs and completed focus minutes are stored in PostgreSQL and appear on your profile work map across devices.

- Adjust the ring (scroll ±1, buttons ±5, tap to start) before each session.
- Breaks can be taken, adjusted, or skipped with **Skip break & continue** to roll straight into the next focus block.
- Other tabs or devices signed into the same account pick up private todo changes live; solo timer phase changes still reload the desk (same as starting a session in another tab).



### Rooms

Accounts are required to create or join shared rooms. Each room has a persistent code and lives at `/r/{code}`.

**Joining and inviting**

- Create a room from the menu, or join with a room code or invite link.
- Each account can have up to **5 rooms**; deleting a room frees its slot.
- Invite links land on a confirmation screen before you join.
- When someone starts a focus lobby while junkie is in the background, you can get a **room invite notification** (toggle in Profile → Preferences).

**Room todos**

- Todos in a room are **public to every member**.
- Lists are grouped into **yours** and **everyone else's**.
- You can complete your own todos; you see teammates' todos read-only (with their display name and profile picture).
- Edits, removes, restores, and deletes apply only to **your** todos.
- Completed and removed todos are deleted automatically after 24 hours — the room board stays focused on current work.
- Room todo lists update **live** across the room page and the homepage desk switcher without full page reloads. When someone completes a task, other members see a short toast.
- On the homepage desk you can switch between **Private todos** and **Room todos** per room you belong to. Inside `/r/{code}` there is no switcher — that page is scoped to one room.

**Room timers**

- Shared focus runs from **server timestamps**; clients count down locally.
- Flow: **lobby** (optional join window) → **focus** → **break** → next focus session, for the configured number of sessions.
- The lobby card shows **who's joined so far** — an avatar stack and count right under the countdown ring, updated live.
- Anyone in the room can rename the room and edit timer defaults (focus length, break length, session count, auto-roll).
- Only the **room creator** can delete the room.
- During focus, late joins are locked out until the next break.
- **Pause / Resume** and **Skip break & continue** are available on breaks (any member, same as pause).
- **Auto-start breaks** (room settings): when **on**, breaks start automatically after focus. When **off**, the break waits on an adjustable ring until someone starts it or skips.
- **Leave focus block** ends your participation in the active timer. Hiding or closing a tab does **not** leave — participant rings show who intentionally joined the block, not who has a tab open.
- **Focus mode** during an active session hides the full todo board so the timer stays central.



### Temporary focus rooms

Sometimes you just want a quick block with a few people, not a room that sticks around. From the menu, **Create temporary room** opens a config popup — set focus/break/sessions and whether breaks auto-run — and drops you onto a stripped-down screen at `/f/{code}` with a share link at the top.

- **Joined by link** — anyone signed in who opens the link is added and queued for the block, no confirmation step. They land straight on the waiting screen, so you can see who's here before you start.
- **Just the timer** — no todo board, no settings page, and the background grid is hidden for a distraction-free look. The invite link shows while you gather and during breaks, and disappears once focus starts.
- **Disposable** — the room deletes itself the moment the run finishes or everyone leaves, and sends everyone back home. Focus minutes still count toward each participant's work map.
- Temporary rooms **don't count** toward your 5-room limit and don't appear in your room list. Ones created but never started are cleaned up automatically.



### Connections

Connections are a separate, mutual link between two accounts — not room membership.

- From Profile → Connections, copy a **one-time connect link** and send it to someone. Each link works once; generate a new one for the next person.
- Opening a connect link shows a **confirmation screen** first — nothing is linked until the recipient clicks to accept (the Discord account link works the same way).
- After connecting, you each see the other's **focus heatmap only** on `/connections` and on public profile pages at `/{username}`.
- Profiles are **invisible to non-connections** — no public directory.
- There is no in-app way to disconnect or remove a connection yet, and the feed shows heatmaps only (no todos, rooms, or timers).



### Cross-device sync

Sign in on each device with the same account.


| What                | Sync                                          |
| ------------------- | --------------------------------------------- |
| Private todos       | Live across your tabs/devices (`/ws/me`)      |
| Room todos & timers | Live for all room members (room WebSocket)    |
| Solo timer phase    | Other tabs reload the desk when phase changes |
| Work map / activity | Stored server-side; same on every device      |
| Guest data          | Never syncs — local only                      |


On supported mobile browsers, junkie requests a **screen wake lock** while a timer is visible or a focus session is active so the phone is less likely to lock mid-session.

## Install on your phone

junkie is a PWA, so it installs to your home screen and runs fullscreen with no browser chrome.

- **iPhone / iPad** — open [junkie](https://junkie-blin.onrender.com) in Safari, tap Share, then **Add to Home Screen**.
- **Android** — either open it in Chrome and tap **Install app**, or download the signed APK from the [latest release](https://github.com/samnodier/junkie/releases/latest) and open it (you may need to allow installing from unknown sources).

Both are the same app pointing at the hosted site; the APK just wraps it so there's nothing to install from a browser.

## Discord bot

junkie has a Discord bot that runs a server's focus room from chat: live countdown messages showing who's in, break notifications, join buttons, stats, and a heatmap picture — no browser needed once you're set up.

**Add it to a server** (needs Manage Server permission there):

> [https://discord.com/oauth2/authorize?client_id=1526585859044933684&scope=bot+applications.commands&permissions=2048](https://discord.com/oauth2/authorize?client_id=1526585859044933684&scope=bot+applications.commands&permissions=2048)

The bot only asks for Send Messages. Once added: each participant runs `/junkie link` once to connect their junkie account, then an admin runs `/junkie register` in the channel the timer should post to (pass an existing room code to connect it, or omit to create a fresh room). Move notifications later with `/junkie channel [#channel]`. `/junkie help` lists all commands.

Self-hosting? The bot is optional — it starts only when `DISCORD_BOT_TOKEN`, `DISCORD_APPLICATION_ID`, and `PUBLIC_BASE_URL` are set (see `.env.example`), and you'd mint your own invite link with your application's client id.

## Where your data lives

**Guest** — todos, solo timer state, and your work map stay in browser `localStorage` on that device. Nothing reaches the server.

**Signed in** — everything lives in PostgreSQL and follows you across devices: account and session, todos, timer runs and focus minutes, rooms and their settings, profile pictures, and connections.

**Finished todos don't stick around.** Once a todo has been completed or removed for 24 hours, junkie deletes it permanently — for every user, in private lists and rooms alike (guest todos follow the same rule locally). Un-completing or restoring a todo within that day resets its clock. Your work map keeps the focus history; the list stays about what's next.

Guest data does not migrate into an account; sign up first if you want to keep progress long-term.

## Admin and owner access

Accounts have one of three roles. `user` is the default and cannot open `/admin`. `admin` adds operational aggregates, account and room metadata, and room deletion. `owner` adds per-user focus summaries and promoting or demoting admins. Admin actions are audited.

To bootstrap the first owner, sign in to the intended account once, set `JUNKIE_OWNER_USERNAME` to its username, and restart. That account is promoted at startup. Clearing the variable never demotes an existing owner, and owners can only be changed with database access.

## Local development

Start Postgres:

```sh
docker compose up -d
```

Run the app:

```sh
GOPATH="$PWD/.gopath" GOCACHE="$PWD/.gocache" go run ./cmd/junkie
```

Open:

```text
http://localhost:8080
```

The default database URL is:

```text
postgres://junkie:junkie@localhost:5432/junkie?sslmode=disable
```

Override it with `DATABASE_URL` if needed. Migrations in `migrations/` run automatically at startup.

## Deployment

The server needs PostgreSQL; SQLite is not supported. People *using* a deployed instance need only a browser.

See [DEPLOY.md](DEPLOY.md) for Render + Neon or self-hosting with the bundled compose files and Caddy.

If you put junkie behind your own proxy, make sure it sets `X-Forwarded-Proto` and `X-Forwarded-For` — the app relies on them to mark cookies `Secure` and to tell clients apart. Caddy and most platform routers do this already. `GET /healthz` returns `200 ok` when the app can reach the database.

## Security

Sessions are cookie-based and stored only as hashes. Cross-site requests and WebSocket handshakes are rejected, sign-in and signup are rate limited, and responses carry a Content-Security-Policy with all scripts served from the app itself.

Found a vulnerability? Open an issue or contact the maintainer rather than filing a public exploit.



## Later

- Terminal client using the same account and room API.
- Discord sign-in (the bot exists — see the Discord bot section above; OAuth sign-in does not yet).

