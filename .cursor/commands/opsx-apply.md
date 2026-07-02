---
name: /opsx:apply
id: opsx-apply
category: Workflow
description: Implement tasks via OAPE command orchestration (task-by-task with code eval gate and approval after each task)
---

Implement an OpenSpec change using OAPE commands, driven by a composed design bundle
(tasks.md + upstream artifacts) scoped to **one task at a time**.

**Per-task flow:** OAPE → verify → **code-generation evals** → **refine code** → **user approves code** → task report → next task.

**Reference:** `oape-ai-e2e/AGENTS.md`, schema `oape_routing`, `code_generation_eval_gate`, `.cursor/commands/oape-*.md`, `{schema_root}/stage-gate/CODE_GENERATION_EVAL_PROMPT.md`

**Input**: Optionally specify a change name (e.g., `/opsx:apply cm-830`). If omitted, infer from context or prompt.

## Steps

1. **Select the change**

   If a name is provided, use it. Otherwise infer, auto-select if only one active change,
   or run `openspec list --json` and ask the user.

   Announce: "Using change: <name>" and how to override.

2. **Check status and get apply instructions**

   ```bash
   openspec status --change "<name>" --json
   openspec instructions apply --change "<name>" --json
   ```

   Handle states:
   - `blocked` → suggest `/opsx:continue`
   - `all_done` → suggest archive
   - otherwise → proceed

3. **Verify prerequisites**

   - OAPE commands in `.cursor/commands/` (api-generate.md, api-implement.md, etc.)
   - `tasks.md`, `constitution.md`, `specs.md`, `plan.md` exist in change dir
   - `gh`, `go`, `git`, `make` available; `gh auth status` OK

4. **Fork setup** (before any OAPE command)

   - Read `openspec/changes/<name>/inputs/jira.yaml` for `fork_repo_url`
   - If missing, ask user once and persist
   - Clone or verify fork; create feature branch per schema `fork_repo.feature_branch`
   - Record `jira_key`; **all OAPE commands run with cwd = fork root**
   - Create `openspec/changes/<name>/implementation/task-reports/` if missing

5. **Read context artifacts**

   Read every path from apply instructions `contextFiles`:
   constitution.md, specs.md, plan.md, tasks.md, repo-assessment.md (if present)

6. **Parse tasks from tasks.md**

   - Order by §2 Linear Execution Order; respect §1 DAG
   - Skip tasks marked `- [x]`

7. **Task loop** (for each pending task in §2 order)

   **Do not ask for user approval until steps 1–4 below are complete.**

   ### 1. Compose design bundle

   Write `openspec/changes/<name>/implementation/design-bundle.md` using
   `openspec/schemas/openspec-agile-workflow/templates/design-bundle-template.md`:
   - Include constitution, specs, plan, repo-assessment excerpts
   - Include §4 payload **ONLY for the current Task ID**
   - Add REVISION FEEDBACK when re-running after task rejection

   ### 2. Run OAPE command (exactly one per task)

   Resolve command:

   1. **IF e2e task** → `/oape:e2e-generate <fork-default-branch>`
   2. **ELIF** `API_Agent` verification-only → `/oape:api-generate-tests <api-path>`
   3. **ELIF** `API_Agent` → `/oape:api-generate --design-doc <bundle>` then `make update && make verify`
   4. **ELIF** `OperatorController_Agent` → `/oape:api-implement --design-doc <bundle>`
   5. **ELIF** manual agent → read `{schema_root}/templates/code-generation-template.md` for FILE OPERATIONS format; implement task payload directly (no OAPE command)

   Read `.cursor/commands/<command_file>` and execute its full workflow in fork cwd.

   ### 3. Verify

   Run Makefile targets from **this task's** Acceptance criteria. Record pass/fail.

   ### 4. Code generation eval gate (mandatory before approval)

   Read `{schema_root}/stage-gate/CODE_GENERATION_EVAL_PROMPT.md`.

   - Load `{schema_root}/evals/code-generation_eval.yaml`; filter by resolved `oape_command`
   - Score fork working copy; write `eval-results/code-generation-<task-id>.yaml`
   - **If cases fail:** fix code in fork → re-verify → re-score (up to 2 refinement passes)
   - **Do not** present user approval until this loop completes

   ### 5. Present task summary

   ```
   ## Task: <TASK_ID> — <title>
   Phase: <phase>

   ### OAPE Commands Executed
   ### Files Touched
   ### Test Results
   ### Code Generation Eval Scorecard (score, cases, refinement rounds, fixes applied)
   ### Deviations (if any)
   ```

   ### 6. User code approval gate

   ASK: **"Code eval score: {N}% ({pass}/{total} cases pass). Approve the code changes for task {task_id} ({task_title}) and proceed to the next task? (Approve / Reject with feedback)"**

   - **Reject** → REVISION FEEDBACK in design-bundle; re-run **this task only** from step 1
   - **Approve** → continue to step 7

   ### 7. On approve — record and advance

   - Write `implementation/task-reports/<task-id>.md` (template: `implementation-task-report.md`)
   - Mark task `- [x]` in tasks.md
   - Advance to next task

8. **Post-loop** (all tasks approved)

   - Write `implementation-report.md` — **aggregate all** `implementation/task-reports/*.md`
   - Write `adrs.md` only if deviations logged
   - Commit, push feature branch, open draft PR on fork
   - Present final summary with draft PR URL

## Output During Implementation

```
Task 3/12: T1_3 — Implement controller reconciliation
→ /oape:api-implement --design-doc .../design-bundle.md
→ make test (PASSED)
→ code evals: 100% (2/2 pass) after 1 refinement round
→ task report: implementation/task-reports/T1_3.md

Code eval score: 100% (2/2 cases pass).
Approve the code changes for task T1_3 (Implement controller reconciliation) and proceed?
(Approve / Reject with feedback)
```

## Guardrails

- **Mandatory order:** OAPE → verify → code evals → refine code → **then** user approval
- **Never** skip code-generation evals when applicable cases exist
- **Never** advance without user **code** approval for the current task
- Invoke exactly one allowed OAPE command per task
- OAPE commands run in fork cwd only
- On reject: re-run current task only (full loop including eval gate)
- Write one task report per approved task
