# Evaluation Report: validation

**Change:** sscsi-254-secret-rotation-wif
**Artifact:** validation (`openspec/changes/sscsi-254-secret-rotation-wif/validation.json`)
**Evaluated at:** 2026-07-03T12:31:00Z
**Gate:** rubric_only (validation.md rubric)

---

## Eval Summary

| Metric | Value |
|--------|-------|
| Overall score | **86%** |
| Completeness score | 87% |
| Quality score | 84% |
| Status | **PASS** (threshold: 80%) |
| Blockers | 0 |
| Non-blockers | 6 |
| Refinement applied | No |

---

## Rubric Scoring Detail

### Completeness Pillars (60% weight → 87)

| Pillar | Present | Notes |
|--------|---------|-------|
| Context & Motivation | ✅ | GA'd at 4.17, hardcoded rotation, no WIF support; RFE-8422 customer pain |
| User Personas / Actors | ✅ | cluster administrator, platform engineer, multi-cloud operator |
| Acceptance Criteria & Edge Cases | ✅ | 8 ACs; AC 1–6 are user-observable; AC 7–8 are test delivery |
| Scope Boundaries & Dependencies | ✅ | In/out-of-scope table; 3 named deps with status (merged/open) |
| Impacted Repositories | ✅ | `openshift/secrets-store-csi-driver-operator`, `openshift/api` |

**Deductions:**
- `-7`: RBAC for ClusterCSIDriver lister in dynamic AssetFunc not specified
- `-6`: Target OCP version (5.0.0) absent from spec body

### Quality Checks (40% weight → 84)

| Check | Result | Notes |
|-------|--------|-------|
| Ambiguity | ⚠️ | `minimumRefreshAge` field name depends on open PR #2906 |
| Testability | ⚠️ | User stories in prose, not Given/When/Then; ACs 7–8 are test delivery |
| Sizing | ✅ | Well-scoped; not trying to do too much |
| Consistency | ✅ | API design aligns with EP; defaults match existing behavior |

---

## Gap Analysis

### Gap 1 — RBAC extension for dynamic ClusterCSIDriver read
**Severity:** MODERATE
**What is missing:** The spec introduces a dynamic `AssetFunc` that reads `ClusterCSIDriver` via a lister at reconcile time. The current operator may already have RBAC to read its own CR (it watches `ClusterCSIDriver` via `StaticResourceController`), but the spec does not explicitly confirm this or state whether new RBAC rules in the CSV are required.
**Input artifact that should address it:** `jira-spec.md` §Scope / §Acceptance Criteria
**Recommendation:** Add an assumption: "A-RBAC-001: The operator's existing RBAC includes GET/LIST/WATCH on `clustercsidriver` resources in `operator.openshift.io/v1`. No new RBAC rules are needed. Verify against current CSV before implementation."

### Gap 2 — DaemonSet rolling update behavior on config change
**Severity:** MODERATE
**What is missing:** When `minimumRefreshAge` or `secretRotation.type` changes, the DaemonSet hook will inject new container args, triggering a rolling update. The spec does not state: (a) whether this rolling update is expected to be non-disruptive, (b) what happens to pods currently mounting secrets during the rolling update, (c) whether there is a graceful drain window.
**Input artifact:** `jira-spec.md` §Acceptance Criteria
**Recommendation:** Add an AC: "AC-9: When secretRotation.type changes, the DaemonSet undergoes a rolling update. Existing pods that have already mounted secrets continue serving the mounted data during the rolling update (no disruption to running workloads)."

### Gap 3 — managementState: Removed interaction with new driverConfig
**Severity:** MINOR
**What is missing:** The spec does not address what the operator does to the CSIDriver and DaemonSet when `ClusterCSIDriver.spec.managementState: Removed` is set while `driverConfig.secretsStore` is configured. The existing operator logic for `Removed` state is in `getOperatorSyncState` and removes the CSI driver entirely — does the new configuration also get cleared?
**Input artifact:** `jira-spec.md` §Acceptance Criteria
**Recommendation:** Add edge case: "When managementState is Removed, the operator removes the CSIDriver object and the DaemonSet regardless of driverConfig settings. No secretsStore configuration is applied."

### Gap 4 — API field name dependency on open PR #2906
**Severity:** MINOR
**What is missing:** PR #2906 (rename `rotationPollIntervalSeconds` → `minimumRefreshAge`) is still open at time of spec creation. If implementation begins before PR #2906 merges, the field name used in the spec will be incorrect.
**Input artifact:** `jira-spec.md` §Dependencies
**Recommendation:** Add assumption: "A-001: Implementation uses `minimumRefreshAge` (from PR #2906). If PR #2906 has not merged at implementation start, use the field name from PR #2846 (`rotationPollIntervalSeconds`) and file a follow-up to rename once #2906 merges."

### Gap 5 — User stories lack Given/When/Then format
**Severity:** MINOR
**What is missing:** User stories 1–5 are in plain prose. The spec template requires Given/When/Then format for user-visible behavior so that stories map directly to automated test scenarios.
**Input artifact:** `jira-spec.md` §User Stories
**Recommendation:** Rewrite each story with at least one Given/When/Then acceptance scenario. Example for US-1: "Given a ClusterCSIDriver with secretRotation.type: None, When the operator reconciles, Then the DaemonSet has --enable-secret-rotation=false and the CSIDriver has requiresRepublish: false."

---

## Quality Assessment

| Dimension | Score | Notes |
|-----------|-------|-------|
| Completeness | 87% | Strong coverage; minor gaps in RBAC and version targeting |
| Consistency | 95% | API design matches EP; defaults match existing behavior |
| Grounding | 92% | All claims backed by EP PR #2012 and openshift/api PRs |
| Operator-specific | 85% | tokenRequests.type semantics and upgrade safety well-covered; DaemonSet rolling update behavior not addressed |

---

## Recommendations for Downstream Stages

1. **specs.md**: Add Given/When/Then format to all user stories. Add assumption A-001 about field name dependency on PR #2906. Move ACs 7–8 to a Test Plan sub-section.
2. **repo-assessment**: Verify RBAC for ClusterCSIDriver lister in dynamic AssetFunc. Confirm whether existing `StaticResourceController` informer wiring already gives the operator GET/LIST/WATCH on its own CR.
3. **plan.md**: Include a phase for DaemonSet rolling update behavior documentation. Address managementState: Removed edge case in §7 Risks.
4. **tasks.md**: Pair the dynamic AssetFunc implementation task with a unit test task covering nil-path permutations (as called out in AC-7). The EP's test plan section is unusually detailed — use it directly as the task acceptance criteria source.
