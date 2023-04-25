ARCH = $(shell go env GOARCH)
OS = $(shell go env GOOS)
FOOTLOOSE_IMAGE ?= footloose-alpine
K0SMOTRON_IMAGES_BUNDLE ?= $(realpath ../k0smotron-image-bundle.tar)
K0SMOTRON_INSTALL_YAML ?= $(realpath ../install.yaml)
K0S_USE_DEFAULT_K0S_BINARIES ?= true
TIMEOUT ?= 3m

.PHONY: $(smoketests)
include Makefile.variables

footloose_alpine_build_cmdline := \
	--build-arg GOLANG_IMAGE=$(golang_buildimage) \
	--build-arg ALPINE_VERSION=$(alpine_patch_version) \
	--build-arg KUBE_VERSION=$(kubernetes_version) \
	-t footloose-alpine \
	-f footloose-alpine/Dockerfile \
	footloose-alpine

.footloose-alpine.stamp:
	docker build --build-arg TARGETARCH=$(ARCH) $(footloose_alpine_build_cmdline)
	touch $@

$(smoketests): K0S_PATH ?= $(realpath ../k0s)
$(smoketests): TEST_PACKAGE ?= $(subst check-,,$@)
$(smoketests): ../k0smotron-image-bundle.tar .footloose-alpine.stamp
	FOOTLOOSE_IMAGE='$(FOOTLOOSE_IMAGE)' \
	K0SMOTRON_IMAGES_BUNDLE='$(K0SMOTRON_IMAGES_BUNDLE)' \
	K0SMOTRON_INSTALL_YAML='$(K0SMOTRON_INSTALL_YAML)' \
	K0S_USE_DEFAULT_K0S_BINARIES=$(K0S_USE_DEFAULT_K0S_BINARIES) \
	go test -count=1 -v -timeout $(TIMEOUT) github.com/k0sproject/k0smotron/inttest/$(TEST_PACKAGE)

clean:
	rm -rf .*.stamp
