# Template Gaps — adrs-template.md
**Round:** 1 | **Source issues:** SSCSI-235-GAP-1 (I-002)

---

## Gap ADR-1: OLM Bundle Placement is an Unrecorded Design Decision
**Severity:** eval-only
**Pattern:** OLM-bundle-placement-decision (I-002)

**Current state:**
ADRs are optional artifacts. No ADR was produced for SSCSI-235, meaning the "install QuickStart
NOT in OLM bundle" decision has no durable record.

**Gap:**
For features where a significant design decision is made mid-implementation (as opposed to
upfront in the spec), an ADR should be generated to record: the decision, alternatives
considered, and rationale. This enables future agents to read the ADR and avoid relitigating
the same decision.

**Recommended addition (eval-only):**
The adrs-template.md is well-structured. The gap is behavioral: tasks-template should have
a rule "if a design decision is made that was not in the original spec, generate an ADR task."
Deferred to tasks-template.

---

## No structural gaps found in the adrs-template itself.
