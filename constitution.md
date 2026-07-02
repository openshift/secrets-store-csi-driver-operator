# Secrets Store CSI Driver Operator — Architectural Constitution

**Version**: 1.0.0 | **Ratified**: 2026-07-02 | **Last Amended**: 2026-07-02

This document defines non-negotiable architectural rules for the `secrets-store-csi-driver-operator` repository. Every rule is grounded in observable repo evidence. Any AI tool, workflow, or contributor working on this repo MUST follow these principles. When a proposed change contradicts a principle, escalate rather than silently override.

<!-- openspec metadata — ignored by non-openspec tools -->
<!-- AgentRoutingMode: PROVIDED -->
<!-- Companion artifact: repo-assessment.md -->

## Core Principles

### I. Single Controller Pattern — Library-go CSIControllerSet Only

This operator uses **only** the library-go `csicontrollerset.CSIControllerSet` pattern. There is no controller-runtime, no addon controller framework, no separate `ctrl.Manager`. All reconciliation is driven by the CSIControllerSet chain in `RunOperator`. Any new operator capability MUST be expressed as either a new `CSIControllerSet` hook, a new static asset in `assets/`, or a new informer — never a separate reconciler loop.

**Evidence:** `pkg/operator/starter.go` — single `csicontrollerset.NewCSIControllerSet` chain; no imports of `sigs.k8s.io/controller-runtime`; `go.mod` does not list `controller-runtime`.

### II. Static Assets Are Embedded YAML — Never Hand-Regenerated

Operand manifests live in `assets/` and are embedded at compile time via `//go:embed *.yaml rbac/*.yaml network-policy/*.yaml` in `assets/assets.go`. They are applied via `ConditionalStaticResourcesController`. New assets MUST be plain YAML files added to `assets/` with `${NAMESPACE}` as the only runtime token. They are NOT generated from a script or Helm — edit them directly. Never add a bindata code-generation step.

**Evidence:** `assets/assets.go` — `//go:embed` directive; `pkg/operator/starter.go` — `replaceNamespaceFunc` replaces `${NAMESPACE}` at apply time; no `hack/update-*-manifests.sh` for assets.

### III. No Custom CRD Types — Use Standard `ClusterCSIDriver`

This operator does NOT define its own CRD. The configuration surface is the standard OpenShift `operator.openshift.io/v1.ClusterCSIDriver` singleton named `secrets-store.csi.k8s.io`. No new `api/` directory, no new `v1alpha1` types, no `zz_generated.deepcopy.go`, no `make generate` needed for API changes. Spec-driven behavior changes MUST be expressed through existing `ClusterCSIDriver` fields (managementState, logLevel, operatorLogLevel) or new controller hooks — not new CRD types.

**Evidence:** `pkg/operator/starter.go` — `gvr := opv1.SchemeGroupVersion.WithResource("clustercsidrivers")`; no `api/` directory in repo; `go.mod` imports `github.com/openshift/api` for `opv1` but defines no custom types.

### IV. Managed/Unmanaged/Removed States Are Mandatory

The operator is **removable** (`WithManagementStateController(operandName, true)`). All resource-sync logic MUST respect the three management states returned by `getOperatorSyncState`:
- `Managed`: apply/sync resources
- `Unmanaged`: skip sync (leave resources as-is)
- `Removed`: delete conditional static resources

Any new controller logic that touches cluster resources MUST gate on the operator sync state. Never apply resources unconditionally.

**Evidence:** `pkg/operator/starter.go` — `WithManagementStateController(..., true)`; `ConditionalStaticResourcesController` uses `getOperatorSyncState` predicates; `getOperatorSyncState` function handles `DeletionTimestamp` as `Removed`.

### V. Verification-First Development — `make check` Before Every PR

All changes MUST pass: `make check` (chains `make verify` + `make test-unit`). E2E (`make test-e2e`) is run in CI and requires a live cluster. Do NOT skip `make check` for "trivial" changes. If the FIPS build (`GOEXPERIMENT=strictfipsruntime`) is available, the binary MUST compile with it — any `CGO_ENABLED=1` dependency must be respected.

**Evidence:** `Makefile` — `check: | verify test-unit`; `GO_TEST_PACKAGES :=./pkg/... ./cmd/...`; FIPS conditional block in `Makefile`; `AGENTS.md` testing table.

### VI. RBAC Is Least-Privilege and Asset-Driven

All RBAC for the operand is defined as explicit YAML manifests in `assets/rbac/`. The operator applies privileged SCC binding (`rbac/node_privileged_binding.yaml`) and `SecretProviderClass` role/binding ONLY when in `Managed` state via `ConditionalStaticResourcesController`. New RBAC requirements MUST be added as YAML files in `assets/rbac/` and registered in the asset list — never granted inline or dynamically at runtime.

**Evidence:** `assets/rbac/` — `privileged_role.yaml`, `node_privileged_binding.yaml`, `secretproviderclasses_role.yaml`, `secretproviderclasses_binding.yaml`; `pkg/operator/starter.go` — asset list in `ConditionalStaticResourcesController`.

### VII. Namespace Isolation — Operator Namespace Is Runtime-Determined

The operator namespace is NOT hardcoded in Go code — it is passed via `controllerConfig.OperatorNamespace` at runtime. Assets use the `${NAMESPACE}` token which is replaced at apply time via `replaceNamespaceFunc`. Any new namespace-scoped asset MUST use `${NAMESPACE}` instead of a literal namespace string. The standard runtime namespace is `openshift-cluster-csi-drivers`.

**Evidence:** `pkg/operator/starter.go` — `operatorNamespace := controllerConfig.OperatorNamespace`; `replaceNamespaceFunc` replaces `${NAMESPACE}` in all assets; `README.md` — `--namespace openshift-cluster-csi-drivers`.

### VIII. Trusted CA Bundle Propagation Is Mandatory for DaemonSet

The DaemonSet (`node.yaml`) MUST always be deployed with the CA bundle hook (`csidrivernodeservicecontroller.WithCABundleDaemonSetHook`). This injects the CNO-managed trusted CA ConfigMap (`secrets-store-csi-driver-trusted-ca-bundle`, generated from `cabundle_cm.yaml`) into the DaemonSet for FIPS/proxy compatibility. Any change to the DaemonSet configuration must preserve this hook.

**Evidence:** `pkg/operator/starter.go` — `csidrivernodeservicecontroller.WithCABundleDaemonSetHook(operatorNamespace, trustedCAConfigMap, configMapInformer)`; `assets/cabundle_cm.yaml` — ConfigMap with `config.openshift.io/inject-trusted-cabundle: "true"`.

### IX. OLM Bundle and Version Conventions

The operator ships via OLM. Bundle artifacts live under `config/manifests/` and `config/metadata/`. OCP version bumps MUST go through `hack/update-metadata.sh VERSION=X.Y` which updates `package.yaml`, `*.clusterserviceversion.yaml`, `README.md`, and `Makefile`. Never manually edit version strings across these files — always use the script. The channel convention is `stable-v1` with the current OCP minor as suffix.

**Evidence:** `Makefile` — `metadata: ensure-yq; ./hack/update-metadata.sh`; `hack/update-metadata.sh`; `config/manifests/secrets-store-csi-driver-operator.package.yaml`; `README.md` — bump-metadata section.

### X. Vendor Mode — Dependencies Must Be Vendored

Dependencies are vendored. Never add a dependency without running `go mod tidy && go mod vendor`. Do NOT modify `vendor/` directly. The `.snyk` file tracks security policy — do not remove it.

**Evidence:** `vendor/modules.txt` present; `.snyk` file present; `build-machinery-go` include `targets/openshift/deps-gomod.mk` manages module hygiene.

## Additional Constraints

- **Go version**: Match `go.mod` directive — currently `go 1.25.0`. — **Evidence:** `go.mod` line `go 1.25.0`
- **Module path**: `github.com/openshift/secrets-store-csi-driver-operator`. Local imports placed after third-party imports. — **Evidence:** `go.mod`
- **Container base image**: UBI-based; operator binary runs as non-root. — **Evidence:** `Dockerfile.openshift`
- **Build tags**: FIPS-capable builds require `strictfipsruntime,openssl` tags and `CGO_ENABLED=1`. — **Evidence:** `Makefile` FIPS block
- **CI system**: Prow via `openshift/release`; no `.github/workflows` in this repo. — **Evidence:** `.ci-operator.yaml`; `AGENTS.md`
- **E2E script**: `hack/e2e.sh` is the e2e entry point (not a Makefile pattern match). — **Evidence:** `Makefile` `test-e2e` target
- **Image registry**: CI uses `registry.svc.ci.openshift.org/ocp/4.22:secrets-store-csi-driver-operator`. — **Evidence:** `Makefile` `IMAGE_REGISTRY`
- **No feature gates**: This operator has no `features.go` or operator-level feature gate framework. — **Evidence:** No `features.go` in repo; no `FeatureGate` imports in `go.mod`

## Development Workflow

| Activity | Requirement | Evidence |
|----------|-------------|----------|
| Local unit tests | `make test-unit` (`go test ./pkg/... ./cmd/...`) | `Makefile` `GO_TEST_PACKAGES` |
| Full verify + unit | `make check` | `Makefile` `check` target |
| E2E tests | `make test-e2e` (requires live cluster + `openshift-tests` in PATH) | `Makefile` `test-e2e`, `hack/e2e.sh` |
| OCP version bump | `make metadata VERSION=4.X` | `hack/update-metadata.sh` |
| Bundle generation | Manual via `hack/create-bundle` script | `hack/create-bundle` |
| Image build | `make image-build image-push` | `Makefile` build-machinery-go targets |
| PR pre-merge | `make check`; commit all changes | `AGENTS.md` |
| PR scope | Small diffs; follow existing CSIControllerSet pattern | `AGENTS.md` |

## Code Ownership

| Area | Scope | Key paths |
|------|-------|-----------|
| Controller logic | CSIControllerSet wiring, operator bootstrap, informers | `pkg/operator/starter.go` |
| Static assets | YAML manifests, RBAC, NetworkPolicy | `assets/`, `assets/rbac/`, `assets/network-policy/` |
| OLM / release | Bundle, CSV, OCP version bumps | `config/manifests/`, `hack/update-metadata.sh` |
| Tests | E2E and unit tests | `pkg/operator/*_test.go`, `hack/e2e.sh` |
| Docs | User-facing documentation | `README.md`, `must-gather/` |

## Governance

- **Amendments**: any principle change requires documented repo evidence and a Version + Last Amended date bump.
- **Conflicts**: when a proposed change contradicts a principle, surface the conflict explicitly — do not silently override. Flag it in design review, PR comments, or planning artifacts before proceeding.
- **Companion docs**:
  - **AGENTS.md** is authoritative for architecture, build commands, controller map, and test instructions.
  - **README.md** is authoritative for human-facing install and local-run procedures.
  - **This constitution** is authoritative for non-negotiable architectural guardrails.
- **New patterns**: any deviation from the existing CSIControllerSet model (e.g. adding controller-runtime, a new CRD type, a separate manager) requires explicit justification and a constitution amendment before implementation.
