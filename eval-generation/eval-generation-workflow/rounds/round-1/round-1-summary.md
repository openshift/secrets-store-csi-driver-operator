# Round 1 Snapshot
**Date:** 2026-07-02
**Feature bundle:** SSCSI-235 — OpenShift Console QuickStart Guides for Secrets Store CSI Driver
**Epic key:** SSCSI-235
**Supplementary:** network-policy-redesign, STOR-2884

---

## Issues Analyzed (5)

| ID | Key | Title | Stage | Severity |
|----|-----|-------|-------|----------|
| I-001 | SSCSI-235-GAP-2 | OLM bundle annotations missing | specs + plan | High |
| I-002 | SSCSI-235-GAP-1 | Install QuickStart bundle placement ambiguous | specs | Medium |
| I-003 | SSCSI-235-BUG-1 | Console nav token changed (Operators→Ecosystem) | repo-assessment | Medium |
| I-004 | SSCSI-235-BUG-2 | Typo: prosessing → possessing | implementation | Low |
| I-005 | network-policy-redesign | Platform NP hook missed; 4 policies reimplemented | plan | Medium |

## Patterns Identified (5)

- P-1: OLM-bundle-annotation-omission (2 instances)
- P-2: OLM-bundle-placement-decision (1 instance)
- P-3: console-api-token-version-coupling (1 instance)
- P-4: platform-pattern-reinvention (1 instance)
- P-5: user-facing-copy-unreviewed (1 instance)

## Templates Patched (5)

| Template | Gap IDs | Severity |
|---------|---------|----------|
| agents.md | AG-1, AG-2, AG-3, AG-4 | High, Med, High, Med |
| spec-template.md | S-1, S-2 | Patchable |
| plan-template.md | PL-1, PL-2 | Patchable |
| tasks-template.md | TK-1, TK-2 | Patchable |
| repo-assessment-template.md | RA-1, RA-2, RA-3 | Patchable |

## Eval Cases Generated

| Stage | Cases |
|-------|-------|
| repo-assessment | 4 (ra-001 – ra-004) |
| constitution | 3 (con-001 – con-003) |
| plan | 3 (plan-001 – plan-003) |
| tasks | 4 (tasks-001 – tasks-004) |
| implementation | 5 (impl-001 – impl-005) |
| code-generation | 5 (cg-001 – cg-005) |
| **Total** | **24** |

## Gap Reports Written (12)

validation-gaps.md, spec-gaps.md, repo-assessment-gaps.md, constitution-gaps.md,
plan-gaps.md, tasks-gaps.md, code-generation-gaps.md, agents-gaps.md,
design-bundle-gaps.md, implementation-report-gaps.md, implementation-task-report-gaps.md,
adrs-gaps.md

## Key Learnings

1. **OLM-bundle-only features have a distinct checklist** not captured by any current template.
   Now captured in spec-template, plan-template, tasks-template, and agents.md.

2. **Console navigation tokens are OCP version-dependent** and must be verified against the
   target release. Now captured in repo-assessment-template §10.5 and agents.md.

3. **Two-PR delivery signals spec incompleteness**: all 3 issues fixed in PR #95 (annotations,
   bundle placement, nav token) were knowable at spec time.

4. **CSO network policy hooks are undiscoverable without explicit documentation**. Now captured
   in agents.md Platform Integration Hooks and repo-assessment-template §10.2.
