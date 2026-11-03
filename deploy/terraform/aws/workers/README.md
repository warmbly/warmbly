# EC2 worker hosts

This root prepares one ARM `t4g.small` per worker in `us-east-1`, each with an
encrypted 16 GiB gp3 disk, static IPv4 address and pinned worker image. Standard
CPU credits cap burst charges; depleted credits throttle to baseline. Monitor
credit balance and fleet health before changing size or credit mode.

```sh
terraform -chdir=deploy/terraform/aws/workers init \
  -backend-config="bucket=$STATE_BUCKET"
terraform -chdir=deploy/terraform/aws/workers plan \
  -var-file=/private/path/workers.tfvars.json \
  -out=/private/path/workers.tfplan
terraform -chdir=deploy/terraform/aws/workers apply /private/path/workers.tfplan
```

Supply the SSH public key, operator CIDRs, cache security group ID and worker
image metadata through private inputs. SSH is restricted to those CIDRs, and
IMDSv2 is required. Hosts have no instance profile or application AWS
credentials. Workers use brokered KMS and blob operations through the backend.

Bootstrap installs Docker and prepares systemd without starting the worker. An
operator delivers configuration from the worker's Secrets Manager container
over SSH into `/etc/warmbly/node.env`, owned by root with mode `0600`. Credentials
never enter user data, Terraform or command arguments. Preserve worker IDs,
mailbox encryption keys, OAuth identities and compatible broker configuration.
Package installation and image download retry while the static IP association
becomes usable during first boot.

Never run both hosts with the same worker ID. Drain and stop the previous
process, confirm it exited, start the replacement, and verify fresh heartbeats,
command consumption and mailbox health before advancing to the next host. The
prepared service allows 120 seconds for Docker shutdown. Preserve the previous
host for rollback. Static AWS IPs remain stable, but differ from the previous
provider's addresses, so verify mailbox authentication during the handover.

Once a host's worker is running and heartbeating, hand its updates to the
fleet so nobody needs to sign in to it again. First make sure the fleet target
matches what the host runs (`warmblyctl fleet version`), then on the host:

```sh
curl -fsSL https://<backend>/join.sh | sudo sh -s -- --adopt --env-refresh
```

`--adopt` keeps the existing `node.env` and worker ID and installs the standard
unit and the `warmbly-node-update` timer. The host then pulls the version its
heartbeat names, in the canary waves a release starts, and puts back the
previous image when an update does not stay up. `--env-refresh` lets it replace
`node.env` when the control plane's rendering changes, which is how a key
rotation reaches it; it refuses, naming the keys, when a refresh would drop a
value only this host holds, so move those into `node.local.env` with
`--keep-local`. Run `warmblyctl fleet env-status` afterwards: every adopted host
should report `current`.

Complete the handover by checking the updater on every host:

```sh
sudo test -x /usr/local/bin/warmbly-node-update
sudo systemctl is-enabled warmbly-node-update.timer
sudo systemctl is-active warmbly-node-update.timer
```

All three commands must succeed before the host is ready for a release canary.
Keep the fleet baseline at the version the workers currently run while adopting
them, then start the target release with `warmblyctl fleet rollout start <target-tag>`.
Automatic release deployments enable the same staged fleet rollout after the
control-plane services pass their health watches.

Private Valkey access is allowed from the worker security group. Coordinate its
activation with every control-plane client, and check configured mail ports and
AWS outbound restrictions before moving work. Do not switch one worker to an
empty independent cache.

The dependency check expects `CACHE_HOST` and `NEW_REDIS_PASSWORD` in addition to
the worker environment. Supply the replacement password through protected
temporary input. It verifies both TLS certificate validation and an authenticated
`PING` against private Valkey, without changing the active cache endpoint.

Instances and addresses have destruction guards. AMI and bootstrap drift do not
silently replace a running worker. Roll out host upgrades deliberately while
preserving worker identity and static addresses.

## Autoscaling fleet

`fleet.tf` and `fleet-network.tf` add an opt-in Auto Scaling group of workers
with no public IPv4 address: dual-stack subnets, an egress-only gateway for
IPv6, one NAT instance per zone for IPv4 and an S3 gateway endpoint. Workers
join at boot with `join.sh`, reading the join token from the SecureString
parameter named by `fleet_join_token_parameter`. Nothing is created until
`fleet_enabled` is true, and the fixed hosts above are unaffected. Apply the
foundation first, since it adds the VPC's IPv6 block.

The fleet scales on server resource usage, not mailbox count. Two
target-tracking policies add workers to hold the fleet's memory in use
(`WorkerMemoryPercent`) at `fleet_target_memory` and its average CPU at
`fleet_target_cpu`. One full worker does not grow the fleet: its `auto` mailbox
target falls, so placement steers new mailboxes elsewhere. The fleet shrinks one worker at a time, and only after fleet memory,
the fullest worker's memory and the busiest worker's CPU have all stayed low
for `fleet_scale_in_after_hours`; missing data never counts as low.

The backend publishes those metrics to `Warmbly/Fleet` with the dimension
`Fleet=<name>` when `FLEET_METRICS_CLOUDWATCH=<name>` is set on it, and its role
needs `cloudwatch:PutMetricData`. Each worker's `node.local.env` gets
`WARMBLY_WORKER_CAPACITY=auto` by default, so it sizes its mailbox target from
its memory, and `GOMEMLIMIT` at 80% of the instance's memory.

The design, variables, scaling metric contract, retirement of fixed hosts and
cost notes are in [the AWS worker fleet guide](https://docs.warmbly.com/development/aws-workers/).

```sh
terraform -chdir=deploy/terraform/aws/workers test
```

runs the mocked-provider checks in `tests/`, with no credentials.
