CURPATH=$(PWD)
BIN_PATH=$(CURPATH)/bin
YQ = $(BIN_PATH)/yq
YQ_VERSION = v4.47.1
export PATH := $(BIN_PATH):$(PATH)

all: build
.PHONY: all

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/deps-gomod.mk \
	targets/openshift/images.mk \
	targets/openshift/yq.mk \
)

# Bump OCP version in CSV and OLM metadata
# Also updates the Makefile and README.md to the new version
#
# Example:
#   make metadata VERSION=4.20.0
metadata: ensure-yq
ifdef VERSION
	./hack/update-metadata.sh $(VERSION)
else
	./hack/update-metadata.sh
endif
.PHONY: metadata

# Check if GOEXPERIMENT=strictfipsruntime is supported
GOEXPERIMENT_SUPPORTED := $(shell GOEXPERIMENT=strictfipsruntime go version >/dev/null 2>&1 && echo "true" || echo "false")

ifeq ($(GOEXPERIMENT_SUPPORTED),true)
$(info strictfipsruntime is supported, building with FIPS compliance)
GO :=CGO_ENABLED=1 GOEXPERIMENT=strictfipsruntime go
GO_BUILD_FLAGS :=-trimpath -tags strictfipsruntime,openssl
else
$(warning WARN: building without FIPS support, GOEXPERIMENT strictfipsruntime is not available in the go compiler)
$(warning WARN: this build cannot be used in CI or production, due to lack of FIPS!!)
GO :=CGO_ENABLED=1 go
GO_BUILD_FLAGS :=-trimpath
endif

# Run core verification and all self contained tests.
#
# Example:
#   make check
check: | verify test-unit
.PHONY: check

IMAGE_REGISTRY?=registry.svc.ci.openshift.org

# This will call a macro called "build-image" which will generate image specific targets based on the parameters:
# $0 - macro name
# $1 - target name
# $2 - image ref
# $3 - Dockerfile path
# $4 - context directory for image build
# It will generate target "image-$(1)" for building the image and binding it as a prerequisite to target "images".
$(call build-image,secrets-store-csi-driver-operator,$(IMAGE_REGISTRY)/ocp/5.0:secrets-store-csi-driver-operator,./Dockerfile.openshift,.)

clean: clean-yq
	$(RM) secrets-store-csi-driver-operator
.PHONY: clean

GO_TEST_PACKAGES :=./pkg/... ./cmd/...

# RUN_IRREVERSIBLE_E2E=true also runs specs that permanently switch
# tokenRequests.type to "Managed" on the target cluster, with no way back.
# Defaults to true since CI always runs against a fresh cluster;
# pass RUN_IRREVERSIBLE_E2E=false to skip these specs (e.g. against a
# persistent local/dev cluster).
RUN_IRREVERSIBLE_E2E ?= true

test-e2e:
	hack/e2e.sh
	RUN_IRREVERSIBLE_E2E=$(RUN_IRREVERSIBLE_E2E) go test ./test/e2e -v -timeout 60m -args -ginkgo.vv -ginkgo.poll-progress-after=30s -ginkgo.poll-progress-interval=30s

.PHONY: test-e2e

##@ E2E Coverage

.PHONY: build-coverage
build-coverage: ## Build the operator binary with coverage instrumentation.
	$(GO) build $(GO_MOD_FLAGS) $(GO_BUILD_FLAGS) $(GO_LD_FLAGS) \
		-cover -covermode=atomic -coverpkg=./... \
		-o secrets-store-csi-driver-operator \
		./cmd/secrets-store-csi-driver-operator

COVERAGE_IMG ?= $(IMAGE_REGISTRY)/ocp/4.22:secrets-store-csi-driver-operator-e2e-coverage

.PHONY: docker-build-coverage
docker-build-coverage: ## Build coverage-instrumented Docker image.
	$(IMAGE_BUILD_BUILDER) $(IMAGE_BUILD_DEFAULT_FLAGS) -t $(COVERAGE_IMG) -f Dockerfile.coverage .

.PHONY: docker-push-coverage
docker-push-coverage: ## Push coverage Docker image.
	$(IMAGE_BUILD_BUILDER) push $(COVERAGE_IMG)

.PHONY: e2e-coverage-collect
e2e-coverage-collect: ## Collect e2e coverage data and optionally upload to Codecov.
	ARTIFACT_DIR=$${ARTIFACT_DIR:-.} hack/e2e-coverage.sh collect
