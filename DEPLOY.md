# Deploying junkie

junkie is a long-running Go process that holds live WebSocket connections for
the shared room timers, so it needs a host that stays up. Serverless and
scale-to-zero platforms drop those connections.

Two ways to run it:

- **[Render + Neon](#render--neon)** — what the hosted instance runs. Managed,
no server to babysit, no card required.
- **[Self-hosting with Docker](#self-hosting-with-docker)** — any VM you like,
using the bundled compose files and Caddy for TLS.

Migrations run automatically at app startup in both cases, and
`/healthz` returns `200 ok` once the app can reach its database.

## Configuration

Every deployment reads the same environment variables. Only `DATABASE_URL` is
required.


| Variable                 | Required | Purpose                                                                                                            |
| ------------------------ | -------- | ------------------------------------------------------------------------------------------------------------------ |
| `DATABASE_URL`           | Yes      | Postgres connection string. Managed providers include `sslmode=require`.                                           |
| `ADDR`                   | No       | Address the app listens on. Defaults to `:8080`.                                                                   |
| `JUNKIE_OWNER_USERNAME`  | No       | Pins the owner account. Set it *after* signing up with that username.                                              |
| `DISCORD_BOT_TOKEN`      | No       | Discord bot token. Leave blank to run without the bot.                                                             |
| `DISCORD_APPLICATION_ID` | No       | Discord application ID, alongside the token.                                                                       |
| `PUBLIC_BASE_URL`        | No       | Public base URL, no trailing slash. Used for the account-link URLs the bot hands out. Required if you run the bot. |
| `DOMAIN`                 | No       | Hostname for the bundled Caddy proxy. Self-hosting only — Render terminates TLS itself.                            |
| `POSTGRES_PASSWORD`      | No       | Password for the bundled Postgres container. Self-hosting with local Postgres only.                                |


See [.env.example](.env.example) for the annotated version.

---



## Render + Neon

No card and no server to manage. The tradeoff is Render's free instance type,
which sleeps after 15 minutes of inactivity: the first visitor after a sleep
waits roughly a minute, and the sleep drops everyone's live WebSocket
connections. Timer state itself survives, since it lives in Postgres. A paid
instance type removes the sleeping.

1. **Database** — create a free project at [https://neon.tech](https://neon.tech) and copy the
  connection string. It already includes `sslmode=require`. 
2. **App** — at [https://render.com](https://render.com), New → Web Service → connect this GitHub
  repo. Render detects the `Dockerfile`. Don't use the compose files here;
   Render replaces both Postgres and Caddy.
3. **Environment variables** — set them on the service:
  - `DATABASE_URL` — the Neon connection string
  - `PUBLIC_BASE_URL` — your `https://<name>.onrender.com` URL, once known
  - `DISCORD_BOT_TOKEN` and `DISCORD_APPLICATION_ID` — only if you want the bot
4. **Deploy.** Render gives you `https://<name>.onrender.com` with TLS included.
5. **Bootstrap the owner** — sign up in the app, then set
  `JUNKIE_OWNER_USERNAME` to that username and redeploy.

Pushes to the default branch redeploy automatically.

---



## Self-hosting with Docker

Any always-on VM works. You need Docker, a hostname pointing at the VM's public
IP, and ports 80 and 443 reachable. Caddy obtains and renews the Let's Encrypt
certificate on its own, so DNS must resolve *before* you start the stack.

If you don't own a domain, [https://www.duckdns.org](https://www.duckdns.org) gives you a free
subdomain; point it at the VM's IP. Any domain works the same way, via an A
record.

### Install Docker and clone

```sh
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker $USER && exit   # re-SSH so the group applies
```

```sh
git clone https://github.com/samnodier/junkie.git && cd junkie
cp .env.example .env
```



### Choose a database

**Bundled Postgres** — everything on one box. Set `POSTGRES_PASSWORD` (e.g.
`openssl rand -hex 24`) and `DOMAIN` in `.env`, then:

```sh
docker compose --profile prod up -d --build
```

**Managed Postgres** (Neon or similar) — set `DATABASE_URL` and `DOMAIN` in
`.env`, then use the Neon-specific compose file, which omits the local Postgres
service:

```sh
docker compose -f docker-compose.neon.yml up -d --build
```

Open `https://<your-domain>` once it's up.

### Bootstrap the owner account

1. Sign up in the app with your username.
2. Set `JUNKIE_OWNER_USERNAME=<that username>` in `.env`.
3. Re-run the same `up -d` command to recreate the app with the new env.



### Updating

```sh
git pull && docker compose --profile prod up -d --build
```

Use `-f docker-compose.neon.yml` instead of `--profile prod` if you're on
managed Postgres.

### Backups

With managed Postgres, the provider handles backups — Neon runs automatic
backups and point-in-time restore on its free tier.

With the bundled Postgres, data lives in the `postgres-data` Docker volume on
the VM. A nightly dump via cron (`crontab -e`):

```cron
0 3 * * * docker compose -f /home/ubuntu/junkie/docker-compose.yml exec -T postgres pg_dump -U junkie junkie | gzip > /home/ubuntu/junkie-backup-$(date +\%u).sql.gz
```

That keeps seven rotating daily dumps. Copy them off the VM occasionally.

### Firewall notes

Some cloud images block inbound traffic beyond SSH even when the provider's
security group allows it. If DNS resolves but Caddy can't get a certificate,
check the VM's own firewall as well as the provider's. On images shipping
restrictive iptables rules:

```sh
sudo iptables -I INPUT 6 -m state --state NEW -p tcp --dport 80 -j ACCEPT
sudo iptables -I INPUT 6 -m state --state NEW -p tcp --dport 443 -j ACCEPT
sudo iptables -I INPUT 6 -m state --state NEW -p udp --dport 443 -j ACCEPT
sudo netfilter-persistent save
```

