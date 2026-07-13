# junkie

`junkie` is a small shared focus app for study groups. Open it in a browser, track what you need to finish, run solo or shared focus timers, and see progress on a GitHub-inspired work map of focused minutes per day.

There is no planting mechanic — progress is measured in time spent focusing.

## How to use junkie

### Guest mode (no account)

You can use junkie without signing up. Everything stays in **browser `localStorage` on this device only** — it is not stored on the server:

- **Private todos** — add, complete, remove, and restore tasks on the desk.
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
- **Username** — change it from Profile → Account. Your focus history stays with the account. The **owner** account's username is pinned (see Admin and owner access) and cannot be renamed in the app.
- **Password change** — Profile → Security. Updating your password **signs out all other devices**; only the browser you used to change it keeps the session.
- **Account deletion** — Profile → Danger zone (not available for the owner account). Permanently deletes your account, rooms you created, todos, and activity history. Requires your current password.
- **Profile picture** — upload or remove from Profile → Preferences. Shown next to your name on room todos. Images are resized in the browser before upload.

### Solo focus (signed in)

Logged-in solo focus works like guest mode, but timer runs and completed focus minutes are stored in PostgreSQL and appear on your profile work map across devices.

- Adjust the ring (scroll ±1, buttons ±5, tap to start) before each session.
- Breaks can be taken, adjusted, or skipped with **Skip break & continue** to roll straight into the next focus block.
- Other tabs or devices signed into the same account pick up private todo changes live; solo timer phase changes still reload the desk (same as starting a session in another tab).

### Rooms

Accounts are required to create or join shared rooms. Each room has a persistent code and lives at `/r/{code}`.

**Joining and inviting**

- Create a room from the menu, or join with a room code or invite link.
- Invite links land on a confirmation screen before you join.
- When someone starts a focus lobby while junkie is in the background, you can get a **room invite notification** (toggle in Profile → Preferences).

**Room todos**

- Todos in a room are **public to every member**.
- Lists are grouped into **yours** and **everyone else's**.
- You can complete your own todos; you see teammates' todos read-only (with their display name and profile picture).
- Edits, removes, restores, and deletes apply only to **your** todos.
- Room todo lists update **live** across the room page and the homepage desk switcher without full page reloads. When someone completes a task, other members see a short toast.
- On the homepage desk you can switch between **Private todos** and **Room todos** per room you belong to. Inside `/r/{code}` there is no switcher — that page is scoped to one room.

**Room timers**

- Shared focus runs from **server timestamps**; clients count down locally.
- Flow: **lobby** (optional join window) → **focus** → **break** → next focus session, for the configured number of sessions.
- Anyone in the room can rename the room and edit timer defaults (focus length, break length, session count, auto-roll).
- Only the **room creator** can delete the room.
- During focus, late joins are locked out until the next break.
- **Pause / Resume** and **Skip break & continue** are available on breaks (any member, same as pause).
- **Auto-start breaks** (room settings): when **on**, breaks start automatically after focus. When **off**, the break waits on an adjustable ring until someone starts it or skips.
- **Leave focus block** ends your participation in the active timer. Hiding or closing a tab does **not** leave — participant rings show who intentionally joined the block, not who has a tab open.
- **Focus mode** during an active session hides the full todo board so the timer stays central.

### Connections

Connections are a separate, mutual link between two accounts — not room membership.

- From Profile → Connections, copy a **one-time connect link** and send it to someone. Each link works once; generate a new one for the next person.
- After connecting, you each see the other's **focus heatmap only** on `/connections` and on public profile pages at `/{username}`.
- Profiles are **invisible to non-connections** — no public directory.
- There is no in-app way to disconnect or remove a connection yet, and the feed shows heatmaps only (no todos, rooms, or timers).

### Cross-device sync

Sign in on each device with the same account.

| What | Sync |
|------|------|
| Private todos | Live across your tabs/devices (`/ws/me`) |
| Room todos & timers | Live for all room members (room WebSocket) |
| Solo timer phase | Other tabs reload the desk when phase changes |
| Work map / activity | Stored server-side; same on every device |
| Guest data | Never syncs — local only |

On supported mobile browsers, junkie requests a **screen wake lock** while a timer is visible or a focus session is active so the phone is less likely to lock mid-session.

## Where your data lives

junkie splits storage by whether you have an account.

### Without an account (guest / solo)

Browser `localStorage` only:

- Private todos (`junkie:todos`)
- Solo timer state (`junkie:soloTimer`)
- Work map / focus minutes per day (`junkie:activity`)

### With an account

PostgreSQL on the server:

- Account credentials and session
- Private todos and todo reactions
- Solo timer runs and completed focus minutes (work map)
- Room membership, room settings, room todos, and shared room timers
- Profile pictures, connections, and connect invite tokens

### Why guest data stays local

Keeping solo progress in the browser avoids anonymous database rows, cleanup overhead, and extra privacy complexity. It also keeps hosted databases small. Server storage is reserved for features that need it — mainly shared rooms and cross-device persistence.

### Keeping progress long-term

Create an account and use junkie while logged in. Your todos, timer history, and work map then live in PostgreSQL and follow you across devices. If you used junkie as a guest first, re-enter todos manually; guest `localStorage` does not migrate today.

## Admin and owner access

Every account has one database-backed role:

- `user`: regular app access; cannot open `/admin`
- `admin`: operational aggregates, account metadata, room metadata, and audited room deletion
- `owner`: all admin access, aggregate per-user focus summaries, and audited promotion/demotion of admins

There is intentionally no public or in-app navigation link to `/admin`. Authorization is enforced server-side. Admins never receive password hashes, session tokens, private todo text, or detailed per-user activity history.

To bootstrap the first owner:

1. Create or sign in to the intended account once.
2. Set `JUNKIE_OWNER_USERNAME` to that account's username.
3. Restart junkie.

At startup, the configured account is promoted to `owner`. Changing or removing the variable never demotes an existing owner. A missing username is logged without exposing the configured value.

A trusted database operator can instead promote an initial owner directly:

```sql
UPDATE users SET role = 'owner' WHERE username = 'your_username';
```

Keep at least one owner. Owner accounts cannot be demoted through the web admin interface; changing an owner role requires trusted database access.

Admin mutations require same-origin browser requests, use `POST`, re-check actor and target roles from PostgreSQL, and write to `admin_audit_log`. Session cookies are `HttpOnly`, `SameSite=Lax`, and marked `Secure` when served over HTTPS.

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

## Deployment and PostgreSQL

For a start-to-finish free hosting walkthrough (Oracle Cloud Always Free with the bundled Caddy TLS proxy, or Render + Neon), see [DEPLOY.md](DEPLOY.md).

The server requires PostgreSQL. SQLite is not supported or bundled. People using a deployed junkie instance need only a browser; they do not need PostgreSQL or any local application installed.

Set `DATABASE_URL` to the connection string for your hosted PostgreSQL database:

```sh
DATABASE_URL='postgres://user:password@host/database?sslmode=require'
```

The server applies every idempotent SQL file in `migrations/` at startup, including role and audit-log schema changes. Run one app instance during a migration rollout, or apply the same migrations through your deployment pipeline before scaling out.

Managed PostgreSQL products such as Neon, Supabase, and Fly Managed Postgres are examples of deployment options. Plans and free-tier availability change, so check current pricing and limits rather than relying on a particular free offering.

Guest mode remains entirely in that browser's `localStorage`. A hosted database only persists signed-in account data across devices.

### Containerized deployment

A `Dockerfile` builds a self-contained image (binary plus `migrations/`), and the compose file has an optional `app` service:

```sh
cp .env.example .env   # then set a strong POSTGRES_PASSWORD
docker compose --profile app up -d --build
```

The app listens on `127.0.0.1:8080` and Postgres on `127.0.0.1:5432`, so neither is reachable from other hosts directly — put a reverse proxy (Caddy, nginx, or your host's ingress) in front for TLS.

`GET /healthz` returns `200 ok` when the app can reach the database; point uptime checks and container health probes at it.

### Running behind a reverse proxy

junkie marks session cookies `Secure` when the request arrived over TLS or with `X-Forwarded-Proto: https`. Make sure your proxy sets `X-Forwarded-Proto` (and `X-Forwarded-For`, which the sign-in rate limiter uses to tell clients apart); Caddy and most platform routers do this by default.

### Security measures

- All state-changing requests are rejected when they originate from another site (`http.CrossOriginProtection`), and WebSocket handshakes enforce same-origin.
- Sign-in is rate limited per IP and per username; signups are rate limited per IP.
- Session cookies are `HttpOnly`, `SameSite=Lax`, `Secure` over HTTPS, and only a SHA-256 hash of the token is stored in PostgreSQL. Expired sessions are swept hourly.
- Responses carry a Content-Security-Policy plus `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, and (over HTTPS) `Strict-Transport-Security`. All scripts are served from the app itself.

## Later

- Terminal client using the same account and room API.
- Discord sign-in and a small bot for notifications/link sharing.
