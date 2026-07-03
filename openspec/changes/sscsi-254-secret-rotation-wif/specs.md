# Feature Specification: Configurable Secret Rotation and Workload Identity Federation for Secrets Store CSI Driver

**Feature Branch**: `sscsi-254-secret-rotation-wif`

**Created**: 2026-07-03

**Status**: Draft

**Input**: SSCSI-254 — Enhancement Proposal https://github.com/openshift/enhancements/pull/2012 (merged 2026-07-02)

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Disable Automatic Secret Rotation (Priority: P1)

A cluster administrator whose workloads use static secrets (secrets that never change at the
provider) wants to stop the driver from periodically contacting the provider. Today the driver
polls every 2 minutes regardless; there is no way to turn this off. This causes unnecessary
rate-limit consumption on providers such as Azure Key Vault, especially at scale (~200 secrets
per cluster per RFE-8422).

**Why this priority**: Core customer pain driving the feature. Incorrect defaults increase cloud
provider costs and risk rate-limit errors. Disabling rotation must be reliable and persistent
across upgrades.

**Independent Test**: Configure the operator to disable rotation; verify the driver stops sending
periodic NodePublishVolume calls by inspecting the CSI driver operand's runtime flags and the
cluster-level CSIDriver object.

**Acceptance Scenarios**:

1. **Given** a cluster with the operator installed and rotation currently enabled (default),
   **When** the cluster administrator sets `secretRotation.type: None` on the `ClusterCSIDriver`,
   **Then** the driver node component has `--enable-secret-rotation=false` in its container
   arguments and the cluster-scoped CSI driver resource has `requiresRepublish: false`,
   and kubelet stops issuing periodic NodePublishVolume calls for already-mounted volumes.

2. **Given** a `ClusterCSIDriver` with rotation disabled (`type: None`),
   **When** the operator pod is restarted or the cluster is upgraded,
   **Then** the rotation-disabled configuration is restored without administrator re-intervention,
   and no secrets are disrupted during the restart.

3. **Given** `secretRotation.type: None` is set,
   **When** the cluster administrator changes it back to `type: Custom`,
   **Then** the driver node component resumes periodic re-fetch behavior with the configured
   refresh age and the CSI driver resource is updated to `requiresRepublish: true`.

---

### User Story 2 — Configure Custom Rotation Interval (Priority: P1)

A cluster administrator wants to control how frequently the driver contacts the secret provider.
The default interval (2 minutes) is too aggressive for some environments (high API cost) and
too slow for others (need faster secret refresh). The administrator needs a way to express
the minimum time between provider contacts.

**Why this priority**: Directly addresses the rate-limiting customer pain (RFE-8422) for
administrators who want rotation enabled but at a longer interval rather than disabling it.

**Independent Test**: Configure a custom refresh age; verify the driver container argument
reflects the correct interval value.

**Acceptance Scenarios**:

1. **Given** a `ClusterCSIDriver` with `secretRotation.type: Custom` and a `minimumRefreshAge`
   of 300 seconds,
   **When** the operator reconciles,
   **Then** the driver container argument for the rotation interval is set to `5m0s` and
   the CSI driver resource has `requiresRepublish: true`.

2. **Given** `secretRotation.type: Custom` with `minimumRefreshAge` omitted,
   **When** the operator reconciles,
   **Then** the operator applies the platform default refresh age (120 seconds / 2 minutes),
   preserving the same behavior as prior operator versions.

3. **Given** `secretRotation.type: Custom` with `minimumRefreshAge: 1` (minimum boundary),
   **When** the operator reconciles,
   **Then** the configuration is accepted and the interval is set to `1s`; note that the
   effective refresh cadence is bounded by the kubelet `syncFrequency` (default: 1 minute).

---

### User Story 3 — Configure Workload Identity Federation Token Audiences (Priority: P1)

A platform engineer wants to enable pods to authenticate with cloud providers (AWS STS, Azure AD,
GCP IAM) using short-lived, pod-bound service account tokens instead of long-lived static
credentials. This requires configuring which token audiences the driver requests from the
API server during each NodePublishVolume call.

**Why this priority**: WIF is required for modern cloud-native secret access patterns. Without
it, workloads must use long-lived credentials, which are a security risk.

**Independent Test**: Configure a managed audience; verify the cluster-scoped CSI driver resource
carries the correct tokenRequests entries that kubelet will use to issue tokens.

**Acceptance Scenarios**:

1. **Given** a `ClusterCSIDriver` with `tokenRequests.type: Managed` and one or more audience
   entries,
   **When** the operator reconciles,
   **Then** the cluster-scoped CSI driver resource has `tokenRequests` populated with exactly
   the audiences specified in the `ClusterCSIDriver`, and kubelet begins issuing service account
   tokens with those audiences during NodePublishVolume calls.

2. **Given** `tokenRequests.type: Managed` with an explicit empty audiences list,
   **When** the operator reconciles,
   **Then** the CSI driver resource has no tokenRequests entries (WIF is cleared).

3. **Given** `tokenRequests.type: Managed` is already set,
   **When** the cluster administrator attempts to change it back to `type: Unmanaged`,
   **Then** the API server rejects the request with an error indicating the type field
   is immutable once set to Managed.

---

### User Story 4 — Preserve Existing Manually-Configured Token Audiences on Upgrade (Priority: P1)

A cluster administrator who already configured `tokenRequests` directly on the CSI driver
resource (before this feature existed, as a manual workaround for Azure WIF) wants the
operator upgrade to preserve their configuration — not overwrite it.

**Why this priority**: Disrupting existing WIF configurations during upgrade would break running
workloads and cause a secret-fetch outage. Upgrade safety is non-negotiable.

**Independent Test**: Simulate a cluster with manually-patched tokenRequests; upgrade; verify
tokenRequests are not cleared and no pod disruption occurs.

**Acceptance Scenarios**:

1. **Given** a cluster where `tokenRequests` was manually patched on the CSI driver resource
   (e.g., Azure WIF `api://AzureADTokenExchange`) and `tokenRequests` is omitted from
   the `ClusterCSIDriver`,
   **When** the operator upgrades to the version that includes this feature,
   **Then** the existing tokenRequests on the CSI driver resource are preserved unchanged,
   the CSI driver resource is not recreated, and pods continue mounting secrets without
   interruption.

2. **Given** `tokenRequests.type: Unmanaged` (explicitly set),
   **When** the operator reconciles,
   **Then** any tokenRequests already present on the live CSI driver resource are preserved
   and not replaced by the operator.

---

### User Story 5 — Multi-Cloud Workload Identity (Priority: P2)

A multi-cloud operator wants a single Secrets Store CSI Driver instance on a cluster that serves
workloads using different cloud providers — some using AWS STS, others using Azure AD. Both
sets of workloads need their respective token audiences available in every NodePublishVolume call.

**Why this priority**: Multi-cloud is an advanced use case; single-cloud WIF is P1. This extends
WIF to support simultaneous multiple audiences.

**Independent Test**: Configure two audiences (AWS + Azure); verify both appear in the CSI
driver resource tokenRequests.

**Acceptance Scenarios**:

1. **Given** `tokenRequests.type: Managed` with two audience entries (e.g., `sts.amazonaws.com`
   and `api://AzureADTokenExchange`),
   **When** the operator reconciles,
   **Then** the CSI driver resource has both tokenRequests entries in the order specified, and
   both audiences are available to provider plugins during NodePublishVolume calls.

---

### Edge Cases

- **When** `managementState` is set to `Removed` on the `ClusterCSIDriver`, **then** the operator
  removes the CSI driver resource and the driver node component regardless of any `driverConfig`
  settings. No secretsStore configuration is applied while in Removed state.
- **When** a `minimumRefreshAge` value below 1 second is submitted, **then** the API server
  rejects it at admission time with a validation error.
- **When** a `minimumRefreshAge` value above 31,560,000 seconds is submitted, **then** the API
  server rejects it at admission time with a validation error.
- **When** an `expirationSeconds` value below 600 is submitted for a token audience, **then**
  the API server rejects it at admission time with a validation error.
- **When** more than 10 audience entries are specified, **then** the API server rejects the
  request at admission time.
- **When** duplicate `audience` values are specified in the audiences list, **then** the API
  server rejects the request (list enforces unique keys).
- **When** the CSI driver resource does not exist on the cluster (e.g., first install), **then**
  the operator creates it with the correct `requiresRepublish` and `tokenRequests` values from
  the `ClusterCSIDriver` configuration.
- **When** the driver node component's rolling update is in progress (configuration changed),
  **then** pods that already have secrets mounted continue serving the mounted data from the
  prior successful provider call until the rolling update completes. The update does not disrupt
  running workloads.

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The operator MUST allow cluster administrators to disable automatic secret rotation
  via the cluster CSI driver configuration resource, causing the driver to fetch secrets only at
  initial pod mount time.

- **FR-002**: The operator MUST allow cluster administrators to configure a custom minimum time
  between consecutive provider calls for secret refresh, with acceptable values between 1 second
  and 31,560,000 seconds (~1 year).

- **FR-003**: When `secretRotation` configuration is omitted or the platform default is selected,
  the operator MUST preserve the pre-upgrade behavior: rotation enabled at a 2-minute interval.

- **FR-004**: The operator MUST allow administrators to configure a list of token audiences
  (workload identity federation) that the driver requests from the cluster API server during
  volume mount operations. The list must support up to 10 entries with unique audience values.

- **FR-005**: When `tokenRequests` configuration is omitted or set to Unmanaged, the operator
  MUST preserve any token audiences already present on the live cluster-scoped CSI driver
  resource and MUST NOT clear or overwrite them.

- **FR-006**: Once a cluster administrator explicitly sets the `tokenRequests` management policy
  to Managed, the operator MUST enforce that this setting cannot be reverted to Unmanaged.
  Clearing WIF audiences is accomplished by setting an explicit empty audience list.

- **FR-007**: The operator MUST propagate `secretRotation` configuration changes to the driver
  node component via a rolling update without disrupting pods that currently have secrets mounted.

- **FR-008**: The operator MUST propagate `tokenRequests` configuration changes to the
  cluster-scoped CSI driver resource. When the configuration changes, the resource is recreated
  atomically; the window where the resource is absent MUST be negligible and MUST NOT affect
  pods that already have secrets mounted.

- **FR-009**: All `secretRotation` and `tokenRequests` configuration MUST survive operator pod
  restarts and cluster upgrades without administrator re-intervention.

### Key Entities

- **Cluster CSI Driver configuration resource** (`ClusterCSIDriver`): Singleton cluster-scoped
  operator configuration. Contains `managementState` (Managed/Unmanaged/Removed) and the new
  `secretsStore` driver configuration block. Source of truth for all rotation and WIF settings.

- **CSI driver cluster resource** (`CSIDriver`): Cluster-scoped Kubernetes object that kubelet
  reads to determine driver capabilities (`requiresRepublish`, `tokenRequests`). Owned by the
  operator; recreated when spec changes.

- **Driver node component**: DaemonSet running the CSI driver on every node. Receives rotation
  behavior via container arguments. Updated via rolling update when configuration changes.

- **Secret rotation configuration**: Sub-object of the operator configuration with a type
  discriminator (`None` | `Custom`) and optional custom block (`minimumRefreshAge`).

- **Token requests configuration**: Sub-object of the operator configuration with a type
  discriminator (`Managed` | `Unmanaged`) and optional managed block (`audiences` list). The
  Managed type is immutable once set.

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: After a cluster administrator sets `secretRotation.type: None`, the driver node
  component container arguments reflect `--enable-secret-rotation=false` within the time it
  takes for one reconcile cycle plus DaemonSet rolling update to complete (typically under 5
  minutes on a standard cluster).

- **SC-002**: After setting `secretRotation.type: Custom` with a non-default `minimumRefreshAge`,
  the driver node component container arguments reflect the correct interval value
  (`minimumRefreshAge` seconds converted to a duration string) within one reconcile cycle.

- **SC-003**: After setting `tokenRequests.type: Managed` with audience entries, the CSI driver
  cluster resource `tokenRequests` field matches the configured audience list exactly. Kubelet
  is able to issue service account tokens with the configured audiences to the driver.

- **SC-004**: A cluster upgraded from a prior version with no `driverConfig` set shows zero
  change in driver node component arguments and zero change to the CSI driver cluster resource
  after upgrade — verified by observing no DaemonSet rolling update is triggered.

- **SC-005**: A cluster where `tokenRequests` was manually configured on the CSI driver cluster
  resource before upgrade retains those audiences after upgrade, verified by comparing the
  `tokenRequests` field before and after with no difference.

- **SC-006**: Attempting to set `tokenRequests.type` from Managed back to Unmanaged is rejected
  by the API server with a validation error, without the configuration being partially modified.

---

## Assumptions

- **A-001**: Implementation uses the field name `minimumRefreshAge` (from `openshift/api` PR
  #2906). If PR #2906 has not merged at implementation start, the field is `rotationPollIntervalSeconds`
  (from PR #2846). A follow-up task must rename it once #2906 merges. The specs.md uses
  `minimumRefreshAge` as the canonical name going forward.

- **A-002**: The operator's existing authorization already includes read access to its own
  cluster CSI driver configuration resource for the purpose of building the desired state.
  No new RBAC rules in the operator packaging are required. If this assumption is false,
  adding RBAC must be treated as a blocking dependency in the plan.

- **A-003**: The CSI driver cluster resource recreation (delete + recreate) that occurs when
  `requiresRepublish` or `tokenRequests` changes is handled transparently by the operator's
  existing resource-apply mechanism using annotation-based spec hashing. Pods that already
  have secrets mounted are not disrupted during the brief absence of the cluster resource.

- **A-004**: `managementState: Managed` (default) is required on the cluster CSI driver
  configuration resource for any `secretsStore` configuration to take effect. `Unmanaged` and
  `Removed` management states cause the operator to skip driver configuration management.

- **A-005**: The platform default for `minimumRefreshAge` when omitted is 120 seconds (2
  minutes), matching the previously hardcoded behavior. This default is not expressed as a
  CRD-level default annotation — the operator code applies it internally.

- **A-006**: Token audience `expirationSeconds` is optional. When omitted, the token expiration
  is determined by the cluster API server. The minimum enforced value is 600 seconds (10
  minutes) at the API admission level; the cluster API server may apply its own lower bound.

- **A-007**: This feature targets OpenShift 5.0.0 and GA directly (no Tech Preview). Older
  `ClusterCSIDriver` objects in etcd without `driverConfig` are handled by nil-checks in the
  operator code; the CRD-level defaults are not retroactively applied to stored objects.

## OLM Bundle Placement *(not applicable)*

This feature makes no changes to OLM bundle manifests or console resources.
All changes are to Go source code and operator controller logic only.
