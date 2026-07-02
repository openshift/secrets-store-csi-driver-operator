# Task Implementation Report

**Change**: [CHANGE_NAME]
**Task ID**: [TASK_ID]
**Task Title**: [TASK_TITLE]
**Phase**: [PHASE_NAME]
**Assigned Agent**: [ASSIGNED_AGENT]
**OAPE Command**: [OAPE_COMMAND or manual]
**Approved**: [ISO8601_DATE]
**User approved by**: [user confirmation]

## Summary

[2–4 sentences: what this task implemented, outcome of OAPE command, eval gate result, and test status.]

## OAPE Commands Executed

| Command | Args | Outcome |
|---------|------|---------|
| /oape:… | … | Success / Failed |

## Code Changes

### Files modified or created

| File | Change |
|------|--------|
| `relative/path` | [brief description] |

### Git diff summary

```
[Key hunks or `git diff --stat` for this task's scope]
```

## Verification

| Check | Result | Notes |
|-------|--------|-------|
| Task acceptance criteria | PASSED / FAILED | |
| make targets | PASSED / FAILED | |

## Code Generation Eval Gate

**Eval results**: `eval-results/code-generation-[TASK_ID].yaml`

| Metric | Value |
|--------|-------|
| Overall score | [N]% |
| Cases pass | [N]/[M] |
| Refinement rounds | [0–2] |

### Cases

| Case ID | Score | Pass | Notes |
|---------|-------|------|-------|
| eval-r…-codegen-… | … | yes/no | |

### Eval-driven code fixes applied

- [List fixes made to pass eval cases — or "None; all cases passed on first score"]

## Deviations

[None — or describe deviation from task payload / constitution and rationale]

## Links

- Design bundle: `implementation/design-bundle.md` (snapshot at approval time)
