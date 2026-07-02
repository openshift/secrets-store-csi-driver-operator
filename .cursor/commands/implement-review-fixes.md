---
name: /oape:implement-review-fixes
id: oape-implement-review-fixes
category: OAPE
description: Automatically applies code fixes from an oape:review report, prioritized by severity
argument-hint: <review_report_json>
---

## Name
oape:implement-review-fixes

## Synopsis
```
/oape:implement-review-fixes <review_report_json>
```

## Description

The `oape:implement-review-fixes` command takes the JSON report produced by `/oape:review` and automatically applies every suggested fix to the codebase. Issues are processed in severity order (CRITICAL first) so the highest-impact problems are resolved even if a later fix fails.

This command is invoked automatically at the end of `/oape:review` when the report contains issues. It can also be run standalone by passing a review report directly.

## Arguments

- `$1` (review_report_json): The full JSON review report produced by `/oape:review`. **Required.**

## Implementation

### Step 1: Parse and Validate the Review Report

Extract the `issues` array from the review report JSON.

```thinking
I must parse the review report and extract:
1. summary.verdict — if "Approved" AND issues array is empty, skip all fixes
2. issues[] — the array of issues to fix

Each issue has:
- severity: "CRITICAL" | "WARNING" | "INFO"
- module: which review module flagged this (Logic, Bash, OLM, Build, Adaptive)
- file: path to the file that needs fixing
- line: line number where the issue occurs
- description: what the problem is
- fix_prompt: instructions on how to fix it

If the issues array is empty or missing, I will output "No issues to fix" and stop.
```

**Exit early if no issues:**
- If `summary.verdict` is `"Approved"` AND the `issues` array is empty, output:
  ```
  Review verdict: Approved — no fixes to apply.
  ```
  and STOP. Do not proceed further.

### Step 2: Prioritize Issues by Severity

Sort the issues array so they are processed in this order:
1. **CRITICAL** — Must-fix issues (logic errors, build drift, safety violations)
2. **WARNING** — Should-fix issues (style, complexity, missing edge cases)
3. **INFO** — Nice-to-fix issues (naming, minor improvements)

Within the same severity level, preserve the original order from the report.

```thinking
I will group and order the issues:
- CRITICAL issues first (these block approval)
- WARNING issues second (these degrade quality)
- INFO issues last (these are improvements)

I will track each issue with an index so I can map fixes back to the original report in the summary.
```

### Step 3: Apply Fixes

For **each** issue in the prioritized list, perform the following sub-steps:

#### 3.1: Read the Target File

```bash
# Read the file referenced by the issue
cat "${issue.file}"
```

Read enough context around `issue.line` to understand the surrounding code — at minimum 20 lines before and 20 lines after the target line. If the function containing the target line is larger, read the entire function.

#### 3.2: Understand the Fix

```thinking
For this issue, I must:
1. Read the `description` to understand WHAT is wrong
2. Read the `fix_prompt` to understand HOW to fix it
3. Read the surrounding code context to understand WHERE the fix goes
4. Consider interactions with other code in the same file
5. Ensure the fix follows the effective-go skill conventions:
   - Proper error handling with fmt.Errorf("...: %w", err)
   - Lowercase error messages without trailing punctuation
   - Short, consistent receiver names
   - Proper import organization
   - No TODOs — generate complete implementation
```

#### 3.3: Apply the Code Change

Make the edit to the file. Follow these rules:

- **Minimal change**: Only modify what the fix requires. Do not refactor unrelated code.
- **Preserve style**: Match the existing code style (indentation, naming, import grouping).
- **Complete implementation**: Do not leave TODOs or placeholders. If the `fix_prompt` describes logic, implement it fully.
- **Error handling**: If adding error handling, use `fmt.Errorf("context: %w", err)` with lowercase messages.
- **Imports**: If the fix requires new imports, add them in the correct import group (stdlib, external, internal).

#### 3.4: Track the Result

For each issue, record whether the fix was:
- **Applied** — the code change was made successfully
- **Skipped** — the fix could not be applied (e.g., the code at the referenced line has already changed, or the fix is ambiguous)

If a fix cannot be applied, log the reason and continue to the next issue. Do NOT stop on individual fix failures.

### Step 4: Verify All Fixes

After all fixes have been applied, run verification:

```bash
# Step 4.1: Check that the code compiles
go build ./...
```

```bash
# Step 4.2: Run static analysis
go vet ./...
```

If compilation or vetting fails:
1. Read the error output to identify which fix introduced the failure
2. Attempt to correct the failing fix
3. Re-run `go build ./...` to confirm the correction
4. If the correction fails after one retry, revert that specific fix and note it in the summary

**Build Consistency Check:**

```bash
# Step 4.3: Check if any types.go files were modified
git diff --name-only
```

```thinking
If any file matching *types*.go or *_types.go was modified by the fixes, I must warn
the user to run:
  make generate && make manifests
This ensures deep copy functions and CRD manifests are regenerated.
```

### Step 5: Generate Fix Summary

Output a structured summary of all fixes applied:

```text
=== Review Fix Summary ===

Total issues in report: <N>
Fixes applied: <N>
Fixes skipped: <N>

CRITICAL Fixes:
  [1] APPLIED — <file>:<line> — <short description of what was changed>
  [2] APPLIED — <file>:<line> — <short description of what was changed>

WARNING Fixes:
  [3] APPLIED — <file>:<line> — <short description of what was changed>
  [4] SKIPPED — <file>:<line> — <reason it was skipped>

INFO Fixes:
  [5] APPLIED — <file>:<line> — <short description of what was changed>

Verification:
  go build ./...  : PASS | FAIL
  go vet ./...    : PASS | FAIL

Post-Fix Actions Required:
  - Run 'make generate && make manifests' (if types were modified)
  - Run 'make test' to validate behavior
```

## Return Value

Returns the fix summary text shown above.

## Behavioral Rules

1. **Never skip silently**: If a fix cannot be applied, always log the reason.
2. **No collateral damage**: Only change code that the issue references. Do not refactor or "improve" unrelated code.
3. **Idempotent**: Running this command twice with the same report should not break previously applied fixes.
4. **Respect existing style**: Match the coding patterns already present in the file being edited.
5. **Follow effective-go**: All generated/modified Go code must follow the `effective-go` skill conventions.
6. **One fix at a time**: Apply each fix individually so failures are isolated.
7. **Build must pass**: If a fix breaks the build, revert it rather than leaving the codebase in a broken state.

## Critical Failure Conditions

The command MUST FAIL and STOP immediately if:

1. **No report provided**: The review report JSON is missing or empty
2. **Invalid report format**: The JSON does not contain the expected `summary` and `issues` fields
3. **Not in a git repository**: The current directory is not inside a git working tree

## Exit Conditions

- **Success**: All applicable fixes applied, build passes, summary printed
- **Partial Success**: Some fixes applied, some skipped — summary details which and why
- **Failure**: Report is invalid or not in a git repository
