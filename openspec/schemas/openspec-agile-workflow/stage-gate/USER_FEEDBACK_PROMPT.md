# User Approval Feedback Gate — Forward workflow (`/opsx-continue`)

When the user **rejects with feedback** at artifact approval, run this feedback stage.
**Do not** modify previously approved artifacts.

**No prompt snapshots:** Do **not** read or write `prompts/<artifact-id>.yaml`.

## When this runs

After **stage eval gate** (if applicable) and **user approval prompt**:

```
Present artifact → Ask approval →
  Approve → lock artifact → unlock next → STOP
  Reject with feedback → FEEDBACK LOOP (below) until user approves
```

### Feedback loop (repeat until Approve or max rounds reached)

```
1. Capture user feedback
2. Check round count against max_feedback_rounds (config.yaml flags, default 3)
   — If round N >= max_feedback_rounds: HALT and inform the user that the
     maximum feedback rounds have been reached. Present the latest artifact
     and suggest revising inputs or starting a new change. Do NOT continue
     the loop.
3. Load context (prior artifacts, current artifact, current template, evals, inputs)
4. Update template for this artifact if feedback requires it
5. Regenerate refined artifact(s)
6. Write round summary → feedback_stage_artifacts/
7. Re-run eval gate (when applicable)
8. Present scorecard + feedback addressed → Ask approval again
```

**Exception — `specs.md`:** Rejection **exits the workflow**. Do **not** run this loop.
See [Specs rejection — exit workflow](#specs-rejection--exit-workflow).

---

## Step 1 — Capture feedback

Record the user's rejection feedback verbatim. You will include it in the round summary file (Step 5).

Do **not** edit any artifact file with status `done` in `openspec status --change "<name>" --json`.

---

## Step 2 — Load revision context

Resolve `{schema_root}` (`openspec/schemas/openspec-agile-workflow/` installed, or `schemas/openspec-agile-workflow/` in distribution).

Load mapping from `{schema_root}/stage-gate/artifact-eval-map.yaml` for the current artifact(s).

| # | Context | Source |
|---|---------|--------|
| 1 | **Prior approved artifacts** | Every dependency / `requires` artifact with status `done` — **read-only** |
| 2 | **Current artifact draft** | Full text at `outputPath` (post eval-gate version) |
| 3 | **Current template** | `{schema_root}/templates/<schema_template>.md` from artifact-eval-map |
| 4 | **Openspec instructions** | `openspec instructions <artifact-id> --change "<name>" --json` (`instruction`, `rules`, `context`) |
| 5 | **Eval scorecard** | `openspec/changes/<change>/eval-results/<artifact-id>.yaml` if stage evals ran |
| 6 | **Prior feedback rounds** | `openspec/changes/<change>/feedback_stage_artifacts/<artifact-id>/round-*.yaml` |
| 7 | **Change inputs** | `inputs/jira.yaml`, `jira-spec.md` if present |

For **joint gates** (`repo-assessment` + `constitution`): load both current artifacts and both templates; revise both in one round.

---

## Step 3 — Update template (if required)

Based on user feedback, decide whether `{schema_root}/templates/<template>.md` needs a structural or guidance update (e.g. missing section skeleton, unclear mandatory headings).

| Update template? | When |
|------------------|------|
| **Yes** | Feedback asks for sections/structure the template does not currently require |
| **No** | Feedback is content-only and the template already supports the requested shape |

When updating:

1. Patch `{schema_root}/templates/<template>.md` in place
2. Keep changes minimal — only what feedback requires
3. Record `template_update.summary` for the round file (Step 5)

**Do not** edit `eval-generation/output-refined-templates/` (eval-loop only).

---

## Step 4 — Regenerate refined artifact(s)

Using:

- Prior approved artifacts (read-only context)
- Current artifact draft
- Updated template (Step 3)
- User feedback (verbatim)
- Openspec instructions

Regenerate **only** the current artifact at `outputPath`. For joint gates, regenerate **both** co-generated files — never upstream approved artifacts.

Address every feedback point. Preserve content that already passes eval cases and does not conflict with feedback.

---

## Step 5 — Write feedback stage summary

Append one round file:

```
openspec/changes/<change>/feedback_stage_artifacts/<artifact-id>/round-<N>.yaml
```

Joint gate:

```
openspec/changes/<change>/feedback_stage_artifacts/repo-assessment+constitution/round-<N>.yaml
```

Use schema in `{schema_root}/feedback_stage_artifacts/README.md`. Include:

- `user_feedback` (verbatim)
- `template_update` (required, path, summary)
- `artifact_regeneration` (paths, summary)
- `feedback_addressed` (bullet list mapping feedback → change)

---

## Step 6 — Re-run eval gate (when applicable)

If `artifact-eval-map.yaml` maps this artifact to `gate: stage_evals` or `gate: rubric_only`:

- Re-score the refined artifact(s)
- Update `openspec/changes/<change>/eval-results/<artifact-id>.yaml`
- Record scores in the round summary

---

## Step 7 — Re-present approval (loop)

Present:

1. **Refined artifact(s)** — path + summary of changes
2. **Template update** — yes/no; what changed in `templates/<name>.md`
3. **Eval scorecard** — if evals ran (updated scores)
4. **Feedback addressed** — bullets mapping feedback → template + artifact changes
5. **Round summary path** — `feedback_stage_artifacts/.../round-<N>.yaml`
6. **Immutable inputs** — confirm no upstream approved artifacts were modified

Ask:

> Approve this artifact and proceed to the next stage?  
> **(Approve / Reject with feedback)**

- **Approve** → mark artifact done; lock as immutable; STOP
- **Reject with feedback** → return to **Step 1** with new feedback (increment round)

---

## Guardrails

- **Never** overwrite files for artifacts already marked `done` (except artifact(s) currently under approval)
- If feedback **requires** changing an approved upstream artifact, **stop** and tell the user which stage must be reopened
- **Do not** use or create `prompts/<artifact-id>.yaml`
- **Do not** edit `eval-generation/output-refined-templates/`
- **Do not** create the next workflow artifact in the same invocation

---

## Co-generated artifacts (repo-assessment + constitution)

Single joint approval covers both. On reject:

- One feedback loop revises **both** artifacts and **both** templates if needed
- One shared round summary under `feedback_stage_artifacts/repo-assessment+constitution/`
- Run eval gate separately for each artifact file
- Treat `specs.md` and all earlier artifacts as immutable

---

## Implementation task approval variant

Implementation runs OAPE **task-by-task**. User approval is required **after every
task** before advancing to the next. On reject, append feedback to
`implementation/design-bundle.md` **REVISION FEEDBACK** and re-run OAPE commands for
the **current task only** — do not use this artifact feedback loop for per-task code.

---

## Specs rejection — exit workflow

When the user **rejects** `specs.md` at the approval gate:

1. **Do NOT** run the feedback loop above
2. Optionally record reason in `feedback_stage_artifacts/specs/round-1.yaml`
3. Present schema `exit_on_reject.specs.exit_message`
4. **STOP** — do not create repo-assessment or downstream artifacts

The user must revise inputs and start fresh (`/opsx-new`) or delete specs and re-run `/opsx-continue`.
