---
name: /opsx-continue
id: opsx-continue
category: Workflow
description: Continue agile-workflow change - create next artifact, eval gate, refine, approve (OPSX)
---

Continue working on a change by creating the **next** artifact (one per invocation), then **eval → refine artifact → user approval**.

**Input**: Optional change name after `/opsx-continue` (e.g. `/opsx-continue cm-830`).

## Schema package (resolve first existing path)

| Role | Installed | Distribution |
|------|-----------|--------------|
| Schema root | `openspec/schemas/openspec-agile-workflow/` |
| Stage gate | `{schema_root}/stage-gate/` | same |
| Stage evals | `{schema_root}/eval-generation/<stage>_eval.yaml` | same |
| Templates | `{schema_root}/templates/` | same |

## Steps

1. Select change (`openspec list --json` if name not given).
2. `openspec status --change "<name>" --json`
3. Read `openspec/changes/<name>/inputs/jira.yaml` (required).
4. **Resolve repo target before repo-assessment** (see schema `target_repo` and `working_folder_repo`):
   - **Working-folder mode:** If the user directs using the working folder as the repo,
     set `use_working_folder_as_repo: true` in `inputs/jira.yaml`, record
     `working_folder_path`, analyze cwd — do not ask for GitHub URL or clone separately.
   - **Default mode:** If the next ready artifact is `repo-assessment` (or `constitution`) and
     `target_repo` is absent or empty in `jira.yaml`:
     - Ask the user once: "Provide the URL of the target GitHub repository
       (e.g. https://github.com/org/repo)."
     - Persist `target_repo` to `inputs/jira.yaml`.
     - Verify the repository is accessible before creating repo-assessment.
     - **Do not** create repo-assessment or constitution until `target_repo` is recorded.
   - For earlier artifacts (`validation`, `specs`), `target_repo` is not required.
5. Pick first artifact with `status: "ready"`.
6. `openspec instructions <artifact-id> --change "<name>" --json` → create artifact at `outputPath` (**v1**).
   - Generation uses **`{schema_root}/templates/`** (from openspec instructions).
7. **Stage eval gate** — read **`{schema_root}/stage-gate/SYSTEM_PROMPT.md`** in full:
   - Load mapping: `{schema_root}/stage-gate/artifact-eval-map.yaml`
   - **Run evals** from `{schema_root}/eval-generation/<stage>_eval.yaml` (when `gate: stage_evals`)
   - Write `openspec/changes/<name>/eval-results/<artifact-id>.yaml`
   - If any case fails: **refine the artifact** at `outputPath` (v2) using the **refinement context bundle** (v1 text + eval failures + openspec instructions + dependencies + inputs + failed case prompts)
   - Re-score after refinement
   - **Do NOT** modify `{schema_root}/templates/` or `eval-generation/output-refined-templates/`
8. **STOP** — present eval scorecard + artifact summary. Ask:

   > Eval score: **X%** (N/M cases pass). Approve this artifact? **(Approve / Reject with feedback)**

   - **Approve** → artifact gate satisfied; next `/opsx-continue` may create the next ready artifact
   - **Reject** → if artifact is **`specs`**: **exit workflow** (schema `exit_on_reject.specs`) — do NOT regenerate specs; STOP
   - **Reject** (other artifacts) → run feedback loop (same invocation, repeat until Approve):
     1. Capture user feedback verbatim
     2. Load context: prior approved artifacts (read-only), current artifact, `{schema_root}/templates/<name>.md`, eval results, openspec instructions
     3. Update template if feedback requires structural/guidance changes
     4. Regenerate refined artifact at `outputPath`
     5. Write round summary → `openspec/changes/<name>/feedback_stage_artifacts/<artifact-id>/round-<N>.yaml`
     6. Re-run eval gate when applicable
     7. Re-present scorecard + feedback addressed → ask approval again
     - Read **`{schema_root}/stage-gate/USER_FEEDBACK_PROMPT.md`** for full steps
     - **Do not** use `prompts/<artifact-id>.yaml`

## Artifact order (openspec-agile-workflow)

validation.json → specs.md → repo-assessment.md → constitution.md → plan.md → tasks.md → …

## Eval gate by artifact

| Artifact | Stage eval file (under `{schema_root}/`) |
|----------|------------------------------------------|
| validation | Rubric in `templates/validation-template.md` only |
| specs | Skip (no stage eval) |
| repo-assessment | `evals/repo-assessment_eval.yaml` |
| constitution | `evals/constitution_eval.yaml` |
| plan | `evals/plan_eval.yaml` |
| tasks | `evals/tasks_eval.yaml` |
| implementation | `evals/implementation_eval.yaml` |

## Guardrails

- ONE artifact per invocation (includes eval + refine + approval for that artifact)
- Do not skip eval gate for artifacts with `gate: stage_evals`
- Do not skip user approval
- Do not refine **templates** during eval gate — refine the **change artifact** only
- User rejection feedback loop **may** patch `{schema_root}/templates/` when required; write summaries to `feedback_stage_artifacts/`
- `target_repo` required before repo-assessment — **not** at `/opsx-new`
- Do not create the next artifact until the user approves the current one
