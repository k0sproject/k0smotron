#!/bin/bash
# NATS server v2.10+ with JetStream enabled
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive
NATS_VERSION="2.10.18"
NATS_USER="nats"
NATS_DATA_DIR="/var/lib/nats/jetstream"

###############################################################################
# 0. System prerequisites
###############################################################################
apt-get update -y
apt-get install -y curl

###############################################################################
# 1. Create NATS system user
###############################################################################
useradd --system --no-create-home --shell /usr/sbin/nologin "$NATS_USER" 2>/dev/null || true

###############################################################################
# 2. Install NATS server binary
###############################################################################
ARCH="amd64"
NATS_URL="https://github.com/nats-io/nats-server/releases/download/v${NATS_VERSION}/nats-server-v${NATS_VERSION}-linux-${ARCH}.tar.gz"

curl -sSLo /tmp/nats-server.tar.gz "$NATS_URL"
tar -C /tmp -xzf /tmp/nats-server.tar.gz
install -m 755 "/tmp/nats-server-v${NATS_VERSION}-linux-${ARCH}/nats-server" /usr/local/bin/nats-server
rm -rf /tmp/nats-server.tar.gz "/tmp/nats-server-v${NATS_VERSION}-linux-${ARCH}"

# Install NATS CLI (useful for debugging JetStream state)
NATSCLI_VERSION="0.1.5"
curl -sSLo /tmp/nats-cli.tar.gz \
  "https://github.com/nats-io/natscli/releases/download/v${NATSCLI_VERSION}/nats-${NATSCLI_VERSION}-linux-amd64.tar.gz"
tar -C /tmp -xzf /tmp/nats-cli.tar.gz
install -m 755 "/tmp/nats-${NATSCLI_VERSION}-linux-amd64/nats" /usr/local/bin/nats
rm -rf /tmp/nats-cli.tar.gz "/tmp/nats-${NATSCLI_VERSION}-linux-amd64"

###############################################################################
# 3. Prepare JetStream storage directory
###############################################################################
mkdir -p "$NATS_DATA_DIR"
chown -R "$NATS_USER:$NATS_USER" "$(dirname $NATS_DATA_DIR)"

###############################################################################
# 4. Write NATS configuration
###############################################################################
mkdir -p /etc/nats
cat > /etc/nats/nats-server.conf << EOF
# NATS server configuration for k0smotron benchmark environment

# Listen on all interfaces
host: 0.0.0.0
port: 4222

# HTTP monitoring endpoint (accessible from sg_internal)
http: 8222

# Enable cluster routing port (useful if adding more NATS nodes later)
cluster {
  port: 6222
}

# JetStream configuration
jetstream {
  store_dir: "$NATS_DATA_DIR"

  # Memory store limit: leave ~1 GB headroom on c5.xlarge (8 GiB total)
  max_memory_store: 6gb

  # File store limit: most of the gp3 root disk (20 GB, reserve 4 GB OS)
  max_file_store: 14gb
}

# Logging
log_time:  true
log_file:  /var/log/nats/nats-server.log

# Limits
max_connections:        10000
max_subscriptions:      50000
max_payload:            8mb
write_deadline:         "5s"
EOF

mkdir -p /var/log/nats
chown "$NATS_USER:$NATS_USER" /var/log/nats

###############################################################################
# 5. systemd service
###############################################################################
cat > /etc/systemd/system/nats.service << EOF
[Unit]
Description=NATS Server
After=network.target

[Service]
User=$NATS_USER
Group=$NATS_USER
ExecStart=/usr/local/bin/nats-server -c /etc/nats/nats-server.conf
ExecReload=/bin/kill -s HUP \$MAINPID
Restart=on-failure
RestartSec=5s
LimitNOFILE=1000000

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable nats
systemctl start nats

# Verify NATS started and JetStream is enabled
sleep 3
if curl -sf http://127.0.0.1:8222/healthz > /dev/null; then
  echo "NATS server started successfully."
  curl -s http://127.0.0.1:8222/jsz | grep -o '"config":{[^}]*}' || true
else
  echo "WARNING: NATS health check failed. Check: journalctl -u nats"
fi

echo "NATS setup complete."
