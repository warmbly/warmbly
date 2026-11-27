# Control plane infrastructure

Use separate Terraform roots and state keys for each provider. AWS provisioning
can proceed independently of PlanetScale operator authentication. The hosted
deployment's event bus is the private NATS cluster (`aws/nats`); Confluent
(Kafka and Schema Registry) was retired on 2026-11-25.

| Root | Scope | State |
| --- | --- | --- |
| `aws/bootstrap` | Protected state bucket | Private bootstrap state outside the checkout |
| `aws/foundation` | Network, ECS cluster, repositories, logs, certificates, PrivateLink, Valkey, configuration stores | `warmbly/production/foundation.tfstate` |
| `aws/runtime` | Application task roles, ECS services, HTTPS ingress and dependency checks | `warmbly/production/runtime.tfstate` |
| `aws/workers` | ARM EC2 worker hosts, static IPs, encrypted disks and configuration containers | `warmbly/production/workers.tfstate` |
| `planetscale` | Existing Postgres branch, with a database metadata lookup | `warmbly/production/planetscale.tfstate` |

The remote states use the bootstrap bucket with encryption, versioning and
S3 locking. Provider lock files belong in source control. State, saved plans,
credentials and installation-specific tfvars belong outside the checkout.

## Adopt existing data services

Initialize each root with `-backend-config="bucket=$STATE_BUCKET"`. The
PlanetScale root contains import blocks and require the actual
identifiers and configuration of the existing resources. Supply those values in
private tfvars files.

Authenticate PlanetScale with `PLANETSCALE_SERVICE_TOKEN_ID` and
`PLANETSCALE_SERVICE_TOKEN`. Supply operator
credentials through the process environment from a protected source. Keep them
separate from the application credentials and out of Terraform input variables.
Short-lived, read-scoped credentials are sufficient for the initial adoption.

Review the adoption plan before applying it. Its managed resources must show
imports only, with zero creates, updates, replacements or deletes. Correct any
configuration mismatch before applying. Apply the saved plan, then verify a
subsequent plan reports no changes. Imported resources have `prevent_destroy`.

Terraform manages configuration metadata. SQL rows and migrations, NATS
messages and consumer state, access rules and existing application credentials retain their current owners.
Bring additional resource types under Terraform only after checking their
current settings and ownership. Resource import does not copy or restore data.

The PlanetScale provider currently exposes Postgres databases as data sources
and branches as managed resources. This root therefore adopts the production
branch and reads database metadata. PostgreSQL parameter overrides retain their
existing owner through `ignore_changes = [parameters]`.

## Configuration and credentials

Use Standard String parameters under `/<name>/config/<service>/<VARIABLE>` for
approved non-secret environment values. The AWS root validates parameter names
against an allowlist and rejects credential-bearing URLs. Connection strings,
passwords, tokens and encryption material belong in Secrets Manager.

Service secret containers hold JSON configuration supplied outside Terraform.
Preserved source values can carry a `MIGRATION_SOURCE` version label. The
operator-only rotation secret holds replacement material under `AWSPENDING`.
These labels describe preparation; workload configuration and the deployed task
revision determine which values an application actually uses.

The Valkey password lives in its own secret. Terraform retrieves it through an
ephemeral secret-version resource and passes it to `auth_token_wo`, so the
password is omitted from plans and state. The `auth_token_wo_version` field is
the deliberate update trigger for later password changes.

ECS task definitions should inject parameters and service secrets through
execution-role access scoped to their ARNs. Injected values are read on task
startup; changing a stored value does not refresh a running task. S3, KMS and SES
calls should use task roles.

## Coordinate credential changes

Prepare a dependency map for every value before changing it. Record the writer,
all readers, encrypted data dependencies, replacement procedure and rollback.
Preserving current values in a new store is separate from rotating them.

| Material | Coordination |
| --- | --- |
| Auth signing key | Backend, consumer and realtime share the signing value. Replacing it invalidates existing tokens. |
| MFA sealing key | Preserve enrollment readability. When `TWOFA_SECRET` is unset, the sealing key derives from `AUTH_SECRET`. |
| Mailbox encryption key | Re-encrypt affected stored credentials and coordinate every worker before replacing it. |
| Organization KMS key | Preserve access to existing wrapped DEKs. IAM access changes and KMS key-material rotation are separate operations. |
| Internal and broker tokens | Update both ends of internal APIs, including forms, tracking and external workers. |
| NATS credentials | Create replacements, validate permissions, update every publisher and subscriber, then retire old access. |
| OAuth and integration secrets | Coordinate provider-side credentials, backend and worker token refresh paths, and any callbacks. |
| Valkey password and endpoint | Update every cache client together and verify shared-state behavior before draining the old cache. |

Re-encrypting stored values under a new key does not revoke the upstream
credentials represented by those values. Treat those as separate operations
when both are required. A rollback must use valid credentials and the keys that
match its data, rather than restoring an incompatible configuration bundle.

## Networking

PlanetScale uses a same-region interface endpoint and its Private Host with TLS
verification. The cache uses private subnets and permits access from the app
security group. Local tools and external workers need a suitable private network
path before they can use those endpoints.

NATS runs in private subnets with TLS (`aws/nats`). The S3 gateway endpoint provides a
private route to S3. Other dependencies still require a working egress path.

See [AWS provisioning](aws/README.md) for the foundation and cache setup.
See [application runtime](aws/runtime/README.md) and
[worker hosts](aws/workers/README.md) for the remaining deployment roots.
