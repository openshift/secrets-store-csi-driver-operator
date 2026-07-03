## Mode: Payload Revision (`pass_mode: payload_revision`)

You will receive:
- The `tasks_index.json` entries for the tasks being revised
- User feedback and revision guidance from the impact assessment
- All accumulated feedback history for context
- Relevant excerpts from plan.md, specs.md, repo-assessment.md, constitution.md

Generate ONLY `### Task <ID>: <Title>` subsections for the listed task IDs, incorporating
the user's feedback. Do not emit §0–§3 or §5. Address every point in the feedback directly.

### § 4. Task Specifications — payload format

### Task <ID>: <Title>
- **Objective:** ...
- **Target file(s):** ... (from repo_assessment/plan only)
- **Non-goals / forbidden edits:** ... (pull from constitution + plan guardrails)
- **Implementation notes:** ... (non-code; constraints, patterns to follow)
- **Acceptance criteria:** ... (must trace to validated_specs.md; include tests to run/areas)
- **Downstream handoff:** expected artifacts for codegen agent (files touched, contracts frozen)

### Quality self-check
- [ ] Every Task ID in the provided list has a matching payload subsection
- [ ] Every point in the user's feedback is addressed
- [ ] No truncated mid-task payloads; last payload ends cleanly
