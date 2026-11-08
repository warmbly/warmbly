# Split deployment

The control plane on a container host, the fleet on machines you own, and the
database, root key and object store in a cloud region next to the control
plane. Three providers, one instance.

```
container host          your machines           cloud region
──────────────          ─────────────           ────────────
backend                 worker                  Postgres
consumer                worker                  KMS key
realtime                nats + redis            S3 bucket
tracking                                        SES
forms
portal
web / admin
```

The full walkthrough, with costs and the order to build it in, is
[Split deployment](https://docs.warmbly.com/development/split-deployment/).

## What is here

| Path | What it is |
|------|------------|
| `control-plane.env.example` | Every setting the container host needs, annotated |
| `bus/setup.sh` | One command that stands the whole bus box up, and re-runs safely |
| `bus/docker-compose.yml` | NATS JetStream and Redis, both over TLS |
| `bus/nats.conf` | The bus config the compose file mounts |
| `bus/certbot-deploy-hook.sh` | Publishes renewed certificates where the containers can read them |
| `node/docker-compose.yml` | A worker run by hand, for the version-controlled path |
| `node/worker.env.example` | What a worker needs, and what it deliberately does not |
| `../../scripts/aws-bootstrap.sh` | The KMS key, bucket, database and IAM policies |

## Two things that are not optional

**Redis over TLS.** The cache holds each organization's decrypted data key for
the life of its entry. A plaintext connection across the internet publishes key
material, and a password does not change that.

**S3 rather than filesystem blobs.** A worker reads the message body the
backend wrote. On another machine it has neither the disk nor the permissions,
so sends fail at the last step with everything else looking healthy.

## What a node does not get

A joining node is handed `KMS_PROVIDER=brokered` and `BLOB_PROVIDER=brokered`
rather than the control plane's own AWS providers, so no machine in the fleet
carries a cloud credential. It opens sealed keys and signs blob operations
through the internal API, with the instance-scoped token it already holds.
Blob bytes still travel directly between the node and the object store.

A worker also never receives `PRIMARY_DB`. A consumer does, because it is
control plane and updates relational state itself.
