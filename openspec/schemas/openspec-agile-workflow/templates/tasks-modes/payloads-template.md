## Mode: Payloads (`pass_mode: payloads`)

You will receive:
- The `tasks_index.json` entries for a SUBSET of tasks (one phase or batch)
- Relevant excerpts from plan.md, specs.md, repo-assessment.md, constitution.md

Generate ONLY `### Task <ID>: <Title>` subsections for the listed task IDs.
Do not emit §0–§3 or §5. Do not skip any task in the provided list — if space is tight,
shorten Implementation notes and Acceptance criteria rather than omitting a task entirely.

### § 4. Task Specifications — payload format

For EACH Task ID, emit:

### Task <ID>: <Title>
- **Objective:** ...
- **Target file(s):** ... (from repo_assessment/plan only)
- **Non-goals / forbidden edits:** ... (pull from constitution + plan guardrails)
- **Implementation notes:** ... (non-code; constraints, patterns to follow)
- **Acceptance criteria:** ... (must trace to validated_specs.md; include tests to run/areas)
- **Downstream handoff:** expected artifacts for codegen agent (files touched, contracts frozen)

### Quality self-check
- [ ] Every Task ID in the provided list has a matching payload subsection
- [ ] Target file(s) trace to repo_assessment.md or plan.md (marked PARTIAL if uncertain)
- [ ] No truncated mid-task payloads; last payload ends cleanly
