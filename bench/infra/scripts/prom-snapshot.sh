#!/bin/bash
# Run ON the observer node (via ssh 'bash -s' < this file).
# Takes a Prometheus TSDB snapshot and tarballs it to ~/prom-snapshot.tar.gz.
set -euo pipefail

PROM_NS="monitoring"
PROM_SVC="prom-kube-prometheus-stack-prometheus"
LOCAL_PORT=19090   # avoid conflict if user already port-forwards 9090

cleanup() {
  kill "$FWD_PID" 2>/dev/null || true
}
trap cleanup EXIT

echo "Starting port-forward to Prometheus..."
kubectl -n "$PROM_NS" port-forward "svc/$PROM_SVC" "${LOCAL_PORT}:9090" &>/dev/null &
FWD_PID=$!

# Wait for port-forward to be ready
for i in $(seq 1 20); do
  curl -sf "http://localhost:${LOCAL_PORT}/-/ready" &>/dev/null && break
  sleep 1
done

echo "Triggering TSDB snapshot..."
SNAP_NAME=$(curl -sf -XPOST \
  "http://localhost:${LOCAL_PORT}/api/v1/admin/tsdb/snapshot" \
  | jq -r '.data.name')

if [ -z "$SNAP_NAME" ]; then
  echo "ERROR: snapshot API returned empty name. Is --enable-admin-api set?" >&2
  exit 1
fi

echo "Snapshot: $SNAP_NAME"

PROM_POD=$(kubectl -n "$PROM_NS" get pod \
  -l "app.kubernetes.io/name=prometheus" \
  -o jsonpath='{.items[0].metadata.name}')

echo "Archiving from pod $PROM_POD..."
kubectl -n "$PROM_NS" exec "$PROM_POD" -- \
  tar czf /tmp/prom-snapshot.tar.gz -C /prometheus/snapshots "$SNAP_NAME"

kubectl -n "$PROM_NS" cp \
  "$PROM_POD:/tmp/prom-snapshot.tar.gz" \
  "$HOME/prom-snapshot.tar.gz"

echo "Snapshot saved to ~/prom-snapshot.tar.gz"
