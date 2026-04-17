#!/bin/bash
# k0s worker node bootstrap
# Template vars: $${k0s_version}, $${cp0_private_ip}
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive
K0S_VERSION="${k0s_version}"
CP0_IP="${cp0_private_ip}"
TOKEN_PORT=8888

apt-get update -y
apt-get install -y curl

cat > /etc/sysctl.d/99-k0smotron-bench.conf <<'EOF'
fs.file-max = 2097152
fs.inotify.max_user_instances = 8192
fs.inotify.max_user_watches = 1048576
EOF
sysctl --system

cat > /etc/security/limits.d/99-k0smotron-bench.conf <<'EOF'
* soft nofile 1048576
* hard nofile 1048576
root soft nofile 1048576
root hard nofile 1048576
EOF

curl -sSLf https://get.k0s.sh | K0S_VERSION="$K0S_VERSION" sh

echo "Waiting for worker token from $CP0_IP:$TOKEN_PORT..."
until curl -sf "http://$CP0_IP:$TOKEN_PORT/worker-token" -o /tmp/worker-token; do
  sleep 5
done

k0s install worker --token-file /tmp/worker-token

mkdir -p /etc/systemd/system/k0sworker.service.d
cat > /etc/systemd/system/k0sworker.service.d/limits.conf <<'EOF'
[Service]
LimitNOFILE=1048576
LimitNPROC=infinity
TasksMax=infinity
EOF
systemctl daemon-reload

k0s start
echo "Worker joined."
