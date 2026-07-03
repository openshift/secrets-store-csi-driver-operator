# Technical Implementation Plan
**Feature:** Configurable Secret Rotation and Workload Identity Federation (SSCSI-254)

---

## 0. Inputs acknowledged

| Input | Status |
|-------|--------|
| Spec source | `SSCSI-254` — validated spec at `openspec/changes/sscsi-254-secret-rotation-wif/specs.md` (score: 86%, PASS) |
| Repo assessment pin | Working-folder `/Users/ckyal/go/src/github.com/chiragkyal/secrets-store-csi-driver-operator`, branch: main (post-rebase), tooling_status: OK |
| `agents.md` | PROVIDED — `AGENTS.md` at repo root |
| `spec_validator_results.json` | PROVIDED — `openspec/changes/sscsi-254-secret-rotation-wif/validation.json` |
| `constitution.md` | PROVIDED — `constitution.md` at repo root; AgentRoutingMode: PROVIDED |

**Constitution guardrails applied (binding):**
- Principle I: Library-go `CSIControllerSet` only — no controller-runtime
- Principle II: Static assets embedded as YAML — no bindata codegen; dynamic CSIDriver must use a custom `AssetFunc` over the existing embed, not a new generation pipeline
- Principle III: No new CRD types — new fields go in `openshift/api`'s `ClusterCSIDriver`
- Principle IV: All resource-sync logic gates on `getOperatorSyncState` (Managed/Unmanaged/Removed)
- Principle V: `make check` before every PR
- Principle VIII: CA bundle hook must be preserved in every DaemonSet change
- Principle X: All new dependencies vendored

---

## 1. Architectural strategy

### Strategy

The feature introduces two independent runtime behaviors:

1. **Secret rotation control** — the operator injects `--enable-secret-rotation` and `--rotation-poll-interval` arguments into the DaemonSet node service container based on the `secretRotation` configuration in `ClusterCSIDriver`. This is expressed as a new `DaemonSetHookFunc` wired into `WithCSIDriverNodeService`.

2. **WIF token audiences** — the operator sets `requiresRepublish` and `tokenRequests` on the cluster-scoped `CSIDriver` Kubernetes object based on the `tokenRequests` configuration in `ClusterCSIDriver`. This requires moving `csidriver.yaml` from static `ConditionalStaticResourcesController` management to dynamic runtime generation via a custom `AssetFunc`.

Both behaviors are additions within the existing `CSIControllerSet` chain in `pkg/operator/starter.go` — consistent with Principle I of the constitution. No new controller types, no new CRDs, no controller-runtime.

### Repo-grounded reality check

**Full greenfield** — confirmed by `repo-assessment.md §0` and verified by direct inspection of `vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go`:

- `CSIDriverConfigSpec` currently has no `SecretsStore` variant — only AWS, Azure, GCP, IBMCloud, vSphere
- `assets/csidriver.yaml` has no `requiresRepublish` or `tokenRequests` fields
- `assets/node.yaml` hardcodes `--enable-secret-rotation=true` and `--rotation-poll-interval=2m`
- No `withSecretRotationHook` or equivalent exists in the codebase

**Upstream API dependency is a blocking prerequisite**: the `openshift/api` PR (#2906, or #2846 with rename pending) must be merged before any operator Go code can be written. Until then, the new `ClusterCSIDriver` spec fields do not exist in the vendor tree.

**Upgrade safety** is a first-class concern (FR-003, FR-005, SC-004, SC-005): the plan must preserve identical runtime behavior when `secretRotation` and `tokenRequests` are absent from the CR, and must preserve manually-patched `tokenRequests` on the live `CSIDriver` object during upgrade.

---

## 2. Persistence & state

**Kubernetes objects (source of truth → derived)**

| Source of truth | Derived / reconciled | Reconcile trigger |
|---|---|---|
| `ClusterCSIDriver` CR (`secrets-store.csi.k8s.io`) — `spec.driverConfig.secretsStore.secretRotation` | DaemonSet `csi-driver` container args `--enable-secret-rotation`, `--rotation-poll-interval` | CR spec change; DaemonSet watch |
| `ClusterCSIDriver` CR — `spec.driverConfig.secretsStore.tokenRequests` | `CSIDriver` object `spec.requiresRepublish`, `spec.tokenRequests` | CR spec change; CSIDriver watch |

**Operand config/state**

- DaemonSet args are set via `DaemonSetHookFunc` at reconcile time — not stored in a ConfigMap. They are regenerated on every reconcile cycle from the CR.
- `CSIDriver` object is applied via SSA (library-go `ApplyCSIDriver`) — the object is a cluster-scoped singleton. Its spec is fully controlled by the operator when `managementState: Managed`. When `managementState: Unmanaged`, the existing object (including any manually-patched `tokenRequests`) is left unchanged per FR-005.

**Upgrade-preservation invariant**

When `spec.driverConfig.secretsStore` is absent from an existing `ClusterCSIDriver` CR (all upgrades from older versions), the operator applies defaults: rotation enabled at 2-minute interval (`requiresRepublish: false` per the current static asset). No DaemonSet rolling update is triggered on upgrade (SC-004). This is enforced via nil-guards in the hook and asset function.

---

## 3. Interfaces & contracts (operator-native)

### 3.1 Kubernetes APIs (CRDs/CRs)

**`ClusterCSIDriver` (operator.openshift.io/v1) — EXTENSION via `openshift/api`**

New spec structure (must be merged upstream before this repo can use it):

```
ClusterCSIDriverSpec.DriverConfig.SecretsStore (SecretsStoreCSIDriverConfigSpec):
  SecretRotation (SecretRotationConfig):
    Type: None | Custom  (discriminator)
    Custom (CustomSecretRotationConfig):
      MinimumRefreshAge: duration (1s–31560000s)
  TokenRequests (TokenRequestsConfig):
    Type: Managed | Unmanaged  (immutable once set to Managed)
    Managed (ManagedTokenRequestsConfig):
      Audiences: []TokenRequestAudienceConfig  (max 10, unique values)
        Audience: string
        ExpirationSeconds: *int64  (≥600 when set)
```

**Field name dependency (A-001)**: use `MinimumRefreshAge` per PR #2906; if #2906 is not yet merged at vendor time, use the name from PR #2846 and track the rename as a follow-up task.

**Immutability rule (FR-006)**: `TokenRequestsConfig.Type` must be validated as immutable once set to `Managed` — this is a CEL validation rule responsibility of `openshift/api`, not this operator. The operator treats `Unmanaged` as "do not overwrite existing `tokenRequests`."

**CRD update path**: the CRD YAML is managed by the `openshift/api` repo and distributed as part of the OpenShift release payload. No CRD changes in this operator's `config/manifests/` are required.

### 3.2 Controller/runtime interfaces (internal)

**`withSecretRotationHook(operatorClient v1helpers.OperatorClientWithFinalizers) csidrivernodeservicecontroller.DaemonSetHookFunc`**

- Signature must match `DaemonSetHookFunc`: `func(*opv1.OperatorSpec, *appsv1.DaemonSet) error`
- Reads `ClusterCSIDriver` via `operatorClient.GetOperatorState()` to access full `ClusterCSIDriverSpec` (the `opv1.OperatorSpec` parameter from the hook does not carry `driverConfig`)
- Mutates `DaemonSet.Spec.Template.Spec.Containers[0].Args` for the `csi-driver` container
- On nil `SecretsStore` or nil `SecretRotation`: applies defaults (`--enable-secret-rotation=true`, `--rotation-poll-interval=2m`) — no change from current hardcoded behavior
- On error reading operator state: returns non-nil error (causes `Degraded` condition via library-go status system)
- Must preserve the `WithCABundleDaemonSetHook` ordering — this hook is appended after the CA bundle hook

**`csiDriverAssetFunc(operatorClient, baseAssetFunc) resourceapply.AssetFunc`**

- Reads the static `csidriver.yaml` bytes via `baseAssetFunc("csidriver.yaml")`
- Deserializes to `storagev1.CSIDriver`
- Sets `spec.requiresRepublish`:
  - `true` when `secretRotation.type == Custom`
  - `false` when `secretRotation.type == None` or omitted
- Sets `spec.tokenRequests`:
  - Populated from `tokenRequests.audiences` when `tokenRequests.type == Managed` and audiences is non-nil/non-empty
  - Empty (cleared) when `tokenRequests.type == Managed` with explicit empty audiences
  - Left as-is when `tokenRequests.type == Unmanaged` (read existing object, preserve)
- Preserves all existing annotations (`csi.openshift.io/managed: "true"`, labels)
- Applied via `resourceapply.ApplyCSIDriver` (verify signature in `vendor/github.com/openshift/library-go/pkg/operator/resource/resourceapply/storage.go` — UNVERIFIED per repo-assessment §11.1)
- Gated on `getOperatorSyncState == Managed`; skipped (no-op) when `Unmanaged`; triggers deletion when `Removed`

### 3.3 Webhooks / admission

N/A — this operator defines no admission webhooks. Validation of new API fields (boundary values for `MinimumRefreshAge`, max 10 audiences, `expirationSeconds ≥ 600`, immutability of `TokenRequestsConfig.Type`) is enforced by CRD CEL rules in `openshift/api`.

### 3.4 RBAC / security boundaries

No new RBAC additions required. **A-002 verified** (repo-assessment §2): the operator already reads `ClusterCSIDriver` on every sync via `operatorClient.GetOperatorState()`. The hook closure reads the operator's own CR — already permitted.

The `ConditionalStaticResourcesController` already manages the `CSIDriver` cluster-scoped object. Moving CSIDriver management to a dynamic asset function does not change RBAC requirements.

N/A — no new service accounts, cluster roles, or bindings.

### 3.5 Packaging / OLM

**No new OLM bundle resources** for this feature (spec §OLM Bundle Placement: not applicable).

The implementation is entirely in Go source code and operator controller logic. No changes to:
- `config/manifests/stable/` (CSV, CRDs, ConsoleQuickStart, ConsoleYAMLSample)
- `config/metadata/annotations.yaml`
- `hack/update-metadata.sh` invocation

**OLM upgrade path**: `olm.skipRange: ">=4.13.0-0 <5.0.0"` is unchanged. The upgrade preserves existing operator behavior when `secretRotation` and `tokenRequests` are omitted (FR-003, SC-004).

**Bundle placement decision matrix**: N/A — SSCSI-254 introduces no new bundle resources. The existing `sscsi-example-quickstart.yaml` is unchanged.

---

## 4. Dependencies & sequencing graph

**Critical path (must be sequential):**

```
[EXT] openshift/api PR #2906 merged
    ↓
[P0]  go mod tidy && go mod vendor (update API + client-go types in vendor/)
    ↓
[P1]  Dynamic CSIDriver asset function (csiDriverAssetFunc)
      — depends on SecretsStoreCSIDriverConfigSpec type being in vendor
    ↓
[P2]  DaemonSet rotation hook (withSecretRotationHook)
      — depends on SecretsStoreCSIDriverConfigSpec type being in vendor
      — can start concurrently with P1 once vendor is done
    ↓
[P3]  Unit tests for P1 + P2
    ↓
[P4]  E2E verification + CI
```

**Parallelizable after Phase 0 (vendor update):**
- Phase 1 (dynamic CSIDriver) and Phase 2 (DaemonSet hook) can proceed concurrently — they touch independent code paths within `starter.go` and different asset files.

**External blockers:**
- `openshift/api` PR #2906 (or #2846 with rename tracked): must be merged before Phase 0 can start
- Field name (`minimumRefreshAge` vs `rotationPollIntervalSeconds`): depends on which PR is vendored first; tracked by A-001

---

## 5. Implementation phases (logical sequence; NOT tasks)

### Phase 0: Upstream Vendor Update

- **Goal:** Bring the new `ClusterCSIDriver` spec fields (`secretsStore.secretRotation`, `secretsStore.tokenRequests`) into the operator's vendor tree so subsequent Go code can reference the types.
- **Dependencies:** `openshift/api` PR #2906 (or #2846) merged upstream.
- **Target files:**
  - `go.mod` — bump `github.com/openshift/api` version to include new types
  - `go.sum` — updated by `go mod tidy`
  - `vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go` — will gain `SecretsStoreCSIDriverConfigSpec` and related types
  - `vendor/github.com/openshift/api/operator/v1/zz_generated.deepcopy.go` — auto-updated by vendor
  - `vendor/github.com/openshift/client-go/operator/applyconfigurations/operator/v1/` — apply configurations updated
  - `vendor/modules.txt` — updated by `go mod vendor`
- **Required capabilities:** Dependency management (Principle X: vendor mode)
- **Verification hooks:**
  - `make verify` — `verify-deps` target validates `vendor/` matches `go.mod`
  - `make build` — confirms new types compile correctly
- **Platform pattern check:** N/A — no platform resources added in this phase.

---

### Phase 1: Dynamic CSIDriver Asset Function

- **Goal:** Replace static `csidriver.yaml` management with a runtime-computed CSIDriver object that carries the correct `requiresRepublish` and `tokenRequests` values derived from `ClusterCSIDriver.Spec.DriverConfig.SecretsStore.TokenRequests`. This enables FR-004, FR-005, FR-008, and the tokenRequests-related user stories (US3, US4, US5).
- **Dependencies:** Phase 0 complete (new API types available in vendor).
- **Target files:**
  - `pkg/operator/starter.go` — PRIMARY: (a) add `csiDriverAssetFunc` function; (b) remove `"csidriver.yaml"` from the `WithConditionalStaticResourcesController` file list; (c) add a separate static-resource-controller registration (or adapt the existing one) to manage the CSIDriver dynamically using `csiDriverAssetFunc`
  - `assets/csidriver.yaml` — BASELINE: retain as the static template with the existing annotations and non-dynamic fields; `requiresRepublish` absent (Go code sets it); `tokenRequests` absent (Go code sets it when Managed)
  - UNVERIFIED: `vendor/github.com/openshift/library-go/pkg/operator/resource/resourceapply/storage.go` — verify `ApplyCSIDriver` function signature before writing the asset function; if absent, use the generic `resourceapply.ApplyGenericUnstructured` path
  - UNVERIFIED: `vendor/github.com/openshift/library-go/pkg/operator/staticresourcecontroller/` — verify whether a per-resource custom asset function is supported; if not, use direct `ApplyCSIDriver` call inside a new minimal controller wrapper
- **Required capabilities:** OperatorController (CSIControllerSet hook, asset function pattern per Principle I + II)
- **Verification hooks:**
  - Unit: new table-driven tests in `pkg/operator/starter_test.go` covering `csiDriverAssetFunc`:
    - `tokenRequests.type == Managed` with audiences → CSIDriver has tokenRequests populated
    - `tokenRequests.type == Managed` with empty audiences → CSIDriver tokenRequests cleared
    - `tokenRequests.type == Unmanaged` → existing tokenRequests preserved (read-only)
    - nil `SecretsStore` → CSIDriver matches static baseline (no requiresRepublish, no tokenRequests)
    - `managementState == Removed` → CSIDriver deletion triggered
  - `make test-unit` — full unit test suite
  - `make verify` — formatting + vet
- **Platform pattern check:** N/A — CSIDriver is a cluster-scoped Kubernetes object, not a platform network policy or SCC. No CSO/CNO survey needed.

---

### Phase 2: DaemonSet Rotation Hook

- **Goal:** Replace hardcoded `--enable-secret-rotation=true` and `--rotation-poll-interval=2m` in `assets/node.yaml` with a runtime-computed `DaemonSetHookFunc` that derives these values from `ClusterCSIDriver.Spec.DriverConfig.SecretsStore.SecretRotation`. Enables FR-001, FR-002, FR-003, and user stories US1, US2. Combined with Phase 1's `requiresRepublish` change, this fully implements SC-001 and SC-002.
- **Dependencies:** Phase 0 complete. Can proceed concurrently with Phase 1.
- **Target files:**
  - `pkg/operator/starter.go` — PRIMARY: add `withSecretRotationHook(operatorClient) DaemonSetHookFunc`; register as second hook in `WithCSIDriverNodeService` (after `WithCABundleDaemonSetHook`)
  - `assets/node.yaml` — The hardcoded arg values (`--enable-secret-rotation=true`, `--rotation-poll-interval=2m`) remain as the fallback defaults in the YAML template; the hook overrides them at runtime. The node.yaml baseline remains the upgrade no-op default.
- **Required capabilities:** OperatorController (DaemonSetHookFunc pattern per Principle VIII — CA bundle hook must be preserved)
- **Verification hooks:**
  - Unit: new table-driven tests in `pkg/operator/starter_test.go` covering `withSecretRotationHook`:
    - `secretRotation.type == None` → `--enable-secret-rotation=false`, no `--rotation-poll-interval` arg
    - `secretRotation.type == Custom` with `minimumRefreshAge: 300` → `--rotation-poll-interval=5m0s`
    - `secretRotation.type == Custom` with `minimumRefreshAge` omitted → applies 2m default
    - `secretRotation.type == Custom` with boundary value `minimumRefreshAge: 1` → `--rotation-poll-interval=1s`
    - nil `SecretsStore` → no arg mutation; DaemonSet retains baseline values from node.yaml
    - `managementState == Unmanaged` → hook does not mutate DaemonSet (getOperatorSyncState guard)
    - Verify `WithCABundleDaemonSetHook` is still registered and invoked before the new hook
  - `make test-unit`
  - `make verify`
- **Platform pattern check:** N/A — the DaemonSet already carries `openshift.storage.network-policy.api-server: allow` for CSO-managed API server egress. No new network policies are introduced. Do not add a standalone NetworkPolicy for API server traffic.

---

### Phase 3: Upgrade Safety and Nil-Path Coverage

- **Goal:** Harden the implementation against upgrade scenarios and nil configurations. Ensure SC-004 (upgrade no-op) and SC-005 (preserve manually-patched tokenRequests) are reliably enforced. This phase adds the remaining unit test cases from the EP §Test Plan that cover nil-path permutations.
- **Dependencies:** Phase 1 and Phase 2 complete.
- **Target files:**
  - `pkg/operator/starter_test.go` — extend with nil-path permutation test table covering:
    - Upgrade from CR with no `driverConfig` → DaemonSet unchanged, CSIDriver unchanged (no rolling update)
    - Upgrade from CR with `managementState: Removed` → all resources deleted including dynamic CSIDriver
    - `managementState: Unmanaged` + `tokenRequests.type: Unmanaged` → no CSIDriver mutation
    - Transition from `secretRotation.type: None` → `type: Custom` → rolling update triggered
    - Transition back from `type: Custom` → `type: None` → second rolling update triggered (FR-007 rolling update non-disruption)
- **Required capabilities:** Testing (table-driven pattern, library-go fakes)
- **Verification hooks:**
  - `make test-unit` — all cases in `pkg/operator/`
  - `make check` — combined verify + test-unit (constitution Principle V)
- **Platform pattern check:** N/A — no platform resources.

---

### Phase 4: E2E Verification and CI Integration

- **Goal:** Validate the complete feature on a live cluster using the E2E suite. Confirm that all SC-001 through SC-006 success criteria are observable. Ensure CI passes with FIPS build.
- **Dependencies:** Phases 1–3 complete; CI cluster available.
- **Target files:**
  - `hack/e2e.sh` — (UNVERIFIED: read to determine whether new E2E test cases need to be added inline or via `openshift-tests` external suite)
  - `config/manifests/stable/secrets-store-csi-driver-operator.clusterserviceversion.yaml` — verify `features.operators.openshift.io/token-auth-aws/azure/gcp` annotations; if WIF changes token-auth support status, these annotations require update
- **Required capabilities:** Testing (E2E, cluster), OLMRelease (CSV annotation review)
- **Verification hooks:**
  - Cluster E2E: `make test-e2e` — test scenarios:
    - SC-001: set `secretRotation.type: None`; verify DaemonSet args within 1 reconcile + rolling update
    - SC-002: set `secretRotation.type: Custom` with custom interval; verify arg value
    - SC-003: set `tokenRequests.type: Managed` with audiences; verify CSIDriver `tokenRequests` matches
    - SC-004: upgrade cluster; verify no DaemonSet rolling update triggered when `driverConfig` absent
    - SC-005: pre-patch CSIDriver `tokenRequests` manually; upgrade; verify tokenRequests preserved
    - SC-006: attempt to revert `tokenRequests.type` from `Managed` to `Unmanaged`; verify API rejection
  - FIPS build: `CGO_ENABLED=1 GOEXPERIMENT=strictfipsruntime make build`
  - `make check` — full local pre-merge check
- **Platform pattern check:** N/A.

---

## 6. Verification matrix (maps to spec acceptance)

| Category | Coverage | Files / Suites |
|----------|----------|----------------|
| Unit | `withSecretRotationHook` — all type discriminator values, boundary intervals, nil paths; `csiDriverAssetFunc` — Managed/Unmanaged/nil paths, tokenRequests population/clearing, managementState gating; upgrade nil-path permutations | `pkg/operator/starter_test.go` |
| Integration | N/A — library-go CSIControllerSet provides framework-level integration; no separate integration test tier exists in this repo | — |
| E2E | SC-001 through SC-006; upgrade no-op (SC-004, SC-005); API immutability (SC-006); FIPS build | `hack/e2e.sh` + `make test-e2e` |
| Manual / Cluster | Verify DaemonSet args in running pod (`kubectl -n openshift-cluster-csi-drivers exec ds/secrets-store-csi-driver-node -c csi-driver -- /secrets-store-csi -- --help` or `kubectl get ds -o yaml`); inspect CSIDriver `spec.tokenRequests` (`kubectl get csidriver secrets-store.csi.k8s.io -o yaml`) | kubectl commands |
| N/A | No webhook tests (no admission webhooks); no storage migration tests (no schema migration) | — |

**Spec FR→verification mapping:**

| FR | Test category | Test scenario |
|----|---------------|---------------|
| FR-001: disable rotation | Unit + E2E | `secretRotation.type: None` → args check |
| FR-002: custom interval | Unit + E2E | `type: Custom`, `minimumRefreshAge: N` → interval arg |
| FR-003: upgrade no-op default | Unit + E2E | nil `SecretsStore` → defaults preserved, no update |
| FR-004: configure token audiences | Unit + E2E | `tokenRequests.type: Managed` + audiences → CSIDriver |
| FR-005: preserve unmanaged tokenRequests | Unit + E2E | `type: Unmanaged` + pre-existing tokenRequests → unchanged |
| FR-006: Managed type immutability | E2E + Manual | API rejection on type revert |
| FR-007: rolling update non-disruption | E2E | Config change → DaemonSet rolling update |
| FR-008: CSIDriver apply atomically | Unit | `csiDriverAssetFunc` SSA path |
| FR-009: survives restart | E2E | Operator restart; verify config re-applied |

---

## 7. Risks, migrations, and operational follow-ups

- **Risk: `openshift/api` PR dependency (BLOCKING)**: Phases 1–4 are blocked until `openshift/api` PR #2906 (or #2846 with rename tracked) merges. Mitigation: monitor upstream PR; do not start any operator Go code until vendor update succeeds. If the field name changes between PRs, an operator-side rename task is required.

- **Risk: CSIDriver SSA apply vs delete+recreate**: Moving CSIDriver from static to dynamic management must use server-side apply (`ApplyCSIDriver`) to avoid creating a window where the resource is absent (which would cause kubelet to reject new CSI mounts). If `ApplyCSIDriver` is not available in the vendored library-go, use `resourceapply.ApplyGenericUnstructured`. Mitigation: verify `storage.go` content before Phase 1 coding begins (currently UNVERIFIED per repo-assessment §11.1).

- **Risk: `tokenRequests.type` immutability enforcement location**: The spec requires API-level rejection (FR-006). If the CEL rule is not in `openshift/api` at vendor time, users can bypass immutability by calling kubectl directly. Mitigation: verify CEL rule presence in `openshift/api` PR diff before finalizing Phase 0; if absent, file a follow-up issue against `openshift/api`.

- **Risk: `managementState: Removed` orphan**: The dynamic CSIDriver apply path must respect the `Removed` state and delete the CSIDriver object when triggered. Failure to do so orphans the resource after operator removal. Mitigation: gate all CSIDriver apply logic on `getOperatorSyncState`; add explicit unit test case for `Removed` state.

- **Upgrade/migration**: No data migration required. Existing `ClusterCSIDriver` CRs without `driverConfig.secretsStore` are handled by nil-guards (A-007). The static CSIDriver baseline (`csidriver.yaml`) serves as the upgrade default. Existing manually-patched `tokenRequests` are preserved when `type: Unmanaged` (A-003).

- **Compatibility (MicroShift)**: Per A-007, MicroShift is explicitly out of scope. No MicroShift-specific code paths needed.

- **Operational follow-up — `token-auth-*` CSV annotations**: The CSV annotations `features.operators.openshift.io/token-auth-aws: "false"`, `token-auth-azure: "false"`, `token-auth-gcp: "false"` may need to be updated to `"true"` for relevant cloud providers once WIF is configurable. This is an annotation-only change that can be done as a follow-up PR after E2E validation confirms WIF works end-to-end.

---

## 8. Open questions / SME decisions

| # | Question | Who can answer | Plan assumes (if no answer) |
|---|---------|----------------|----------------------------|
| Q1 | Is `openshift/api` PR #2906 (`minimumRefreshAge` rename) merged or will implementation start with the earlier field name from PR #2846? | openshift/api maintainers / SSCSI-254 tracking | A-001: implementation uses `minimumRefreshAge`; if #2906 not merged, uses current name from #2846 with a rename tracked separately |
| Q2 | Does `vendor/github.com/openshift/library-go/pkg/operator/resource/resourceapply/storage.go` export `ApplyCSIDriver`? | Read the file directly at Phase 1 start (UNVERIFIED in repo-assessment §11.1) | Plan assumes `ApplyCSIDriver` exists; if absent, use `resourceapply.ApplyGenericUnstructured` |
| Q3 | Does `staticresourcecontroller.NewStaticResourceController` support a per-resource dynamic asset function (i.e., can a single controller entry use a custom function for one file while others remain static)? | Read `vendor/.../staticresourcecontroller/` at Phase 1 start | Plan assumes it does not; a direct `ApplyCSIDriver` call wired into the `CSIControllerSet` sync is the fallback path |
| Q4 | Is the CEL immutability rule for `tokenRequests.type` (Managed→Unmanaged revert rejection) present in the `openshift/api` PR diff? | openshift/api PR reviewer / SSCSI-254 EP author | Plan assumes CEL rule is in `openshift/api`; if absent, operator-side admission is out of scope (constraint is advisory only until API merge) |
| Q5 | Should `features.operators.openshift.io/token-auth-aws/azure/gcp` CSV annotations be updated to `"true"` as part of this feature or as a separate follow-up? | AOS storage team / PM | Plan defers to follow-up PR; no CSV annotation change in scope for SSCSI-254 implementation |
