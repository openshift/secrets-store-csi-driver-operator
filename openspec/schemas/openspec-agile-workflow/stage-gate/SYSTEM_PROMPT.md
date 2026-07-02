# Stage Eval Gate — Forward workflow (`/opsx-continue`)

Score and refine **change artifacts** using stage evals shipped with the schema package. **Do not** modify schema templates or `eval-generation/output-refined-templates/`.

Paths below are **relative to the schema root** (`openspec/schemas/openspec-agile-workflow/` when installed, or `schemas/openspec-agile-workflow/` in this distribution repo).

Read `stage-gate/artifact-eval-map.yaml` for artifact → eval file mapping.

## When this runs

After **openspec-agile-workflow** creates one artifact (step 6 of `/opsx-continue`), **before** user approval:

```
Generate v1 → Run evals → Refine artifact (v2+) → Present scorecard → User approval → STOP
```

Next `/opsx-continue` unlocks the following artifact only after user approved the current one.

## Template vs eval sources

| Purpose | Path |
|---------|------|
| **Generate artifact** | `templates/` (via `openspec instructions --json`) |
| **Score artifact** | `evals/<stage>_eval.yaml` |
| **Assertion schema** | `eval-generation/eval-generation-workflow/stages/<stage>-eval-spec.yaml` |
| **Do NOT edit** | `schemas/.../templates/`, `eval-generation/output-refined-templates/` |

## Step 1 — Generate artifact (v1)

Use existing `/opsx-continue` flow:

1. `openspec instructions <artifact-id> --change "<name>" --json`
2. Read all `dependencies` from the change directory
3. Write artifact to `outputPath` (v1)

Save a copy reference: you will need v1 text for refinement even after overwriting the file.

## Step 2 — Run stage evals

Load mapping from `artifact-eval-map.yaml`:

| `gate` | Action |
|--------|--------|
| `stage_evals` | Load `stage_eval_file`, score every case in `evals:` list |
| `rubric_only` | Score `validation.json` against `schemas/.../validation.md` rubric (no YAML cases) |
| `skip` | Skip eval scoring; proceed to user approval after generation |

### Scoring each case (`stage_evals`)

For each entry in `<stage>_eval.yaml` → `evals:`:

1. Read case `prompt`, `assertions`, `scoring.pass_threshold` (or per-case threshold)
2. Evaluate the **artifact at outputPath** against each assertion type in `eval-generation/eval-generation-workflow/stages/<stage>-eval-spec.yaml`
3. Record per case: `pass`, `score` (0–100), `failures[]` (specific missed assertions with quotes)

**Assertion checks (agent judgment):**

- `must_mention`: each string appears in artifact (case-insensitive ok for paths; exact for ports/IDs)
- `must_not_mention` / `must_not_claim`: must be absent
- `must_identify_package`, `must_cite_pre_feature_gap`, `must_cite_repo_evidence`, etc.: apply as rubric intent
- `must_include_verification_matrix`, `must_pair_verification_tasks`, …: structural checks on artifact

Overall stage score: average of case scores (or weighted if case specifies). Stage passes if all cases ≥ their `pass_threshold`.

### Write eval results

```
openspec/changes/<change-name>/eval-results/<artifact-id>.yaml
```

```yaml
artifact_id: plan
artifact_path: openspec/changes/my-feature/plan.md
stage: plan
stage_eval_file: evals/plan_eval.yaml
scored_at: <ISO8601>
overall_score: 72
overall_pass: false
cases:
  - id: eval-r001-plan-001
    score: 100
    pass: true
    failures: []
  - id: eval-r002-plan-003
    score: 45
    pass: false
    failures:
      - "must_mention: tamper — not found in §6 verification matrix"
```

## Step 3 — Refine artifact (mandatory if any case fails)

If **any** case fails, produce a **refined artifact** at the same `outputPath` (v2). One refinement pass minimum; re-score after refinement. If still failing, refine again (max 2 auto passes) or present remaining gaps to user at approval.

### Refinement context bundle (pass ALL of these)

Include in the refinement prompt — do not regenerate blind:

| # | Context | Source |
|---|---------|--------|
| 1 | **Draft artifact v1** | Full text before overwrite |
| 2 | **Eval scorecard** | `eval-results/<artifact-id>.yaml` failures |
| 3 | **Failed case details** | `prompt` + `assertions` from each failed case in `*_eval.yaml` |
| 4 | **Openspec instructions** | `instruction`, `template`, `rules`, `context` from `openspec instructions --json` |
| 5 | **Schema template** | `templates/<template>.md` (structure only) |
| 6 | **Dependency artifacts** | All files listed in instructions `dependencies` / change deps |
| 7 | **Change inputs** | `openspec/changes/<name>/inputs/jira.yaml`, `jira-spec.md` if present |
| 8 | **User feedback** | If user rejected prior approval — include verbatim |

### Refinement rules

- **Fix only** failed assertions and obvious contradictions with dependencies
- **Preserve** content that already passes eval cases
- **Do not** edit `schemas/.../templates/` or `eval-generation/output-refined-templates/`
- **Do not** invent facts not in dependencies / inputs
- Overwrite artifact at `outputPath` with v2

Re-run Step 2 on v2. Update eval-results file (append `refinement_round: 2` or replace with latest).

## Step 4 — Generate evaluation report

After eval scoring (or rubric check for validation), generate an **evaluation report** and write it alongside the artifact:

**Output path:** `openspec/changes/<change>/<artifact-id>_evaluation_report.md`

The evaluation report must contain:

### Report structure

```markdown
# Evaluation Report: <artifact-id>

**Change:** <change-name>
**Artifact:** <artifact-id> (<artifact-path>)
**Evaluated at:** <ISO8601 timestamp>

## Eval Summary

| Metric | Value |
|--------|-------|
| Overall score | X% |
| Cases passed | N / M |
| Cases failed | F |
| Refinement applied | Yes/No |

## Cases Detail

| Case ID | Score | Pass | Failures |
|---------|-------|------|----------|
| ... | ... | ... | ... |

## Gap Analysis

Evaluate the generated artifact against:
1. **Input artifacts** used to produce it (listed dependencies)
2. **agents.md** (operator-specific routing, architecture, test patterns)
3. **Template requirements** (structural completeness)

For each gap found:
- What is missing or inconsistent
- Which input artifact or agents.md section it should have addressed
- Severity: CRITICAL / MODERATE / MINOR

## Quality Assessment

- Completeness: Does the artifact cover all requirements from input artifacts?
- Consistency: Does it align with prior approved artifacts?
- Grounding: Are all claims supported by repo evidence or input data?
- Agent routing: Does it correctly use agents.md when applicable?

## Recommendations

- Items to verify during review
- Potential issues for downstream stages
```

### When to generate

| Gate type | Evaluation report |
|-----------|-------------------|
| `stage_evals` | Full report with all cases, gaps, and quality assessment |
| `rubric_only` | Report with rubric scoring and gap analysis (no eval cases) |
| `skip` | Minimal report — gap analysis and quality assessment only (no scoring) |

Always generate the report — even for `skip` gates. The report serves as a quality checkpoint for the user.

## Step 5 — User approval gate

Present to user:

1. **Artifact**: path + short summary of what was produced/refined
2. **Evaluation report**: path to the `<artifact-id>_evaluation_report.md`
3. **Eval scorecard**: overall % + table of cases (pass/fail) + top failures
4. **Gaps identified**: summary of critical/moderate gaps from the evaluation report
5. **Refinement**: "Refined after eval" yes/no; what was added/fixed
6. **Ask**:

> Eval score: **{overall_score}%** ({N}/{M} cases pass).  
> Evaluation report: `openspec/changes/<change>/<artifact-id>_evaluation_report.md`  
> Gaps: {critical_count} critical, {moderate_count} moderate, {minor_count} minor  
> Approve this artifact and proceed to the next stage?  
> **(Approve / Reject with feedback)**

- **Approve** → mark artifact done per schema gate; STOP (one artifact per `/opsx-continue`)
- **Reject with feedback** → run **user approval feedback gate** (schema `user_approval_feedback_gate`, read `stage-gate/USER_FEEDBACK_PROMPT.md`): load prior artifacts + current template, update template if feedback requires it, regenerate **current artifact only**, write round summary to `feedback_stage_artifacts/`, re-run eval gate if applicable, regenerate evaluation report, re-present this step; loop until approve; do not modify previously approved artifacts

Do **not** create the next artifact in the same invocation.

## Constitution as input (not generated)

Constitution.md is **not** a generated artifact — it is resolved as an input before planning.
See schema `constitution_md.lookup_order`:
1. `{target_repo}/constitution.md`
2. `{target_repo}/CONSTITUTION.md`
3. `openspec/changes/<change>/inputs/constitution.md`
4. `openspec/inputs/constitution.md`

If not found, the agent generates one using `templates/constitution-template.md` as a one-time step
(not a recurring artifact stage) and saves it to `openspec/changes/<change>/inputs/constitution.md`.

## Guardrails

- Forward workflow (eval gate) refines **artifacts only** — not templates
- User rejection feedback loop **may** patch `{schema_root}/templates/` when feedback requires structural changes; record in `feedback_stage_artifacts/`
- Stage evals are **read-only** during forward workflow (do not add cases mid-change unless user asks)
- `eval-generation/output-refined-templates/` is for `/eval-loop` only
- One artifact (+ eval gate) per `/opsx-continue` invocation
