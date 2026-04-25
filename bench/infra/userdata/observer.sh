#!/bin/bash
# Benchmark / observer node setup
# Template variables (injected by Terraform templatefile()):
#   $${cp0_private_ip} — control-plane node 0, serves kubeconfig over HTTP:8888
#
# NOTE: bash $${UPPER_CASE} variable references are escaped with double $$ so that
# Terraform's templatefile() does not attempt to interpolate them.
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive
CP0_IP="${cp0_private_ip}"
TOKEN_PORT=8888
GO_VERSION="1.22.5"
K9S_VERSION="v0.32.5"
HEY_VERSION="v0.1.4"

###############################################################################
# 0. System prerequisites
###############################################################################
apt-get update -y
apt-get install -y curl wget unzip git build-essential jq apt-transport-https \
  ca-certificates gnupg lsb-release

###############################################################################
# 1. kubectl
###############################################################################
KUBECTL_VERSION=$(curl -sSL https://dl.k8s.io/release/stable.txt)
curl -sSLo /usr/local/bin/kubectl \
  "https://dl.k8s.io/release/$${KUBECTL_VERSION}/bin/linux/amd64/kubectl"
chmod +x /usr/local/bin/kubectl

###############################################################################
# 2. Go
###############################################################################
curl -sSLo /tmp/go.tar.gz "https://go.dev/dl/go$${GO_VERSION}.linux-amd64.tar.gz"
tar -C /usr/local -xzf /tmp/go.tar.gz
rm /tmp/go.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin:/root/go/bin' >> /etc/profile.d/go.sh
echo 'export PATH=$PATH:/usr/local/go/bin:/home/ubuntu/go/bin' >> /home/ubuntu/.bashrc
export PATH=$PATH:/usr/local/go/bin

###############################################################################
# 3. hey — HTTP load tester
###############################################################################
curl -sSLo /usr/local/bin/hey \
  "https://github.com/rakyll/hey/releases/download/$${HEY_VERSION}/hey_linux_amd64"
chmod +x /usr/local/bin/hey

###############################################################################
# 4. k9s — cluster TUI (useful for live observation)
###############################################################################
curl -sSLo /tmp/k9s.tar.gz \
  "https://github.com/derailed/k9s/releases/download/$${K9S_VERSION}/k9s_Linux_amd64.tar.gz"
tar -C /usr/local/bin -xzf /tmp/k9s.tar.gz k9s
chmod +x /usr/local/bin/k9s
rm /tmp/k9s.tar.gz

###############################################################################
# 5. promtool (Prometheus CLI, useful for metrics validation)
###############################################################################
PROM_VERSION="2.53.1"
curl -sSLo /tmp/prometheus.tar.gz \
  "https://github.com/prometheus/prometheus/releases/download/v$${PROM_VERSION}/prometheus-$${PROM_VERSION}.linux-amd64.tar.gz"
tar -C /tmp -xzf /tmp/prometheus.tar.gz
cp /tmp/prometheus-$${PROM_VERSION}.linux-amd64/promtool /usr/local/bin/
chmod +x /usr/local/bin/promtool
rm -rf /tmp/prometheus.tar.gz /tmp/prometheus-$${PROM_VERSION}.linux-amd64

###############################################################################
# 6. Retrieve kubeconfig from control-plane node 0 HTTP token server
###############################################################################
mkdir -p /home/ubuntu/.kube /root/.kube
echo "Waiting for kubeconfig from $CP0_IP:$TOKEN_PORT..."
until curl -sf "http://$CP0_IP:$TOKEN_PORT/kubeconfig-admin" -o /home/ubuntu/.kube/config; do
  sleep 10
done

# Rewrite server URL in the kubeconfig to point to cp0's private IP
sed -i "s|server: .*|server: https://$CP0_IP:6443|" /home/ubuntu/.kube/config

cp /home/ubuntu/.kube/config /root/.kube/config
chown -R ubuntu:ubuntu /home/ubuntu/.kube
chmod 600 /home/ubuntu/.kube/config /root/.kube/config

###############################################################################
# 7. Install Helm (so benchmark setup scripts can deploy k0smotron charts)
###############################################################################
curl -sSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash

###############################################################################
# 8. Clone k0smotron repo (needed to run go test -tags bench)
###############################################################################
REPO_DIR="/home/ubuntu/k0smotron"
git clone --depth=1 https://github.com/k0sproject/k0smotron.git "$REPO_DIR"
chown -R ubuntu:ubuntu "$REPO_DIR"

# Pre-download Go module deps so the first bench run is faster
cd "$REPO_DIR"
sudo -u ubuntu /usr/local/go/bin/go mod download

###############################################################################
# 9. Convenience aliases for ubuntu user
###############################################################################
cat >> /home/ubuntu/.bashrc << 'BASHEOF'

# k0smotron bench shortcuts
alias k='kubectl'
alias kns='kubectl config set-context --current --namespace'
export KUBECONFIG=/home/ubuntu/.kube/config
export GOPATH=/home/ubuntu/go
export PATH=$PATH:/usr/local/go/bin:/home/ubuntu/go/bin

# Quick bench env check
bench-status() {
  echo "=== Nodes ==="
  kubectl get nodes -o wide
  echo ""
  echo "=== k0smotron HCP pods ==="
  kubectl get pods -A -l app.kubernetes.io/managed-by=k0smotron 2>/dev/null || true
}
BASHEOF

###############################################################################
# 10. Signal bootstrap complete (Makefile polls this via SSH)
###############################################################################
touch /tmp/bootstrap-done
echo "Observer setup complete."
