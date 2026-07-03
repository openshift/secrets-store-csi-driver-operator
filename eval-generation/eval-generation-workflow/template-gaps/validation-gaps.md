# Template Gaps — validation-template.md
**Round:** 1 | **Source issues:** SSCSI-235-GAP-2 (I-001), SSCSI-235-GAP-1 (I-002)

---

## Gap V-1: No OLM Bundle Completeness Trigger
**Severity:** patchable
**Pattern:** OLM-bundle-annotation-omission (I-001)

**Current state:**
The validation template defines generic completeness pillars (Context, Personas, ACs, Scope,
Impacted Repos). It does not define any OLM-specific pillar or trigger.

**Gap:**
When a spec mentions placing resources in an OLM bundle, the validator does not check for:
- Which specific resources enter `config/manifests/stable/` vs. `demo/`
- Whether `capability.openshift.io/name` and `include.release.openshift.io/*` annotations are specified
- Whether the circular-dependency problem for install-type QuickStarts is addressed

**Recommended addition (patchable — add to Rubric A and project_ecosystem):**
Add an OLM-bundle trigger: if the spec mentions OLM bundle, CSV, or `config/manifests/stable/`,
require the spec to list: (a) each resource and its target location (bundle vs demo), (b) required
deployment-profile annotations. Flag as `missing_elements` if absent.

For projects using AGENTS.md with a `Validation Stage Hints` section that defines an OLM pillar,
this can be expressed as a project ecosystem boolean: `olm_bundle_annotations_specified: true/false`.

---

## Gap V-2: No Console API Version Trigger
**Severity:** eval-only (cannot be generically auto-detected from spec text)
**Pattern:** console-api-token-version-coupling (I-003)

**Current state:**
The template does not prompt for console navigation token version compatibility.

**Gap:**
If a spec mentions ConsoleQuickStart or console navigation highlights, the validator cannot check
whether the spec pins the OCP version for navigation token compatibility.

**Recommended addition (eval-only):**
Add a few-shot calibration example in the template for specs that include ConsoleQuickStart
resources, showing that `qs-nav-*` tokens should be called out with OCP version compatibility note.

---

## No gaps found in: scoring math, quality rubric sections B, output JSON schema
