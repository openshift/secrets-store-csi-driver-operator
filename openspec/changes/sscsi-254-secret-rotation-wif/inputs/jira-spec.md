# Jira Spec — SSCSI-254
**Source**: Enhancement Proposal https://github.com/openshift/enhancements/pull/2012 (merged Jul 2, 2026)
**Jira**: https://redhat.atlassian.net/browse/SSCSI-254

---

## Summary

Extend the OpenShift Secrets Store CSI Driver Operator to allow cluster administrators to
configure secret rotation behavior and workload identity federation (WIF) token audiences
through the `ClusterCSIDriver` custom resource (`operator.openshift.io/v1`).

These settings are dynamically propagated by the operator to:
- The `storage.k8s.io/v1` `CSIDriver` object (`requiresRepublish`, `tokenRequests`)
- The driver node DaemonSet (`--enable-secret-rotation`, `--rotation-poll-interval` container args)

Aligns with upstream Secrets Store CSI Driver v1.6.0 which replaced the internal rotation
controller with kubelet-native `requiresRepublish`.

---

## Motivation

The Secrets Store CSI Driver was GA'd in OpenShift 4.17 with hardcoded rotation enabled at a
fixed 2-minute poll interval. Two problems:

1. **No user control over rotation behavior.** Administrators cannot disable rotation for
   workloads that do not need it (avoiding unnecessary provider API calls / rate limit
   exhaustion), nor can they tune the polling interval.

2. **No WIF support.** Cloud providers (AWS, Azure, GCP) support federated identity using
   pod-bound service account tokens. The upstream driver v1.6.0 added `tokenRequests` support
   in the `CSIDriver` spec, but the OpenShift operator does not yet expose this to administrators.

**Customer pain** (RFE-8422): Customer manages ~200 secrets per cluster. Continuous Azure Key
Vault polling causes unnecessary API transactions and cost. Ability to control rotation behavior
is needed for cost efficiency and operational flexibility.

---

## User Stories

1. **As a cluster administrator**, I want to disable automatic secret rotation for workloads
   that use static secrets, so that the driver does not make unnecessary provider API calls
   that may count against rate limits.

2. **As a cluster administrator**, I want to configure the rotation polling interval so that
   I can tune the trade-off between secret freshness and provider API load for my workloads.

3. **As a platform engineer**, I want to configure `tokenRequests` audiences on the `CSIDriver`
   object through the operator configuration, so that pods can use workload identity federation
   to authenticate with AWS STS, Azure AD, or GCP IAM when fetching secrets from external vaults.

4. **As a multi-cloud operator**, I want to configure multiple token audiences on a single
   Secrets Store CSI Driver instance, so that different workloads on the same cluster can
   federate identity with different cloud providers (e.g., AWS and Azure simultaneously).

5. **As a cluster administrator**, I want my rotation and token configuration to persist across
   operator upgrades and pod restarts without manual re-intervention.

---

## Proposed API

New `SecretsStore` variant in `CSIDriverConfigSpec` discriminated union (`openshift/api` PR #2846,
merged). Fields:

```yaml
spec:
  driverConfig:
    driverType: SecretsStore
    secretsStore:
      secretRotation:
        type: None | Custom        # discriminator; Custom requires 'custom'
        custom:
          minimumRefreshAge: 300   # seconds, [1, 31560000]; omit = platform default (120s)
      tokenRequests:
        type: Managed | Unmanaged  # discriminator; Managed requires 'managed'
        managed:
          audiences:               # list, max 10, listType=map, listMapKey=audience
            - audience: "sts.amazonaws.com"
              expirationSeconds: 3600  # [600, 315360000]
            - audience: "api://AzureADTokenExchange"
```

**Key design decisions (resolved during EP review)**:

- `tokenRequests.type: Unmanaged` (default) → operator preserves any existing `tokenRequests`
  on the `CSIDriver` object (e.g., Azure WIF audiences manually patched pre-upgrade). No
  disruption on upgrade.
- `tokenRequests.type: Managed` → operator is sole source of truth; replaces all existing
  `tokenRequests`. **Immutable once set to "Managed"** — cannot revert to Unmanaged.
  To clear WIF: set `managed.audiences: []`.
- `requiresRepublish` mirrors `secretRotation.type`: set to `false` when type is `"None"`,
  `true` otherwise. Avoids unnecessary kubelet calls when rotation explicitly disabled.
- Upgrade safety: no `driverConfig` set → operator applies defaults matching existing hardcoded
  behavior (`requiresRepublish: true`, rotation enabled at 2m, tokenRequests preserved).

---

## Acceptance Criteria

1. Cluster administrator can set `secretRotation.type: None` via `ClusterCSIDriver` and the
   driver DaemonSet receives `--enable-secret-rotation=false`; `CSIDriver` has
   `requiresRepublish: false`.

2. Cluster administrator can set `secretRotation.type: Custom` with `minimumRefreshAge: 300`
   and the DaemonSet receives `--rotation-poll-interval=5m0s`; `CSIDriver` has
   `requiresRepublish: true`.

3. Platform engineer can set `tokenRequests.type: Managed` with one or more audiences and the
   `CSIDriver.spec.tokenRequests` is populated from the audiences list.

4. After upgrade with no `driverConfig` set, DaemonSet args and CSIDriver spec are unchanged
   from the previous hardcoded values.

5. Clusters that had manually patched `tokenRequests` on `CSIDriver` (e.g., Azure WIF) before
   upgrade see those tokenRequests preserved when `tokenRequests` is omitted or set to Unmanaged.

6. Setting `tokenRequests.type: Managed` then attempting to revert to Unmanaged is rejected by
   the API server (CEL immutability rule).

7. All unit tests pass for nil-path permutations of `driverConfig`, `secretsStore`,
   `secretRotation`, `tokenRequests`.

8. E2E tests cover: rotation on/off/custom interval, tokenRequests managed/unmanaged, upgrade
   scenario, multi-cloud WIF with multiple audiences.

---

## Scope

### In scope
- `ClusterCSIDriver.spec.driverConfig.secretsStore.secretRotation` (type + custom.minimumRefreshAge)
- `ClusterCSIDriver.spec.driverConfig.secretsStore.tokenRequests` (type + managed.audiences)
- Dynamic `AssetFunc` for `CSIDriver` object replacing static `csidriver.yaml`
- `DaemonSetHookFunc` for `--enable-secret-rotation` and `--rotation-poll-interval`
- Unit tests for all nil-path permutations
- E2E tests for all acceptance criteria scenarios
- Upgrade safety (Unmanaged default)

### Out of scope
- Auto-detection of cloud provider for auto-configuring token audiences
- Modifications to upstream Secrets Store CSI Driver
- Provider-specific configuration (AWS Secrets Manager, Azure Key Vault, etc.)
- MicroShift support

---

## Dependencies

- `openshift/api` PR #2846 (merged) — adds `SecretsStoreCSIDriverConfigSpec` and related types
- `openshift/api` PR #2906 (open) — renames `rotationPollIntervalSeconds` to `minimumRefreshAge`
- Upstream Secrets Store CSI Driver v1.6.0+ (requires `requiresRepublish` support)

---

## Impacted Repositories

- `github.com/openshift/secrets-store-csi-driver-operator` — primary implementation
- `github.com/openshift/api` — API types (already merged via PR #2846)

---

## References
- EP: https://github.com/openshift/enhancements/pull/2012
- API PR: https://github.com/openshift/api/pull/2846
- Rename PR: https://github.com/openshift/api/pull/2906
- Upstream v1.6.0: https://github.com/kubernetes-sigs/secrets-store-csi-driver/releases/tag/v1.6.0
- RFE-8422 (customer request): Azure Key Vault rate limiting from excessive rotation polling
