# Design Bundle — Task T4_1: E2E rotation scenarios (SC-001, SC-002)

## Constitution Guardrails (selected)
- E2E tests must not modify Go source or operator binary
- Use `oc` commands matching the existing `hack/e2e.sh` bash pattern
- Cleanup must restore ClusterCSIDriver state and prevent test pollution
- Follow SC-001 and SC-002 acceptance criteria from specs.md

## Spec Excerpt — SC-001
Apply `ClusterCSIDriver` with `secretRotation.type: None`; wait for DaemonSet rolling update;
inspect all node pods' `csi-driver` container args for `--enable-secret-rotation=false`;
verify CSIDriver `spec.requiresRepublish == false`.

## Spec Excerpt — SC-002
Apply `ClusterCSIDriver` with `secretRotation.type: Custom, rotationPollIntervalSeconds: 300`;
wait for rolling update; inspect `--rotation-poll-interval=5m0s` in args.

## Key Discovered Values (from repo analysis)
- DaemonSet: `secrets-store-csi-driver-node` in `${E2E_PROVIDER_NAMESPACE}` (openshift-cluster-csi-drivers)
- Container: `csi-driver`
- ClusterCSIDriver singleton: `secrets-store.csi.k8s.io` (= $PROVISIONER_NAME)
- API: `spec.driverConfig.driverType: SecretsStore`, `spec.driverConfig.secretsStore.secretRotation`
- 300s → `5m0s` (Go duration.String())
- Rollout timeout: 120s
- Existing e2e pattern: bash functions with `oc` commands, `set -euo pipefail`

## Task T4_1 Payload
- SC-001: test_rotation_none() — patch ClusterCSIDriver, wait rollout, check args + requiresRepublish
- SC-002: test_rotation_custom() — patch with Custom/300s, wait rollout, check args
- Cleanup: restore ClusterCSIDriver to default (no driverConfig)
- Wire both into main execution flow before test_teardown
