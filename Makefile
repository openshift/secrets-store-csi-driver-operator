all: build
.PHONY: all

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/deps-gomod.mk \
	targets/openshift/images.mk \
)

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
$(call build-image,secrets-store-csi-driver-operator,$(IMAGE_REGISTRY)/ocp/4.21:secrets-store-csi-driver-operator,./Dockerfile.openshift,.)

clean:
	$(RM) secrets-store-csi-driver-operator
.PHONY: clean

GO_TEST_PACKAGES :=./pkg/... ./cmd/...

# Run e2e tests. Requires openshift-tests in $PATH.
#
# Example:
#   make test-e2e
test-e2e:
	hack/e2e.sh

.PHONY: test-e2e
