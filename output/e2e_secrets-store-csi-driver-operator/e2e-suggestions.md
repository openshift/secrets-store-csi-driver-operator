# E2E Suggestions: secrets-store-csi-driver-operator

## Detected Operator Structure

- **Framework**: library-go
- **API Types**: External (openshift/api `operator.openshift.io/v1`) — `ClusterCSIDriver` cluster-scoped singleton
- **Managed Kubernetes Resources**: `CSIDriver secrets-store.csi.k8s.io`, `DaemonSet secrets-store-csi-driver-node`
- **E2E Pattern**: Bash (`hack/e2e.sh`), `set -euo pipefail`, `test_<scenario>()` functions
- **Operator Namespace**: `openshift-cluster-csi-drivers`
- **Install**: OLM (CSV `secrets-store-csi-driver-operator`), namespace `openshift-cluster-csi-drivers`

## Changes Analyzed (main...HEAD — operator source only)

| File | Change |
|---|---|
| `pkg/operator/starter.go` | `withSecretRotationHook` — injects `--enable-secret-rotation`, `--rotation-poll-interval` args |
| `pkg/operator/starter.go` | `csiDriverAssetFunc` — dynamic CSIDriver generation (tokenRequests, requiresRepublish) |
| `pkg/operator/starter.go` | `SecretsStoreCSIDriverController` — dynamic `csidriver.yaml` via `csiDriverAssetFunc` |
| `go.mod` | Bumped `openshift/api`, `client-go`, `library-go` to pick up `SecretsStoreCSIDriverConfigSpec` |

## Highly Recommended Scenarios

| Scenario | Priority | Why |
|---|---|---|
| SC-001: rotation.type=None | HIGH | Core customer pain (RFE-8422); rate-limit reduction |
| SC-002: rotation.type=Custom/300s | HIGH | Confirms interval calculation (300s→5m0s) end-to-end |
| SC-004: upgrade no-op | HIGH | Regression risk; must not trigger unnecessary rollout |
| SC-003: managed audiences | MEDIUM | WIF feature validation |
| SC-005: unmanaged preserved | MEDIUM | Manual-override contract |

## Optional / Nice-to-Have

| Scenario | Why |
|---|---|
| SC-006: API immutability | CEL rule may be absent (T0_2 finding); informational |
| Default interval test | Custom with rotationPollIntervalSeconds=0 → default 120s |
| Idempotency: apply same config twice | No second rollout triggered |
| Mixed rotation+WIF config | Both features enabled simultaneously |

## Gaps (Hard to Test Automatically)

- **CEL immutability (SC-006)**: The CRD CEL validation rule for `tokenRequests.type` immutability may not be present in the cluster's installed CRD version. The test handles this non-fatally and documents the gap. Follow-up: file issue against `openshift/api`.
- **kubelet NodePublishVolume rate**: Cannot directly measure kubelet call frequency in a short E2E run; args inspection is a valid proxy.
- **CSV token-auth annotations (T4_4)**: Requires PM/team decision; no automated test possible.
