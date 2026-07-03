# Design Bundle — SSCSI-254 / T1_3
**Task:** T1_3 — Implement `csiDriverAssetFunc`
**Phase:** 1 — Dynamic CSIDriver Asset Function
**Prepared:** 2026-07-03

---

## Key Findings from Prior Tasks

- **T0_2**: `SecretRotation.Type` discriminator (`None`/`Custom`). Interval: `Custom.RotationPollIntervalSeconds`. TokenRequests: `TokenRequests.Type` (`Managed`/`Unmanaged`), audiences in `TokenRequests.Managed.Audiences *[]SecretsStoreTokenRequest{Audience *string, ExpirationSeconds int32}`.
- **T1_1**: `ApplyCSIDriver` uses spec-hash to detect spec changes. `RequiresRepublish = nil` (absent from JSON) matches the current static `csidriver.yaml`. `TokenRequests` omitted in generated spec → hash stable → live value preserved (FR-005 Unmanaged path).
- **T1_2**: T1_4 will wire using composite AssetFunc pattern. T1_3 implements only `generateCSIDriverBytes`.

## Serialization Strategy

- Parse: `resourceread.ReadCSIDriverV1OrDie(staticBytes)` → `*storagev1.CSIDriver`
- Mutate: set `RequiresRepublish` and/or `TokenRequests` conditionally
- Serialize: `encoding/json` with TypeMeta explicitly set (`apiVersion: storage.k8s.io/v1`, `kind: CSIDriver`)
- The `ReadGenericWithUnstructured` codec accepts JSON bytes

## Behavior Contract

| Config | requiresRepublish | tokenRequests |
|--------|-------------------|---------------|
| driverType ≠ SecretsStore / absent | nil (omit) | nil (omit) |
| SecretRotation.Type = None | nil (omit, matches static) | — |
| SecretRotation.Type = Custom | `true` | — |
| TokenRequests.Type = Managed | — | populated from audiences list |
| TokenRequests.Type = Unmanaged | — | nil (omit, hash stable → live value preserved) |
