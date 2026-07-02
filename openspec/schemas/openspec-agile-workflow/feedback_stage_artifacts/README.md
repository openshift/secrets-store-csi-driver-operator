# Feedback stage artifacts — user rejection loop

When the user **rejects with feedback** at an artifact approval gate (`/opsx-continue`),
the agent runs the feedback stage (`user_approval_feedback_gate` in `schema.yaml`,
`stage-gate/USER_FEEDBACK_PROMPT.md`).

## Runtime summaries (per change)

Write one file per feedback round:

```
openspec/changes/<change-name>/feedback_stage_artifacts/<artifact-id>/round-<N>.yaml
```

Co-generated gates (`repo-assessment` + `constitution`) use one shared round file per rejection:

```
openspec/changes/<change-name>/feedback_stage_artifacts/repo-assessment+constitution/round-<N>.yaml
```

## Round file schema

```yaml
round: 1
artifact_ids: [repo-assessment, constitution]
timestamp: <ISO8601>
user_feedback: |
  Verbatim rejection feedback from the user.

context:
  prior_artifacts_read_only:
    - openspec/changes/<change>/specs.md
  current_artifacts:
    - openspec/changes/<change>/repo-assessment.md
    - openspec/changes/<change>/constitution.md
  template: templates/repo-assessment-template.md

template_update:
  required: true
  path: templates/repo-assessment-template.md
  summary: |
    Added In scope vs out of scope section to template skeleton.

artifact_regeneration:
  paths:
    - openspec/changes/<change>/repo-assessment.md
    - openspec/changes/<change>/constitution.md
  summary: |
    Reframed §0 against RFE first-release scope; added scope table.

eval_gate:
  rerun: true
  results_path: openspec/changes/<change>/eval-results/repo-assessment.yaml
  overall_score: 88
  overall_pass: true

feedback_addressed:
  - "User: unclear TP scope → Added In scope / Out of scope table in §0"
  - "User: delta/hardening framing → Reframed against RFE recommendation"
```

## This directory

The `feedback_stage_artifacts/` folder under `{schema_root}` holds this README and format
spec only. **Do not** store change-specific summaries here — they live under each change
(see paths above) so they survive schema reinstall.
