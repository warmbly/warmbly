#!/bin/bash
set -euo pipefail
node=$(awk '$1 == "server_name:" {gsub(/"/, "", $2); print $2; exit}' /etc/nats/server.conf)
# Run on every broker; only each group's leader knows all follower progress.
stable=0
attempt=0
while [ "$attempt" -lt 60 ]; do
  attempt=$((attempt + 1))
  if systemctl is-active --quiet nats &&
    curl -fsS --max-time 5 'http://127.0.0.1:8222/healthz?js-enabled-only=true' >/dev/null &&
    curl -fsS --max-time 5 'http://127.0.0.1:8222/jsz?accounts=true&streams=true&consumers=true' |
    jq -e --arg node "$node" '
      def ready:
        (.leader | length) > 0 and
        (.replicas | length) == 2 and
        (if .leader == $node then all(.replicas[]; .current == true and .offline != true) else true end);
      .meta_cluster.cluster_size == 3 and
      (.meta_cluster | ready) and
      any(.account_details[]?.stream_detail[]?;
        .name == "warmbly" and
        (.cluster | ready) and
        all(.consumer_detail[]?; .cluster | ready))
    ' >/dev/null; then
    stable=$((stable + 1))
    if [ "$stable" -ge 3 ]; then echo 'Local replica health and leader-owned quorum checks passed'; exit 0; fi
  else
    stable=0
  fi
  sleep 5
done
echo 'Replica readiness did not converge; do not maintain another broker' >&2
exit 1
