# E2E Test Cases: secrets-store-csi-driver-operator

## Operator Information
- **Repository**: github.com/openshift/secrets-store-csi-driver-operator
- **Framework**: library-go
- **API Group**: operator.openshift.io/v1 (external — openshift/api)
- **Managed CRDs**: ClusterCSIDriver (cluster-scoped singleton)
- **Managed Kubernetes Resources**: CSIDriver `secrets-store.csi.k8s.io`, DaemonSet `secrets-store-csi-driver-node`
- **Operator Namespace**: openshift-cluster-csi-drivers
- **Changes Analyzed**: git diff main...HEAD — `pkg/operator/starter.go`, `go.mod`

## Prerequisites
- OpenShift cluster 4.18+ with admin access
- `oc` CLI installed and authenticated (`oc whoami`)
- Operator, driver DaemonSet, and e2e-provider already deployed (prerequisite for existing tests)
- `ClusterCSIDriver` `secrets-store.csi.k8s.io` exists and is `Managed`

## Installation
No change to install mechanism. Operator is pre-deployed via OLM or direct manifest apply.

## CR Deployment
The `ClusterCSIDriver` singleton already exists post-install. Tests mutate it via `oc apply` /
`oc patch` and restore it after each scenario.

---

## Test Cases

### SC-001 — Disable Secret Rotation (T4_1)

- **Test**: Apply `ClusterCSIDriver` with `secretRotation.type: None`
- **Steps**:
  1. `oc apply` a `ClusterCSIDriver` with `spec.driverConfig.secretsStore.secretRotation.type: None`
  2. `oc rollout status daemonset/secrets-store-csi-driver-node -n openshift-cluster-csi-drivers --timeout=120s`
  3. `oc get pods -n openshift-cluster-csi-drivers -l app=secrets-store-csi-driver-node -o jsonpath` to inspect `csi-driver` container args
  4. `oc get csidriver secrets-store.csi.k8s.io -o jsonpath={.spec.requiresRepublish}`
- **Expected**:
  - All node pods have `--enable-secret-rotation=false` in `csi-driver` container args
  - `CSIDriver.spec.requiresRepublish == false` (or absent)
- **Cleanup**: Restore `ClusterCSIDriver` to no `driverConfig`

### SC-002 — Custom Rotation Interval (T4_1)

- **Test**: Apply `ClusterCSIDriver` with `secretRotation.type: Custom`, `rotationPollIntervalSeconds: 300`
- **Steps**:
  1. `oc apply` a `ClusterCSIDriver` with `type: Custom` and `custom.rotationPollIntervalSeconds: 300`
  2. `oc rollout status daemonset/secrets-store-csi-driver-node --timeout=120s`
  3. Inspect `csi-driver` container args on all pods
- **Expected**:
  - All node pods have `--rotation-poll-interval=5m0s` (300s = 5m)
  - All node pods have `--enable-secret-rotation=true`
- **Cleanup**: Restore `ClusterCSIDriver`

### SC-003 — WIF Managed Audiences (T4_2)

- **Test**: Apply `ClusterCSIDriver` with `tokenRequests.type: Managed` and two audiences
- **Steps**:
  1. `oc apply` with `type: Managed`, `audiences: [{audience: "sts.amazonaws.com"}, {audience: "api://AzureADTokenExchange"}]`
  2. Wait 5s for reconcile
  3. `oc get csidriver secrets-store.csi.k8s.io -o jsonpath={.spec.tokenRequests}`
- **Expected**:
  - `CSIDriver.spec.tokenRequests` contains both `sts.amazonaws.com` and `api://AzureADTokenExchange`
- **Cleanup**: Restore `ClusterCSIDriver`

### SC-004 — Upgrade No-Op (T4_2)

- **Test**: Restart operator with no `driverConfig`; confirm no DaemonSet rollout
- **Steps**:
  1. Ensure `ClusterCSIDriver` has no `driverConfig` (`oc patch ... '{"spec":{"driverConfig":null}}'`)
  2. Record `DaemonSet.metadata.generation`
  3. Restart operator pod (`oc delete pod -l name=secrets-store-csi-driver-operator`)
  4. Wait 15s
  5. Compare `DaemonSet.metadata.generation`
- **Expected**: `generation` unchanged
- **Cleanup**: None needed

### SC-005 — Unmanaged TokenRequests Preserved (T4_2)

- **Test**: Manually patch `CSIDriver.spec.tokenRequests`; verify operator does not overwrite
- **Steps**:
  1. Set `ClusterCSIDriver tokenRequests.type: Unmanaged`
  2. `oc patch csidriver secrets-store.csi.k8s.io --type=merge -p '{"spec":{"tokenRequests":[{"audience":"manual-audience-test"}]}}'`
  3. Force reconcile (annotate `ClusterCSIDriver`)
  4. Wait 10s; inspect `csidriver` tokenRequests
- **Expected**: `tokenRequests[0].audience == "manual-audience-test"` (unchanged)
- **Cleanup**: Restore `ClusterCSIDriver`

### SC-006 — API Immutability (T4_3)

- **Test**: Attempt to change `tokenRequests.type` from `Managed` back to `Unmanaged`
- **Steps**:
  1. Apply `ClusterCSIDriver` with `tokenRequests.type: Managed`
  2. `oc patch clustercsidriver secrets-store.csi.k8s.io --type=merge -p '{"spec":{"driverConfig":{"secretsStore":{"tokenRequests":{"type":"Unmanaged"}}}}}'`
- **Expected**:
  - If CEL rule present: API server returns 422 with CEL validation message (immutable field)
  - If CEL rule absent: gap documented; follow-up against `openshift/api`; operator unit test T3_1 covers the code path
- **Gap note (from T0_2)**: CEL immutability for `tokenRequests.type` may not yet be present in the installed CRD. The test documents this gap non-fatally.

---

## Verification

```bash
oc get clustercsidriver secrets-store.csi.k8s.io -o yaml
oc get csidriver secrets-store.csi.k8s.io -o yaml
oc get pods -n openshift-cluster-csi-drivers -l app=secrets-store-csi-driver-node
oc logs -n openshift-cluster-csi-drivers -l name=secrets-store-csi-driver-operator --tail=50
```

## Cleanup

```bash
oc patch clustercsidriver secrets-store.csi.k8s.io --type=merge -p '{"spec":{"driverConfig":null}}'
oc delete project ${E2E_TEST_NAMESPACE}
```
