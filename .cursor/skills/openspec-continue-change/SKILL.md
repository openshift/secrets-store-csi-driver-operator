---
name: openspec-continue-change
description: Continue working on an OpenSpec change by creating the next artifact, running eval gate, refining artifact, and requesting user approval. Use when the user wants to progress their change or continue their workflow.
license: MIT
compatibility: Requires openspec CLI.
metadata:
  author: openspec
  version: "1.2"
  generatedBy: "1.3.1"
---

Continue working on a change: create the next artifact, **run baseline evals**, **refine the artifact** if needed, then **ask user approval**.

**Input**: Optionally specify a change name. If omitted, check if it can be inferred from conversation context. If vague or ambiguous you MUST prompt for available changes.

## Steps

1. **If no change name provided, prompt for selection**

   Run `openspec list --json` to get available changes sorted by most recently modified. Then use the **AskUserQuestion tool** to let the user select which change to work on.

   Present the top 3-4 most recently modified changes as options, showing:
   - Change name
   - Schema (from `schema` field if present, otherwise "spec-driven")
   - Status (e.g., "0/5 tasks", "complete", "no tasks")
   - How recently it was modified (from `lastModified` field)

   Mark the most recently modified change as "(Recommended)" since it's likely what the user wants to continue.

   **IMPORTANT**: Do NOT guess or auto-select a change. Always let the user choose.

2. **Check current status**
   ```bash
   openspec status --change "<name>" --json
   ```
   Parse the JSON to understand current state. The response includes:
   - `schemaName`: The workflow schema being used (e.g., "spec-driven")
   - `artifacts`: Array of artifacts with their status ("done", "ready", "blocked")
   - `isComplete`: Boolean indicating if all artifacts are complete

3. **Act based on status**:

   ---

   **If all artifacts are complete (`isComplete: true`)**:
   - Congratulate the user
   - Show final status including the schema used
   - Suggest: "All artifacts created! You can now implement this change or archive it."
   - STOP

   ---

   **If artifacts are ready to create** (status shows artifacts with `status: "ready"`):
   - Pick the FIRST artifact with `status: "ready"` from the status output
   - Get its instructions:
     ```bash
     openspec instructions <artifact-id> --change "<name>" --json
     ```
   - Parse the JSON. The key fields are:
     - `context`: Project background (constraints for you - do NOT include in output)
     - `rules`: Artifact-specific rules (constraints for you - do NOT include in output)
     - `template`: The structure to use for your output file
     - `instruction`: Schema-specific guidance
     - `outputPath`: Where to write the artifact
     - `dependencies`: Completed artifacts to read for context
   - **Create the artifact file (v1)**:
     - Read any completed dependency files for context
     - **openspec-agile-workflow — repo target gate**: Before creating
       `repo-assessment` or `constitution`, read `inputs/jira.yaml`.
       - **Working-folder mode** (schema `working_folder_repo`): if the user directs
         using the working folder as the repo, set `use_working_folder_as_repo: true`,
         record `working_folder_path`, use cwd for assessment — no fork, no draft PR.
       - **Default mode**: If `target_repo` is absent or empty, ask the user once for
         the target GitHub repository URL, persist to `jira.yaml`, and verify access.
         Do not proceed until recorded (see schema `target_repo`).
     - Use `template` as the structure - fill in its sections
     - Apply `context` and `rules` as constraints when writing - but do NOT copy them into the file
     - Write to the output path specified in instructions
     - Templates come from **`{schema_root}/templates/`** via openspec instructions (resolve `openspec/schemas/openspec-agile-workflow/`)

   - **Stage eval gate (openspec-agile-workflow only)**:
     - Resolve schema root: `openspec/schemas/openspec-agile-workflow/`
     - Read **`{schema_root}/stage-gate/SYSTEM_PROMPT.md`** and **`{schema_root}/stage-gate/artifact-eval-map.yaml`**
     - If artifact has `gate: stage_evals`:
       1. Score artifact at `outputPath` against all cases in `{schema_root}/eval-generation/<stage>_eval.yaml`
       2. Write `openspec/changes/<name>/eval-results/<artifact-id>.yaml`
       3. If any case fails: refine **artifact only** (overwrite `outputPath`) using refinement context bundle:
          - v1 artifact full text
          - eval failures + failed case prompts/assertions
          - openspec instructions (`instruction`, `template`, `rules`, `context`)
          - all dependency artifacts
          - `inputs/jira.yaml` and related change inputs
          - user feedback if rejecting approval
       4. Re-score refined artifact
       5. **Never** edit `{schema_root}/templates/` or `eval-generation/output-refined-templates/`
     - If `gate: rubric_only` (validation): score against `validation.md` rubric
     - If `gate: skip` (specs, reports): skip to approval

   - **User approval** — present eval scorecard (if run) + summary. Ask:
     > Approve this artifact? **(Approve / Reject with feedback)**
     - **Reject on `specs`**: **exit workflow** — do NOT regenerate specs.md (see schema `user_approval_feedback_gate.exit_on_reject.specs` and `USER_FEEDBACK_PROMPT.md`). STOP.
     - **Reject on other artifacts**: run feedback gate per **`{schema_root}/stage-gate/USER_FEEDBACK_PROMPT.md`**:
       1. Capture user feedback verbatim
       2. Load prior approved artifacts (read-only), current artifact, current template, eval results, openspec instructions
       3. Update `{schema_root}/templates/<name>.md` if feedback requires structural changes
       4. Regenerate refined artifact at `outputPath`
       5. Write round summary → `openspec/changes/<name>/feedback_stage_artifacts/<artifact-id>/round-<N>.yaml`
       6. Re-run eval gate when applicable
       7. Re-present scorecard + feedback addressed → ask approval again (loop until Approve)
       - **Do not** use `prompts/<artifact-id>.yaml`
     - **Approve**: STOP (one artifact per invocation)

   - Show what was created and what's now unlocked
   - STOP after ONE artifact (including eval gate + approval)

   ---

   **If no artifacts are ready (all blocked)**:
   - This shouldn't happen with a valid schema
   - Show status and suggest checking for issues

4. **After completing the artifact + gate, show progress**
   ```bash
   openspec status --change "<name>"
   ```

**Output**

After each invocation, show:
- Which artifact was created/refined
- Eval score (if baseline evals ran): overall % and pass/fail per case
- Path to `eval-results/<artifact-id>.yaml`
- Schema workflow being used
- Current progress (N/M complete)
- What artifacts are now unlocked (after user approval)
- Prompt: "Approve to continue to the next artifact on the next `/opsx-continue`."

**Artifact Creation Guidelines**

The artifact types and their purpose depend on the schema. Use the `instruction` field from the instructions output to understand what to create.

For **openspec-agile-workflow**, eval gate mapping:

| Artifact | Eval file |
|----------|-----------|
| repo-assessment | `repo-assessment_eval.yaml` |
| constitution | `constitution_eval.yaml` |
| plan | `plan_eval.yaml` |
| tasks | `tasks_eval.yaml` |
| implementation | `implementation_eval.yaml` |

**Guardrails**
- Create ONE artifact per invocation (eval + refine + approval included)
- Always read dependency artifacts before creating a new one
- Never skip artifacts or create out of order
- Never skip eval gate for gated artifacts
- Never refine templates during **eval gate** — refine change artifacts only
- User rejection feedback loop **may** patch `{schema_root}/templates/` when required; write summaries to `feedback_stage_artifacts/`
- If context is unclear, ask the user before creating
- Verify the artifact file exists after writing before marking progress
- Use the schema's artifact sequence, don't assume specific artifact names
- **IMPORTANT**: `context` and `rules` are constraints for YOU, not content for the file
  - Do NOT copy `<context>`, `<rules>`, `<project_context>` blocks into the artifact
  - These guide what you write, but should never appear in the output
