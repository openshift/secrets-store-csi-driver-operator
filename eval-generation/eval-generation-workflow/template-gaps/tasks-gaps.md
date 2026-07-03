# Template Gaps — tasks-template.md
**Round:** 1 | **Source issues:** SSCSI-235-GAP-2 (I-001), SSCSI-235-BUG-2 (I-004)

---

## Gap TK-1: No OLM Bundle Annotation Checklist in Task Acceptance Criteria
**Severity:** patchable
**Pattern:** OLM-bundle-annotation-omission (I-001)

**Current state:**
Task acceptance criteria say "must trace to validated_specs.md; include tests to run/areas."
There is no guidance to include an OLM annotation checklist for tasks that add resources to
the OLM bundle.

**Gap:**
Any task whose target files include `config/manifests/stable/*.yaml` MUST have an acceptance
criterion: "Verify the new resource carries: capability.openshift.io/name and all applicable
include.release.openshift.io/* annotation keys. Cross-check with the operator's existing bundle
resources for the expected annotation set."

**Recommended addition (patchable):**
Add to the Implementation notes section guidance: "For tasks adding OLM bundle resources: include
an acceptance criterion explicitly checking for deployment-profile annotations. Do not rely on the
spec alone — treat this as a non-negotiable bundle contract item."

---

## Gap TK-2: No User-Facing Content Review Step for Manifest-Only Tasks
**Severity:** patchable
**Pattern:** user-facing-copy-unreviewed (I-004)

**Current state:**
The task payload template has Implementation notes and Acceptance criteria. Neither mentions
a content review step for user-visible text in YAML manifests (QuickStart descriptions, task
summaries, prerequisite text, etc.).

**Gap:**
For tasks that create ConsoleQuickStart, ConsoleYAMLSample, or other user-facing YAML
resources, the acceptance criteria should include: "Review all user-visible text fields
(description, spec.introduction, spec.tasks[].description) for typos, grammatical correctness,
and accuracy of navigation references."

**Recommended addition (patchable):**
Add to Acceptance criteria guidance: "For manifest-only tasks producing user-visible content:
add an explicit 'content review' AC: 'All user-visible text fields are free of typos, correctly
reference current navigation paths, and use consistent terminology.'"

---

## No gaps found in: task decomposition granularity guidance, DAG/dependency format, §5 orchestration
##   notes structure, agent routing instructions, manifest table format
