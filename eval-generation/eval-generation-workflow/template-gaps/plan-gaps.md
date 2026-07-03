# Template Gaps — plan-template.md
**Round:** 1 | **Source issues:** SSCSI-235-GAP-1 (I-002), network-policy-redesign (I-005)

---

## Gap PL-1: §3.5 Packaging/OLM — No Bundle Placement Decision Matrix
**Severity:** patchable
**Pattern:** OLM-bundle-placement-decision (I-002)

**Current state:**
§3.5 Packaging/OLM says: "OLM/CSV ownership rules, bundle layout, image references,
feature gates/TechPreview markers." It does not require a per-resource placement decision.

**Gap:**
For OLM-bundle-only features (manifest-only changes to config/manifests/stable/), the plan MUST
include an explicit decision for each new resource: does it go in the OLM bundle, in demo/, or
both? Without this, the task agent defaults to the spec description, which may be incomplete
(as seen with SSCSI-235 install QuickStart).

**Recommended addition (patchable):**
Add to §3.5 guidance: "For any feature adding new resources to the operator's bundle, produce a
placement matrix:
| Resource | Location | Rationale | Circular dependency risk? |
|---|---|---|---|
Include a row per resource. Note if a resource's presence in the bundle creates circular
dependency (e.g., install QuickStarts that describe how to install the operator)."

---

## Gap PL-2: §5 Phase Guidance — No 'Survey Existing Platform Patterns' Prerequisite
**Severity:** patchable
**Pattern:** platform-pattern-reinvention (I-005)

**Current state:**
The phase template requires: Goal, Dependencies, Target files, Required capabilities, Verification
hooks. Nothing prompts the planner to explicitly check for platform-level patterns before designing
new infrastructure.

**Gap:**
For features that add network policies, security contexts, or other infrastructure that platform
operators (CSO, CNO) might already manage, the plan should have an early discovery phase or a
prerequisite bullet: "Survey existing platform patterns — check CSO/CNO for shared network
policies, shared CA bundles, shared SCCs — before designing new infrastructure."

**Recommended addition (patchable):**
Add to phase template guidance: "For phases that add infrastructure (network policies, RBAC,
TLS, SCC overrides): add a prerequisite bullet 'Survey existing platform operator patterns to
check for reusable hooks (CSO, CNO, etc.) before designing custom resources.'"

---

## No gaps found in: §0–§2 input handling, §4 dependencies/sequencing, §6 verification matrix format,
##   §7 risks section, §8 open questions format
