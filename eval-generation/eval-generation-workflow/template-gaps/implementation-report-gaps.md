# Template Gaps — implementation-report-template.md
**Round:** 1 | **Source issues:** SSCSI-235-BUG-2 (I-004), SSCSI-235-GAP-2 (I-001)

---

## Gap IR-1: No OLM Annotation Verification in Implementation Report
**Severity:** eval-only
**Pattern:** OLM-bundle-annotation-omission (I-001)

**Current state:**
The implementation report template summarizes what was implemented. It does not include a
post-implementation verification checklist for OLM bundle conventions.

**Gap:**
For manifest-only features, the implementation report should include a section confirming:
- All OLM bundle resources have the required deployment-profile annotations
- The bundle.Dockerfile includes all new manifest files
- No install-type resources with circular dependency were accidentally bundled

**Recommended addition (eval-only):**
Best enforced in tasks-template acceptance criteria (TK-1) rather than patching the report
template. The report template can reference the tasks checklist as complete.

---

## No other gaps found from this feature bundle.
