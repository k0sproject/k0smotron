###############################################################################
# k0smotron benchmark suite
#
# One-command full run (provision EC2 + run both tests + destroy):
#   make run KEY=my-ec2-key CIDR=$(curl -s -4 ifconfig.me)/32
#
# Run against an existing cluster:
#   make scale KUBECONFIG=/path/to/kubeconfig
#   make perf  KUBECONFIG=/path/to/kubeconfig
###############################################################################

SHELL := /bin/bash
.DEFAULT_GOAL := help

BENCH_DIR     := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
INFRA_DIR     := $(BENCH_DIR)infra

# ── Test flags ────────────────────────────────────────────────────────────── #
# Kubeconfig defaults to a bench-owned file. Never writes to $$HOME/.kube/config.
KUBECONFIG    ?= $(BENCH_DIR)kubeconfig
BENCH_ENV     ?= $(BENCH_DIR)bench.env
BENCH_STORAGE ?=
BENCH_LARGE   ?=
BENCH_PARALLEL?= 10
BENCH_K0S_VER ?= v1.31.2-k0s.0
SCALE_TIMEOUT ?= 4h
PERF_TIMEOUT  ?= 2h
PERF_OPS      ?= 500
PERF_CONCURRENCY ?= 10

# ── Infra pass-through ────────────────────────────────────────────────────── #
KEY           ?=
CIDR          ?= 0.0.0.0/0
SSH_KEY       ?= ~/.ssh/id_rsa

LOCAL_IMG     ?= k0smotron:bench-local

CP_INSTANCE_TYPE     ?=
CP_ETCD_IOPS         ?=
WORKER_INSTANCE_TYPE ?=

TF_EXTRA_VARS = \
  $(if $(CP_INSTANCE_TYPE),-var cp_instance_type=$(CP_INSTANCE_TYPE)) \
  $(if $(CP_ETCD_IOPS),-var cp_etcd_iops=$(CP_ETCD_IOPS)) \
  $(if $(WORKER_INSTANCE_TYPE),-var worker_instance_type=$(WORKER_INSTANCE_TYPE))

INFRA_MAKE = $(MAKE) -C $(INFRA_DIR) \
  KEY=$(KEY) CIDR=$(CIDR) SSH_KEY=$(SSH_KEY) \
  LOCAL_IMG=$(LOCAL_IMG) \
  TF_EXTRA_VARS="$(TF_EXTRA_VARS)" \
  BENCH_STORAGE=$(BENCH_STORAGE) BENCH_LARGE=$(BENCH_LARGE) \
  BENCH_PARALLEL=$(BENCH_PARALLEL) BENCH_K0S_VER=$(BENCH_K0S_VER) \
  BENCH_TIMEOUT=$(SCALE_TIMEOUT)

GO_TEST = go test -tags bench -v \
  -bench.kubeconfig=$(KUBECONFIG) \
  -bench.k0s-version=$(BENCH_K0S_VER) \
  -bench.parallel=$(BENCH_PARALLEL) \
  $(if $(BENCH_STORAGE),-bench.storage=$(BENCH_STORAGE)) \
  $(BENCH_LARGE)

define LOAD_BENCH_ENV
BENCH_POSTGRES_URL_OVERRIDE=$${BENCH_POSTGRES_URL:-}; \
BENCH_MYSQL_URL_OVERRIDE=$${BENCH_MYSQL_URL:-}; \
BENCH_WORKER_EXTERNAL_ADDRESSES_OVERRIDE=$${BENCH_WORKER_EXTERNAL_ADDRESSES:-}; \
if [ -f "$(BENCH_ENV)" ]; then \
  set -a; . "$(BENCH_ENV)"; set +a; \
fi; \
if [ -z "$${BENCH_POSTGRES_URL:-}" ] || [ -z "$${BENCH_MYSQL_URL:-}" ] || [ -z "$${BENCH_WORKER_EXTERNAL_ADDRESSES:-}" ]; then \
  TF_ENV=$$(cd "$(INFRA_DIR)" && terraform output -raw bench_env 2>/dev/null \
    | awk '/^[[:space:]]*export[[:space:]]+BENCH_/ { sub(/^[[:space:]]*/, ""); print }'); \
  if [ -n "$$TF_ENV" ]; then \
    eval "$$TF_ENV"; \
    export BENCH_POSTGRES_URL BENCH_MYSQL_URL BENCH_WORKER_EXTERNAL_ADDRESSES; \
  fi; \
fi; \
if [ -z "$${BENCH_WORKER_EXTERNAL_ADDRESSES:-}" ]; then \
  WORKER_ADDRS=$$(cd "$(INFRA_DIR)" && terraform output -json worker_ips 2>/dev/null | jq -r 'join(",")' 2>/dev/null || true); \
  if [ -n "$$WORKER_ADDRS" ]; then export BENCH_WORKER_EXTERNAL_ADDRESSES="$$WORKER_ADDRS"; fi; \
fi; \
if [ -n "$$BENCH_POSTGRES_URL_OVERRIDE" ]; then export BENCH_POSTGRES_URL="$$BENCH_POSTGRES_URL_OVERRIDE"; fi; \
if [ -n "$$BENCH_MYSQL_URL_OVERRIDE" ]; then export BENCH_MYSQL_URL="$$BENCH_MYSQL_URL_OVERRIDE"; fi; \
if [ -n "$$BENCH_WORKER_EXTERNAL_ADDRESSES_OVERRIDE" ]; then export BENCH_WORKER_EXTERNAL_ADDRESSES="$$BENCH_WORKER_EXTERNAL_ADDRESSES_OVERRIDE"; fi; \
if [ -z "$${BENCH_POSTGRES_URL:-}" ]; then echo "WARNING: BENCH_POSTGRES_URL not set — kine-postgres will be skipped"; fi; \
if [ -z "$${BENCH_MYSQL_URL:-}" ]; then echo "WARNING: BENCH_MYSQL_URL not set — kine-mysql will be skipped"; fi
endef

.PHONY: help run scale perf all kubeconfig bench-env infra-up infra-down worker-limits k0smotron-stable deploy-local

help:
	@echo "Targets:"
	@echo "  run         provision + scale + perf + destroy  (requires KEY=)"
	@echo "  scale       run TestScaleMatrix  (auto-fetches kubeconfig)"
	@echo "  perf        run TestStoragePerformance  (auto-fetches kubeconfig)"
	@echo "  all         scale + perf"
	@echo "  kubeconfig  fetch kubeconfig from observer → $(BENCH_DIR)kubeconfig"
	@echo ""
	@echo "  infra-up         provision EC2 cluster (requires KEY=)"
	@echo "  worker-limits    raise worker node file-descriptor limits"
	@echo "  k0smotron-stable install stable k0smotron from docs.k0smotron.io"
	@echo "  deploy-local     build + deploy local k0smotron image to the cluster"
	@echo "  infra-down       destroy EC2 cluster"
	@echo ""
	@echo "Key vars: KUBECONFIG KEY CIDR BENCH_STORAGE BENCH_LARGE"
	@echo "          BENCH_PARALLEL BENCH_K0S_VER SCALE_TIMEOUT PERF_TIMEOUT"
	@echo "          PERF_OPS PERF_CONCURRENCY"
	@echo "          BENCH_ENV"

# ── Kubeconfig ────────────────────────────────────────────────────────────── #
# Never touches $$HOME/.kube/config. Writes to ./kubeconfig only.

kubeconfig: $(KUBECONFIG)

$(KUBECONFIG):
	@echo "Fetching kubeconfig from observer → $@"
	@cd $(INFRA_DIR) && \
	  OBSERVER_IP=$$(terraform output -raw observer_ip) && \
	  CP_IP=$$(terraform output -json control_plane_ips | sed -E 's/[][",]//g' | xargs | awk '{print $$1}') && \
	  test -n "$$OBSERVER_IP" && test -n "$$CP_IP" || { echo "ERROR: terraform outputs not found"; exit 1; } && \
	  ssh -o StrictHostKeyChecking=no -o BatchMode=yes -i $(SSH_KEY) \
	    ubuntu@$$OBSERVER_IP 'cat ~/.kube/config' > $@.tmp && \
	  sed -i.bak -E "s|server: .*|server: https://$$CP_IP:6443|" $@.tmp && \
	  rm -f $@.tmp.bak && \
	  mv $@.tmp $@
	@echo "Wrote $@  (server: $$(awk '/server:/ {print $$2; exit}' $@))"

# ── Full end-to-end ───────────────────────────────────────────────────────── #

run: infra-up deploy-local all infra-down

# ── Individual test targets ───────────────────────────────────────────────── #

scale: kubeconfig bench-env
	@$(LOAD_BENCH_ENV); \
	$(GO_TEST) \
	  -timeout $(SCALE_TIMEOUT) \
	  -bench.report=$(BENCH_DIR)bench-results.csv \
	  -run TestScaleMatrix \
	  $(BENCH_DIR)

perf: kubeconfig bench-env
	@$(LOAD_BENCH_ENV); \
	$(GO_TEST) \
	  -timeout $(PERF_TIMEOUT) \
	  -bench.perf-report=$(BENCH_DIR)bench-perf-results.csv \
	  -bench.perf-ops=$(PERF_OPS) \
	  -bench.perf-concurrency=$(PERF_CONCURRENCY) \
	  -run TestStoragePerformance \
	  $(BENCH_DIR)

all: scale perf

bench-env:
	@mkdir -p "$(BENCH_DIR)"
	@if cd "$(INFRA_DIR)" && terraform output -raw bench_env >/tmp/k0smotron-bench-env.$$PPID 2>/dev/null; then \
	  awk '/^[[:space:]]*export[[:space:]]+BENCH_/ { sub(/^[[:space:]]*/, ""); print }' /tmp/k0smotron-bench-env.$$PPID > /tmp/k0smotron-bench-env-parsed.$$PPID; \
	  WORKER_ADDRS=$$(cd "$(INFRA_DIR)" && terraform output -json worker_ips 2>/dev/null | jq -r 'join(",")' 2>/dev/null || true); \
	  if [ -n "$$WORKER_ADDRS" ] && ! grep -q '^export BENCH_WORKER_EXTERNAL_ADDRESSES=' /tmp/k0smotron-bench-env-parsed.$$PPID; then \
	    echo "export BENCH_WORKER_EXTERNAL_ADDRESSES=\"$$WORKER_ADDRS\"" >> /tmp/k0smotron-bench-env-parsed.$$PPID; \
	  fi; \
	  if [ -s /tmp/k0smotron-bench-env-parsed.$$PPID ]; then \
	    { echo "# Generated by make bench-env. Do not commit."; cat /tmp/k0smotron-bench-env-parsed.$$PPID; } > "$(BENCH_ENV)"; \
	    echo "Wrote benchmark storage env → $(BENCH_ENV)"; \
	  else \
	    if [ -f "$(BENCH_ENV)" ] && grep -q '^# Generated by make bench-env' "$(BENCH_ENV)"; then rm -f "$(BENCH_ENV)"; fi; \
	    echo "Terraform bench_env did not include BENCH_* exports; using BENCH_* variables from the current environment."; \
	  fi; \
	  rm -f /tmp/k0smotron-bench-env.$$PPID /tmp/k0smotron-bench-env-parsed.$$PPID; \
	else \
	  rm -f /tmp/k0smotron-bench-env.$$PPID; \
	  echo "No Terraform bench_env output found; using BENCH_* variables from the current environment."; \
	fi

# ── Infrastructure ────────────────────────────────────────────────────────── #

infra-up:
	$(INFRA_MAKE) provision wait monitoring
	$(MAKE) bench-env

infra-down:
	$(INFRA_MAKE) destroy

worker-limits:
	$(INFRA_MAKE) worker-limits

# Install stable k0smotron from docs.k0smotron.io
k0smotron-stable:
	$(INFRA_MAKE) k0smotron-stable

# Build local k0smotron image + deploy (requires infra-up first)
deploy-local:
	$(INFRA_MAKE) deploy-local
