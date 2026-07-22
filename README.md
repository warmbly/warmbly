<p align="center">
  <img src="docs/assets/logo.svg" alt="Warmbly" width="420" />
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

Warmbly is a cold outreach platform you run yourself: your sending IPs, your
Postgres, your servers, nothing tied to a vendor's database. Connect your
mailboxes, write sequenced campaigns, and it sends the mail, tracks the replies,
and keeps your sender reputation healthy.

https://github.com/user-attachments/assets/378a510a-bb99-425f-925e-04300184938b

Everything a sending team needs sits in one dashboard, collaborative in real time
so teammates see each other's presence and edits with no refresh:

- **Campaigns** send multi-step sequences with per-mailbox daily caps and spacing.
- **The unified inbox** pulls every connected mailbox and its replies into one view.
- **A built-in CRM** tracks contacts, pipelines, deals, tasks, and meetings.
- **Deliverability** surfaces bounces, complaints, suppression, and inbox placement.
- **Automations** run branching reply playbooks on a visual canvas, with AI steps that read a reply and act on it.
- **Integrations** sync the CRM and automations out to HubSpot, Slack, and more.
- **Warmup** builds real sender reputation through a pool of monitored mailboxes.

The same code runs on a single VPS or across a fleet of cheap servers, so you add
capacity by adding machines.

## Star the repository ⭐

<img width="1280" height="720" alt="warmbly-star" src="https://github.com/user-attachments/assets/c9bd34f7-c384-4f10-91e4-215fcea09986" />

## Connect your mailboxes

Connect Gmail and Google Workspace with one-click OAuth, Microsoft 365 over its
own OAuth, or any other provider (Zoho, Fastmail, a self-hosted server) over
SMTP + IMAP. Each mailbox warms, sends, and syncs on its own, with its own daily
cap and spacing, and credentials sealed with per-organization envelope
encryption. See [Mailboxes](https://docs.warmbly.com/guides/mailboxes/).

## Integrations

Automations and the built-in CRM connect out to the tools you already run: ping
Slack on a positive reply, push a won deal to your CRM, or book meetings straight
from replies.

| Category      | Providers                              |
|---------------|----------------------------------------|
| CRM           | HubSpot, Salesforce, Pipedrive, Close  |
| Automation    | Zapier, Make, n8n                      |
| Notifications | Slack, Discord                         |
| Meetings      | Calendly, Cal.com                      |
| Data          | Google Sheets                          |

Everything is also reachable through a scoped REST API, HMAC-signed webhooks, and
a realtime WebSocket. See the [API reference](https://docs.warmbly.com/api/).

## Warmup

Warmup only produces real results with a pool of real mailboxes warming against
each other, and Warmbly maintains that pool. Volume starts low and ramps
gradually per mailbox, replies happen at a natural rate, and mailboxes that show
spam patterns or forged tokens are scored and auto-blocked so the pool stays
clean for everyone. See [Warmup](https://docs.warmbly.com/guides/warmup/).

## Quick start

You need Docker, Go 1.25, and pnpm.

```bash
git clone https://github.com/warmbly/warmbly && cd warmbly
make dev
```

One command brings up the backing services in Docker, applies migrations, seeds
demo data, and starts the backend, worker, and dashboard natively. Open
`http://localhost:5173` and log in with `dev@warmbly.com` / `password123`.
Full setup, seeding, and troubleshooting live in the
[local development guide](https://docs.warmbly.com/development/local-development/).

## How it works

Warmbly splits into a control plane (backend API, consumer, Postgres, Redis, and
the event bus) that owns every piece of state, and an execution plane of
interchangeable Go workers that send and sync mail. **Workers never connect to
Postgres**, and outbound mail leaves through each mailbox's own provider (Gmail
API, Microsoft Graph, or SMTP relay), not the worker's IP, so you add throughput
by running more workers. Secrets use envelope encryption, with a local AES master
key by default or AWS KMS if you prefer. Full write-up in the
[architecture docs](https://docs.warmbly.com/development/architecture/).

## Self-hosting

Warmbly runs with **no cloud account of any kind**: no AWS, no GCP, no Stripe, no
Kafka. One command brings up the whole platform on local, open-source pieces:

```bash
git clone https://github.com/warmbly/warmbly && cd warmbly
make up               # or: docker compose up --build
```

Dashboard on `:5173`, admin on `:5174`, API on `:8080`. Every external dependency
is picked by an environment variable, so you swap in a cloud service only if you
want one:

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

Scaling is by mailboxes and workers, not IPs: add throughput by connecting more
mailboxes and running more workers. Reaching it from another machine, connecting
Gmail mailboxes, the secrets you must set, and day-2 operations are all in the
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
