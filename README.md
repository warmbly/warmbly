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
  <a href="#quick-start">Quick start</a> ·
  <a href="#warmup">Warmup</a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="#self-hosting">Self-hosting</a> ·
  <a href="#documentation">Docs</a> ·
  <a href="./CONTRIBUTING.md">Contributing</a>
</p>

<p align="center">
  <img src="docs/assets/dashboard-campaigns.png" alt="Campaigns" width="100%" /><br />
  <sub><b>Campaigns</b> · multi-step sequences with per-mailbox daily caps and spacing</sub>
</p>

<p align="center">
  <img src="docs/assets/dashboard-inbox.png" alt="Unified inbox" width="100%" /><br />
  <sub><b>Unified inbox</b> · every connected mailbox and reply in one place</sub>
</p>

<p align="center">
  <img src="docs/assets/dashboard-mailboxes.png" alt="Mailboxes" width="100%" /><br />
  <sub><b>Mailboxes</b> · warmup state and health for every account</sub>
</p>

## What is Warmbly

Warmbly is a cold outreach platform. You connect your mailboxes, write sequenced
campaigns, and it sends the mail, tracks the replies, and keeps your sender
reputation healthy. The difference from hosted tools is where it runs: your
sending IPs, your Postgres, your servers. Nothing is tied to a vendor's database.

Everything a sending team needs sits in one dashboard:

- **Campaigns** send multi-step sequences with per-mailbox daily caps and spacing.
- **The unified inbox** pulls every connected mailbox and its replies into one view.
- **A built-in CRM** tracks contacts, pipelines, deals, tasks, and meetings.
- **Deliverability** surfaces bounces, complaints, suppression, and inbox placement.
- **Automations** run branching reply playbooks on a visual canvas.
- **Warmup** builds real sender reputation through our pool, covered next.

The same code runs on a single VPS or across a fleet of cheap servers with many
IPs per box, so you add capacity by adding machines.

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

Run the whole stack with Docker:

```bash
git clone https://github.com/warmbly/warmbly && cd warmbly
make up
```

That starts every service against local infra. The dashboard comes up at
`http://localhost:5173`.

For day-to-day development, skip the Docker app images and run the Go services on
the host instead. It recompiles in a second or two on save:

```bash
make infra   # backing services in Docker, run once and leave up
make run     # backend, consumer, and worker in one terminal
make web     # dashboard dev server
```

The first admin account cannot be created from the UI. Sign up through the
dashboard, then promote yourself from the host with
`make grant-admin EMAIL=you@example.com` and open the admin app with `make admin`.
Full local setup, seeding, and troubleshooting live in
[docs/development/local-development.md](docs/development/local-development.md).

## How it works

Warmbly is split into a control plane and an execution plane.

The control plane is the backend API, the event consumer, Postgres, Redis, and
the event bus. It owns every piece of stateful data and decides what gets sent
and from where.

The execution plane is the worker fleet: one Go binary per machine, one worker
process per IP. Workers take commands off the event bus, fetch their encryption
keys over HTTPS, send and sync mail, and report telemetry back. **Workers never
connect to Postgres.** Each one is a sending identity rather than a database
client, so outbound volume spreads across many IPs instead of piling up in a
single runtime. Reputation is tracked per IP.

Secrets use envelope encryption: a per-organization data key, wrapped by KMS, is
what seals mailbox credentials and message content. The full write-up is in
[docs/development/architecture.md](docs/development/architecture.md).

## Self-hosting

Every external dependency has an open-source path, picked with an environment
variable, so a self-hosted install can run without any cloud account.

| Concern        | Self-host default         | Cloud option            |
|----------------|---------------------------|-------------------------|
| Database       | PostgreSQL 16             | RDS / Cloud SQL         |
| Cache          | Redis (or Valkey)         | ElastiCache             |
| Event bus      | NATS JetStream (1 binary) | Kafka, MSK              |
| Blob storage   | Filesystem                | S3, MinIO, R2, B2       |
| KMS / root key | Local AES master key      | AWS KMS, Vault, GCP     |
| Codec          | JSON                      | Avro + Schema Registry  |
| Captcha        | Bypass token (trusted)    | Cloudflare Turnstile    |
| Payments       | Off                       | Stripe                  |

One machine with several attached IPs becomes several sending identities in a
single command. Each IP gets its own systemd unit and a stable identity, so
reputation survives reinstalls:

```bash
sudo ./scripts/install-worker.sh \
  --kafka kafka.yourdomain.com:9092 \
  --redis redis://cache.yourdomain.com:6379 \
  --ips 5.6.7.11,5.6.7.12,5.6.7.13
```

Production deployment, the full env reference, and day-2 operations are in
[docs/development/deployment-guide.md](docs/development/deployment-guide.md).

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

| Doc | What it covers |
|-----|----------------|
| [docs/development/architecture.md](docs/development/architecture.md) | Control plane vs execution plane, encryption model |
| [docs/development/local-development.md](docs/development/local-development.md) | Make targets, native services, seeding |
| [docs/development/deployment-guide.md](docs/development/deployment-guide.md) | Production control plane and worker fleet |
| [docs/development/Events.md](docs/development/Events.md) | Event bus reference |
| [docs.warmbly.com](https://docs.warmbly.com) | Product guides and public API reference |

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
