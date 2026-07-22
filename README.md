<p align="center">
  <img src="docs/assets/banner.jpg" alt="Warmbly" width="100%" />
</p>

<p align="center">
  Open-source cold email and mailbox warmup you can self-host.<br />
  Your sending IPs, your database, your servers.
</p>

<p align="center">
  <a href="https://github.com/warmbly/warmbly/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/warmbly/warmbly/ci.yml?branch=main&style=flat-square&label=CI" alt="CI status" /></a>
  <a href="https://github.com/warmbly/warmbly/releases"><img src="https://img.shields.io/github/v/release/warmbly/warmbly?style=flat-square" alt="Latest release" /></a>
  <img src="https://img.shields.io/github/go-mod/go-version/warmbly/warmbly?style=flat-square&label=go" alt="Go version" />
  <a href="./LICENSE"><img src="https://img.shields.io/github/license/warmbly/warmbly?style=flat-square" alt="License" /></a>
  <a href="https://docs.warmbly.com"><img src="https://img.shields.io/badge/docs-docs.warmbly.com-1f6feb?style=flat-square" alt="Documentation" /></a>
</p>

<p align="center">
  <a href="#connect-your-mailboxes">Mailboxes</a> ·
  <a href="#integrations">Integrations</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="#warmup">Warmup</a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="#self-hosting">Self-hosting</a> ·
  <a href="#documentation">Docs</a> ·
  <a href="./CONTRIBUTING.md">Contributing</a>
</p>

## What is Warmbly
Warmbly is a cold outreach platform. You connect your mailboxes, write sequenced
campaigns, and it sends the mail, tracks the replies, and keeps your sender
reputation healthy.

https://github.com/user-attachments/assets/378a510a-bb99-425f-925e-04300184938b

## Star the repository ⭐

<img width="1280" height="720" alt="warmbly-star-ezgif com-optimize" src="https://github.com/user-attachments/assets/b6fb6df6-f0ad-4410-805d-4d4f17ac1b50" />


## Connect your mailboxes

Warmbly sends and receives through the mailboxes you already own. There are three
ways to connect one, and you can mix them freely across a workspace:

- **Google / Gmail and Google Workspace.** Connect with one-click OAuth, no app
  password to store or rotate. Sending goes through the Gmail API.
- **Microsoft 365 / Outlook.** Connect with one-click OAuth over authenticated
  SMTP and IMAP.
- **Any other provider over SMTP + IMAP.** Zoho, Fastmail, a self-hosted mail
  server, anything that speaks SMTP and IMAP. Add the host, port, and an app
  password.

Each mailbox warms, sends, and syncs on its own, with its own daily cap, minimum
spacing between sends, and reputation tracked per IP. Replies stream into the
unified inbox in near real time. Credentials and OAuth tokens are sealed with
per-organization envelope encryption and are only decrypted on the worker that
owns the mailbox, never stored in plaintext.

## Integrations

Automations and the built-in CRM connect out to the tools you already run: ping
Slack on a positive reply, push a won deal to your CRM, book meetings straight
from replies, or fan events out to your own stack.

| Category      | Providers                              |
|---------------|----------------------------------------|
| CRM           | HubSpot, Salesforce, Pipedrive, Close  |
| Automation    | Zapier, Make, n8n                      |
| Notifications | Slack, Discord                         |
| Meetings      | Calendly, Cal.com                      |
| Data          | Google Sheets                          |

Everything is also reachable through a scoped REST API, HMAC-signed webhooks, and
a realtime WebSocket, so you can wire Warmbly into anything that is not on the
list. Open tracking, click tracking, and reply detection feed the same event
stream. See the [API reference](https://docs.warmbly.com/api/).

## Warmup

Warmup only produces meaningful results with a pool of real mailboxes warming
against each other. Warmbly maintains that pool, so the practical path to real
reputation is to run warmup through Warmbly: your mailboxes hold genuine
conversations with monitored inboxes instead of throwaway accounts, even if you
only have a few. If you operate enough mailboxes of your own to sustain a healthy
pool, you can host warmup yourself instead.

Either way the safeguards are the same. Volume starts low and ramps gradually per
mailbox, replies happen at a natural rate, and every warmup message carries a
verification token. Mailboxes that show spam patterns or forged tokens are scored
and auto-blocked from the pool, so it stays clean for everyone in it. Free and
premium pools are kept separate.

## Quick start

You need Docker, Go 1.25, and pnpm.

```bash
git clone https://github.com/warmbly/warmbly && cd warmbly
make dev
```

That one command brings up the backing services in Docker (Postgres, Redis,
NATS), waits for them, applies migrations, seeds demo data, and starts the
backend, consumer, worker, dashboard, and admin natively, with realtime and
tracking as containers (so you need no Elixir or Rust toolchain). Open
`http://localhost:5173` and log in with `dev@warmbly.com` / `password123`.
Ctrl-C stops the app; the Docker infra stays up so the next `make dev` is fast.

The Go services run natively (recompiling in a second or two on save), so the
same command is also the day-to-day loop. If you prefer separate terminals, the
stack splits into `make infra` + `make run` + `make web`. To run the whole
no-cloud stack in Docker instead, use `make up` (see [Self-hosting](#self-hosting)).

The first admin account cannot be created from the UI. Sign up through the
dashboard, then promote yourself from the host with
`make grant-admin EMAIL=you@example.com` and open the admin app with `make admin`.
Full local setup, seeding, and troubleshooting live in the
[local development guide](https://docs.warmbly.com/development/local-development/).

## How it works

Warmbly is split into a control plane and an execution plane.

The control plane is the backend API, the event consumer, Postgres, Redis, and
the event bus. It owns every piece of stateful data and decides what gets sent
and from where.

The execution plane is the worker fleet: one Go binary per machine. Workers take
commands off the event bus, fetch their encryption keys over HTTPS, send and sync
mail, and report telemetry back. **Workers never connect to Postgres.** They are
interchangeable executors, so you add throughput by running more of them; outbound
mail leaves through each mailbox's own provider (Gmail API, Graph, or SMTP relay),
not the worker's IP.

Secrets use envelope encryption: a per-organization data key, wrapped by a root
key, seals mailbox credentials and message content. The root key is a local AES
master key by default (no cloud), or AWS KMS if you prefer. The full write-up is
in the [architecture docs](https://docs.warmbly.com/development/architecture/).

## Self-hosting

Warmbly runs with **no cloud account of any kind**: no AWS, no GCP, no Stripe,
no Kafka. One command brings up the whole platform on local, open-source pieces:

```bash
git clone https://github.com/warmbly/warmbly && cd warmbly
make up               # or: docker compose up --build
```

Dashboard on `:5173`, admin on `:5174`, API on `:8080`. The first build compiles
the images once (a couple of minutes; they are CGO-free), then it is up. Load
optional demo data with `make up` running via
`docker compose --profile seed run --rm seed`.

Before exposing it, set your own secrets in a `.env` next to `docker-compose.yml`.
At minimum: `AUTH_SECRET`, `CREDENTIALS_ENCRYPTION_KEY`, `INTERNAL_API_TOKEN`,
and `KMS_LOCAL_MASTER_KEY` (run `make gen-key`). The full list is in
[`deploy/config/env.example`](deploy/config/env.example).

**Reaching it from another machine.** Every service already binds to `0.0.0.0`
(the Docker port mappings), so it listens on all interfaces. To make the app
usable from a LAN IP or a domain instead of `localhost`, set one variable in
`.env`, and everything (API URL, CORS, websocket, tracking, blob URLs) derives
from it:

```bash
PUBLIC_HOST=192.168.1.50      # your machine's LAN IP, or your domain
```

Then open `http://192.168.1.50:5173`. For a public domain over HTTPS, put a
reverse proxy (Caddy or nginx) in front terminating TLS, set `PUBLIC_HOST` to the
domain, and build the frontends as static bundles rather than serving the Vite
dev server.

Every external dependency is picked by an environment variable, so you swap in a
cloud service only if you want one:

| Concern        | Self-host default          | Optional / cloud             |
|----------------|----------------------------|------------------------------|
| Database       | PostgreSQL 16              | RDS / Cloud SQL, any Postgres |
| Cache          | Redis (or Valkey)         | ElastiCache                  |
| Event bus      | **NATS JetStream** (~15 MB, one binary) | Kafka (`-tags kafka`) |
| Blob storage   | **Filesystem**            | S3, MinIO, R2, B2            |
| KMS / root key | **Local AES master key**  | AWS KMS                      |
| Task scheduler | **In-process Postgres poller** | GCP Cloud Tasks         |
| Codec          | JSON                      | Avro + Schema Registry       |
| Captcha        | Off                       | Cloudflare Turnstile         |
| Payments       | **Off (everything unlocked)** | Stripe                   |

NATS is the default because it is one small binary versus Kafka's
JVM + Zookeeper + Schema Registry, and it keeps the image CGO-free so builds are
fast. Kafka is fully supported: build the images with `--build-arg GO_TAGS=kafka`
(and the tracking image with `CARGO_FEATURES=kafka`), set `EVENTBUS_PROVIDER=kafka`,
and point `KAFKA_BOOTSTRAP_SERVERS` at your cluster.

**Scaling is by mailboxes and workers, not IPs.** Outbound mail goes through each
mailbox's own provider (Gmail API, Microsoft Graph, or the mailbox's SMTP relay),
so the source IP is the provider's, never the worker's. Add throughput by
connecting more mailboxes and running more workers: `docker compose up --scale
worker=3`, or attach a machine you already own through the admin panel's SSH
enrollment. Workers are interchangeable executors.

**Connecting Gmail mailboxes** needs your own Google Cloud OAuth client: set
`BOX_GOOGLE_CLIENT_ID` / `BOX_GOOGLE_CLIENT_SECRET` (a Web application client with
the authorized redirect URI `<your-api-host>/addresses/google/callback`) on the
backend and every worker. Without it you can still connect any mailbox over
SMTP/IMAP with an app password, and Microsoft 365 over its own OAuth client.

Keep two secrets safe: `KMS_LOCAL_MASTER_KEY` (`make gen-key`) and
`CREDENTIALS_ENCRYPTION_KEY` seal every stored mailbox credential, and losing them
is unrecoverable. Full env reference and day-2 operations are in the
[deployment guide](https://docs.warmbly.com/development/deployment-guide/).

## Tech stack

| Component   | Tech                              |
|-------------|-----------------------------------|
| Backend API | Go 1.25 + Gin                     |
| Consumer    | Go (event-bus driven)             |
| Worker      | Go (Kafka / NATS subscriber)      |
| Tracking    | Rust + Axum                       |
| Realtime    | Elixir + Phoenix Channels         |
| Dashboard   | React 19 + Vite + Tailwind v4     |
| Admin UI    | React 19 + Vite + Tailwind v4     |
| Database    | PostgreSQL 16                     |
| Cache       | Redis 7 (or Valkey / KeyDB)       |
| Event bus   | NATS JetStream (default) or Kafka |

## Documentation

The full docs live at **[docs.warmbly.com](https://docs.warmbly.com)**: product
guides, the API reference, and the engineering docs. Start here:

| Read this | To learn |
|-----------|----------|
| [Local development](https://docs.warmbly.com/development/local-development/) | Every make target, the native services, and how seeding works |
| [Sandbox](https://docs.warmbly.com/development/sandbox/) | One command spins up a full demo org that sends, replies, opens, clicks, and warms itself |
| [Architecture](https://docs.warmbly.com/development/architecture/) | How the control plane and the workers split the job, plus the encryption model |
| [Deployment guide](https://docs.warmbly.com/development/deployment-guide/) | Taking it to production and scaling the worker fleet |
| [Event system](https://docs.warmbly.com/development/events/) | The event bus and every topic that flows across it |
| [API reference](https://docs.warmbly.com/api/) | Endpoints, auth, permissions, and webhooks |

## Contributing

Pull requests are welcome. Keep each one to a single logical change, and open an
issue first for larger design or product changes. Before you open a PR, run the
checks for the tree you touched (`make fmt` and `make lint` for Go, `pnpm
typecheck` and `pnpm lint` for the frontends). See [CONTRIBUTING.md](CONTRIBUTING.md).

## Security

Found a vulnerability? Email `team@warmbly.com` instead of opening a public issue.
We prefer responsible disclosure and credit reporters in the release notes.

## License

Apache License 2.0. Copyright 2026 Mindroot Ltd. See [LICENSE](./LICENSE).
