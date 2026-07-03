# Design Bundle — SSCSI-254 / T2_1
**Task:** T2_1 — Implement `withSecretRotationHook`
**Phase:** 2 — DaemonSet Rotation Hook
**Prepared:** 2026-07-03

---

## Key Findings from Discovery Tasks

- **T0_2**: `SecretRotationType` is a discriminator: `"None"` | `"Custom"`. The interval field is `Custom.RotationPollIntervalSeconds int32` (under `CustomSecretRotation`).
- **T1_1**: `DaemonSetHookFunc` signature: `func(*opv1.OperatorSpec, *appsv1.DaemonSet) error`.
- The `*opv1.OperatorSpec` parameter does NOT include `DriverConfig.SecretsStore` — confirmed by inspecting `getOperatorSpecFromUnstructured`.
- **T1_2**: `csi-driver` is the target container name (from `node.yaml` line 31).

## Deviation from tasks.md

`tasks.md` says: "Read the full `ClusterCSIDriverSpec` via `operatorClient.GetOperatorState()`". This is **incorrect** — `GetOperatorState()` wraps only `*operatorv1.OperatorSpec` (base type), which does not include `DriverConfig`. The correct approach is to pass `dynamicInformers.ForResource(gvr).Lister()` (a `cache.GenericLister`) and convert the returned `*unstructured.Unstructured` to `*opv1.ClusterCSIDriver`.

## Task Payload

- Add `withSecretRotationHook(lister cache.GenericLister) DaemonSetHookFunc` to `starter.go`
- Add helper `applySecretRotationArgs` and `removeRotationArgs`
- Add constants: `csiDriverContainerName`, `enableSecretRotationArg`, `rotationPollIntervalArg`, `defaultRotationPollInterval`
- T2_2 will wire it into the `WithCSIDriverNodeService` call
