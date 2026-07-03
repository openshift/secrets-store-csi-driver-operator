# Epic Bug Analysis — Pattern Analysis
**Feature bundle:** SSCSI-235: OpenShift Console QuickStart Guides for Secrets Store CSI Driver
**Round:** 1 | **Date:** 2026-07-02

---

## Recurring Failure Patterns

### Pattern P-1: OLM Bundle Annotation Omission
**Issues:** SSCSI-235-GAP-2 (primary), STOR-2884 (supplementary)
**Recurrence:** 2 instances across the same feature cycle (2 PRs)

**Description:**
New resources added to the OLM bundle (`config/manifests/stable/`) consistently omit the required
OpenShift release/capability annotations. For ConsoleQuickStart in PR #94, four annotations were
absent and only added in the follow-up PR #95:
```yaml
capability.openshift.io/name: "Console"
include.release.openshift.io/ibm-cloud-managed: "true"
include.release.openshift.io/self-managed-high-availability: "true"
include.release.openshift.io/single-node-developer: "true"
```
Without these, resources are invisible on IBM Cloud Managed and single-node developer deployments.
The STOR-2884 TLS feature also added a metrics Service to the OLM bundle, requiring its own
annotations for proper deployment profile visibility.

**Root cause:** These annotations are not documented in the spec, not called out in any planning
checklist, and are not enforced by the OLM bundle CI until the operator is actually deployed on
the target profiles. There is no "OLM bundle annotation audit" step in templates.

**Workflow stage that should have caught it:** Specs (as a requirement) and Plan (as a checklist
item); currently neither template surfaces this pattern.

---

### Pattern P-2: OLM Bundle Placement Decision Made During Implementation
**Issue:** SSCSI-235-GAP-1
**Recurrence:** 1 instance (design flip between PR #94 and PR #95)

**Description:**
The original enhancement proposal stated both QuickStart guides would be in the OLM bundle.
During implementation (PR #95 review), the team decided the *install* QuickStart should NOT be
in the bundle because it would be circular (users need OLM to install but the guide appears only
after install). Only the example-usage QuickStart was moved to the bundle.

**Root cause:** The spec did not require a decision on "which artifacts go into the OLM bundle vs.
demo/ directory." The question of circular dependency with install QuickStarts is a well-known OLM
convention not surfaced by the spec or planning templates.

**Workflow stage that should have caught it:** Specs (scope decision: bundle-vs-demo placement)
and Plan (§3.5 Packaging/OLM phase should include bundle-placement audit per artifact).

---

### Pattern P-3: Console API Navigation Token Drift
**Issue:** SSCSI-235-BUG-1
**Recurrence:** 1 instance (Operators → Ecosystem rename)

**Description:**
QuickStart YAML manifests reference OpenShift Console section navigation via highlight tokens
(e.g., `{{highlight qs-nav-ecosystem}}`). These tokens are coupled to specific OpenShift Console
versions. Between PR #94 and PR #95, the Console renamed the "Operators" section to "Ecosystem",
invalidating the navigation reference `{{highlight qs-nav-operator-hub}}`.

**Root cause:** No repo documentation covers console API token stability or how to verify the
correct token for the target OCP version. The feature spec and repo-assessment had no mention of
this version-dependent coupling.

**Workflow stage that should have caught it:** Repo-Assessment §10.5 (Console/UI Integration)
should document navigation token conventions and version coupling.

---

### Pattern P-4: Platform Pattern Reinvention (Network Policies)
**Issue:** network-policy-redesign (supplementary feature)
**Recurrence:** 1 instance (4 policies → 1 policy + pod label)

**Description:**
The initial implementation added 4 custom network policies. A follow-up commit replaced 3 with a
single pod label (`openshift.storage.network-policy.api-server: allow`) that hooks into
CSO-managed shared network policies. This is a well-known pattern in OpenShift storage operators
where CSO manages common egress policies and operators opt-in by labeling their pods.

**Root cause:** The repo-assessment and planning templates do not prompt agents to research
"existing platform patterns your pods can adopt" before designing new infrastructure. The anti-
duplication section (§5 Reusable Assets) is generic; it does not flag platform-level hooks.

**Workflow stage that should have caught it:** Repo-Assessment §5 (Reusable Assets) and §10.2
(Proxy & Network Configuration) should document the CSO network policy label hook.

---

### Pattern P-5: Content Quality Gap (Typo)
**Issue:** SSCSI-235-BUG-2
**Recurrence:** 1 instance ("prosessing" → "possessing")

**Description:**
A simple typo in the QuickStart task description text survived PR #94 review and was caught only
by automated review (CodeRabbit) during PR #95.

**Root cause:** No acceptance criterion in the spec or task payload required a content quality
review step for user-facing copy in manifest YAML files.

**Workflow stage that should have caught it:** Implementation tasks for manifest-only features
should include a content/copy review step in acceptance criteria.

---

## Cross-Cutting Observations

1. **Two-PR feature delivery is a signal of spec incompleteness.** When a feature requires a
   follow-up PR to fix design decisions (not bugs), the spec failed to resolve those decisions
   upfront. SSCSI-235 needed PR #95 for: bundle annotations, bundle-vs-demo placement, navigation
   token correction. All three were knowable at spec time.

2. **OLM-bundle-only features have a distinct checklist.** Manifest-only changes that touch
   `config/manifests/stable/` need: (a) annotation audit, (b) bundle-placement decision, (c)
   CSV RBAC review, (d) bundle.Dockerfile inclusion verification. None of these are in the
   current templates.

3. **No unit tests for any of these features.** All three feature areas (QuickStarts, network
   policies, TLS service) are manifest-only with zero unit test coverage. E2E is the only safety
   net. Templates should explicitly acknowledge this gap and require an E2E test plan.
