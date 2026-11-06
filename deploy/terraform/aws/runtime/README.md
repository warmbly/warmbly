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
when activating them. Once a service is running, `autoscaling` (in
`autoscaling.tf`) moves each listed service between its bounds: the consumer
on its worker-event backlog, other listed services on CPU. A service left out
of the map keeps its count.

`.github/workflows/deploy-aws.yml` selects the image and preserves the live managed
attachment-download setting unless Terraform explicitly supplies its environment
value. The opt-in `.github/workflows/configure-backend-aws.yml` changes only
`CAMPAIGN_ATTACHMENT_DOWNLOAD_PROXY` from a verified live backend definition,
using release provenance, preflight task identities checked against the service
count and deployment, a fixed 300-second bake and verified revision/count rollback.
Its `warmbly-config-attachment-download-proxy` task tag records
ownership across later deploys and secret refreshes. Explicit Terraform `true` or
`false` takes ownership; a rollback below v0.6.75 requires disabling the proxy
first and waiting for existing links to expire. The operator collects current
task ARNs through normal read-only inventory access and supplies them to the
workflow, which uses existing deployment permissions. After the bake, independently
inspect every new running task and the actual private-download response before
claiming end-to-end verification. No additional IAM policy is provisioned.

A stable release tag on
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
services' internal calls to the backend onto TLS, in six stages (`off`,
`serve`, `both`, `route`, `drain`, `on`) advanced one apply at a time. Route keeps
the healthy HTTP path while HTTPS bakes; drain removes references and waits for
registrations to empty before a separately approved cleanup. The backend serves the
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

## Isolated origin ALB policy configuration

`configure-origin-alb-aws.yml` implements the ordinary in-place-update category
for the hosted origin HTTPS listener. It uses `aws-infrastructure`, its existing
main branch restriction, the existing Terraform apply OIDC role, and the shared
`aws-production` queue. Environment protection settings and the general Terraform
workflow remain unchanged. This workflow does not enable the general stack
workflow or perform a service or fleet deployment.

Set the existing nonsecret environment variables `AWS_TERRAFORM_APPLY_ROLE_ARN`
and `AWS_TERRAFORM_STATE_BUCKET` in `aws-infrastructure` through normal operator
configuration. Missing values stop the run. There is no fallback to another
environment. The existing role is intersected with a session policy: plan mode
can describe infrastructure and create private evidence; apply and rollback add
only `ModifyListener` on the bound listener and versioned reads of the selected
plan. Missing permissions require the normal IAM approval process, never an
automatic policy expansion. No secret value or Terraform variable file is read.

1. Merge the reviewed source, then dispatch on `main` with `mode=plan` (the
   default) and leave the three plan selectors empty. This is a dry readback and
   plan, with no infrastructure configuration write.
2. Privately review the complete `plan.json` in the state bucket under
   `warmbly/production/diagnostics/origin-alb-policy/<run-id>/1/`. The summary
   reports its exact version ID and SHA-256. The plan binds the workflow source,
   origin identity and complete before/after snapshots. Only the bound HTTPS
   listener's `SslPolicy` may change from
   `ELBSecurityPolicy-TLS13-1-2-2021-06` to
   `ELBSecurityPolicy-TLS13-1-2-Res-2021-06`.
3. Dispatch `mode=apply` on the same main source with `plan_run_id`,
   `plan_version` and `plan_sha256` from that review. The complete plan must be
   no more than 24 hours old. A moved main source, changed policy baseline or any
   other origin configuration drift requires a new plan and review.
4. Inspect the private `intent.json` and `result.json` for the apply run. The
   intent must be archived before the write. Every object uses conditional
   creation, SHA-256, AES256 encryption and a non-null S3 version ID. Raw
   snapshots stay in the private bucket and runner directory, never public logs
   or Actions artifacts. Listener certificates, mutual authentication, listener
   and ALB attributes, rules, target group configuration and security groups
   must match the plan. Routed HTTPS targets must be healthy before and after.
5. Independently check public application health and retain certificate-verified
   TLS 1.2 AEAD and TLS 1.3 origin handshakes plus CBC-only refusal from an
   authorized origin-access network. An edge handshake or a timeout does not
   establish origin policy behavior.

For scoped rollback within the same 24-hour plan window, dispatch
`mode=rollback` with the same reviewed plan selectors and workflow source.
The entire live origin must match that plan's desired snapshot before only
`SslPolicy` is restored to its recorded baseline. Rollback verifies the complete
original snapshot and HTTPS target health. It does not restore rules, certificates,
target groups, security groups or application state. Any drift stops it for
operator review. Failed writes and readbacks produce private failure evidence;
they do not trigger an automatic rollback or retry. Rerunning a workflow attempt
is refused; inspect the evidence before a new dispatch. This configuration path
supports the current and previous application releases without changing their
data, task definitions, credentials or protocols. Terraform's declared desired
policy remains the restricted policy; a scoped rollback must be reconciled
through a fresh reviewed configuration plan before a later stack apply.

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

`tls_recovery_evidence_writer` is a separate hosted-only opt-in, defaulting to
`false`. It creates the dedicated `warmbly-production-tls-recovery-evidence` role
and one conditional private S3 upload policy, with no read, delete or deployment
access. It requires the production account, bucket and name, the Warmbly
repository and `terraform_ci`. Every managed change to this role or its policy
requires `aws-infrastructure-approval`, including creation. Its optional output
is `tls_recovery_evidence_role_arn`. Provisioning does not enable recovery; the
runbook in `aws-fargate.mdx` describes the separate configuration and review.

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

## Disposable origin TLS diagnostic

`probe-origin-tls-aws.yml` verifies the hosted origin's TLS negotiation from the
existing application task security group. It uses `aws-infrastructure`, its main
branch restriction, `AWS_TERRAFORM_APPLY_ROLE_ARN`,
`AWS_TERRAFORM_STATE_BUCKET`, and the shared `aws-production` queue. It makes no
listener, service, security group, IAM or Terraform changes. Existing deployment
and self-host configuration remain compatible; no application release is needed.

The dedicated Fargate definition has no task role, secrets, repository credentials,
mounts or ports. It uses the existing validation execution role and validation
log group, without running the dependency validation definition. The client is
the official Docker Library Node 22 bookworm ARM64 image at
`sha256:a288699fc0f9676950742fae6efcd015180aa7ef2bf8a6477e4b0a8cc0ab1b3f`
(official manifest from `node:22-bookworm`, source revision
`80f02a542da739e6a662451d1db66456a0dea253`). OpenSSL and the Debian public CA bundle
must be present. Nothing is installed at runtime. The source and image pin pass
through normal review; there is no image or command override input.

Operator procedure:

1. Merge the reviewed source normally. Complete the separate normal origin ALB
   policy procedure first. The diagnostic requires the bound HTTPS listener's
   `ELBSecurityPolicy-TLS13-1-2-Res-2021-06` readback.
2. Confirm the existing infrastructure OIDC role can register/list/deregister
   this run's `warmbly-production-origin-tls-<run-id>` task family; run one task
   in `warmbly-production`; list/describe/stop its tasks; and pass only
   `warmbly-production-validation-execution` to ECS. It also needs the ALB,
   listener, network interface, subnet and backend service describe calls in
   `.github/scripts/aws-origin-tls.cjs`, plus `logs:GetLogEvents` on
   `/ecs/warmbly-production/validation` and conditional `s3:PutObject` on
   `warmbly/production/diagnostics/origin-tls/<run-id>/1/*` in the private state
   bucket. The workflow's phase-specific sessions only restrict existing
   permissions.
   Missing access requires the normal IAM process; the workflow never expands
   a role or switches environments. Read-only TLS metadata does not require
   access to secret values, SSM parameters or application task credentials.
3. The existing execution role must already support logging to that group. The
   existing application subnets must support Fargate ARM64 platform `1.4.0`,
   public image pulls and CloudWatch Logs, with the existing application task
   security group reaching the ALB private interfaces on TCP 443. The workflow
   reuses the backend's existing public-subnet network configuration. It does
   not add a network path or enable ECS Exec.
4. Dispatch exactly once on merged `main`:
   `gh workflow run probe-origin-tls-aws.yml --repo warmbly/warmbly --ref main`.
   Apply the environment's normal protection/reviewer process. Reruns are
   refused; investigate the private result before starting a new dispatch.
5. Read the run summary for the private result's exact S3 version ID and
   SHA-256. Retrieve `intent.json` and `result.json` privately from
   `warmbly/production/diagnostics/origin-tls/<run-id>/1/`. Both use conditional
   creation, AES256 encryption, SHA-256 and a non-null version ID. Do not attach
   raw output to a public issue, PR or Actions artifact. The result retains
   UTC times, exact ALB/ENI/AZ identities, image and source identities, task
   metadata, client version, arguments, exit status, stdout, stderr and peer
   certificate chains. Success requires the complete raw handshake evidence,
   exact task image digest and confirmed diagnostic cleanup.

Probe credentials permit discovery, one registration and launch, task readback
and private intent archival. Cleanup uses a separate OIDC session acquired
immediately before the finalizer, with only task/definition discovery, task
readback and stop, deregistration, exact-run log retrieval and private archival.
It cannot register definitions, launch tasks or pass roles. AWS requires
`ecs:DeregisterTaskDefinition` on `Resource: "*"`; the application validates
every deregistration candidate against the exact account, region and run family
with a numeric revision. The Logs ARN names only this run's streams.
Both credential steps clear existing credentials before acquisition. Cleanup
credential acquisition runs after probe failure, and the finalizer requires its
success. Credential failures fail the workflow without reusing an earlier session.

The transport targets are freshly described private interfaces of the exact
ALB, never the public API DNS address. The SNI and verified hostname stay
`api.warmbly.com`; the client trusts its system public CAs. Each interface gets
both TLS 1.2 RSA AES-GCM positives and a TLS 1.3 AES-GCM positive, then separate
CBC-only TLS 1.2 offers, then fresh TLS 1.2 and TLS 1.3 positives. Only explicit
peer handshake-failure alerts without a negotiated cipher, bracketed by those
trusted positives, establish refusal. A negative connection that negotiates a
cipher remains accepted if its peer keeps it open until the process deadline;
that outcome fails the diagnostic. Timeout without negotiation, reset alone, unavailable local
ciphers, missing CA, certificate errors or incomplete evidence are inconclusive.
Negative offers ignore stdin EOF and retain bounded OpenSSL state and message
traces. Refusal requires the transmitted TLS 1.2 ClientHello to contain only the
intended CBC suite (plus optional renegotiation signalling) followed by an inbound
fatal `handshake_failure` alert with matching bytes, no certificate and no cipher.
`close_notify` and bare EOF remain inconclusive. Session summaries and ticket
message bodies are omitted from new logs; certificate and verification evidence
is retained, and session tickets are disabled in the client.
The task sends TLS handshakes only, with empty stdin and no HTTP or authentication.

Each OpenSSL process has a 12-second kill bound, and the container command has a
210-second hard kill bound after it starts. The probe step is limited to ten
minutes. An `always()` final step has eight minutes to recover lost task/definition
responses, verify task cluster, `startedBy` and exact definition before stopping
tasks or collecting their logs, confirm they stopped,
deregister only this run's diagnostic definitions, and privately archive logs and
cleanup results. Recovery lists running tasks by this run's `startedBy` and
stopped tasks by the exact run-specific family with `desiredStatus=STOPPED`.
The `startedBy` query has no additional filter. An ambiguous launch with no
verified task identity remains unresolved and does not confirm stopped cleanup.
Cleanup failure fails the workflow, even after successful
handshakes. Cancellation, runner loss or AWS API/access failures cannot guarantee
control-plane cleanup; the in-container limit still bounds a running client.
If the finalizer cannot complete, use the archived diagnostic ARNs or those
running/stopped task queries and list definitions by the exact family. Describe
each task and verify its cluster, `startedBy` and exact run definition before
stopping it or retrieving logs. Confirm STOPPED and deregister only those
resources through normal operator access. Do not delete log groups, ENIs,
security groups or application resources.
