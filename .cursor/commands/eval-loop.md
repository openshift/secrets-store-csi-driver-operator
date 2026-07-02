---
name: /eval-loop
id: eval-loop
category: Eval Pipeline
description: Run full retrospective eval loop for one feature bundle (Epic Bug Analysis → Eval Generation → output-evals)
---

Run the **complete eval improvement loop** for whatever is currently in `eval-generation/input/`.

One command. One feature bundle. When done, update `eval-generation/input/feature-bundle.yaml` with the next bundle and run again.

## What this command does

```
1. Validate eval-generation/input/feature-bundle.yaml       (stop if PASTE_ placeholders remain)
2. Load prior output-evals/ + output-refined-templates/ + template-gaps/  (round 2+)
3. Epic Bug Analysis                              → eval-generation-workflow/outputs/epic-bug-analysis/*
4. Eval Generation
   a. Identify gaps per template                  → eval-generation-workflow/template-gaps/<template>-gaps.md
   b. Apply patchable gaps                        → patch eval-generation/output-refined-templates/ in place
   c. Merge eval cases per stage                  → eval-generation/output-evals/<stage>/<stage>_eval.yaml
   d. Create code-generation evals                → eval-generation/output-evals/code-generation/code-generation_eval.yaml
   e. Sync flat stage evals                       → openspec/schemas/openspec-agile-workflow/evals/<stage>_eval.yaml
   f. Update round state
5. Increment round                                → eval-generation-workflow/round-state.yaml
```

## Before running

Fill `eval-generation/input/feature-bundle.yaml` with data from **one completed feature**:

| Field | What to paste |
|-------|---------------|
| `feature_name` | Feature name |
| `epic_key` | Jira epic key |
| `target_repo` | Target repository URL |
| `enhancement_proposal` | Full EP/ARD content |
| `jira_epic` | Jira epic export |
| `repo_state` | Pre-feature repo state |
| `user_stories` | User stories linked to the epic |
| `repo_prs` | PR links and diffs |
| `bugs` | Bug list with root causes |

## Agent instructions

1. Read `eval-generation/eval-generation-workflow/pipeline.yaml` for phase order and paths.
2. Read **`eval-generation/eval-generation-workflow/epic-bug-analysis/SYSTEM_PROMPT.md`** — execute Epic Bug Analysis fully.
3. Read **`eval-generation/eval-generation-workflow/generation-phase/SYSTEM_PROMPT.md`** — execute Eval Generation fully.
4. Do **not** stop between Epic Bug Analysis and Eval Generation unless the user explicitly asks.

### Template path (eval workflow)

| Read / write | Path |
|--------------|------|
| **Sources (read-only)** | `openspec/schemas/openspec-agile-workflow/templates/`, `openspec/inputs/agents.md` |
| **Working copy + output** | `eval-generation/output-refined-templates/` |
| **Gap reports** | `eval-generation/eval-generation-workflow/template-gaps/` (one file per template + agents) |

Seed `output-refined-templates/` from sources on round 1 if empty. Patch in place each round. User reviews and applies changes back to sources.

### Consolidated eval files

One YAML per stage — all cases in `evals:` list:

| Stage | File |
|-------|------|
| repo-assessment | `eval-generation/output-evals/repo-assessment/repo-assessment_eval.yaml` |
| constitution | `eval-generation/output-evals/constitution/constitution_eval.yaml` |
| plan | `eval-generation/output-evals/plan/plan_eval.yaml` |
| tasks | `eval-generation/output-evals/tasks/tasks_eval.yaml` |
| implementation | `eval-generation/output-evals/implementation/implementation_eval.yaml` |
| code-generation | `eval-generation/output-evals/code-generation/code-generation_eval.yaml` |

Also sync each merged file to **`openspec/schemas/openspec-agile-workflow/evals/<stage>_eval.yaml`** for forward `/opsx-continue`.

Do **not** write scattered `eval-r001-*.yaml` per-case files.

### Feedback loop (critical)

| Asset | Round 1 | Round 2+ |
|-------|---------|----------|
| `eval-generation/output-evals/<stage>/<stage>_eval.yaml` | Empty → populated | **Read + merge** |
| `eval-generation/output-refined-templates/` | Seed from sources → refine | Read **refined** copies → refine again |
| `eval-generation/eval-generation-workflow/template-gaps/` | Empty → populated | **Read + update** |

Epic Bug Analysis on round 2+ must cross-check bugs against prior evals in `output-evals/` files.

## Outputs

| Location | Content |
|----------|---------|
| `eval-generation/eval-generation-workflow/outputs/epic-bug-analysis/` | pattern-analysis, rca-summary, issue-taxonomy |
| `eval-generation/eval-generation-workflow/template-gaps/<template>-gaps.md` | Gap report per template |
| `eval-generation/eval-generation-workflow/template-gaps/agents-gaps.md` | Gap report for agents.md |
| `eval-generation/output-evals/<stage>/<stage>_eval.yaml` | Consolidated eval cases per stage |
| `openspec/schemas/openspec-agile-workflow/evals/<stage>_eval.yaml` | Forward workflow stage evals (synced) |
| `eval-generation/output-refined-templates/` | Refined templates for user to review and apply |
| `eval-generation/eval-generation-workflow/rounds/round-N/` | Round snapshot |
| `eval-generation/eval-generation-workflow/round-state.yaml` | Incremented round |

## After completion

Tell the user:

> Loop complete (round N). Review:
> - `eval-generation/eval-generation-workflow/template-gaps/` — gaps found per template and agents.md
> - `eval-generation/output-refined-templates/` — refined templates (apply desired changes back to your sources)
> - `eval-generation/output-evals/` — generated eval cases
>
> Update `eval-generation/input/feature-bundle.yaml` with the next feature bundle and run `/eval-loop` again.

## Guardrails

- Do not use `/opsx-*` commands in this pipeline
- Do not modify `openspec/schemas/.../templates/` or `openspec/inputs/agents.md` — only `output-refined-templates/`
- Write all eval cases into `<stage>_eval.yaml` — not per-case files
- Do not delete prior eval cases without explicit user approval
- Process bugs one at a time during Epic Bug Analysis
