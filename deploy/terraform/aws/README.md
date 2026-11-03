# AWS foundation

Terraform provisions the control plane's foundation in `us-east-1`. The existing
Postgres database, Kafka cluster, object bucket and KMS key remain external to
this state. The sibling PlanetScale and Confluent roots adopt their existing
configuration. This root does not deploy application services or change
production traffic. A standalone cache-check task definition supports verification.

## Bootstrap state storage

Use an AWS operator identity with temporary credentials when available. Keep
bootstrap state and saved plans outside the checkout in a private directory.

```sh
mkdir -p "$HOME/.local/state/warmbly-aws/bootstrap"
chmod 700 "$HOME/.local/state/warmbly-aws"
terraform -chdir=deploy/terraform/aws/bootstrap init
terraform -chdir=deploy/terraform/aws/bootstrap plan \
  -state="$HOME/.local/state/warmbly-aws/bootstrap/terraform.tfstate" \
  -out="$HOME/.local/state/warmbly-aws/bootstrap/create.tfplan"
terraform -chdir=deploy/terraform/aws/bootstrap apply \
  "$HOME/.local/state/warmbly-aws/bootstrap/create.tfplan"
```

The bucket has encryption, versioning, public access blocking, TLS enforcement
and a destruction guard. Keep a protected backup of bootstrap state. Subsequent
bootstrap plans must use that same state path. The foundation uses S3 state with
native locking; it needs no DynamoDB table.

## Provision the foundation

Use the bootstrap output as `STATE_BUCKET`. Set `planetscale_service_name` to the
existing branch's Private Service Name. An empty value defers the endpoint.
Keep installation-specific values in an untracked tfvars file outside the repo.

```sh
terraform -chdir=deploy/terraform/aws/foundation init \
  -backend-config="bucket=$STATE_BUCKET"
terraform -chdir=deploy/terraform/aws/foundation plan \
  -var-file=/private/path/foundation.tfvars \
  -out=/private/path/foundation.tfplan
terraform -chdir=deploy/terraform/aws/foundation apply \
  /private/path/foundation.tfplan
```

Resources:

- A dedicated VPC with public and isolated private subnets in two zones, and an
  Amazon-provided IPv6 block that the optional autoscaling worker fleet in
  `workers/` uses for its dual-stack subnets.
- A free S3 gateway endpoint. No NAT gateway or load balancer is started.
- An optional two-zone PlanetScale interface endpoint. Endpoint hours and data
  processing are billed by AWS; PlanetScale also bills private traffic.
- An ECS cluster with Fargate capacity providers, defaulting to on-demand.
- Immutable ECR repositories and log groups with 14-day retention for backend,
  consumer, forms, realtime and tracking.
- Five Secrets Manager containers. Secret **values** are written separately,
  never supplied as Terraform variables or stored in Terraform state.
- A separate rotation secret holds pending replacements for the coordinated
  application and worker rollout. Keep it accessible only to operators.
- A private single-node Valkey 9.0 `cache.t4g.micro`, with TLS, encryption at
  rest, password authentication, one-day snapshots and no replica failover.
- A separate Valkey credential container. An ephemeral secret read feeds the
  write-only authentication field, keeping the password out of state.
- Standard String Parameter Store entries for allowlisted non-secret values
  supplied through `service_configuration`.
- Optional DNS-validated ACM certificates. Their validation records are output
  for the existing DNS provider; Terraform does not change DNS.

Public subnets provide internet access only to future tasks explicitly assigned
a public IP. Private subnets have no internet route. Applications still need
outbound connectivity for Kafka, Schema Registry, identity providers and other
APIs even when Postgres uses PrivateLink. Runtime networking, application task roles,
ingress, service deployment and coordinated credential activation are separate
work required before this foundation can serve the application.

## Bootstrap the cache password

The cache credential container must have a JSON secret version with a
`REDIS_PASSWORD` field before Terraform provisions the cache. On a fresh
installation, apply only `aws_secretsmanager_secret.cache` to create the
container, then populate it securely outside Terraform. Use a strong random
alphanumeric password of 16 to 128 characters. Follow with a full saved plan and
apply, including the installation's configuration tfvars.

The smallest node supports the application's standalone Redis clients and
multi-key scripts. Serverless requires cluster-aware clients and different
multi-key handling. `noeviction` preserves shared coordination keys under memory
pressure; monitor memory and write failures as usage grows.

`cache_check_task_definition` is a one-shot Fargate definition that checks
certificate-verified TLS and password authentication with `AUTH` and `PING`.
Run it in a public subnet with a public IP and the application task security
group. It retrieves only the cache password and writes a success or failure
message to its own one-day log group. It exits after checking and creates no
application service.

The cache has no public endpoint. Fargate tasks can reach it inside the VPC;
local tools and external workers need a private route or tunnel. Creating it
does not switch any existing Redis client.

See [the Fargate preparation guide](https://docs.warmbly.com/development/aws-fargate/)
for sizing and the deployment sequence.

## Verify

```sh
terraform fmt -check -recursive deploy/terraform/aws
terraform -chdir=deploy/terraform/aws/bootstrap validate
terraform -chdir=deploy/terraform/aws/foundation validate
```
