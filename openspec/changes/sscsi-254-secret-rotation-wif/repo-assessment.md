# Repository Assessment Report
**Feature:** Configurable Secret Rotation and Workload Identity Federation (SSCSI-254)

---

## 0. Inputs & Tooling

- `repo`: `/Users/ckyal/go/src/github.com/chiragkyal/secrets-store-csi-driver-operator` (working-folder mode; `use_working_folder_as_repo: true`)
- `branch`: main (local working copy, post-rebase)
- `commit`: HEAD (current state as of 2026-07-03)
- `tooling_status`: OK — all source files and vendor tree readable
- **Spec status**: PASS (validation score 86%, specs approved). Feature adds `secretRotation` and `tokenRequests` configuration to `ClusterCSIDriver` spec; operator must propagate these to the DaemonSet args and the cluster-scoped `CSIDriver` resource.

---

## 1. Architecture Overview

### 1.1 Project Type & Tech Stack

| Item | Value |
|------|-------|
| Language | Go 1.25 |
| Build system | GNU Make (via `build-machinery-go` shared Makefile includes) |
| Operator framework | `library-go` `csicontrollerset` (NOT controller-runtime) |
| Key API dependency | `github.com/openshift/api v0.0.0-20260302174620` |
| Key framework dependency | `github.com/openshift/library-go v0.0.0-20260303171201` |
| Client dependency | `github.com/openshift/client-go v0.0.0-20260302182750` |
| Kubernetes client | `k8s.io/client-go v0.35.2` |
| FIPS | `GOEXPERIMENT=strictfipsruntime`, `CGO_ENABLED=1`, tags `strictfipsruntime,openssl` |

### 1.2 Component Map

| Package/Dir | Responsibility | Hand-written? |
|---|---|---|
| `cmd/secrets-store-csi-driver-operator/` | CLI wiring (cobra + `controllercmd.NewControllerCommandConfig`). No business logic. | Hand-written |
| `pkg/operator/` | `RunOperator`: creates clients, informers, and the `CSIControllerSet` chain. **All controller composition is here.** | Hand-written |
| `pkg/version/` | Prometheus gauge for build version (ldflags injection). | Hand-written |
| `pkg/dependencymagnet/` | Build-tag-guarded import to keep `build-machinery-go` vendored. **Do not edit.** | Hand-written |
| `assets/` | Static YAML manifests embedded via `go:embed`. `ReadFile(name)` is the only API. | Hand-written |
| `config/manifests/stable/` | OLM bundle: CSV, CRDs, ConsoleQuickStart, ConsoleYAMLSamples. | Hand-written |
| `config/metadata/` | OLM bundle annotations. | Hand-written |
| `vendor/` | All dependencies; committed to the repo. **Do not add un-vendored imports.** | Generated/vendored |

Generated code (do not hand-edit):
- `vendor/github.com/openshift/api/operator/v1/zz_generated.*` — deepcopy, swagger docs
- `vendor/github.com/openshift/client-go/operator/applyconfigurations/` — apply configurations
- `vendor/github.com/openshift/library-go/pkg/operator/csi/` — CSI controller set framework

### 1.3 Framework & Pattern Architecture

The operator is built on `library-go`'s `CSIControllerSet` pattern. There is **no controller-runtime** in this codebase — do not introduce `sigs.k8s.io/controller-runtime` reconcilers.

**Entry point**: `cmd/secrets-store-csi-driver-operator/main.go` → `operator.RunOperator(ctx, controllerConfig)`.

**Bootstrap sequence** (`pkg/operator/starter.go:RunOperator`):
1. Create `kubeClient` (core Kubernetes)
2. Create `kubeInformersForNamespaces` (scoped to `operatorNamespace` + `""`)
3. Create `configClient` + `configInformers` (for cluster infra, proxy, apiserver)
4. Create `operatorClient` via `goc.NewClusterScopedOperatorClientWithConfigName` — wraps the `ClusterCSIDriver` CR (identified by `providerName = "secrets-store.csi.k8s.io"`) using `extractOperatorSpec` / `extractOperatorStatus` as the typed extract functions
5. Create `dynamicClient`
6. Build and wire `CSIControllerSet` (method chain)
7. Start informers and run controller set

**Dead-code trap**: `extractOperatorSpec` and `extractOperatorStatus` in `starter.go` only extract the base `OperatorSpec`/`OperatorStatus` from the apply configuration. They do NOT surface driver-specific fields (e.g., `driverConfig.secretsStore`). For SSCSI-254, the new configuration fields must be read directly from the `ClusterCSIDriver` object via the `operatorClient`, not from the apply extract functions.

### 1.4 Runtime Data/Control Flow

```
ClusterCSIDriver CR change
    → dynamicInformers trigger CSIControllerSet sync
    → [1] LogLevelController — syncs log level to operator pod spec
    → [2] ManagementStateController — determines Managed/Unmanaged/Removed
        - If Removed + removable=true → triggers resource deletion in [3]
        - DeletionTimestamp on CR is treated as Removed (see getOperatorSyncState)
        - On error reading CR → returns Unmanaged (safe default)
    → [3] ConditionalStaticResourcesController — applies/deletes static assets:
             node_sa.yaml, csidriver.yaml, cabundle_cm.yaml,
             rbac/privileged_role.yaml, rbac/node_privileged_binding.yaml,
             rbac/secretproviderclasses_role.yaml, rbac/secretproviderclasses_binding.yaml,
             network-policy/allow-ingress-to-metrics-operand.yaml
        - Creates when: getOperatorSyncState == Managed
        - Deletes when: getOperatorSyncState == Removed
        - Assets read via replaceNamespaceFunc(operatorNamespace) → assets.ReadFile(name)
    → [4] CSIConfigObserverController — observes cluster config (proxy, infra, apiserver)
           and propagates observations to operand
    → [5] CSIDriverNodeService — reconciles node.yaml DaemonSet
           - Reads DaemonSet from node.yaml via replaceNamespaceFunc
           - library-go substitutes ${DRIVER_IMAGE}, ${NODE_DRIVER_REGISTRAR_IMAGE},
             ${LIVENESS_PROBE_IMAGE}, ${LOG_LEVEL} from operator env vars
           - Applies optional DaemonSetHookFunc hooks AFTER substitution
           - Current hooks: WithCABundleDaemonSetHook (injects CA bundle volume)
           - Compares desired vs existing; applies delta; triggers rolling update if changed
           - Sets status conditions on ClusterCSIDriver
```

**Critical gap for SSCSI-254**: `csidriver.yaml` is currently a **static** asset in `[3]`. The feature requires `csidriver.yaml` to carry dynamic spec fields (`requiresRepublish`, `tokenRequests`). Static resources are applied as-is from disk — they cannot have runtime-computed spec fields. The implementation must either:
- (a) Move CSIDriver management out of `[3]` into a new dynamic asset function, OR
- (b) Generate `csidriver.yaml` bytes at runtime using a custom `AssetFunc` that overlays the dynamic fields.
Option (b) is lower-risk (smaller diff, no new controller). See §9.4 for the walkthrough.

---

## 2. Target Files (Modification & Creation)

### API Types (upstream dependency — external to this repo)

- `vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go`: (New content after vendor update) Add `SecretsStore *SecretsStoreCSIDriverConfigSpec` to `CSIDriverConfigSpec`. This field does **NOT exist yet** in the vendored copy. **SSCSI-254 depends on `openshift/api` PR #2846 (and rename via #2906) being merged and vendored first.** (confidence: high — confirmed by reading vendored types)

### Operator Source (this repo)

- `pkg/operator/starter.go`: Primary modification target. Add:
  - A new `DaemonSetHookFunc` (`withSecretRotationHook`) that reads `ClusterCSIDriver.Spec.DriverConfig.SecretsStore.SecretRotation` and mutates DaemonSet args `--enable-secret-rotation` and `--rotation-poll-interval`.
  - A new dynamic `AssetFunc` (`csiDriverAssetFunc`) that generates `csidriver.yaml` bytes with `requiresRepublish` and `tokenRequests` computed at runtime from `ClusterCSIDriver.Spec.DriverConfig.SecretsStore.TokenRequests`.
  - Wire the new hook into `WithCSIDriverNodeService(..., withSecretRotationHook(...))`.
  - Update `WithConditionalStaticResourcesController` to remove `"csidriver.yaml"` from the static list and instead manage CSIDriver creation/deletion through the new dynamic asset function.
  (confidence: high)

- `assets/csidriver.yaml`: Modify the static baseline. Remove hardcoded `requiresRepublish`/`tokenRequests` absence (add them as template-worthy defaults). OR remove from static management and manage entirely in Go. (confidence: high)

- `assets/node.yaml`: Currently hardcodes `--enable-secret-rotation=true` and `--rotation-poll-interval=2m`. These become the defaults; the DaemonSet hook overrides them based on config. The YAML baseline must remain as the fallback values (matches FR-003). (confidence: high — read from file)

### Tests

- `pkg/operator/starter_test.go`: Add test cases for:
  - `withSecretRotationHook` — table-driven, covering `type: None`, `type: Custom` with various intervals, nil `SecretsStore`, and the `Removed`/`Unmanaged` management state interaction.
  - `csiDriverAssetFunc` — covering `type: Managed` with audiences, `type: Unmanaged`, nil `TokenRequests`, and the upgrade-preservation case.
  (confidence: high)

### Vendor Update (separate prerequisite PR)

- `vendor/github.com/openshift/api/...`: Re-vendor after `openshift/api` PR #2906 merges. Run `go mod tidy && go mod vendor`. (confidence: high)

### No Changes Required

- `assets/rbac/` — RBAC for operator → ClusterCSIDriver read access is already in place (confirmed: the operator reads its own CR on every sync via `operatorClient.GetOperatorState()`). A-002 is verified: no new RBAC needed.
- `cmd/` — entry point is unchanged
- `config/manifests/` — no OLM bundle changes for this feature; no new Console resources; no CSV RBAC additions

---

## 3. Reference Context (Read-Only)

### 3.1 Entry Points & Wiring

- `cmd/secrets-store-csi-driver-operator/main.go`: Cobra + `controllercmd.NewControllerCommandConfig` → calls `operator.RunOperator`. Wiring only; do not add logic here.
- `pkg/operator/starter.go:RunOperator`: The single location where all clients, informers, and controllers are wired. This is where new hooks are added.
- `vendor/github.com/openshift/library-go/pkg/operator/csi/csicontrollerset/csi_controller_set.go`: `CSIControllerSet.WithCSIDriverNodeService` signature — variadic `DaemonSetHookFunc` params; `WithConditionalStaticResourcesController` signature.

### 3.2 API / Interface Patterns

- `vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go`: `ClusterCSIDriver`, `ClusterCSIDriverSpec`, `CSIDriverConfigSpec` — the current type tree before SSCSI-254 additions. Study pattern for adding a new discriminated union (see `CSIDriverType` enum + per-driver structs).
- `vendor/github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller/csi_driver_node_service_controller.go`: `DaemonSetHookFunc` type definition (`func(*opv1.OperatorSpec, *appsv1.DaemonSet) error`). Hook receives `OperatorSpec` — **note**: this is the base `OperatorSpec`, not the full `ClusterCSIDriverSpec`. If the hook needs `driverConfig.secretsStore`, it must capture `operatorClient` in a closure.
- `vendor/github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller/helpers.go`: `WithCABundleDaemonSetHook` — pattern to follow when writing the new `withSecretRotationHook`.
- `vendor/github.com/openshift/library-go/pkg/operator/resource/resourceapply/`: `AssetFunc` type (`func(name string) ([]byte, error)`). Understand how static resources apply path uses it before writing a dynamic variant.
- `vendor/github.com/openshift/client-go/operator/applyconfigurations/operator/v1/`: Apply configurations used by `extractOperatorSpec` / `extractOperatorStatus`. Do not modify these (generated).

### 3.3 Build, CI & Tooling

- `Makefile`: Includes `golang.mk`, `deps-gomod.mk`, `images.mk`, `yq.mk` from `vendor/github.com/openshift/build-machinery-go/make/`. Key targets: `build`, `test-unit`, `verify`, `check`.
- `Dockerfile.openshift`: Multi-stage build; note FIPS flags are baked in at build time.
- `.ci-operator.yaml`: Specifies CI build root image. CI config lives in `openshift/release`.
- `.snyk`: Snyk security scanning configuration.

### 3.4 Manifest / Config Generation Pipelines

- `assets/assets.go`: `//go:embed *.yaml rbac/*.yaml network-policy/*.yaml` — the complete embed glob. Adding a new subdirectory requires updating this directive.
- `hack/update-metadata.sh`: Bumps OCP version in CSV, `Makefile`, README. Run as `./hack/update-metadata.sh X.Y`. Uses `yq` (auto-downloaded to `bin/yq`).
- `hack/create-bundle`: Build OLM bundle and index images.
- `config/manifests/art.yaml`: ART (Automated Release Tooling) substitution rules — automatically substitutes image digests during release build. Do not manually edit image references in CSV if they will be auto-substituted.

### 3.5 Test Patterns & Fixtures

- `pkg/operator/starter_test.go`: The only unit test file. Uses standard Go `testing` + `library-go`'s `v1helpers.NewFakeOperatorClientWithObjectMeta`. Pattern: table-driven `[]struct{name, input, expected}` with `t.Run` subtests; `t.Fatalf` for failures. No assertion library. **Follow this exact pattern for new tests.**

---

## 4. Configuration Surface & Runtime Behavior

### 4.1 Current Configuration Surface

**`ClusterCSIDriver` (cluster-scoped, name: `secrets-store.csi.k8s.io`)**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `spec.managementState` | `Managed`/`Unmanaged`/`Removed` | `Managed` | Operator-level lifecycle |
| `spec.logLevel` | string | `Normal` | Propagated to operator log flags |
| `spec.storageClassState` | `Managed`/`Unmanaged`/`Removed` | `Managed` | StorageClass lifecycle (not used by sscsi — no storage classes) |
| `spec.driverConfig.driverType` | `""`/`AWS`/`Azure`/`GCP`/`IBMCloud`/`vSphere` | `""` | No `SecretsStore` type exists yet — **greenfield** |

**DaemonSet (`node.yaml`) — current hardcoded args for `csi-driver` container**

| Arg | Current value | Notes |
|-----|---------------|-------|
| `--enable-secret-rotation` | `true` | Hardcoded; must become dynamic |
| `--rotation-poll-interval` | `2m` | Hardcoded; must become dynamic |
| `--provider-health-check` | `false` | Static; not part of SSCSI-254 |
| `--metrics-addr` | `:8095` | Static |
| `--endpoint`, `--logtostderr`, `--v`, `--nodeid`, `--provider-volume`, `--additional-provider-volume-paths` | Static values | Not part of SSCSI-254 |

**CSIDriver (`csidriver.yaml`) — current static spec**

| Field | Current value | Must become dynamic for SSCSI-254 |
|-------|---------------|-----------------------------------|
| `spec.podInfoOnMount` | `true` | Static — unchanged |
| `spec.attachRequired` | `false` | Static — unchanged |
| `spec.fsGroupPolicy` | `File` | Static — unchanged |
| `spec.volumeLifecycleModes` | `[Ephemeral]` | Static — unchanged |
| `spec.requiresRepublish` | absent (false) | **Must be set dynamically** from `secretRotation.type` |
| `spec.tokenRequests` | absent | **Must be set dynamically** from `tokenRequests` config |

### 4.2 Reconciliation / Processing Flow (Detailed)

All controller execution is inside `csiControllerSet.Run(ctx, 1)` (single worker).

| Step | Controller | Trigger | Error behavior |
|------|-----------|---------|----------------|
| 1 | `LogLevelController` | CR spec change | Log warning, continue |
| 2 | `ManagementStateController` | CR spec change | On error reading CR: returns `Unmanaged` (safe default, no resource mutation) |
| 3 | `ConditionalStaticResourcesController` | CR spec change; resource watch | Apply delta to 8 static assets. On conflict: retry. On perm-error: set degraded condition |
| 4 | `CSIConfigObserverController` | Cluster config change | Log error, propagate partial observation |
| 5 | `CSIDriverNodeService` | CR change; DaemonSet change; ConfigMap change | On hook error: set degraded condition, do not apply DaemonSet; on apply error: set degraded condition |

**DaemonSet hook invocation order** (step 5, inside library-go `CSIDriverNodeService`):
1. Template substitution: `${DRIVER_IMAGE}`, `${NODE_DRIVER_REGISTRAR_IMAGE}`, `${LIVENESS_PROBE_IMAGE}`, `${LOG_LEVEL}`
2. Hooks invoked in registration order: currently `WithCABundleDaemonSetHook` only
3. **New hook for SSCSI-254** will be appended as a second hook after CA bundle

**Asset function for CSIDriver (proposed for SSCSI-254)**:
The current `WithConditionalStaticResourcesController` applies `csidriver.yaml` as a static byte read. For SSCSI-254, `csidriver.yaml` management must change:
- **Remove** `"csidriver.yaml"` from the static file list in `WithConditionalStaticResourcesController`
- **Add** a new controller (or modify the static controller) that uses a dynamic `AssetFunc` — at runtime, reads `ClusterCSIDriver.Spec.DriverConfig.SecretsStore`, generates the CSIDriver YAML bytes with the correct `requiresRepublish` and `tokenRequests`, and applies via `resourceapply.ApplyCSIDriver`

Note: `resourceapply.ApplyCSIDriver` is available in the vendored `library-go/pkg/operator/resource/resourceapply/storage.go`. Verify the function signature before use.

### 4.3 Image / Dependency Resolution

Images reach the DaemonSet via **environment variable injection**:
1. OLM CSV (`config/manifests/stable/*.clusterserviceversion.yaml`) defines three env vars on the operator deployment:
   - `DRIVER_IMAGE` → `quay.io/openshift/origin-secrets-store-csi-driver:latest`
   - `NODE_DRIVER_REGISTRAR_IMAGE` → `quay.io/openshift/origin-csi-node-driver-registrar:latest`
   - `LIVENESS_PROBE_IMAGE` → `quay.io/openshift/origin-csi-livenessprobe:latest`
2. OLM replaces `latest` tags with digests at install time using `image-references` (`config/manifests/stable/image-references` — the ImageStream manifest).
3. The `library-go` DaemonSet controller substitutes `${DRIVER_IMAGE}` etc. in `node.yaml` from the operator pod's runtime environment.

**No RELATED_IMAGE env vars** in Go code — image pinning is done entirely at the OLM/CSV layer. Do not introduce Go-level image resolution logic.

### 4.4 Status / Health Reporting

`ClusterCSIDriver` status uses `library-go`'s `OperatorStatus` conditions system:

| Condition | Set by | Meaning |
|-----------|--------|---------|
| `Available` | `ManagementStateController` | Operator is running and managing resources |
| `Progressing` | `CSIDriverNodeService` | DaemonSet rolling update in progress |
| `Degraded` | Any controller | A hook error, apply error, or unrecoverable error occurred |

**Error classification**:
- `Retryable`: Transient API errors — controller-set retries automatically (exponential backoff)
- `Irrecoverable` / `Degraded`: Configuration errors that won't fix themselves — condition set to `Degraded=true`; admin must correct config

**Relevant to SSCSI-254**: If the new hook cannot read `ClusterCSIDriver` (e.g., permissions error), it should return an error that causes the controller to set `Degraded`, not silently skip. Following the CA bundle hook pattern: return a non-nil error to propagate to the status condition system.

### 4.5 Feature Gate / Feature Flag Mechanism

The operator does not implement its own feature gate system. It relies on `ClusterCSIDriver.managementState` as the only operational gate. There is no `TechPreviewNoUpgrade` or `CustomNoUpgrade` feature set integration in the current codebase.

**Relevant to SSCSI-254**: The feature targets GA directly (A-007 in specs.md). No feature gate code is needed.

---

## 5. Reusable Assets (Anti-Duplication)

- `vendor/github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller/helpers.go:WithCABundleDaemonSetHook`: Use as the **direct pattern template** for the new `withSecretRotationHook`. It closes over informer/namespace; the new hook should close over `operatorClient`. Signature: `func withSecretRotationHook(operatorClient v1helpers.OperatorClientWithFinalizers) DaemonSetHookFunc { return func(opSpec *opv1.OperatorSpec, ds *appsv1.DaemonSet) error { ... } }`.

- `vendor/github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller/helpers.go:WithObservedProxyDaemonSetHook`: Additional hook pattern showing how to mutate container args. Study before writing the rotation args mutation.

- `vendor/github.com/openshift/library-go/pkg/operator/resource/resourceapply/storage.go:ApplyCSIDriver`: Use this to apply the dynamically-generated CSIDriver object. Do not call the Kubernetes client directly.

- `pkg/operator/starter.go:replaceNamespaceFunc`: **Reuse** as the base for any new `AssetFunc`. The pattern is: read raw bytes from `assets.ReadFile`, transform, return. Do not create a different file-reading path.

- `pkg/operator/starter.go:getOperatorSyncState`: **Reuse** in the new dynamic CSIDriver controller to guard against applying when the operator is `Removed` or `Unmanaged`.

- `vendor/github.com/openshift/library-go/pkg/operator/v1helpers:NewFakeOperatorClientWithObjectMeta`: **Reuse** in new unit tests, matching the pattern in `starter_test.go`. Do not introduce testify or other assertion libraries.

- `assets/node.yaml` network-policy label: `openshift.storage.network-policy.api-server: allow` on the pod template. **Do not create a standalone egress NetworkPolicy for API server access** — this label already opts the DaemonSet pods into CSO-managed egress policies. Do not add a new `network-policy/*.yaml` asset for API server egress.

---

## 6. Architectural Guardrails

### Structural

- **Single binary, all controllers in one `starter.go`**: Do not split `RunOperator` into multiple files. Add new hooks and helpers in `starter.go` or as new files in `pkg/operator/` that are called from `starter.go`.
- **No controller-runtime**: This repo uses library-go `CSIControllerSet` exclusively. Do not introduce `sigs.k8s.io/controller-runtime` controllers, managers, or reconcilers.
- **Static resources use `WithConditionalStaticResourcesController`; DaemonSet uses `WithCSIDriverNodeService`**: Mixing these causes incorrect lifecycle management (evidence: `AGENTS.md`, `starter.go:116`). For dynamic CSIDriver, use a custom static-resource-like pattern via `resourceapply.ApplyCSIDriver` called from a new controller or the existing conditional controller's custom asset function.

### API / Schema

- **No new `CSIDriverType` discriminator value without upstream `openshift/api` PR**: The `CSIDriverType` enum is in `openshift/api`. Adding a `SecretsStore` value in the vendored copy locally will not be valid — it must go through `openshift/api` PR process and then be re-vendored.
- **`ClusterCSIDriver` name is fixed**: `"secrets-store.csi.k8s.io"` — the `providerName` constant. Do not change this.
- **Apply configurations use SSA (Server-Side Apply)**: `extractOperatorSpec` and `extractOperatorStatus` use `applyoperatorv1.ExtractClusterCSIDriver`. Any new status conditions must go through the same SSA path.

### Build / Tooling

- **FIPS is mandatory for CI**: `CGO_ENABLED=1 GOEXPERIMENT=strictfipsruntime go build -tags strictfipsruntime,openssl`. Local builds without FIPS-capable Go are not valid for CI.
- **All dependencies must be vendored**: `go mod tidy && go mod vendor`. Never add `go.sum`-only dependencies.
- **`dependencymagnet.go` must not be removed**: It keeps `build-machinery-go` in `vendor/` for the Makefile include.

### Deployment / Packaging

- **OLM is the only install path**: No Helm or kustomize. Manifest changes go in `config/manifests/stable/`.
- **Operator namespace is `openshift-cluster-csi-drivers`**: Set via `operatorframework.io/suggested-namespace` in CSV. Always use `${NAMESPACE}` in asset YAML — never hardcode.
- **`csi.openshift.io/managed: "true"` annotation on CSIDriver**: This annotation is required (present in current `csidriver.yaml`). Any dynamic generation of CSIDriver YAML must preserve this annotation.
- **No new Console resources for SSCSI-254**: The existing `sscsi-example-quickstart.yaml` is in the OLM bundle and is not affected. No new `ConsoleQuickStart` or `ConsoleYAMLSample` is needed for this feature.

### Code Generation

- **`zz_generated.*` files in `vendor/` are auto-generated**: Do not hand-edit. They are regenerated when `openshift/api` is re-vendored.
- **No `go:generate` directives in this repo**: Code generation happens in `openshift/api` and `openshift/client-go` upstream. The outputs are consumed here via vendoring.

### Security

- **DaemonSet requires `privileged: true` SCC**: The `secrets-store-privileged-role` ClusterRole grants `use` on `securitycontextconstraints/privileged`. Any change to the DaemonSet pod spec must preserve this requirement.
- **Operator pod runs as `nonRoot` with `readOnlyRootFilesystem`**: Do not loosen the operator container security context.
- **`tokenRequests` audiences are cluster-admin configurable**: When writing the dynamic CSIDriver asset function, the audience values come from `ClusterCSIDriver.Spec` (admin input) — no escaping or sanitization is needed beyond what the Kubernetes API server enforces at admission.

---

## 7. Change Cascade Checklist

| When you change... | You must also... | Verify with... |
|---|---|---|
| `openshift/api` types (add SecretsStore fields) | Re-vendor: `go mod tidy && go mod vendor`; update `vendor/github.com/openshift/api/...` and `vendor/github.com/openshift/client-go/...` | `make verify` (deps-gomod check) |
| `assets/node.yaml` (args or labels) | Verify `//go:embed` in `assets/assets.go` still covers `*.yaml`; check no hardcoded values remain that the hook should own | `make build && make test-unit` |
| `assets/csidriver.yaml` (or remove it from static management) | Update `WithConditionalStaticResourcesController` file list in `starter.go` to remove `"csidriver.yaml"`; add dynamic CSIDriver apply logic | `make test-unit` |
| `starter.go` (add new DaemonSetHookFunc) | Add corresponding unit test in `starter_test.go` | `make test-unit` |
| `assets/` — add new subdirectory | Update `//go:embed` glob in `assets/assets.go` | Compile + `make build` |
| `config/manifests/stable/` CSV or bundle | Run `./hack/create-bundle` to validate bundle format | Bundle lint (via `operator-sdk bundle validate` in CI) |
| OCP version bump | Run `./hack/update-metadata.sh X.Y` | `make verify` |
| `vendor/` (any change) | `go mod tidy && go mod vendor` | `make verify` (verify-deps target) |

---

## 8. Test & CI Reference

### 8.1 Test Structure

```
pkg/
  operator/
    starter_test.go   -- Only unit test file; standard Go testing + library-go fakes
cmd/
  secrets-store-csi-driver-operator/
    (no test files currently)
hack/
  e2e.sh              -- E2E test script; requires running OpenShift cluster + oc in PATH
```

**Frameworks**:
- Unit: standard `testing` package + `library-go` fakes (`v1helpers.NewFakeOperatorClientWithObjectMeta`)
- E2E: `openshift-tests` binary (external) invoked via `hack/e2e.sh`
- No testify, no gomega, no mockery — do not introduce them

### 8.2 How to Run Tests Locally

```bash
# Unit tests (fast, no cluster needed)
make test-unit

# Formatting + vet check (run before every commit)
make verify

# Both together (CI equivalent)
make check

# E2E tests (requires running OpenShift cluster + oc in PATH)
make test-e2e
```

Expected runtimes: `test-unit` < 30s; `verify` < 30s; `test-e2e` depends on cluster.

### 8.3 CI Pipeline

- CI platform: OpenShift Prow (config in `openshift/release`, not this repo)
- Every PR triggers:
  - `make verify` — `go vet`, `gofmt`, Go version consistency, vendor consistency
  - `make test-unit` — runs all tests in `./pkg/... ./cmd/...`
  - FIPS build enforcement (CI Go has `strictfipsruntime`)
- E2E runs as separate Prow jobs (labeled, not on every PR)
- `.ci-operator.yaml` sets build root; `make images` builds the operator image

### 8.4 Test Coverage Gaps

| Area | Coverage | Gap |
|------|----------|-----|
| `getOperatorSyncState` | Good (4 table cases in `starter_test.go`) | None for happy path |
| `replaceNamespaceFunc` | None (trivial bytes.ReplaceAll — acceptable) | Low risk |
| DaemonSet hook logic | **None** — no hook tests exist yet | **New hooks for SSCSI-254 must have unit tests** |
| Dynamic CSIDriver asset generation | **None** | **Must be added for SSCSI-254** |
| E2E: rotation enable/disable/custom interval | None in repo | Requires cluster |
| E2E: tokenRequests audience propagation | None in repo | Requires cluster |

---

## 9. Developer Workflow

### 9.1 Key Commands Reference

| Target | Command | When to use |
|--------|---------|------------|
| Build binary | `make build` | After Go source changes |
| Unit tests | `make test-unit` | Before every commit |
| Format check + vet | `make verify` | Before every commit |
| Auto-format | `make update-gofmt` | After writing new Go code |
| Both (CI) | `make check` | Before PR |
| E2E tests | `make test-e2e` | On cluster; after major changes |
| Version bump | `./hack/update-metadata.sh X.Y` | When OCP version changes |
| Build OLM bundle | `./hack/create-bundle` | For OLM install testing |
| Update deps | `go mod tidy && go mod vendor` | After `go.mod` changes |

**Preflight before PR** (exact sequence):
```bash
make verify && make test-unit
```

### 9.2 Version Variables

| Variable | Location | Controls |
|----------|----------|---------|
| Go version | `go.mod:go 1.25.0` | Minimum Go toolchain version |
| OCP version | `config/manifests/secrets-store-csi-driver-operator.package.yaml` | Current channel version |
| OLM `skipRange` | CSV `metadata.annotations["olm.skipRange"]` | Upgrade path |
| `olm.maxOpenShiftVersion` | CSV `metadata.annotations["olm.properties"]` | Max OCP version |
| Image tags | CSV env vars (`DRIVER_IMAGE`, etc.) | Operand image versions |
| Build image | `Makefile:$(call build-image,...ocp/5.0:...)` | CI image tag |

All version variables except `go.mod` are updated by `./hack/update-metadata.sh`.

### 9.3 Local Development Setup

```bash
# Prerequisites
# - Go 1.25+ (FIPS-capable build is CI-only; local builds without FIPS are fine)
# - GNU Make
# - yq (auto-downloaded to bin/ by make targets)
# - oc CLI (for E2E only)

# Build
make build

# Verify environment
make check
```

### 9.4 Common Development Scenarios

#### How to Add a New DaemonSet Hook (SSCSI-254 rotation hook)

This is the primary pattern for SSCSI-254.

1. **Write the hook function** in `pkg/operator/starter.go`:
   ```go
   func withSecretRotationHook(operatorClient v1helpers.OperatorClientWithFinalizers) csidrivernodeservicecontroller.DaemonSetHookFunc {
       return func(_ *opv1.OperatorSpec, ds *appsv1.DaemonSet) error {
           // Read ClusterCSIDriver to get SecretsStore config
           opSpec, _, _, err := operatorClient.GetOperatorState()
           if err != nil {
               return fmt.Errorf("failed to get operator state for rotation hook: %w", err)
           }
           // Access opSpec.DriverConfig.SecretsStore (after API re-vendor)
           // Mutate ds.Spec.Template.Spec.Containers[0].Args
           // ...
           return nil
       }
   }
   ```
   Pattern evidence: `helpers.go:WithCABundleDaemonSetHook` (same signature, closure over external state).

2. **Register the hook** in `RunOperator`, inside `WithCSIDriverNodeService`:
   ```go
   ).WithCSIDriverNodeService(
       "SecretsStoreDriverNodeServiceController",
       replaceNamespaceFunc(operatorNamespace),
       "node.yaml",
       kubeClient,
       kubeInformersForNamespaces.InformersFor(operatorNamespace),
       nil,
       csidrivernodeservicecontroller.WithCABundleDaemonSetHook(...),
       withSecretRotationHook(operatorClient),  // ← add here
   )
   ```

3. **Write unit tests** in `starter_test.go` following the table-driven pattern from `TestGetOperatorSyncState`. Use `v1helpers.NewFakeOperatorClientWithObjectMeta` to inject `ClusterCSIDriver` state.

4. **Run**: `make test-unit && make verify`

#### How to Make CSIDriver Management Dynamic (SSCSI-254 tokenRequests)

1. Remove `"csidriver.yaml"` from the string slice in `WithConditionalStaticResourcesController`.
2. Write a `csiDriverAssetFunc(operatorClient, baseAssetFunc)` function that:
   - Calls `baseAssetFunc("csidriver.yaml")` to get the static template bytes
   - Deserializes to `storagev1.CSIDriver`
   - Sets `spec.requiresRepublish` based on `secretRotation.type`
   - Sets `spec.tokenRequests` based on `tokenRequests.type == Managed && audiences != nil`
   - Serializes back to bytes
3. Register using `WithConditionalStaticResourcesController` with the custom asset function (instead of the standard `replaceNamespaceFunc`), or use a dedicated `staticresourcecontroller.NewStaticResourceController` call with the dynamic asset function.

---

## 10. Platform & Environment Integration

### 10.1 Security Context & Permissions

- **DaemonSet SCC**: `privileged` SCC granted via `ClusterRole` `secrets-store-privileged-role` → `ClusterRoleBinding` `node_privileged_binding.yaml`. The DaemonSet container `csi-driver` runs `privileged: true` (required for hostPath mounts and CSI node plugin operation).
- **Operator pod SCC**: `nonRoot`, `readOnlyRootFilesystem`, `allowPrivilegeEscalation: false`, `capabilities.drop: [ALL]` — standard restricted profile. The operator pod does NOT need elevated SCC.
- **Service accounts**: `secrets-store-csi-driver-node-sa` — node service account for DaemonSet. Operator uses its own `secrets-store-csi-driver-operator` SA (from CSV RBAC).

### 10.2 Proxy & Network Configuration

- **Proxy propagation**: `WithCSIConfigObserverController` observes cluster-level proxy settings and propagates to operand via `WithObservedProxyDaemonSetHook` pattern (available in library-go but NOT currently wired in `starter.go`). If the driver needs proxy settings, use the existing hook rather than adding custom env var injection.
- **CA bundle**: `WithCABundleDaemonSetHook` is wired — injects the trusted CA ConfigMap (`secrets-store-csi-driver-trusted-ca-bundle`) as a volume+mount in the DaemonSet. This is already handled.
- **Platform network policy hooks (CSO-managed)**: The DaemonSet pod template carries label `openshift.storage.network-policy.api-server: allow` (evidence: `assets/node.yaml:18`). This label opts the driver pods into the CSO-managed egress policy that allows API server access. **Do not create a standalone egress NetworkPolicy for API server traffic.** The existing ingress NetworkPolicy (`network-policy/allow-ingress-to-metrics-operand.yaml`) allows Prometheus scraping on port 8095 — managed as a conditional static resource.

### 10.3 Cloud Provider Integration

- **No `CredentialsRequest` resources**: This operator does not manage cloud credentials for itself. Credential provisioning (for WIF) is done by the secret provider plugins, not the operator.
- **Workload Identity Federation (SSCSI-254)**: Configures `tokenRequests` on the cluster-scoped `CSIDriver` object so kubelet issues service account tokens. The operator does not call cloud provider APIs directly — it configures the CSI driver to do so.
- **Supported cloud providers**: AWS STS, Azure AD, GCP IAM, HashiCorp Vault — all via provider plugins (separate from this operator).

### 10.4 Build & Compliance Constraints

- **FIPS**: Mandatory for CI. Build flags: `CGO_ENABLED=1 GOEXPERIMENT=strictfipsruntime go build -trimpath -tags strictfipsruntime,openssl`. Go stdlib crypto replaced with OpenSSL via `go-openssl`. New code must not use deprecated crypto primitives; use only `crypto/tls`, `crypto/sha256`, etc.
- **Multi-arch**: CSV labels declare `amd64`, `arm64`, `s390x` support. The Dockerfile.openshift must support multi-arch; no architecture-specific build logic in Go source.
- **Disconnected support**: `features.operators.openshift.io/disconnected: "true"` in CSV. Image digests pinned via OLM at install time. The `image-references` ImageStream enables air-gapped mirroring.

### 10.5 Console / UI Integration

- **Current QuickStart**: `sscsi-example-quickstart.yaml` is in the OLM bundle (`config/manifests/stable/`). It uses navigation token `qs-nav-ecosystem` (verified in the file: `[Ecosystem]{{highlight qs-nav-ecosystem}}`). This token is correct for OCP 4.18+; in older releases it was `qs-nav-operator-hub`. The token is version-coupled — verify against the target release branch before modifying.
- **SSCSI-254 scope**: No new ConsoleQuickStart or ConsoleYAMLSample resources are needed for this feature (the spec explicitly calls out no OLM bundle changes).
- **Annotation requirement** (for future Console resources): Any new `ConsoleQuickStart` or `ConsoleYAMLSample` added to the OLM bundle must carry:
  - `capability.openshift.io/name: Console`
  - `include.release.openshift.io/ibm-cloud-managed: "true"`
  - `include.release.openshift.io/self-managed-high-availability: "true"`
  - `include.release.openshift.io/single-node-developer: "true"`
  Evidence: `sscsi-example-quickstart.yaml:6-9`.

### 10.6 Packaging & Lifecycle

- **OLM channel**: `stable` only (single channel). `olm.skipRange: ">=4.13.0-0 <5.0.0"` allows upgrades from any 4.x version.
- **Install modes**: `AllNamespaces: true` only — no single/own/multi-namespace support.
- **OLM bundle annotations** (for non-CSV resources in bundle): Any resource added to `config/manifests/stable/` that is not the CSV or a CRD requires the four release annotations listed in §10.5. SSCSI-254 does not add new bundle resources.
- **Install QuickStart circular dependency**: Install-type QuickStarts (guiding users through the install process) must **NOT** be placed in the OLM bundle. Such resources appear after OLM install completes, making install guidance circular. Install guides belong in `demo/console/` (if it exists) or separate documentation. Example-usage QuickStarts (like the existing `sscsi-example-quickstart.yaml`) are appropriate in the bundle.
- **Upgrade edge safety for SSCSI-254**: On upgrade from a cluster with no `driverConfig.secretsStore` set, the operator must apply defaults (rotation=true, interval=2m) that exactly match the previous hardcoded behavior. Verified by FR-003 and SC-004.

---

## 11. Risks & Downstream Impacts

- **Risk: `openshift/api` PR dependency**: SSCSI-254 requires new Go types (`SecretsStoreCSIDriverConfigSpec`, `SecretRotationConfig`, `TokenRequestsConfig`) in `openshift/api`. **If the `openshift/api` PR is not merged and vendored before implementation starts, the operator code cannot be written.** Impact: blocking. Mitigation: confirm PR #2906 (or #2846 with rename tracked) is merged; then re-vendor and begin implementation.

- **Risk: CSIDriver recreation disruption**: Moving CSIDriver from static to dynamic management involves a potential delete+recreate cycle on first upgrade. Library-go's `ApplyCSIDriver` uses SSA (server-side apply) and should not delete the resource if the spec diff is additive. However, if the implementation uses `delete + create` explicitly, there is a brief window where the CSIDriver resource is absent, causing kubelet to reject new CSI volume mounts. Impact: medium. Mitigation: use `ApplyCSIDriver` (SSA patch) rather than delete+create; add an integration test that verifies the resource is never absent during the upgrade.

- **Risk: Immutable `tokenRequests.type` enforcement**: FR-006 requires that `tokenRequests.type` is immutable once set to `Managed`. This validation must be in `openshift/api` as a CEL validation rule (kubebuilder `+kubebuilder:validation:XValidation`). If the validation is in the API CRD, the CRD must be updated in the bundle. If it is only in operator code, it can be bypassed. Impact: low for user safety but is a spec requirement. Mitigation: confirm the immutability is implemented as a CRD CEL rule in `openshift/api`.

- **Risk: `managementState: Removed` interaction**: When the operator is in `Removed` state, `ConditionalStaticResourcesController` deletes all static assets including the CSIDriver. The new dynamic CSIDriver controller must respect the same lifecycle and delete the CSIDriver when `Removed`. Impact: medium if missed — CSIDriver resource orphaned after operator removal. Mitigation: guard dynamic CSIDriver apply with `getOperatorSyncState` check.

- **Risk: DaemonSet rolling update during config change**: When `minimumRefreshAge` changes, the DaemonSet rolling update will evict pods node by node. During the update, some nodes will have the old interval and some the new. This is expected behavior and matches how all other DaemonSet arg changes work in this operator. No mitigation needed beyond documenting in release notes.

- **Risk: `extractOperatorSpec` does not surface `driverConfig`**: The `DaemonSetHookFunc` signature receives `*opv1.OperatorSpec`, not `*opv1.ClusterCSIDriverSpec`. The new hook must call `operatorClient.GetOperatorState()` inside the hook closure to read the `ClusterCSIDriverSpec` directly. Impact: medium design complexity. Mitigation: write the hook as a closure over `operatorClient` (pattern: `withSecretRotationHook(operatorClient) DaemonSetHookFunc`).

### 11.1 Assessment Limitations / UNVERIFIED Items

- `resourceapply.ApplyCSIDriver` — signature and SSA behavior not verified by reading the file directly (`vendor/github.com/openshift/library-go/pkg/operator/resource/resourceapply/storage.go`). Verify: open the file and confirm function exists, accepts `context.Context, storagev1client.StorageV1Interface, recorder, *storagev1.CSIDriver` (standard pattern for resource apply functions).
- `staticresourcecontroller.NewStaticResourceController` — whether it can accept a per-resource dynamic asset function (vs. a single shared asset function) was not verified. Verify by reading `vendor/github.com/openshift/library-go/pkg/operator/staticresourcecontroller/`.
- `openshift/api` PR #2906 (field rename) — field name `minimumRefreshAge` vs `rotationPollIntervalSeconds` was not verified as merged. Verify: check `vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go` after re-vendor.
- CEL validation rule for `tokenRequests.type` immutability — location and existence in `openshift/api` not verified. Verify: check `openshift/api` PR diff for CEL `+kubebuilder:validation:XValidation` rules on the `SecretsStoreTokenRequestsConfig` type.
- E2E test path — `hack/e2e.sh` contents not read. Verify: read the file to understand what `openshift-tests` label expressions are used and whether new e2e cases for rotation/WIF need to be labeled differently.

---

## 12. Quick Reference Card

### Preflight Checklist (run before every PR)
```
1. make verify          # go vet + gofmt + deps check
2. make test-unit       # all unit tests in ./pkg/... ./cmd/...
3. (CI only) FIPS build: CGO_ENABLED=1 GOEXPERIMENT=strictfipsruntime make build
```

### Key File Quick-Nav

| I want to... | Look at... |
|---|---|
| Understand the controller wiring | `pkg/operator/starter.go` (all of it) |
| Add a DaemonSet hook | `pkg/operator/starter.go` + `vendor/…/csidrivernodeservicecontroller/helpers.go` (patterns) |
| Add a static asset | `assets/` + update `//go:embed` in `assets/assets.go` + add to file list in `starter.go` |
| Make CSIDriver dynamic | `pkg/operator/starter.go:replaceNamespaceFunc` (base AssetFunc pattern) + `vendor/…/resourceapply/storage.go` (ApplyCSIDriver) |
| Add a new API field | `vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go` (upstream; re-vendor after merge) |
| Change RBAC | `assets/rbac/` → NOT the OLM bundle CSV directly |
| Change OLM packaging | `config/manifests/stable/` (CSV + any bundle manifests) |
| Bump OCP version | `./hack/update-metadata.sh X.Y` |
| Add a unit test | `pkg/operator/starter_test.go` (follow table-driven pattern) |
| Check current DaemonSet defaults | `assets/node.yaml` (current `--enable-secret-rotation=true`, `--rotation-poll-interval=2m`) |
| Check current CSIDriver spec | `assets/csidriver.yaml` (no `requiresRepublish`, no `tokenRequests` currently) |
| Understand image resolution | `config/manifests/stable/image-references` + CSV env vars `DRIVER_IMAGE` etc. |
