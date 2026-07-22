<div align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="docs/assets/logo-light.svg">
    <img src="docs/assets/logo-dark.svg" alt="Warmbly" width="360">
  </picture>
  <br />
  <br />

  <p>The open-source agentic cold email and warmup platform.</p>
  <br />

  <p>
    <a href="https://dc.warmbly.com"><img src="https://img.shields.io/badge/Discord-5865F2?logo=discord&logoColor=white&style=flat-square" alt="Discord" /></a>
    <a href="https://docs.warmbly.com"><img src="https://img.shields.io/badge/Docs-1f6feb?style=flat-square" alt="Docs" /></a>
    <a href="https://github.com/warmbly/warmbly/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/warmbly/warmbly/ci.yml?branch=main&style=flat-square&label=CI" alt="CI status" /></a>
    <a href="https://github.com/warmbly/warmbly/releases"><img src="https://img.shields.io/github/v/release/warmbly/warmbly?style=flat-square" alt="Latest release" /></a>
    <a href="./LICENSE"><img src="https://img.shields.io/github/license/warmbly/warmbly?style=flat-square" alt="License" /></a>
  </p>

  <p>
    <a href="#features">Features</a> ·
    <a href="#how-it-works">How it works</a> ·
    <a href="#quick-start">Quick start</a> ·
    <a href="#self-hosting">Self-hosting</a> ·
    <a href="#documentation">Docs</a> ·
    <a href="#community">Community</a>
  </p>
  <br />
  <h3>⭐ Help us reach more senders and grow the Warmbly community. Star this repo!</h3>
  <br />
  <img width="640" alt="Star Warmbly on GitHub" src="https://github.com/user-attachments/assets/c9bd34f7-c384-4f10-91e4-215fcea09986" />
</div>

## Warmbly

Warmbly runs cold email campaigns from the mailboxes you already own and warms
them so they keep landing in the inbox. Opens, clicks, and replies land in a
shared dashboard the moment they happen, and it's AI-native, so your team and its
agents work in it together, live.

https://github.com/user-attachments/assets/378a510a-bb99-425f-925e-04300184938b

## Features

- **Campaigns** - multi-step sequences with per-mailbox caps and spacing
- **Unified inbox** - every mailbox and reply in one place
- **CRM** - contacts, pipelines, deals, tasks, meetings
- **Warmup** - a pool of monitored mailboxes, not throwaway accounts
- **Deliverability** - bounces, complaints, suppression, inbox placement
- **Automations** - visual reply playbooks with AI steps
- **Integrations** - HubSpot, Slack, Zapier, REST API, webhooks
- **Realtime** - live presence and edits across your team

<p align="center">
  <img src="docs/assets/dashboard-campaigns.png" alt="Campaigns" width="49%" />
  <img src="docs/assets/dashboard-inbox.png" alt="Unified inbox" width="49%" />
</p>

## How it works

Warmbly splits into a **control plane** (backend API, consumer, Postgres, Redis,
and the event bus) that owns all state, and an **execution plane** of
interchangeable Go workers that send and sync mail. Workers never touch Postgres,
and outbound mail leaves through each mailbox's own provider, not the worker's IP,
so you add throughput by running more workers.

```mermaid
flowchart LR
  MB["Your mailboxes"] --> API
  subgraph CP["Control plane"]
    direction TB
    API["Backend API"] --> DB[("Postgres")]
    API --> BUS{{"Event bus"}}
  end
  BUS --> W1["Worker"]
  BUS --> W2["Worker"]
  BUS --> W3["Worker"]
  W1 --> P["Gmail · Microsoft · SMTP"]
  W2 --> P
  W3 --> P
  P --> R["Recipients"]
```

Secrets use envelope encryption, with a local AES master key by default or AWS KMS
if you prefer. Full write-up in the
[architecture docs](https://docs.warmbly.com/development/architecture/).

## Quick start

You need Docker, Go 1.25, and pnpm.

```bash
git clone https://github.com/warmbly/warmbly && cd warmbly
make dev
```

One command brings up the backing services in Docker, applies migrations, seeds
demo data, and starts the backend, worker, and dashboard natively. Open
`http://localhost:5173` and log in with `dev@warmbly.com` / `password123`. Full
setup lives in the
[local development guide](https://docs.warmbly.com/development/local-development/).

## Self-hosting

Warmbly runs with **no cloud account of any kind**: no AWS, no GCP, no Stripe, no
Kafka. One command brings up the whole platform on local, open-source pieces:

```bash
git clone https://github.com/warmbly/warmbly && cd warmbly
make up               # or: docker compose up --build
```

Every external dependency is picked by an environment variable, so you swap in a
cloud service only if you want one:

| Concern        | Self-host default          | Optional / cloud             |
|----------------|----------------------------|------------------------------|
| Database       | PostgreSQL 16              | RDS / Cloud SQL, any Postgres |
| Cache          | Redis (or Valkey)         | ElastiCache                  |
| Event bus      | **NATS JetStream**        | Kafka (`-tags kafka`)        |
| Blob storage   | **Filesystem**            | S3, MinIO, R2, B2            |
| KMS / root key | **Local AES master key**  | AWS KMS                      |
| Payments       | **Off (everything unlocked)** | Stripe                   |

Scaling is by mailboxes and workers, not IPs. Reaching it from another machine,
connecting Gmail, and day-2 operations are all in the
[deployment guide](https://docs.warmbly.com/development/deployment-guide/).

## Documentation

The full docs live at **[docs.warmbly.com](https://docs.warmbly.com)**.

| Read this | To learn |
|-----------|----------|
| [Local development](https://docs.warmbly.com/development/local-development/) | Every make target, the native services, and how seeding works |
| [Architecture](https://docs.warmbly.com/development/architecture/) | How the control plane and workers split the job, plus the encryption model |
| [Deployment guide](https://docs.warmbly.com/development/deployment-guide/) | Taking it to production and scaling the worker fleet |
| [API reference](https://docs.warmbly.com/api/) | Endpoints, auth, permissions, and webhooks |

## Community

Have a question, found a bug, or want to shape where Warmbly goes next?

- **[Discord](https://dc.warmbly.com)** - chat with the team and other senders
- **[GitHub Issues](https://github.com/warmbly/warmbly/issues)** - report bugs and request features
- **Email** - reach us at `team@warmbly.com`

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
