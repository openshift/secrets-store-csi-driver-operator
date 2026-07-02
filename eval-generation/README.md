# Eval Pipeline — Retrospective Workflow Improvement

Continuous improvement loop for **openspec-agile-workflow**: derive evals from completed feature bundles, identify template gaps, refine templates, and accumulate learnings.

**Stage evals for `/opsx-continue`** ship with the schema package — not under `eval-generation/`:

`openspec/schemas/openspec-agile-workflow/evals/*_eval.yaml`

`/eval-loop` writes eval cases to `eval-generation/output-evals/` **and** syncs copies to the schema `evals/` directory.

---

## Directory Structure

```
eval-generation/
├── input/                              # Single input file for each eval-loop round
│   └── feature-bundle.yaml             # All links and content for one feature bundle
├── output-evals/                       # Stage-wise cumulative eval results
│   ├── repo-assessment/                # repo-assessment_eval.yaml
│   ├── constitution/                   # constitution_eval.yaml
│   ├── plan/                           # plan_eval.yaml
│   ├── tasks/                          # tasks_eval.yaml
│   ├── implementation/                 # implementation_eval.yaml
│   └── code-generation/                # code-generation_eval.yaml
├── output-refined-templates/           # Working copy + output (seeded round 1, patched in place)
│   └── tasks-modes/                    # Task mode templates
└── eval-generation-workflow/           # All internal workflow machinery
    ├── pipeline.yaml                   # Phase definitions and paths
    ├── round-state.yaml                # Current round counter
    ├── template-gaps/                  # Per-template gap reports
    │   ├── validation-gaps.md
    │   ├── repo-assessment-gaps.md
    │   ├── constitution-gaps.md
    │   ├── plan-gaps.md
    │   ├── tasks-gaps.md
    │   ├── code-generation-gaps.md
    │   ├── design-bundle-gaps.md
    │   ├── implementation-report-gaps.md
    │   ├── implementation-task-report-gaps.md
    │   ├── adrs-gaps.md
    │   ├── spec-gaps.md
    │   └── agents-gaps.md
    ├── epic-bug-analysis/              # SYSTEM_PROMPT for analysis phase
    ├── generation-phase/               # SYSTEM_PROMPT + template-inventory
    ├── stages/                         # eval-spec.yaml rubrics per stage
    ├── outputs/                        # Intermediate outputs per round
    │   ├── epic-bug-analysis/          # pattern-analysis, rca-summary, taxonomy
    │   └── eval-generation/            # patches
    │       └── patches/
    └── rounds/                         # Round snapshots (round-1/, round-2/, ...)
```

---

## Getting Started

### 1. Fill the input file

Edit `eval-generation/input/feature-bundle.yaml` with data from **one completed feature**:

- Feature name and epic key
- Enhancement Proposal content
- Jira epic export
- Pre-feature repo state
- User stories
- PR links and diffs
- Bugs with root causes

### 2. Run `/eval-loop`

```
/eval-loop
```

### 3. Review outputs

- **`eval-generation/eval-generation-workflow/template-gaps/`** — gap reports per template and agents.md
- **`eval-generation/output-refined-templates/`** — refined templates (review and apply back to sources)
- **`eval-generation/output-evals/<stage>/`** — cumulative eval cases per stage
- **`openspec/schemas/.../evals/*_eval.yaml`** — synced for forward workflow

### 4. Apply refinements (manual)

After reviewing `output-refined-templates/`, apply desired changes back to:
- `openspec/inputs/agents.md`
- `openspec/schemas/openspec-agile-workflow/templates/`

### 5. Repeat

Update `eval-generation/input/feature-bundle.yaml` with the next completed feature and run `/eval-loop` again. Prior evals accumulate — each round builds on the last.

---

## Data Flow

```
eval-generation/input/feature-bundle.yaml ──► Epic Bug Analysis ──► workflow/outputs/epic-bug-analysis/*
                                            │
eval-generation/                            │
  output-refined-templates/ ───────┐       │
  eval-generation-workflow/        │       ▼
    template-gaps/          ───────┤
output-evals/<stage>_eval.yaml ──┴────────► Eval Generation
                                            │
                                            ├──► template-gaps/<template>-gaps.md (per template)
                                            ├──► PATCH output-refined-templates/ in place
                                            ├──► output-evals/<stage>/<stage>_eval.yaml
                                            └──► openspec/schemas/.../evals/<stage>_eval.yaml (sync)
```

**Round 2+:** Eval Generation reads **output-refined-templates/** and **template-gaps/** and **consolidated stage eval files** from prior rounds.

---

## Forward Workflow Integration

After `/eval-loop`, the forward workflow (`/opsx-continue` and `/opsx-apply`) reads:

| Forward workflow reads | Populated by |
|------------------------|--------------|
| `openspec/schemas/.../evals/<stage>_eval.yaml` | `/eval-loop` sync |
| `openspec/schemas/.../evals/code-generation_eval.yaml` | `/eval-loop` code-gen eval authoring |

---

## Rules

- Do NOT modify `openspec/schemas/.../templates/` or `openspec/inputs/agents.md` during eval — use `eval-generation/output-refined-templates/`
- Gap reports go to `eval-generation-workflow/template-gaps/` — one file per template + agents.md
- `output-refined-templates/` is both working copy and output — no separate publish step
- Write all eval cases into ONE `<stage>_eval.yaml` per stage — no scattered per-case files
- code-generation evals are tagged with `oape_command` and run during `/opsx-apply`
