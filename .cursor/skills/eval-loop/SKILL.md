---
name: eval-loop
description: Run full retrospective eval loop for one feature bundle. Use for /eval-loop.
license: MIT
metadata:
  author: openspec
  version: "4.0"
---

Single command for the eval improvement pipeline. One feature bundle per invocation.

## When to use

User runs `/eval-loop` after filling `eval-generation/input/feature-bundle.yaml`.

## Execution

1. Read `eval-generation/eval-generation-workflow/pipeline.yaml`
2. Validate `eval-generation/input/feature-bundle.yaml` — halt on `PASTE_` placeholders
3. Load `eval-generation/output-evals/`, `eval-generation/output-refined-templates/`, `template-gaps/`, and `round-state.yaml`
4. Follow `eval-generation/eval-generation-workflow/epic-bug-analysis/SYSTEM_PROMPT.md` → write outputs
5. Follow `eval-generation/eval-generation-workflow/generation-phase/SYSTEM_PROMPT.md`:
   - Templates: read/write **`eval-generation/output-refined-templates/` only** (not `schemas/` or `openspec/inputs/`)
   - Identify gaps per template → write to `eval-generation/eval-generation-workflow/template-gaps/<template>-gaps.md`
   - Patch `output-refined-templates/` in place
   - Merge evals into **`eval-generation/output-evals/<stage>/<stage>_eval.yaml`** (one file per stage)
   - Author **code-generation** evals → `eval-generation/output-evals/code-generation/code-generation_eval.yaml`
   - Sync flat copies to **`openspec/schemas/openspec-agile-workflow/evals/<stage>_eval.yaml`**
   - Update round state
6. Increment round in `eval-generation/eval-generation-workflow/round-state.yaml`

## Template path

**Eval workflow:** `eval-generation/output-refined-templates/` — read and write (working copy + output).

**Do not** patch `openspec/schemas/openspec-agile-workflow/templates/` or `openspec/inputs/agents.md` during eval. Seed `output-refined-templates/` from sources on round 1 if empty.

## Gap reports

One file per template + agents.md in `eval-generation/eval-generation-workflow/template-gaps/`:

| Template | Gap file |
|----------|----------|
| validation-template.md | `template-gaps/validation-gaps.md` |
| repo-assessment-template.md | `template-gaps/repo-assessment-gaps.md` |
| constitution-template.md | `template-gaps/constitution-gaps.md` |
| plan-template.md | `template-gaps/plan-gaps.md` |
| tasks-template.md | `template-gaps/tasks-gaps.md` |
| code-generation-template.md | `template-gaps/code-generation-gaps.md` |
| agents.md | `template-gaps/agents-gaps.md` |

## Consolidated eval files

| Stage | File |
|-------|------|
| repo-assessment | `eval-generation/output-evals/repo-assessment/repo-assessment_eval.yaml` |
| constitution | `eval-generation/output-evals/constitution/constitution_eval.yaml` |
| plan | `eval-generation/output-evals/plan/plan_eval.yaml` |
| tasks | `eval-generation/output-evals/tasks/tasks_eval.yaml` |
| implementation | `eval-generation/output-evals/implementation/implementation_eval.yaml` |
| code-generation | `eval-generation/output-evals/code-generation/code-generation_eval.yaml` |

## Feedback loop

- Round 2+ reads `output-evals/<stage>/<stage>_eval.yaml`, `template-gaps/`, and `output-refined-templates/`
- Templates accumulate refinements in `eval-generation/output-refined-templates/`
- User reviews `output-refined-templates/` after each loop and applies changes back to sources

## Do not

- Split into multiple commands
- Patch `openspec/schemas/.../templates/` or `openspec/inputs/agents.md` during eval workflow
- Write per-case `eval-r*.yaml` files — use consolidated `*_eval.yaml`
- Skip Eval Generation after Epic Bug Analysis
