# Template Gaps — spec-template.md
**Round:** 1 | **Source issues:** SSCSI-235-GAP-1 (I-002), SSCSI-235-GAP-2 (I-001)

---

## Gap S-1: No OLM Bundle Placement Decision Section
**Severity:** patchable
**Pattern:** OLM-bundle-placement-decision (I-002)

**Current state:**
The spec template defines User Scenarios, Functional Requirements, Key Entities, Success Criteria,
and Assumptions. It has no section for packaging/deployment decisions (OLM bundle vs demo/manual).

**Gap:**
When a feature involves OLM-deliverable resources, the spec must explicitly answer:
- Which resources go into the OLM bundle (`config/manifests/stable/`)? Which go into `demo/`?
- Is there a circular dependency risk? (e.g., install QuickStart bundled when OLM triggers the install)
- Which deployment profiles (IBM Cloud Managed, SNO, HA) must the resource appear on?

**Recommended addition (patchable):**
Add to the Assumptions section: "**A-OLM-001** pattern" — if a spec includes OLM-bundled resources,
force an explicit A-xxx assumption: `A-OLM-001: The following resources enter the OLM bundle:
[list]; the following remain in demo/ only: [list]. Rationale: [reason].`

Add to Quality rules: "For OLM bundle features, at least one FR must specify required deployment-
profile annotations OR an explicit assumption must state which deployment profiles are targeted."

---

## Gap S-2: No OLM Annotation Requirement
**Severity:** patchable
**Pattern:** OLM-bundle-annotation-omission (I-001)

**Current state:**
The spec template FR template says `System MUST [specific capability]`. Nothing prompts the
spec author to enumerate required OLM metadata (annotations) for resources entering the bundle.

**Gap:**
For any resource that ships via OLM, the spec should include a FR of the form:
`FR-OLM-001: All resources in the OLM bundle MUST carry capability.openshift.io/name and
include.release.openshift.io/* annotations matching the operator's target deployment profiles.`

Without this FR, the planning and task templates have no constraint to pass down.

**Recommended addition (patchable):**
Add to FR template guidance: "For OLM-bundled resources, add an FR-OLM-xxx: [Resource kind]
MUST include deployment-profile annotations: capability.openshift.io/name and all applicable
include.release.openshift.io/* keys."

---

## No gaps found in: User Story format, Given/When/Then structure, Success Criteria measurability
