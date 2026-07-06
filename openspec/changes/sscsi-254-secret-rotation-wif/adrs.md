# Architectural Decision Records: SSCSI-254

## ADR-001 — Use `cache.GenericLister` instead of `operatorClient.GetOperatorState()` in `withSecretRotationHook`

**Status**: Accepted  
**Task**: T2_1  

### Context
The `tasks.md` implementation notes for `withSecretRotationHook` suggested reading
`ClusterCSIDriverSpec` via `operatorClient.GetOperatorState()`. However, this method
returns `*operatorv1.OperatorSpec` — the generic `OperatorSpec` base type — which
does not expose `DriverConfig.SecretsStore` fields. The new `SecretsStoreCSIDriverConfigSpec`
is a field of the full `ClusterCSIDriver` object, not of `OperatorSpec`.

### Decision
`withSecretRotationHook` accepts a `cache.GenericLister` obtained from
`dynamicInformers.ForResource(gvr).Lister()`. The full `ClusterCSIDriver` singleton is
retrieved as `*unstructured.Unstructured`, then unmarshalled to `*opv1.ClusterCSIDriver`
via `encoding/json` to access `spec.driverConfig.secretsStore.secretRotation`.

### Consequences
- Correct access to the full `ClusterCSIDriver` type (including new fields)
- Consistent with `csiDriverAssetFunc` which uses the same lister pattern
- No dependency on `operatorClient.GetOperatorState()`
- The `dynamicInformers.ForResource(gvr)` wiring must be present before `RunOperator`
  calls `WithCSIDriverNodeService`

---

## ADR-002 — Composite AssetFunc instead of separate controller for `csidriver.yaml`

**Status**: Accepted  
**Task**: T1_2, T1_3, T1_4  

### Context
Options for dynamically managing `CSIDriver` configuration:
1. Separate `WithCSIDriverController` hook → modifies a different controller chain
2. Replace the entire `StaticResourceController` for `csidriver.yaml` → larger blast radius
3. **Composite AssetFunc** → single `AssetFunc` that intercepts `csidriver.yaml` requests
   and generates the manifest dynamically, delegating all other paths to the existing
   namespace-substitution function

### Decision
Option 3 (Composite AssetFunc). A new `SecretsStoreCSIDriverController`
(`WithConditionalStaticResourcesController`) manages only `["csidriver.yaml"]` using
`csiDriverAssetFunc`. The original `SecretsStoreConditionalStaticResourcesController` has
`"csidriver.yaml"` removed from its file list.

### Consequences
- Minimal blast radius: only `csidriver.yaml` handling changes
- Existing lifecycle predicates (`isRunningFunc`) are reused for both controllers
- `ApplyCSIDriver` spec-hash annotation mechanism (`library-go`) ensures idempotent apply
- `TokenRequestsUnmanaged` branch returns the static baseline bytes from `assets/csidriver.yaml`,
  ensuring SC-005 (manually-patched entries preserved via spec-hash stability)

---

## ADR-003 — SC-006 non-fatal E2E test for CEL immutability

**Status**: Accepted  
**Task**: T4_3  

### Context
T0_2 could not confirm that the CEL immutability rule for `tokenRequests.type`
(`Managed` → `Unmanaged` blocked) is present in the installed CRD version at time of
development. If the rule is absent, a hard-fail E2E test would break CI on clusters
with older CRD versions.

### Decision
`test_api_immutability_managed_to_unmanaged` is implemented as a **non-fatal informational
function**:
- If the API server returns 422 → test passes and logs "CEL rule enforced ✓"
- If the patch succeeds → test logs a WARNING, documents the gap, and exits with 0
  (not blocking CI); references unit test T3_1 as operator-side coverage

### Consequences
- Prevents CI breakage on clusters without the CEL rule
- Gap is clearly documented for follow-up against `openshift/api`
- Operator-side behaviour (ignores `Unmanaged` after `Managed`) is validated by T3_1
