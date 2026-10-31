# Application runtime

This root reads the foundation state and creates application services in
`us-east-1`. Keep installation-specific inputs outside the checkout. Supply
digest-pinned ARM64 images, valid Fargate sizes, desired task counts, secret key
names and immutable secret version IDs. Secret values never enter Terraform.

```sh
terraform -chdir=deploy/terraform/aws/runtime init \
  -backend-config="bucket=$STATE_BUCKET"
terraform -chdir=deploy/terraform/aws/runtime plan \
  -var-file=/private/path/runtime.tfvars.json \
  -out=/private/path/runtime.tfplan
terraform -chdir=deploy/terraform/aws/runtime apply /private/path/runtime.tfplan
```

Start at desired count zero. Separate execution roles retrieve each service's
configuration and write its logs. Application roles receive reviewed existing
permissions or IAM Policy Autopilot output through `application_policies`.

## Deploy releases

Terraform owns each task definition's shape: environment, secret references,
sizes, health checks and the `warmbly-image-variant` tag that says whether a
service runs the default or the `-kafka` image. Service revision and desired
count are deployment operations; Terraform ignores those two fields after
service creation, and its task definition outputs describe prepared revisions,
not necessarily running ones. Start new services at zero and use explicit counts
when activating them.

`.github/workflows/deploy-aws.yml` owns only the image. A stable release tag on
`main` reaches it from `release.yml` when the repository variable
`AWS_DEPLOY_ENABLED` is `true`; dispatch it by hand to redeploy, roll back or
roll out changed secret values. Each run verifies the release's build
provenance, renders every service from its latest Terraform revision with the
release's digests, runs the dependency validation and migration tasks, then
rolls backend, consumer, and forms, tracking and realtime in waves. Each service
is watched against its CloudWatch alarms for a bake after it is stable, and any
failure restores every service already moved, in reverse. Workers and consumers
on hosts follow through a canary rollout started from the `ops` task. The
procedure, modes and one-time GitHub setup are in
`docs/content/docs/development/aws-fargate.mdx`.

Set `github_repository` to the repository running the workflows. The root
creates an OIDC provider and a deployment role that can register task
definition revisions, run the validation, migration, `ops` and rotator tasks,
update the five services and read secret metadata, never a secret value. Import
an existing account-wide GitHub OIDC provider instead of creating a second one.
Set the `aws-production` environment's variables:

| Variable | Value |
| --- | --- |
| `AWS_DEPLOY_ROLE_ARN` | Terraform `github_deploy_role_arn` output |
| `AWS_ECS_CLUSTER` | Runtime `cluster_arn` output |
| `AWS_ECS_SERVICE_PREFIX` | Runtime `name` input |

Prepare a backend secret version with `PRIMARY_DB_DIRECT`, pointing to port 5432.
The migration task injects only that key, with no application task role. The
workflow never retrieves database credentials. Its backend and migration tasks
must use the same immutable image digest and direct secret reference. Keep the
default `migration_secret_key=PRIMARY_DB_DIRECT` for workflow deployments.

The migration runs `/app/migrate` inside the VPC over the existing PrivateLink
route, and an unsuccessful or unresolved migration blocks every service update.
A rollback skips it, because schemas stay compatible with the previous release.
The workflow never runs down migrations. Task definitions use `skip_destroy` so
earlier revisions remain active for rollback. Retain their referenced images and
secret versions throughout the rollback window; retire them separately after it
closes.

## TLS behind the load balancer

`target_tls` moves the load balancer's target groups and the forms and tracking
services' internal calls to the backend onto TLS, in four stages (`off`,
`serve`, `both`, `on`) advanced one apply at a time. The backend serves the
certificate in `backend_internal_certificate` with `SERVE_TLS_KEY` from its
secret, and its callers trust only that certificate. Prerequisites, the stages
and what each one changes are in `aws-fargate.mdx` under "Encrypt traffic inside
the VPC". `terraform test` in this directory exercises every stage against a
mocked provider.

## Rotate keys

`rotation.tf` registers a rotator task that generates replacement values with
its own narrow role, writes them as new secret versions and reports only
version ids. `.github/workflows/rotate-keys.yml` drives it, rolls services onto
those versions and runs `warmblyctl` in the `ops` task to prove each step before
the old value is retired. No value passes through GitHub or a log. The runbook
is in `aws-fargate.mdx`.

## Terraform in CI

Set `terraform_ci = true` to create the `<name>-terraform-plan` and
`<name>-terraform-apply` roles `.github/workflows/terraform-aws.yml` uses. The
plan role reads configuration and is denied secret values, decryption, logs and
data reads; the apply role administers resources named for this deployment and
is reachable only from `main` through protected environments. The apply role
cannot change the two CI roles themselves, so a change to their policies or
trust is applied by an administrator by hand. Var files stay in
the state bucket under `warmbly/production/vars/`; only resource addresses and
actions are ever printed.

The runtime sets `DB_MIGRATIONS_ON_START=false` for the backend. New application
images must support that setting and schema readiness before activation. Keep
migrations additive and compatible with the previous release throughout rollout
and rollback. Do not interrupt an active migration; if a workflow is cancelled or
times out, inspect the ECS task before rerunning it.

Run `database_check_task_definition` to exercise the backend image's actual Go
client against its configured pooler without starting the API or background jobs.
It collects operator status into temporary task storage and logs only completion.
The separate dependency validation task checks provider connectivity and stored
credential decryption. Neither check activates the backend service.

Tasks use public IPs for outbound access, avoiding NAT gateway charges. Inbound
application traffic is restricted to the HTTPS load balancer. The backend also
accepts private task traffic through service discovery. Restrict the load
balancer to approved caller CIDRs, such as Cloudflare's published ranges. Public
DNS changes are separate from deploying the services.

Run `validation_task_definition` once in a public subnet with a public IP and the
task security group. It checks private database DNS and logins, stored mailbox
and MFA decryption, KMS, both caches, Kafka metadata, Schema Registry and S3 using
the actual backend task role. It writes no production data, logs only counts and
results, and exits. Observe its stopped exit code and logs before activation.
The pinned validation packages are installed in temporary task storage.

Keep one cache authoritative throughout deployment overlap. A backend starts
background jobs immediately; its desired count is the
activation control. Preserve compatible images, encryption material and shared
coordination while the previous deployment drains. Verify target health and
HTTPS routes before switching DNS. New injected values require replacing tasks.
