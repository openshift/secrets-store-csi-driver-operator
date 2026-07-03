# Epic Bug Analysis — Root Cause Analysis Summary
**Feature bundle:** SSCSI-235 | **Round:** 1 | **Date:** 2026-07-02

---

## RCA Categories

### RCA-1: Missing Spec Requirements for OLM Bundle Conventions
**Issues mapped:** SSCSI-235-GAP-1, SSCSI-235-GAP-2

**Root cause (direct):**
The feature spec did not enumerate OLM bundle resource requirements:
- No mention of which artifacts belong in `config/manifests/stable/` vs `demo/`
- No annotation requirements for resources entering the OLM bundle
- No circular-dependency analysis for install-type QuickStarts

**Root cause (systemic):**
The spec template (Stage 1) has no section or checklist item covering OLM bundle placement or
bundle annotation conventions. The validation template (Stage 0) does not trigger any
OLM-specific completeness check when the spec mentions bundle resources.

**Fix target:** spec-template.md, validation-template.md

---

### RCA-2: Missing Repo-Assessment Coverage for Console API Tokens
**Issue mapped:** SSCSI-235-BUG-1

**Root cause (direct):**
The repo-assessment produced for this feature did not document console navigation tokens
(e.g., `qs-nav-ecosystem`) or their version coupling. When PR #94 was written, the token was
`qs-nav-operator-hub`. It changed before PR #95 merged.

**Root cause (systemic):**
The repo-assessment template §10.5 (Console/UI Integration) is generic. For operators that
ship ConsoleQuickStart resources, the template does not require the agent to:
- List existing navigation tokens and their OCP version bindings
- Flag navigation token stability as a known risk

**Fix target:** repo-assessment-template.md (§10.5 hint addition)

---

### RCA-3: Planning Template Does Not Require Bundle Placement Audit Phase
**Issue mapped:** SSCSI-235-GAP-1 (cascades from RCA-1)

**Root cause (direct):**
The plan had no explicit phase or checklist item for "decide which resources go into OLM bundle
vs demo/" or "perform OLM bundle annotation audit." This decision was deferred to PR review.

**Root cause (systemic):**
The plan template §3.5 (Packaging/OLM) exists but is generic. It does not include a bundle-
placement decision matrix, nor does it require listing every new resource and its target location
(bundle, demo, both). OLM annotation requirements are not listed as a planning constraint.

**Fix target:** plan-template.md (§3.5 and §5 phase guidance)

---

### RCA-4: Anti-Duplication Check Missed Platform Network Policy Hook
**Issue mapped:** network-policy-redesign (supplementary)

**Root cause (direct):**
The initial implementation author was unaware that CSO manages shared egress-to-api-server
network policies and that storage operators opt in by adding a pod label. This is a well-known
platform hook but was not in any repo or workflow documentation.

**Root cause (systemic):**
The repo-assessment template §5 (Reusable Assets) and §10.2 (Proxy & Network Configuration) do
not require the agent to survey platform-level hooks (CSO, CNO, etc.) before proposing new
infrastructure. AGENTS.md does not document the CSO network policy label pattern.

**Fix target:** agents.md (add CSO NP label pattern), repo-assessment-template.md (§10.2 hint)

---

### RCA-5: No Content Review Step for User-Facing Copy
**Issue mapped:** SSCSI-235-BUG-2

**Root cause (direct):**
The task for "add QuickStart manifest" had no acceptance criterion requiring a human review of
user-visible text in the manifest YAML before merging. The typo survived one full PR review cycle.

**Root cause (systemic):**
Task templates for manifest-only features do not include a content/copy acceptance criterion.
The tasks-template.md does not mention user-facing content quality for YAML manifests.

**Fix target:** tasks-template.md (acceptance criteria guidance for manifest tasks)

---

## Severity Matrix

| ID | Issue | Root Cause Category | Stage | Severity | Patchable in Templates |
|----|-------|---------------------|-------|----------|------------------------|
| SSCSI-235-GAP-2 | OLM annotation omission | RCA-1 | specs + plan | High | Yes — spec + plan |
| SSCSI-235-GAP-1 | Bundle placement ambiguity | RCA-1 + RCA-3 | specs + plan | Medium | Yes — spec + plan |
| SSCSI-235-BUG-1 | Console nav token drift | RCA-2 | repo-assessment | Medium | Yes — repo-assessment |
| network-policy | Platform NP pattern missed | RCA-4 | repo-assessment | Medium | Yes — agents.md |
| SSCSI-235-BUG-2 | Typo in content | RCA-5 | implementation | Low | Yes — tasks |

---

## Recommended Eval Focus Areas (per stage)

| Stage | Eval Focus |
|-------|-----------|
| repo-assessment | Does §10.5 document ConsoleQuickStart navigation token conventions? Does §5 mention CSO NP label? |
| constitution | Does it enforce OLM bundle annotation conventions for new resources? |
| plan | Does §3.5 include bundle-placement decision per artifact? |
| specs | Does it require OLM annotation spec for any resource entering the bundle? |
| tasks | Does manifest-only task payload require content review + annotation checklist? |
| code-generation | Does implementation of OLM resources verify annotation presence? |
