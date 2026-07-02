## Mode: Single-pass (default)

Generate the complete tasks.md (§0 through §5) in a single response.

### Completeness rules
- **§5 Orchestration notes is MANDATORY** — never omit. Include Retry Boundaries, Merge Conflict
  Hotspots (bindata, zz_generated, vendor), and Open Questions blocking specific Task IDs.
- **Every Task ID in §3 manifest MUST have a matching §4 payload subsection.** If output length is
  constrained, shorten Implementation notes and Acceptance criteria bullets — do NOT skip tasks.
- **Generation priority when space-constrained:** §0 coverage checklist → §3 manifest (all tasks) →
  §2 linear order → §1 DAG → §4 payloads (all tasks, brief) → §5 orchestration notes.
- Verification tasks: pair substantive implementation tasks with test tasks when constitution requires.
  Use actual Makefile targets from repo_assessment (e.g., `make test`, not `make test-unit` unless evidenced).

### Output sections — use these EXACT `##` headings in your response

## 0. Input coverage checklist
One bullet per spec requirement (FR-xx, SC-xx, AC-xx) and plan phase, each with the Task IDs that
cover it. Every spec goal and every plan phase must appear.

## 1. Task Dependency Graph (Mermaid)
```mermaid
graph TD
    subgraph phase1 [Phase 1: PHASE_NAME]
        T1_1[Task 1.1: TITLE]
        T1_2[Task 1.2: TITLE]
        T1_1 --> T1_2
    end
```

## 2. Linear Execution Order
1. T1_1 — [TITLE]
2. T1_2 — [TITLE]
...

## 3. Task Execution Manifest
| Task ID | Task Title | Assigned Agent | Phase | Depends On | Parallel OK | Complexity | Risk |
|---------|-----------|---------------|-------|-----------|------------|-----------|------|
| T1_1 | [TITLE] | [AGENT_ID] | [PHASE] | none | No | [1-8] | [Low/Med/High] |

## 4. Task Specifications (Payloads)
### Task <ID>: <Title>
- **Objective:** ...
- **Target file(s):** ... (from repo_assessment/plan only)
- **Non-goals / forbidden edits:** ...
- **Implementation notes:** ... (non-code)
- **Acceptance criteria:** ... (trace to validated_specs.md)
- **Downstream handoff:** ...

## 5. Orchestration Notes
- Retry Boundaries
- Merge Conflict Hotspots
- Open Questions Requiring SME Before Execution

### Quality self-check
- [ ] §0 lists every FR-xx, SC-xx, and plan phase with covering Task IDs
- [ ] AgentRoutingMode matches constitution.md (PROVIDED vs PROVISIONAL)
- [ ] §3 manifest row count equals §4 payload subsection count (every ID covered)
- [ ] §2 linear order is a valid topological sort of §1 DAG
- [ ] Assigned Agent values exist in agents.md (when PROVIDED) or match provisional IDs exactly
- [ ] Target file(s) in each payload trace to repo_assessment.md or plan.md (marked PARTIAL if uncertain)
- [ ] §5 present with Retry Boundaries, Merge Conflict Hotspots, and Open Questions
- [ ] No truncated mid-task payloads; document ends cleanly after §5
