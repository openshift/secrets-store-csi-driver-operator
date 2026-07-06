# Implementation Report: SSCSI-254 — Configurable Secret Rotation and Workload Identity Federation

**Change**: `sscsi-254-secret-rotation-wif`  
**Feature Branch**: `feat/sscsi-254-rotation-wif`  
**Completed**: 2026-07-02  
**Total Tasks**: 15 (all complete)

---

## Summary

Implemented configurable secret rotation and Workload Identity Federation (WIF) support for
the Secrets Store CSI Driver Operator. This allows cluster administrators to:

1. **Disable or throttle secret rotation** — configure `secretRotation.type: None` to stop
   periodic NodePublishVolume calls, or `type: Custom` with a specific `rotationPollIntervalSeconds`
2. **Enable WIF audiences** — configure `tokenRequests.type: Managed` with explicit audience
   strings so the CSI driver requests projected tokens for AWS STS, Azure AD, and GCP IAM
3. **Preserve manual tokenRequests** — set `tokenRequests.type: Unmanaged` to opt out of
   operator management and manually patch `CSIDriver.spec.tokenRequests`

---

## Phase 0: Dependencies and API Discovery

### T0_1 — Update go.mod + vendor tree
Updated `openshift/api`, `openshift/client-go`, `openshift/library-go` to post-SSCSI-254-merge
pseudo-versions (Jul 2026) to bring in `SecretsStoreCSIDriverConfigSpec` types.

**Deviation**: Initial `go get openshift/api@latest` was blocked by pinning from `library-go` and
`client-go`. Fixed by simultaneously bumping all three modules.

### T0_2 — Verify API types and field names
Confirmed `rotationPollIntervalSeconds` (not `minimumRefreshAge`), nested structure
`secretRotation.custom.rotationPollIntervalSeconds`, CEL immutability gap noted (Q4).

---

## Phase 1: CSIDriver Controller

### T1_1 — Discovery: ApplyCSIDriver signature and RBAC
Confirmed `ApplyCSIDriver` from `library-go/pkg/operator/resource/resourceapply` is the correct
approach; existing RBAC (`csidrivers` list/watch/create/update/delete) is sufficient.

### T1_2 — Discovery: staticresourcecontroller dynamic asset support
Confirmed `WithConditionalResources` accepts per-call `AssetFunc`s; decided on **Composite
AssetFunc** pattern to avoid per-file controller registration.

### T1_3 — Implement `csiDriverAssetFunc`
Implemented composite `AssetFunc` in `pkg/operator/starter.go`:
- `csiDriverAssetFunc(clusterCSIDriverLister, namespace)` — dispatches `csidriver.yaml` to
  `generateCSIDriverBytes`, delegates other assets to namespace substitution
- `generateCSIDriverBytes` — builds `storagev1.CSIDriver` from `ClusterCSIDriver` config;
  sets `requiresRepublish` and `tokenRequests` based on `secretRotation` and `tokenRequests` spec
- `makeCSIDriverTokenRequests` — converts `opv1.SecretsStoreTokenRequest` slices

### T1_4 — Rewire ConditionalStaticResourcesController
Removed `"csidriver.yaml"` from `SecretsStoreConditionalStaticResourcesController` file list.
Chained new `WithConditionalStaticResourcesController` (`SecretsStoreCSIDriverController`) managing
only `["csidriver.yaml"]` via `csiDriverAssetFunc`. Same lifecycle predicates preserved.

---

## Phase 2: DaemonSet Rotation Hook

### T2_1 — Implement `withSecretRotationHook`
**ADR-001**: Used `cache.GenericLister` (from `dynamicInformers`) instead of
`operatorClient.GetOperatorState()` (which lacks `DriverConfig.SecretsStore` fields).

Implemented:
- `withSecretRotationHook(clusterCSIDriverLister)` — `DaemonSetHookFunc` that injects/removes
  `--enable-secret-rotation` and `--rotation-poll-interval` args on the `csi-driver` container
- `applySecretRotationArgs` — idempotent arg injection; handles `None` (false) and `Custom`
  (interval or default 120s)
- `removeRotationArgs` — filters out stale rotation args before re-applying

### T2_2 — Wire `withSecretRotationHook` into `RunOperator`
Constructed `clusterCSIDriverLister := dynamicInformers.ForResource(gvr).Lister()`;
appended `withSecretRotationHook(clusterCSIDriverLister)` to `WithCSIDriverNodeService` hook list.

---

## Phase 3: Unit Tests

### T3_1 — `TestCSIDriverAssetFunc` (7 sub-tests)
Covers: Managed/Unmanaged tokenRequests, Custom/None rotation, non-SecretsStore driver,
absent `ClusterCSIDriver`. Uses `cache.NewIndexer` + `cache.NewGenericLister` for mock listers.

### T3_2 — `TestWithSecretRotationHook` (9 sub-tests)
Covers: rotation None/Custom/default-interval, nil paths, idempotency,
non-SecretsStore driver, absent `ClusterCSIDriver`. Verifies DaemonSet arg mutation.

### T3_3 — `TestUpgradeSafety` + `TestManagementStateTransitions` (7 sub-tests)
Covers: SC-004 (upgrade no-op), SC-005 (Unmanaged tokenRequests preserved),
`None→Custom` and `Custom→None` transitions, repeated Custom idempotency (FR-007).

---

## Phase 4: E2E Tests

### T4_1 — Rotation scenarios (`hack/e2e.sh`)
Added `test_rotation_none` (SC-001) and `test_rotation_custom` (SC-002) + helpers
`wait_for_daemonset_rollout`, `check_all_pods_arg`, `restore_clustercsidriver`.

### T4_2 — WIF and upgrade-safety (`hack/e2e.sh`)
Added `test_wif_managed_audiences` (SC-003), `test_upgrade_no_op` (SC-004),
`test_unmanaged_tokenrequests_preserved` (SC-005).

### T4_3 — API immutability (`hack/e2e.sh`)
Added `test_api_immutability_managed_to_unmanaged` (SC-006) as a non-fatal informational
function; handles both CEL-present (rejects with 422) and CEL-absent (logs gap) cases.

### T4_4 — CSV annotation review (manual)
Confirmed `token-auth-{aws,azure,gcp}` annotations are currently `"false"`.  
**Decision**: Annotation update deferred to a follow-up PR pending PM/AOS storage team review.

---

## Architectural Decision Records

See `adrs.md`.

---

## Files Changed

### Go source
- `pkg/operator/starter.go` — core implementation (T2_1, T1_3, T2_2, T1_4)
- `pkg/operator/starter_test.go` — unit tests (T3_1, T3_2, T3_3)

### Modules / vendor
- `go.mod` — bumped `openshift/{api,client-go,library-go}` (T0_1)
- `go.sum` — regenerated (T0_1)
- `vendor/` — updated via `go mod vendor` (T0_1)

### E2E
- `hack/e2e.sh` — extended with SC-001–SC-006 test functions (T4_1, T4_2, T4_3)

### E2E artifacts (output/ — not committed to repo)
- `output/e2e_secrets-store-csi-driver-operator/test-cases.md`
- `output/e2e_secrets-store-csi-driver-operator/execution-steps.md`
- `output/e2e_secrets-store-csi-driver-operator/e2e-suggestions.md`

---

## Open Follow-ups

1. **SC-006 gap**: CEL immutability rule for `tokenRequests.type` not confirmed in cluster CRD.
   Follow-up issue against `openshift/api` recommended.
2. **CSV `token-auth-*` annotations**: Deferred update pending PM decision (T4_4).
3. **E2E cluster run**: T4_1–T4_3 functions require a live OpenShift cluster to execute.
   Run `make test-e2e` or `bash hack/e2e.sh` against a 4.18+ cluster before merge.
