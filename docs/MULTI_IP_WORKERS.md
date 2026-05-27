# Multi-IP Worker Deployment

This runbook covers the hosted Warmbly deployment pattern where one physical
box runs many worker processes, one per IP. The intent is to spread sending
across many sender identities (one worker per IP) without exploding the
number of VMs we operate.

## Why one worker per IP

Each row in the `workers` table is effectively an *egress*: a unique
`(worker_id, ip_addr)` that the control plane assigns mailboxes to and
routes Kafka work toward. The worker process binds outbound SMTP/IMAP/HTTPS
to that IP via `WORKER_BIND_IP` and the `internal/client/netbind` helper.
So:

- 1 IP per process keeps reputation per-IP measurable and isolatable
- N processes per box keeps ops density high without inflating VM count
- A bad IP can be quarantined by stopping a single instance, not the box

## Reference recipe: Hetzner CX32 + 16 Primary IPs

CX32 = 4 vCPU, 8GB RAM, 80GB disk. Comfortable for 16 lightweight worker
processes plus a Docker daemon. If you need more headroom, step up to CX42.

### 1. Order the IPs

In the Hetzner Cloud console, allocate 16 Primary IPv4 addresses in the
same datacenter as the server and attach all of them to the CX32.

### 2. Configure them at the OS level

Hetzner only auto-configures the first Primary IP. The others must be added
explicitly. With `iproute2`:

```bash
# eth0 is the public interface; check `ip link` to confirm.
for ip in 5.6.7.11 5.6.7.12 5.6.7.13 5.6.7.14 5.6.7.15 5.6.7.16 \
          5.6.7.17 5.6.7.18 5.6.7.19 5.6.7.20 5.6.7.21 5.6.7.22 \
          5.6.7.23 5.6.7.24 5.6.7.25 5.6.7.26; do
  ip addr add "${ip}/32" dev eth0
done
```

To make that survive reboot, use netplan (Ubuntu) or systemd-networkd. A
netplan example:

```yaml
# /etc/netplan/60-extra-ips.yaml
network:
  version: 2
  ethernets:
    eth0:
      addresses:
        - 5.6.7.11/32
        - 5.6.7.12/32
        # ...one entry per extra IP
```

Run `netplan apply` and confirm with `ip -4 addr show eth0` that all 16
addresses are bound.

### 3. Set rDNS per IP

Reverse DNS (PTR) records help inbox providers identify and trust your
sending. Set the PTR for each IP via the Hetzner API:

```bash
HCLOUD_TOKEN="..."
for ip in 5.6.7.11 5.6.7.12 5.6.7.13 5.6.7.14 5.6.7.15 5.6.7.16 \
          5.6.7.17 5.6.7.18 5.6.7.19 5.6.7.20 5.6.7.21 5.6.7.22 \
          5.6.7.23 5.6.7.24 5.6.7.25 5.6.7.26; do
  hostname="mail-$(echo "$ip" | tr . -).send.warmbly.com"
  curl -fsS -X POST "https://api.hetzner.cloud/v1/primary_ips/<PRIMARY_IP_ID>/actions/change_dns_ptr" \
    -H "Authorization: Bearer ${HCLOUD_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"ip\":\"${ip}\",\"dns_ptr\":\"${hostname}\"}"
done
```

Then add matching forward A records for those hostnames so rDNS + forward
DNS agree. SPF, DKIM, and DMARC alignment for the sending domain still
applies at the mailbox level, not the IP level.

### 4. Install the workers

One command brings up 16 worker processes, each bound to a distinct IP:

```bash
sudo ./install-worker.sh \
  --kafka kafka.warmbly.com:9092 \
  --schema-registry https://schema.warmbly.com \
  --redis redis://cache.warmbly.com:6379 \
  --aws-key AKIA... --aws-secret ... --aws-region us-east-1 \
  --ips 5.6.7.11,5.6.7.12,5.6.7.13,5.6.7.14,5.6.7.15,5.6.7.16,5.6.7.17,5.6.7.18,5.6.7.19,5.6.7.20,5.6.7.21,5.6.7.22,5.6.7.23,5.6.7.24,5.6.7.25,5.6.7.26
```

Under the hood:

- A `warmbly-worker@.service` systemd template unit is written once
- Per-instance env files land at `/etc/warmbly/instances/<dashed-ip>.env`
  each carrying `WORKER_BIND_IP` and the derived `WORKER_ID`
- 16 instances are enabled and started:
  `warmbly-worker@5-6-7-11`, `warmbly-worker@5-6-7-12`, ...
- All instances share `/etc/warmbly/worker.env` for Kafka, KMS, Redis, etc.

The worker IDs are deterministic UUIDv5 values derived from the IP, so
reinstalling with the same `--ips` set produces the same IDs and reuses the
existing `workers` rows / reputation. Adding or removing an IP only
affects that instance; the rest keep sending.

## Day-2 operations

### Check status across all instances

```bash
sudo ./install-worker.sh --status
```

Prints one row per instance with IP, instance name, systemd state, and
worker ID.

### Tail one instance

```bash
journalctl -u warmbly-worker@5-6-7-11 -f
```

### Restart one instance

```bash
sudo systemctl restart warmbly-worker@5-6-7-11
```

### Quarantine one IP

If an IP's reputation tanks, take just that instance down. The rest of the
box keeps sending and the control plane will stop assigning new work to the
silent worker via the existing heartbeat-based assignment logic.

```bash
sudo systemctl stop warmbly-worker@5-6-7-11
sudo systemctl disable warmbly-worker@5-6-7-11
```

When the IP is recovered (rDNS fixed, mailbox health restored, etc.):

```bash
sudo systemctl enable --now warmbly-worker@5-6-7-11
```

### Update all instances to a new image tag

```bash
sudo WARMBLY_WORKER_IMAGE=ghcr.io/warmbly/worker:v1.42.0 \
  ./install-worker.sh --update
```

`--update` detects multi-IP installs automatically and restarts every
instance.

### Uninstall

```bash
sudo ./install-worker.sh --uninstall
```

Stops every instance, removes the template unit and per-instance env
files, and keeps the shared config under `/etc/warmbly/` so a reinstall is
fast. Use `--purge` to also delete the shared config.

## Health-check model

Each worker process runs its own heartbeat goroutine
(`WorkerService.Heartbeat`). The control plane sees N independent heartbeats
from one box and treats each one as a distinct sending identity:

- one IP down -> one missed heartbeat -> control plane stops routing
  campaign/warmup work to that worker only
- box down -> all N heartbeats stop -> control plane reassigns affected
  mailboxes to other workers per the existing tier rules

There is no box-level health check: the unit of health is the worker
process, which is also the unit of sending identity. This is intentional.

## Blast radius guidance

Do not concentrate too much of your fleet on one box. If a box goes hard
down, you lose every IP it was hosting at once. Rule of thumb:

- never put more than ~25% of your total fleet IPs on a single box
- spread boxes across at least two Hetzner datacenters (e.g. `nbg1` + `fsn1`)
- keep premium tier and free tier on different boxes so a free-tier
  abuse incident cannot starve premium customers of capacity

For the default CX32 + 16 IP recipe, that means: don't run hosted Warmbly
on fewer than 4 such boxes once you're past the pilot stage.

## Compatibility

`install-worker.sh` without `--ips` is unchanged: one `warmbly-worker.service`
unit, IP auto-detected or supplied via `--ip`. Existing single-IP
deployments keep working with no migration needed. The control-plane
schema (`workers` table) and assignment logic
(`internal/app/worker/assignment.go`) are not affected by this change;
each per-IP process simply registers itself as a normal worker.
