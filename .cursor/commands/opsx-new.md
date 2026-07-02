---
name: /opsx-new
id: opsx-new
category: Workflow
description: Start a new agile-workflow change from a Jira ticket (OPSX)
---

Start a new change for the **openspec-agile-workflow** pipeline.

## Inputs — what is required when

| Input | Required at `/opsx-new`? | Required later? | When |
|-------|--------------------------|-----------------|------|
| **Jira ticket key** | **YES** | — | Always the first input |
| **Change name** (kebab-case) | No | — | Optional; defaults to lowercase ticket slug (`PROJ-123` → `proj-123`) |
| **Target GitHub repo URL** | **NO** | **YES** | Before **repo-assessment** (`/opsx-continue` ~3rd artifact) |
| **AGENTS.md** | No | No | Optional |

**At `/opsx-new` you only need the Jira key.** Do not ask for the repo URL unless the user includes it inline.

## Command syntax

```
/opsx-new CM-830
/opsx-new CM-830 my-change-name
/opsx-new CM-830 my-change-name https://github.com/org/repo
```

Jira key pattern: `[A-Z][A-Z0-9]+-\d+`.

If no Jira key, ask once. Do **not** proceed without it.

## Steps

1. Parse Jira key (required), optional change name, optional repo URL.
2. `openspec new change "<name>"` — uses `openspec-agile-workflow` from `openspec/config.yaml`.
3. Write `openspec/changes/<name>/inputs/jira.yaml` with `jira_key`, `target_repo`, `created_at`.
4. Fetch ticket → `inputs/jira-spec.md`:
   - Use Jira MCP if configured, **or**
   - Ask the user to paste ticket content into `inputs/jira-spec.md`.
5. `openspec status --change "<name>"` and `openspec instructions validation --change "<name>"`.
6. **STOP** — do not create artifacts yet.

Prompt: `/opsx-continue` to create `validation.json`.

## Guardrails

- Jira key required; repo URL optional at this step
- No planning artifacts in this command
