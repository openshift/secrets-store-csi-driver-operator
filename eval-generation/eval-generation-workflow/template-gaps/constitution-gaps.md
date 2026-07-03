# Template Gaps — constitution-template.md
**Round:** 1 | **Source issues:** SSCSI-235-GAP-2 (I-001), SSCSI-235-BUG-1 (I-003)

---

## Gap C-1: No OLM Bundle Annotation Principle
**Severity:** patchable
**Pattern:** OLM-bundle-annotation-omission (I-001)

**Current state:**
The constitution template has principles for: controller patterns, upstream operand separation,
test gates, generated code discipline, RBAC/security, OLM/release constraints. The OLM/release
section (Principle VI) is about CSV ownership and relatedImages, but does not explicitly call out
annotation requirements for individual resources inside the bundle.

**Gap:**
A constitution principle should exist: "Any resource placed into the OLM bundle MUST carry the
complete set of deployment-profile annotations (capability.openshift.io/name,
include.release.openshift.io/*) appropriate for this operator's supported profiles."

This is a non-negotiable guardrail (missed it → resources invisible on some deployment profiles)
that belongs in constitution, not just in repo-assessment.

**Recommended addition (patchable):**
Add a sub-bullet to Principle VI (or as Principle VII if the repo already has VI filled) in
the Additional Constraints section: "OLM bundle resource annotations: any resource type added
to config/manifests/stable/ MUST include the full set of capability + include.release annotations."

---

## Gap C-2: No Console API Token Stability Constraint
**Severity:** eval-only (project-specific, low generalizability)
**Pattern:** console-api-token-version-coupling (I-003)

**Current state:**
Constitution templates don't generally cover UI API token stability — it's too project-specific
to put in the generic template.

**Gap:**
For operators that ship ConsoleQuickStart, the constitution should note: "Navigation tokens
(qs-nav-*) in ConsoleQuickStart manifests MUST be verified against the target OCP release branch.
Do not copy tokens from older examples without checking the current console-operator API."

**Recommended addition (eval-only):**
This is best captured as an eval case for repo-assessment (test that §10.5 documents tokens)
rather than patching the generic constitution template. Deferred.

---

## No gaps found in: core principle structure, evidence-backing requirement, governance section format
