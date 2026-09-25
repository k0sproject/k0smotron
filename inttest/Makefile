ARCH = $(shell go env GOARCH)
OS = $(shell go env GOOS)
FOOTLOOSE_IMAGE ?= footloose-alpine
K0SMOTRON_IMAGES_BUNDLE ?= $(realpath ../k0smotron-image-bundle.tar)
K0SMOTRON_STANDALONE_INSTALL_YAML ?= $(realpath ../install-standalone.yaml)
LOCAL_STORAGE_INSTALL_YAML ?= $(realpath ./footloose-alpine/local-path-storage.yaml)
CERT_MANAGER_INSTALL_YAML ?= $(realpath ./footloose-alpine/cert-manager.yaml)
WEBHOOK_CHECKER_INSTALL_YAML ?= $(realpath ./footloose-alpine/webhook-checker.yaml)
K0S_USE_DEFAULT_K0S_BINARIES ?= true
K0S_VERSION ?= v1.30.2
K8S_VERSION ?= v$(kubernetes_version)
# Tag of the kindest/node image the kubeadm-node image is built from. Must be
# a real published tag (see https://github.com/kubernetes-sigs/kind/releases),
# and is also what KubeadmControlPlane.spec.version is set to for that test,
# since the workload cluster's version has to match the kubelet/kubeadm
# baked into the image.
KUBEADM_NODE_IMAGE_VERSION ?= v1.31.6
TIMEOUT ?= 8m

.PHONY: $(smoketests)
include Makefile.variables

footloose_alpine_build_cmdline := \
	--build-arg GOLANG_IMAGE=$(golang_buildimage) \
	--build-arg ALPINE_VERSION=$(alpine_patch_version) \
	--build-arg KUBE_VERSION=$(kubernetes_version) \
	-t footloose-alpine \
	-f footloose-alpine/Dockerfile \
	footloose-alpine

.footloose-alpine.stamp: Makefile.variables footloose-alpine/Dockerfile
	docker build --build-arg TARGETARCH=$(ARCH) $(footloose_alpine_build_cmdline)
	touch $@

kubeadm_node_build_cmdline := \
	--build-arg NODE_IMAGE=kindest/node:$(KUBEADM_NODE_IMAGE_VERSION) \
	-t kubeadm-node \
	-f kubeadm-node/Dockerfile \
	kubeadm-node

.kubeadm-node.stamp: Makefile.variables kubeadm-node/Dockerfile
	docker build $(kubeadm_node_build_cmdline)
	touch $@

$(smoketests): K0S_PATH ?= $(realpath ../k0s)
$(smoketests): TEST_PACKAGE ?= $(subst check-,,$@)
$(smoketests): ../k0smotron-image-bundle.tar ../install-standalone.yaml .footloose-alpine.stamp
	FOOTLOOSE_IMAGE='$(FOOTLOOSE_IMAGE)' \
	K0SMOTRON_IMAGES_BUNDLE='$(K0SMOTRON_IMAGES_BUNDLE)' \
	K0SMOTRON_STANDALONE_INSTALL_YAML='$(K0SMOTRON_STANDALONE_INSTALL_YAML)' \
	K0S_VERSION='$(K0S_VERSION)' \
	K8S_VERSION='$(K8S_VERSION)' \
	LOCAL_STORAGE_INSTALL_YAML='$(LOCAL_STORAGE_INSTALL_YAML)' \
	CERT_MANAGER_INSTALL_YAML='$(CERT_MANAGER_INSTALL_YAML)' \
	WEBHOOK_CHECKER_INSTALL_YAML='$(WEBHOOK_CHECKER_INSTALL_YAML)' \
	K0S_USE_DEFAULT_K0S_BINARIES=$(K0S_USE_DEFAULT_K0S_BINARIES) \
	go test -count=1 -v -timeout $(TIMEOUT) github.com/k0sproject/k0smotron/v2/inttest/$(TEST_PACKAGE)

clean:
	rm -rf .*.stamp

check-scaling-etcd: TIMEOUT=10m
check-monitoring: TIMEOUT=7m
check-ha-controller: TIMEOUT=7m
check-ha-controller-nats: TIMEOUT=10m
check-pvc: TIMEOUT=15m
check-pvc: LOCAL_STORAGE_INSTALL_YAML=$(realpath ./footloose-alpine/seaweedfs.yaml)
check-capi-controlplane-docker-tunneling: TIMEOUT=10m
check-capi-controlplane-docker-downscaling: TIMEOUT=15m
check-capi-controlplane-docker-etcd-health: TIMEOUT=25m
check-capi-remote-machine: TIMEOUT=12m
check-capi-remote-machine-template: TIMEOUT=12m
check-capi-remote-machine-template-update: TIMEOUT=10m
check-capi-docker-machine-template-update: TIMEOUT=15m
check-capi-docker-machine-template-update-recreate: TIMEOUT=25m
check-capi-docker-machine-template-update-recreate-kine: TIMEOUT=15m
check-capi-docker-machine-change-template: TIMEOUT=35m
check-capi-docker-machine-change-args: TIMEOUT=20m
check-capi-remote-machine-job-provision: TIMEOUT=10m
check-capi-multi-instance-watch-selector: TIMEOUT=10m
check-upgrade: TIMEOUT=20m
check-capi-controlplane-kubeadm-remote-machine: TIMEOUT=15m
check-capi-controlplane-kubeadm-remote-machine: K8S_VERSION=$(KUBEADM_NODE_IMAGE_VERSION)
check-capi-controlplane-kubeadm-remote-machine: .kubeadm-node.stamp
