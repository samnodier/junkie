# junkie

`junkie` is a small shared focus app for study groups. It gives people a link they can open in the browser, create an account, join a persistent room, share public room todos, and run locked focus sessions with break windows.

There is no planting mechanic. Progress is represented by a GitHub-inspired work map of focused minutes per day.

## V1 behavior

- Use solo without an account: private todos, solo timer, and work map stay on your device.
- Accounts are only required to create or join shared rooms.
- Username/password accounts backed by PostgreSQL for room features.
- Persistent rooms with generated invite codes at `/r/{code}`.
- Anyone in a room can rename the room and edit timer defaults.
- Only the room creator can delete the room.
- Room todos are public to everyone in that room.
- Room timers run from server timestamps, so clients count down locally without timer spam.
- Focus blocks lock late joins out of the active timer.
- Break blocks allow new people to join for the next focus block.
- Focus mode hides todo boards so the app does not become the distraction.

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

Override it with `DATABASE_URL` if needed.

## Later

- Terminal client using the same account and room API.
- Discord sign-in and a small bot for notifications/link sharing.
- Production deploy on a host that supports a long-running Go process and WebSockets, such as Fly.io or Railway.
