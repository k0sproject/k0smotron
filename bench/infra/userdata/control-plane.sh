#!/bin/bash
# k0s control-plane node bootstrap
# Template variables (injected by Terraform templatefile()):
#   $${node_index}     — 0, 1, or 2  (rendered as {node_index} after template)
#   $${k0s_version}
#   $${cp0_private_ip} — private IP of control-plane node 0 (used by 1, 2)
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive
NODE_INDEX="${node_index}"
K0S_VERSION="${k0s_version}"
CP0_IP="${cp0_private_ip}"
TOKEN_PORT=8888
TOKEN_DIR=/var/lib/bench-tokens

###############################################################################
# 0. System prerequisites
###############################################################################
apt-get update -y
apt-get install -y curl python3

###############################################################################
# 1. Mount io2 data disk for k0s / etcd data
###############################################################################
for i in $(seq 1 20); do
  [ -b /dev/sdb ] || [ -b /dev/nvme1n1 ] && break
  sleep 3
done
DATA_DEVICE="/dev/sdb"
[ -b /dev/nvme1n1 ] && DATA_DEVICE="/dev/nvme1n1"

if ! blkid "$DATA_DEVICE" > /dev/null 2>&1; then
  mkfs.ext4 -F "$DATA_DEVICE"
fi

mkdir -p /var/lib/k0s
DATA_UUID=$(blkid -s UUID -o value "$DATA_DEVICE")
echo "UUID=$DATA_UUID /var/lib/k0s ext4 defaults,noatime 0 2" >> /etc/fstab
mount -a

###############################################################################
# 2. Install k0s
###############################################################################
curl -sSLf https://get.k0s.sh | K0S_VERSION="$K0S_VERSION" sh

###############################################################################
# 3. Bootstrap logic
#
# Node 0: fetch its own public IP from IMDS, configure k0s with it as
#   externalAddress + SAN so the issued kubeconfig works from outside the VPC.
#   Then publish tokens + kubeconfig over HTTP on port 8888.
# Nodes 1, 2: curl controller token from node 0, join.
###############################################################################
if [ "$NODE_INDEX" = "0" ]; then
  # Fetch own public IP via IMDSv2.
  IMDS_TOKEN=$(curl -sS -X PUT "http://169.254.169.254/latest/api/token" \
    -H "X-aws-ec2-metadata-token-ttl-seconds: 300")
  CP0_PUBLIC_IP=$(curl -sS -H "X-aws-ec2-metadata-token: $IMDS_TOKEN" \
    http://169.254.169.254/latest/meta-data/public-ipv4)
  CP0_PRIVATE_IP=$(curl -sS -H "X-aws-ec2-metadata-token: $IMDS_TOKEN" \
    http://169.254.169.254/latest/meta-data/local-ipv4)

  # externalAddress = private IP: workers in the VPC join directly without
  # going through the IGW hairpin (AWS SG sees public source on that path,
  # which is blocked). Public IP is added to sans so the cert validates when
  # an external client hits the apiserver via the public IP.
  cat > /etc/k0s.yaml <<EOF
apiVersion: k0s.k0sproject.io/v1beta1
kind: ClusterConfig
spec:
  api:
    externalAddress: "$CP0_PRIVATE_IP"
    sans:
      - "$CP0_PRIVATE_IP"
      - "$CP0_PUBLIC_IP"
  network:
    # kube-router (k0s default) has known cross-node pod-network issues on EC2.
    # Calico with VXLAN works reliably.
    provider: calico
    calico:
      mode: vxlan
EOF

  # ---- Primary controller ---------------------------------------------------
  k0s install controller --enable-worker=false --config /etc/k0s.yaml
  k0s start

  echo "Waiting for k0s API..."
  until k0s kubectl get nodes > /dev/null 2>&1; do sleep 5; done

  # Generate tokens + kubeconfig into a dir served by python http.server.
  mkdir -p "$TOKEN_DIR"
  k0s token create --role=controller > "$TOKEN_DIR/controller-token"
  k0s token create --role=worker     > "$TOKEN_DIR/worker-token"
  k0s kubeconfig admin                > "$TOKEN_DIR/kubeconfig-admin"
  chmod 600 "$TOKEN_DIR"/*

  # Systemd unit for the token server (keeps running for observer to fetch kubeconfig).
  cat > /etc/systemd/system/bench-tokens.service <<EOF
[Unit]
Description=Bench bootstrap token server
After=network.target

[Service]
Type=simple
WorkingDirectory=$TOKEN_DIR
ExecStart=/usr/bin/python3 -m http.server $TOKEN_PORT --bind 0.0.0.0
Restart=always

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now bench-tokens.service

  echo "Primary controller bootstrapped. Tokens served on :$TOKEN_PORT."

else
  # ---- Secondary controllers ------------------------------------------------
  echo "Waiting for controller token from $CP0_IP:$TOKEN_PORT..."
  until curl -sf "http://$CP0_IP:$TOKEN_PORT/controller-token" -o /tmp/controller-token; do
    sleep 5
  done

  k0s install controller --token-file /tmp/controller-token --enable-worker=false
  k0s start
  echo "Joined as secondary controller."
fi
