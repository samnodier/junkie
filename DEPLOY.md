# Deploying junkie for free

junkie is a long-running Go process that holds live WebSocket connections, so
it needs an always-on host — serverless and scale-to-zero platforms break the
shared room timers. Two paths cost nothing:

| | Path A: Oracle Cloud Always Free | Path B: Render free + Neon free |
|---|---|---|
| Cost | $0 forever | $0 forever |
| Card needed at signup | Yes (identity check only, never charged) | No |
| Always on | Yes | No — sleeps after 15 min idle, ~1 min cold start, drops live WebSockets |
| Ops | You manage a VM | Fully managed |

**Use Path A for real users.** Use Path B if Oracle signup rejects your card
or you just want a quick demo link.

---

## Path A: Oracle Cloud Always Free VM

You get a permanent free ARM VM (currently 2 OCPUs / 12 GB RAM — far more
than junkie needs) and run the existing compose file on it.

### 1. Create the account

Sign up at <https://www.oracle.com/cloud/free/>.

- A credit/debit card is required for identity verification. Always Free
  resources never charge it. If the card is rejected, retry later or with a
  different card — this is the most common signup failure.
- Choose your **home region** carefully; it cannot be changed. Ampere (ARM)
  capacity varies by region — US East (Ashburn) and US West (Phoenix) are
  usually stocked.
- Optional but recommended once signed up: upgrade the account to
  **Pay As You Go**. Always Free resources stay free, but the upgrade removes
  the idle-VM reclamation policy and fixes most "out of capacity" errors.

### 2. Create the VM

Console → Compute → Instances → Create instance:

- Image: **Ubuntu 24.04**
- Shape: **VM.Standard.A1.Flex**, 2 OCPUs, 12 GB RAM (all within Always Free)
- Networking: create the default VCN, **assign a public IPv4 address**
- Add your SSH public key

If creation fails with "out of host capacity", retry at a different time of
day or after upgrading to Pay As You Go.

### 3. Open ports 80 and 443

Oracle blocks inbound traffic in two places; open both.

**VCN security list** (Console → Networking → your VCN → security list →
Add Ingress Rules), source `0.0.0.0/0`:

- TCP 80
- TCP 443
- UDP 443 (optional, enables HTTP/3)

**On the VM** — Oracle's Ubuntu images ship iptables rules that reject
everything except SSH:

```sh
sudo iptables -I INPUT 6 -m state --state NEW -p tcp --dport 80 -j ACCEPT
sudo iptables -I INPUT 6 -m state --state NEW -p tcp --dport 443 -j ACCEPT
sudo iptables -I INPUT 6 -m state --state NEW -p udp --dport 443 -j ACCEPT
sudo netfilter-persistent save
```

### 4. Get a free domain

Caddy needs a hostname to issue a TLS certificate. Free option:
<https://www.duckdns.org> — sign in, create a subdomain (e.g.
`myjunkie.duckdns.org`), and set its IP to the VM's public IPv4 address.
Any domain you own works the same way (an A record pointing at the VM).

### 5. Install Docker and deploy

SSH in (`ssh ubuntu@<public-ip>`), then:

```sh
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker $USER && exit   # re-SSH so the group applies
```

```sh
git clone https://github.com/<you>/junkie.git && cd junkie
cp .env.example .env
```

Edit `.env`:

- `POSTGRES_PASSWORD` — strong value (`openssl rand -hex 24`)
- `DOMAIN` — the hostname from step 4

Then:

```sh
docker compose --profile prod up -d --build
```

Caddy obtains the certificate automatically (needs DNS from step 4 already
pointing at the VM). Open `https://<your-domain>` — migrations run at app
startup, so the site should just work. `https://<your-domain>/healthz`
returns `200 ok` when the app can reach Postgres.

### 6. Bootstrap the owner account

1. Sign up in the app with your username.
2. Set `JUNKIE_OWNER_USERNAME=<that username>` in `.env`.
3. `docker compose --profile prod up -d` (recreates the app with the new env).

### Updating

```sh
git pull && docker compose --profile prod up -d --build
```

### Backups (recommended)

Postgres data lives in the `postgres-data` Docker volume on the VM. A nightly
dump via cron (`crontab -e`):

```cron
0 3 * * * docker compose -f /home/ubuntu/junkie/docker-compose.yml exec -T postgres pg_dump -U junkie junkie | gzip > /home/ubuntu/junkie-backup-$(date +\%u).sql.gz
```

This keeps seven rotating daily dumps. Copy them off the VM occasionally.

---

## Path B: Render free + Neon free

No card, no server to manage — but the free instance sleeps after 15 minutes
of inactivity. The first visitor after a sleep waits ~1 minute, and a sleep
drops everyone's live WebSocket connections (timer state itself survives, it
lives in Postgres).

1. **Database**: create a free project at <https://neon.tech> and copy the
   connection string (it already includes `sslmode=require`).
2. **App**: at <https://render.com>, New → Web Service → connect this GitHub
   repo. Render detects the `Dockerfile`; pick the **Free** instance type.
3. **Environment variables** on the service:
   - `DATABASE_URL` — the Neon connection string
   - `ADDR` — `:8080` (Render auto-detects the port the container opens)
   - `JUNKIE_OWNER_USERNAME` — set after your first signup, then redeploy
4. Deploy. Render gives you `https://<name>.onrender.com` with TLS included;
   migrations run at startup. Don't use the `prod` compose profile here —
   Render replaces both Postgres and Caddy.

Neon's free tier also suspends idle databases; the app reconnects on the next
request, which just adds a moment to the first cold hit.
