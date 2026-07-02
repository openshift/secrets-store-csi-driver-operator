---
name: openspec-new-change
description: Start openspec-agile-workflow change from Jira ticket. Use for /opsx-new.
license: MIT
compatibility: Requires openspec CLI.
metadata:
  author: openspec
  version: "1.1"
---

Jira key required at `/opsx-new`. Write `inputs/jira.yaml`. Obtain spec via Jira MCP or user paste into `inputs/jira-spec.md`. Do not create artifacts. Next: `/opsx-continue`.

Syntax: `/opsx-new CM-830` or `/opsx-new CM-830 change-name`

Repo URL optional now; required before repo-assessment stage.
