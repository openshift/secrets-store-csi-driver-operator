# Implementation Design Bundle

**Change:** [CHANGE_NAME]
**Jira:** [JIRA_KEY]
**Phase:** [PHASE_NAME]
**Current Task:** [TASK_ID — e.g. T1_1]
**Task Title:** [TASK_TITLE]

This bundle replaces an OpenShift Enhancement Proposal (EP) when driving OAPE
commands from `/opsx:apply`. It is composed from approved OpenSpec artifacts
and scoped to the **current task only** (one Task ID per OAPE invocation).

---

## Input precedence (conflicts)

1. constitution.md (non-negotiable guardrails)
2. specs.md (requirements and acceptance criteria)
3. plan.md (architectural context and verification hooks)
4. repo-assessment.md (target files, Makefile targets, evidence)
5. tasks.md §4 payload for the **current Task ID** (most specific)

---

## Constitution (guardrails)

<!-- Paste or summarize constitution.md sections relevant to this phase -->

---

## Specifications (requirements)

<!-- Paste or summarize specs.md: user stories, FR-*, SC-*, AC-* traced by this task -->

---

## Plan (architectural context)

<!-- Paste or summarize plan.md phase goals, target files, verification hooks for this task -->

---

## Repo assessment (grounding)

<!-- Paste repo-assessment.md excerpts: target paths, Makefile targets, patterns -->

---

## Task payload (current task)

<!-- Paste tasks.md §4 subsection for the current Task ID only -->

---

## API specification (derived — for oape:api-generate)

- **Group:**
- **Version:**
- **Kind:**
- **Scope:** Cluster | Namespaced
- **FeatureGate:** (if applicable)

### Spec fields

- `fieldName` (type): description
  - Validation:
  - Default:
  - Immutable:

### Status fields

- `conditions`: Standard OpenShift conditions
- `observedGeneration`: int64

---

## Reconciliation workflow (derived — for oape:api-implement)

1. Validate spec
2. …

### Dependent resources

- ConfigMap: …
- Deployment: …

### Status conditions

- Available: …
- Progressing: …
- Degraded: …

### Events

- …

### Cleanup / finalizers

- …

---

## Verification (this task)

<!-- From current task Acceptance criteria and plan §6 verification matrix -->

| Hook | Command / test | Task ID |
|------|----------------|---------|
| Unit | make test | [TASK_ID] |
| Integration | … | … |
| E2E | … | … |

---

## Revision feedback (when re-running after task rejection)

<!-- User feedback from prior task gate rejection — omit when none -->
<!-- Re-run the current task only; compose bundle for that Task ID -->
