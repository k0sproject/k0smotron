###############################################################################
# k0smotron benchmark infrastructure — top-level automation
#
# Typical full run:
#   make all KEY=my-ec2-key CIDR=$(curl -s -4 ifconfig.me)/32
#
# Step by step:
#   make provision KEY=my-ec2-key CIDR=$(curl -s -4 ifconfig.me)/32
#   make monitoring
#   make bench
#   make retrieve            # CSV + Prometheus snapshot land in ./results/
#   make destroy             # refuses if results not yet retrieved
###############################################################################

SHELL := /bin/bash
.DEFAULT_GOAL := help

# Ensure terraform is findable even if installed via asdf/tfenv/etc.
export PATH := $(HOME)/bin:/usr/local/bin:/opt/homebrew/bin:$(PATH)

# --------------------------------------------------------------------------- #
# Configuration — override on the command line or via environment
# --------------------------------------------------------------------------- #
KEY         ?=                          # EC2 key pair name (required for provision)
CIDR        ?= 0.0.0.0/0               # CIDR for SSH + kubectl access
SSH_KEY     ?= ~/.ssh/id_rsa           # local private key for SSH
# Seconds to wait for observer and storage bootstrap readiness.
SSH_TIMEOUT ?= 600

# Benchmark flags — passed through to go test
BENCH_TIMEOUT  ?= 4h
BENCH_PARALLEL ?= 10
BENCH_K0S_VER  ?= v1.31.2-k0s.0
BENCH_STORAGE  ?=                      # empty = all enabled backends
BENCH_LARGE    ?=                      # set to "--bench.large" to add 500-cluster run

# Local k0smotron build (for testing dev versions like the one with NATS support)
LOCAL_IMG      ?= k0smotron:bench-local
IMAGE_TAR      := $(CURDIR)/out/k0smotron-image.tar
INSTALL_YAML   := $(CURDIR)/out/install.yaml

# --------------------------------------------------------------------------- #
# Derived values (do not override)
# --------------------------------------------------------------------------- #
TF_DIR        := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
RESULTS_TS    := $(shell date +%Y%m%d-%H%M%S)
RESULTS_DIR   := $(TF_DIR)results/$(RESULTS_TS)
REPO_ROOT     := $(abspath $(TF_DIR)../..)

TF_EXTRA_VARS ?=
TF_VARS := $(if $(KEY),-var key_name=$(KEY)) \
           $(if $(CIDR),-var allowed_cidr=$(CIDR)) \
           $(TF_EXTRA_VARS)

OBSERVER_IP   = $(shell cd $(TF_DIR) && terraform output -raw observer_ip 2>/dev/null)
SSH_OPTS      := -o StrictHostKeyChecking=no -o ConnectTimeout=10 -i $(SSH_KEY) -o BatchMode=yes
SSH           = ssh $(SSH_OPTS) ubuntu@$(OBSERVER_IP)
SCP           = scp $(SSH_OPTS)

# --------------------------------------------------------------------------- #
# Targets
# --------------------------------------------------------------------------- #

.PHONY: help all provision wait monitoring k0smotron-stable worker-limits bench \
        prom-snapshot destroy image manifests deploy-local

help:
	@echo "Usage: make <target> [KEY=...] [CIDR=...]"
	@echo ""
	@echo "  all           provision + wait + monitoring + bench"
	@echo "  provision     terraform apply (requires KEY=<ec2-key-pair>)"
	@echo "  wait          wait for observer cloud-init to complete"
	@echo "  monitoring        install cert-manager + metrics-server + Prometheus"
	@echo "  k0smotron-stable  install stable k0smotron from docs.k0smotron.io"
	@echo "  worker-limits     raise worker node file-descriptor limits and restart k0s workers"
	@echo "  deploy-local      build + push local k0smotron image + apply local install.yaml"
	@echo "  bench         run go test -tags bench (CSV written by the test itself)"
	@echo "  prom-snapshot copy Prometheus TSDB snapshot from cluster (optional)"
	@echo "  destroy       terraform destroy"
	@echo ""
	@echo "Override vars: KEY CIDR SSH_KEY LOCAL_IMG BENCH_TIMEOUT BENCH_PARALLEL"
	@echo "               BENCH_K0S_VER BENCH_STORAGE BENCH_LARGE"

all: provision wait monitoring k0smotron-stable bench

# --------------------------------------------------------------------------- #

provision:
	@test -n "$(KEY)" || (echo "ERROR: KEY is required — make provision KEY=my-ec2-key" && exit 1)
	cd $(TF_DIR) && terraform init -upgrade
	cd $(TF_DIR) && terraform apply -auto-approve $(TF_VARS)
	@cd $(TF_DIR) && for i in 1 2 3 4 5; do \
	  IP=$$(terraform output -raw observer_ip 2>/dev/null); \
	  if [ -n "$$IP" ]; then echo ""; echo "Infrastructure ready. Observer IP: $$IP"; echo "Next: make wait"; exit 0; fi; \
	  sleep 2; \
	done; \
	echo "WARNING: observer_ip output empty after apply; state may be partial"

# --------------------------------------------------------------------------- #

wait:
	@cd $(TF_DIR); \
	for i in 1 2 3 4 5; do \
	  OBSERVER_IP=$$(terraform output -raw observer_ip 2>/dev/null); \
	  [ -n "$$OBSERVER_IP" ] && break; \
	  sleep 2; \
	done; \
	test -n "$$OBSERVER_IP" || { echo "ERROR: terraform output observer_ip empty after 5 tries"; exit 1; }; \
	echo "Waiting for observer cloud-init on $$OBSERVER_IP (up to $(SSH_TIMEOUT)s)..."; \
	elapsed=0; \
	while ! ssh $(SSH_OPTS) ubuntu@$$OBSERVER_IP 'test -f /tmp/bootstrap-done' 2>/dev/null; do \
	  elapsed=$$((elapsed + 15)); \
	  if [ $$elapsed -ge $(SSH_TIMEOUT) ]; then \
	    echo "ERROR: timed out waiting for bootstrap-done on $$OBSERVER_IP"; exit 1; \
	  fi; \
	  printf "  %3ds elapsed — still bootstrapping...\r" $$elapsed; \
	  sleep 15; \
	done; \
	echo "Observer ready.                                    "; \
	echo "SSH: ssh $(SSH_OPTS) ubuntu@$$OBSERVER_IP"; \
	POSTGRES_IP=$$(terraform output -raw postgres_ip); \
	MYSQL_IP=$$(terraform output -raw mysql_ip); \
	test -n "$$POSTGRES_IP" && test -n "$$MYSQL_IP" || { echo "ERROR: storage terraform outputs empty"; exit 1; }; \
	echo "Waiting for PostgreSQL on $$POSTGRES_IP:5432 (up to $(SSH_TIMEOUT)s)..."; \
	ssh $(SSH_OPTS) ubuntu@$$OBSERVER_IP "elapsed=0; until timeout 1 bash -c '</dev/tcp/$$POSTGRES_IP/5432' >/dev/null 2>&1; do elapsed=\$$((elapsed + 2)); if [ \$$elapsed -ge $(SSH_TIMEOUT) ]; then echo 'ERROR: timed out waiting for PostgreSQL'; exit 1; fi; sleep 2; done"; \
	echo "Waiting for MySQL on $$MYSQL_IP:3306 (up to $(SSH_TIMEOUT)s)..."; \
	ssh $(SSH_OPTS) ubuntu@$$OBSERVER_IP "elapsed=0; until timeout 1 bash -c '</dev/tcp/$$MYSQL_IP/3306' >/dev/null 2>&1; do elapsed=\$$((elapsed + 2)); if [ \$$elapsed -ge $(SSH_TIMEOUT) ]; then echo 'ERROR: timed out waiting for MySQL'; exit 1; fi; sleep 2; done"; \
	echo "Storage backends are accepting TCP connections."

# --------------------------------------------------------------------------- #

monitoring:
	@OBSERVER_IP=$$(cd $(TF_DIR) && terraform output -raw observer_ip); \
	test -n "$$OBSERVER_IP" || { echo "ERROR: observer_ip empty"; exit 1; }; \
	SSH="ssh $(SSH_OPTS) ubuntu@$$OBSERVER_IP"; \
	echo "Installing cert-manager..."; \
	$$SSH 'kubectl apply --server-side --force-conflicts \
	    -f https://github.com/cert-manager/cert-manager/releases/latest/download/cert-manager.yaml; \
	  kubectl -n cert-manager rollout status deployment/cert-manager            --timeout=5m; \
	  kubectl -n cert-manager rollout status deployment/cert-manager-webhook    --timeout=5m; \
	  kubectl -n cert-manager rollout status deployment/cert-manager-cainjector --timeout=5m; \
	  echo "Waiting for cert-manager webhook endpoints..."; \
	  for i in $$(seq 1 60); do \
	    [ -n "$$(kubectl -n cert-manager get endpoints cert-manager-webhook -o jsonpath="{.subsets[*].addresses[*].ip}" 2>/dev/null)" ] && break; \
	    sleep 5; \
	  done; \
	  sleep 10'; \
	echo "Installing metrics-server..."; \
	$$SSH 'kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml; \
	  kubectl -n kube-system patch deployment metrics-server --type=json \
	    -p="[{\"op\": \"add\", \"path\": \"/spec/template/spec/containers/0/args/-\", \"value\": \"--kubelet-insecure-tls\"}]" || true'; \
	echo "Scaling CoreDNS for benchmark load..."; \
	$$SSH 'kubectl -n kube-system scale deployment/coredns --replicas=3; \
	  kubectl -n kube-system set resources deployment/coredns \
	    --limits=cpu=1,memory=512Mi --requests=cpu=200m,memory=128Mi; \
	  kubectl -n kube-system rollout status deployment/coredns --timeout=3m'; \
	echo "Installing local-path-provisioner (default StorageClass)..."; \
	$$SSH 'kubectl apply --server-side --force-conflicts \
	    -f https://raw.githubusercontent.com/rancher/local-path-provisioner/master/deploy/local-path-storage.yaml; \
	  kubectl -n local-path-storage rollout status deployment/local-path-provisioner --timeout=5m; \
	  kubectl patch storageclass local-path \
	    -p "{\"metadata\":{\"annotations\":{\"storageclass.kubernetes.io/is-default-class\":\"true\"}}}"'; \
	echo "Installing Prometheus (kube-prometheus-stack)..."; \
	$$SSH 'helm repo add prometheus-community https://prometheus-community.github.io/helm-charts 2>/dev/null; \
	  helm repo update; \
	  helm upgrade --install prom prometheus-community/kube-prometheus-stack \
	    --namespace monitoring --create-namespace \
	    --set prometheus.prometheusSpec.scrapeInterval=15s \
	    --set prometheus.prometheusSpec.retention=14d \
	    --set prometheus.prometheusSpec.enableAdminAPI=true \
	    --wait --timeout 10m'; \
	echo "Cluster support stack ready. Deploy k0smotron via: make k0smotron-stable OR make deploy-local"

# Install stable k0smotron from docs.k0smotron.io
k0smotron-stable:
	@OBSERVER_IP=$$(cd $(TF_DIR) && terraform output -raw observer_ip); \
	test -n "$$OBSERVER_IP" || { echo "ERROR: observer_ip empty"; exit 1; }; \
	SSH="ssh $(SSH_OPTS) ubuntu@$$OBSERVER_IP"; \
	echo "Installing stable k0smotron..."; \
	$$SSH 'for i in 1 2 3 4 5; do \
	    kubectl apply --server-side --force-conflicts -f https://docs.k0smotron.io/stable/install.yaml && break; \
	    echo "attempt $$i failed; retrying in 15s..."; \
	    sleep 15; \
	  done; \
	  kubectl -n k0smotron rollout status deployment/k0smotron-controller-manager --timeout=5m; \
	  echo "Waiting for k0smotron CRDs..."; \
	  for crd in clusters.k0smotron.io k0smotroncontrolplanes.controlplane.cluster.x-k8s.io; do \
	    kubectl wait --for=condition=Established --timeout=2m crd/$$crd 2>/dev/null || true; \
	  done; \
	  echo "Smoke-testing k0smotron apiservice..."; \
	  for i in $$(seq 1 30); do \
	    kubectl get --raw /apis/k0smotron.io/v1beta2 >/dev/null 2>&1 && break; \
	    sleep 2; \
	  done; \
	  kubectl get --raw /apis/k0smotron.io/v1beta2 >/dev/null || { echo "ERROR: k0smotron.io/v1beta2 not serving"; exit 1; }'

# --------------------------------------------------------------------------- #

worker-limits:
	@cd $(TF_DIR); \
	WORKER_IPS=$$(terraform output -json worker_ips | jq -r '.[]'); \
	test -n "$$WORKER_IPS" || { echo "ERROR: worker_ips empty"; exit 1; }; \
	for w in $$WORKER_IPS; do \
	  echo "Applying worker limits on $$w"; \
	  ssh $(SSH_OPTS) ubuntu@$$w 'sudo bash -lc '"'"'set -euo pipefail; \
	    printf "%s\n" "fs.file-max = 2097152" "fs.inotify.max_user_instances = 8192" "fs.inotify.max_user_watches = 1048576" > /etc/sysctl.d/99-k0smotron-bench.conf; \
	    sysctl --system; \
	    printf "%s\n" "* soft nofile 1048576" "* hard nofile 1048576" "root soft nofile 1048576" "root hard nofile 1048576" > /etc/security/limits.d/99-k0smotron-bench.conf; \
	    mkdir -p /etc/systemd/system/k0sworker.service.d; \
	    printf "%s\n" "[Service]" "LimitNOFILE=1048576" "LimitNPROC=infinity" "TasksMax=infinity" > /etc/systemd/system/k0sworker.service.d/limits.conf; \
	    systemctl daemon-reload; \
	    systemctl restart k0sworker; \
	    systemctl show k0sworker -p LimitNOFILE -p LimitNPROC -p TasksMax'"'"'' & \
	done; \
	wait; \
	echo "Worker limits applied."

# --------------------------------------------------------------------------- #

bench:
	@OBSERVER_IP=$$(cd $(TF_DIR) && terraform output -raw observer_ip); \
	test -n "$$OBSERVER_IP" || { echo "ERROR: observer_ip empty"; exit 1; }; \
	BENCH_ENV=$$(cd $(TF_DIR) && terraform output -raw bench_env | awk '/^[[:space:]]*export[[:space:]]+BENCH_/ { sub(/^[[:space:]]*/, ""); print }' | tr '\n' ';'); \
	test -n "$$BENCH_ENV" || { echo "ERROR: terraform output bench_env did not contain BENCH_* exports"; exit 1; }; \
	SSH="ssh $(SSH_OPTS) ubuntu@$$OBSERVER_IP"; \
	echo "Running benchmark on observer $$OBSERVER_IP..."; \
	echo "Results will be at: ~/bench-results.csv on the observer"; \
	$$SSH "cd ~/k0smotron && \
	  source ~/.bashrc; \
	  $$BENCH_ENV \
	  $(if $(BENCH_STORAGE),export BENCH_STORAGE="$(BENCH_STORAGE)";) \
	  go test -tags bench -v -timeout $(BENCH_TIMEOUT) \
	    -bench.kubeconfig=\$$HOME/.kube/config \
	    -bench.report=\$$HOME/bench-results.csv \
	    -bench.parallel=$(BENCH_PARALLEL) \
	    -bench.k0s-version=$(BENCH_K0S_VER) \
	    $(if $(BENCH_STORAGE),-bench.storage=$(BENCH_STORAGE)) \
	    $(BENCH_LARGE) \
	    ./bench/ 2>&1 | tee \$$HOME/bench.log"; \
	echo "Benchmark complete. Run 'make prom-snapshot' to collect results."

# --------------------------------------------------------------------------- #

prom-snapshot:
	@OBSERVER_IP=$$(cd $(TF_DIR) && terraform output -raw observer_ip); \
	test -n "$$OBSERVER_IP" || { echo "ERROR: observer_ip empty"; exit 1; }; \
	echo "Taking Prometheus TSDB snapshot and copying locally..."; \
	ssh $(SSH_OPTS) ubuntu@$$OBSERVER_IP 'bash -s' < $(TF_DIR)scripts/prom-snapshot.sh; \
	scp $(SSH_OPTS) ubuntu@$$OBSERVER_IP:~/prom-snapshot.tar.gz ./prom-snapshot-$(RESULTS_TS).tar.gz; \
	echo "Saved to ./prom-snapshot-$(RESULTS_TS).tar.gz"; \
	echo "Extract: tar -xzf prom-snapshot-$(RESULTS_TS).tar.gz -C /tmp/prom-restore"

# --------------------------------------------------------------------------- #

destroy:
	@echo "Destroying infrastructure..."
	cd $(TF_DIR) && terraform destroy -auto-approve $(TF_VARS)
	@echo "Infrastructure destroyed."

# --------------------------------------------------------------------------- #
# Local k0smotron build + deploy
# --------------------------------------------------------------------------- #

image:
	@mkdir -p $(TF_DIR)out
	# Ensure a buildx builder that supports cross-platform builds.
	# The default `docker` driver doesn't; `docker-container` does.
	@docker buildx inspect bench-builder >/dev/null 2>&1 \
	  || docker buildx create --name bench-builder --driver docker-container
	@rm -f $(IMAGE_TAR)
	cd $(REPO_ROOT) && docker buildx build \
	  --builder bench-builder \
	  --platform=linux/amd64 \
	  --provenance=false \
	  --sbom=false \
	  --build-arg BUILD_IMG=golang:$$(awk -F'[ =]+' '/^GO_VERSION/ {print $$3}' Makefile.variables) \
	  -t $(LOCAL_IMG) \
	  -o type=docker,dest=$(IMAGE_TAR) \
	  .
	@echo "Image saved (linux/amd64): $(IMAGE_TAR)"
	@echo "Verifying platform in tar manifest:"
	@tar -xOf $(IMAGE_TAR) manifest.json 2>/dev/null | python3 -m json.tool 2>/dev/null | head -30 || true

manifests:
	@mkdir -p $(TF_DIR)out
	$(MAKE) -C $(REPO_ROOT) release IMG=$(LOCAL_IMG)
	cp $(REPO_ROOT)/install.yaml $(INSTALL_YAML)
	# image is imported locally on workers, not pulled from registry
	sed -i.bak 's/imagePullPolicy: IfNotPresent/imagePullPolicy: Never/g' $(INSTALL_YAML)
	sed -i.bak 's/imagePullPolicy: Always/imagePullPolicy: Never/g' $(INSTALL_YAML)
	rm -f $(INSTALL_YAML).bak
	@echo "Manifest: $(INSTALL_YAML)"

# Copy image tar to every worker in parallel, import into k0s containerd,
# then apply the local install.yaml via observer. cert-manager must already
# be installed (via `make monitoring`).
deploy-local: image manifests
	@cd $(TF_DIR); \
	OBSERVER_IP=$$(terraform output -raw observer_ip); \
	WORKER_IPS=$$(terraform output -json worker_ips | jq -r '.[]'); \
	test -n "$$OBSERVER_IP" || { echo "ERROR: observer_ip empty"; exit 1; }; \
	test -n "$$WORKER_IPS"  || { echo "ERROR: worker_ips empty"; exit 1; }; \
	echo "Copying image tar to workers:"; echo "$$WORKER_IPS"; \
	for w in $$WORKER_IPS; do \
	  scp $(SSH_OPTS) $(IMAGE_TAR) ubuntu@$$w:/tmp/k0smotron-image.tar & \
	done; wait; \
	echo "Importing image into k0s containerd on workers..."; \
	for w in $$WORKER_IPS; do \
	  ssh $(SSH_OPTS) ubuntu@$$w 'set -e; \
	    sudo k0s ctr images import /tmp/k0smotron-image.tar; \
	    sudo k0s ctr images ls -q | grep k0smotron || { echo "[$$(hostname)] IMPORT FAILED"; exit 1; }; \
	    rm /tmp/k0smotron-image.tar; \
	    echo "[$$(hostname)] OK"' & \
	done; wait; \
	echo "Copying install.yaml to observer and applying..."; \
	scp $(SSH_OPTS) $(INSTALL_YAML) ubuntu@$$OBSERVER_IP:~/k0smotron-install.yaml; \
	ssh $(SSH_OPTS) ubuntu@$$OBSERVER_IP 'for i in 1 2 3 4 5; do \
	    kubectl apply --server-side --force-conflicts -f ~/k0smotron-install.yaml && break; \
	    echo "apply attempt $$i failed (cert-manager webhook race?); retrying in 15s..."; \
	    sleep 15; \
	  done; \
	  kubectl -n k0smotron rollout restart deployment/k0smotron-controller-manager || true; \
	  kubectl -n k0smotron rollout status deployment/k0smotron-controller-manager --timeout=5m; \
	  echo "Waiting for k0smotron CRDs to be Established..."; \
	  for crd in clusters.k0smotron.io k0smotroncontrolplanes.controlplane.cluster.x-k8s.io; do \
	    kubectl wait --for=condition=Established --timeout=2m crd/$$crd 2>/dev/null || true; \
	  done; \
	  echo "Waiting for k0smotron webhook endpoints..."; \
	  for i in $$(seq 1 60); do \
	    [ -n "$$(kubectl -n k0smotron get endpoints -o jsonpath="{.items[*].subsets[*].addresses[*].ip}" 2>/dev/null)" ] && break; \
	    sleep 2; \
	  done; \
	  echo "Smoke-testing k0smotron apiservice..."; \
	  for i in $$(seq 1 30); do \
	    kubectl get --raw /apis/k0smotron.io/v1beta2 >/dev/null 2>&1 && break; \
	    sleep 2; \
	  done; \
	  kubectl get --raw /apis/k0smotron.io/v1beta2 >/dev/null || { echo "ERROR: k0smotron.io/v1beta2 not serving"; exit 1; }'; \
	echo "Local k0smotron deployed and ready."
