---
name: /oape:predict-regressions
id: oape-predict-regressions
category: OAPE
description: Predict potential regressions and breaking changes in newly developed APIs by analyzing git diff and API schema changes
argument-hint: "<base-branch> [--output <path>]"
---

## Name
oape:predict-regressions

## Synopsis
```shell
/oape:predict-regressions <base-branch> [--output <path>]
```

## Description

Analyzes the current OpenShift operator repository and predicts potential regressions, breaking changes, and backward compatibility issues by comparing changes between `<base-branch>` and HEAD.

The command generates a comprehensive regression risk report that includes:

1. **Static Analysis** — Rule-based detection of common breaking changes
2. **LLM-Powered Prediction** — Deep semantic analysis using Claude to identify subtle regressions
3. **Impact Assessment** — Severity ratings (Critical/High/Medium/Low) for each finding
4. **Mitigation Recommendations** — Actionable steps to address each issue
5. **Test Scenarios** — Suggested e2e tests to validate fixes

**Output**: `<output-dir>/regression-report.md` (default: `output/regression-report.md`)

**You MUST follow ALL steps strictly. If any precheck fails, you MUST stop immediately and report the failure.**

## Implementation

### Phase 0: Prechecks

All prechecks must pass before proceeding. If ANY precheck fails, STOP immediately and report the failure.

#### Precheck 1 — Validate Arguments

```bash
BASE_BRANCH="$1"

if [ -z "$BASE_BRANCH" ]; then
  echo "PRECHECK FAILED: No base branch provided."
  echo "Usage: /oape:predict-regressions <base-branch> [--output <path>]"
  exit 1
fi

# Parse optional --output
OUTPUT_DIR="output"
if echo "$@" | grep -q '\-\-output'; then
  OUTPUT_DIR=$(echo "$@" | sed 's/.*--output[ =]*//' | awk '{print $1}')
fi

echo "Base branch: $BASE_BRANCH"
echo "Output directory: $OUTPUT_DIR"
```

#### Precheck 2 — Verify Required Tools

```bash
MISSING_TOOLS=""

if ! command -v git &> /dev/null; then
  MISSING_TOOLS="$MISSING_TOOLS git"
fi

if ! command -v go &> /dev/null; then
  MISSING_TOOLS="$MISSING_TOOLS go"
fi

if [ -n "$MISSING_TOOLS" ]; then
  echo "PRECHECK FAILED: Missing required tools:$MISSING_TOOLS"
  exit 1
fi

echo "Required tools available."
```

#### Precheck 3 — Verify Current Repository is a Valid OpenShift Operator Repo

```bash
if ! git rev-parse --is-inside-work-tree &> /dev/null 2>&1; then
  echo "PRECHECK FAILED: Not inside a git repository."
  exit 1
fi

REPO_ROOT=$(git rev-parse --show-toplevel)
echo "Repository root: $REPO_ROOT"

if ! test -f "$REPO_ROOT/go.mod"; then
  echo "PRECHECK FAILED: No go.mod found at repository root."
  echo "This command must be run from within a Go-based OpenShift operator repository."
  exit 1
fi

GO_MODULE=$(head -1 "$REPO_ROOT/go.mod" | awk '{print $2}')
REPO_NAME=$(basename "$GO_MODULE")
echo "Go module: $GO_MODULE"
echo "Repo name: $REPO_NAME"

# Detect framework
HAS_CR=false
HAS_LIBGO=false
grep -q "sigs.k8s.io/controller-runtime" "$REPO_ROOT/go.mod" && HAS_CR=true
grep -q "github.com/openshift/library-go" "$REPO_ROOT/go.mod" && HAS_LIBGO=true

if [ "$HAS_CR" = true ]; then
  FRAMEWORK="controller-runtime"
elif [ "$HAS_LIBGO" = true ]; then
  FRAMEWORK="library-go"
else
  echo "PRECHECK FAILED: Cannot determine operator framework."
  echo "go.mod does not reference sigs.k8s.io/controller-runtime or github.com/openshift/library-go."
  exit 1
fi

echo "Detected framework: $FRAMEWORK"
```

#### Precheck 4 — Validate Git Diff is Non-Empty

```bash
if ! git rev-parse --verify "$BASE_BRANCH" &> /dev/null 2>&1; then
  echo "PRECHECK FAILED: Base branch '$BASE_BRANCH' does not exist."
  echo "Available branches:"
  git branch -a | head -20
  exit 1
fi

DIFF_STAT=$(git diff "$BASE_BRANCH"...HEAD --stat 2>/dev/null)

if [ -z "$DIFF_STAT" ]; then
  echo "PRECHECK FAILED: No changes detected between '$BASE_BRANCH' and HEAD."
  exit 1
fi

echo "Changes detected:"
echo "$DIFF_STAT"
```

**If ALL prechecks above passed, proceed to Phase 1.**
**If ANY precheck FAILED (exit 1), STOP. Do NOT proceed further.**

---

### Phase 1: Extract Git Diff Information

Gather all changed files and their diffs, categorized by type.

#### Step 1.1: Get Diff Statistics

```bash
git diff "$BASE_BRANCH"...HEAD --stat
git log "$BASE_BRANCH"...HEAD --oneline | head -10
```

#### Step 1.2: Extract API Type Changes

```bash
# Find changed API type files
git diff "$BASE_BRANCH"...HEAD --name-only | grep -E '(_types\.go|types_.*\.go)$' | grep -v vendor | grep -v zz_generated
```

For each changed types file, get the full diff:
```bash
git diff "$BASE_BRANCH"...HEAD -p -- <types-file>
```

#### Step 1.3: Extract CRD Schema Changes

```bash
# Find changed CRD files
git diff "$BASE_BRANCH"...HEAD --name-only | grep -E '\.yaml$' | grep -E '(crd|crds)/' | grep -v vendor
```

For each changed CRD file, get the full diff:
```bash
git diff "$BASE_BRANCH"...HEAD -p -- <crd-file>
```

#### Step 1.4: Extract Controller/Reconciler Changes

```bash
# Find changed controller files
git diff "$BASE_BRANCH"...HEAD --name-only | grep -E '(controller|reconcil).*\.go$' | grep -v vendor | grep -v _test.go
```

For each changed controller file, get the full diff:
```bash
git diff "$BASE_BRANCH"...HEAD -p -- <controller-file>
```

#### Step 1.5: Extract RBAC and Config Changes

```bash
# RBAC changes
git diff "$BASE_BRANCH"...HEAD --name-only | grep -E 'rbac.*\.yaml$' | grep -v vendor

# Webhook configuration changes
git diff "$BASE_BRANCH"...HEAD --name-only | grep -E 'webhook.*\.yaml$' | grep -v vendor

# Conversion webhook changes
git diff "$BASE_BRANCH"...HEAD --name-only | grep -E 'conversion.*\.go$' | grep -v vendor
```

```thinking
I need to organize the diff extraction to capture all relevant changes that could cause regressions:
- API type changes (field additions/removals/modifications)
- CRD schema changes (validation, defaults, versions)
- Controller logic changes
- RBAC changes
- Webhook changes (validation, conversion)

These will be analyzed both statically and via LLM.
```

---

### Phase 2: Static Analysis for Common Breaking Changes

Apply rule-based detection for well-known breaking change patterns. This phase runs before LLM analysis to catch obvious issues quickly.

Load and execute the **regression-analysis skill** to perform static checks.

**Note**: If the changes are purely additive (new fields, new features) with no modifications to existing APIs, static analysis may return 0 findings. This is expected and LLM analysis will handle the deeper semantic review.

The static analysis should detect:

1. **Field Removals** — Any struct field removed from API types
2. **Required Field Additions** — New `+kubebuilder:validation:Required` markers in CRDs
3. **Type Changes** — Field type changes (e.g., `string` → `int`)
4. **Enum Value Removals** — Removed values from `+kubebuilder:validation:Enum`
5. **Default Value Changes** — Modified `+kubebuilder:default:` values
6. **API Version Additions Without Conversion** — New version added without conversion webhook
7. **Validation Rule Tightening** — More restrictive validation (min/max, pattern changes)
8. **Breaking Condition Changes** — Condition type removals or semantic changes

For each detected issue, record:
- **Finding ID** (e.g., `STATIC-001`)
- **Severity** (Critical/High/Medium/Low)
- **Category** (Breaking Change, Backward Incompatible, Upgrade Path, etc.)
- **Location** (file:line)
- **Description**
- **Impact**
- **Suggested mitigation**

---

### Phase 3: LLM-Powered Regression Prediction

Use Claude to analyze the diffs for subtle regressions that static analysis might miss.

#### Step 3.1: Prepare Analysis Context

Build a comprehensive context document with:
- Repository information (name, framework, API groups)
- Commit summary (number of commits, files changed)
- Extracted diffs (types, CRDs, controllers)
- Static analysis findings

#### Step 3.2: Construct LLM Analysis Prompt

Create a detailed prompt for Claude following this template:

```markdown
You are an OpenShift operator regression analysis expert. Analyze the following API and controller changes to predict potential regressions, breaking changes, and backward compatibility issues.

## Repository Context

- **Operator**: {repo_name}
- **Framework**: {controller-runtime|library-go}
- **Go Module**: {go_module}
- **Base Branch**: {base_branch}
- **Commits Analyzed**: {commit_count} commits
- **Files Changed**: {files_changed}

## Static Analysis Findings

{static_findings_summary}

## API Type Changes

```go
{api_types_diff}
```

## CRD Schema Changes

```yaml
{crd_diff}
```

## Controller/Reconciler Changes

```go
{controller_diff}
```

## RBAC Changes

```yaml
{rbac_diff}
```

## Webhook Changes

```go
{webhook_diff}
```

## Analysis Required

Perform a deep analysis to identify:

### 1. Breaking Changes (CRITICAL severity)
- Changes that will cause existing CRs to fail validation
- Changes that will break existing operator deployments
- Changes that will fail upgrades from previous versions

### 2. Backward Compatibility Issues (HIGH severity)
- New required fields without defaults
- Removed API fields still in use
- Changed field semantics
- Controller behavior changes affecting existing deployments

### 3. Upgrade Path Problems (HIGH severity)
- Missing conversion webhooks for new API versions
- Status field incompatibilities
- Condition type changes
- State migration issues

### 4. Subtle Behavioral Regressions (MEDIUM severity)
- Changed reconciliation logic
- Modified condition update patterns
- Resource ownership changes
- Different error handling

### 5. Performance/Scalability Concerns (MEDIUM/LOW severity)
- Unbounded list fields
- Missing pagination
- Inefficient reconciliation patterns
- Resource-intensive operations

## Output Format

For each finding, provide:

```yaml
finding_id: LLM-XXX
severity: CRITICAL|HIGH|MEDIUM|LOW
category: Breaking Change|Backward Incompatible|Upgrade Path|Behavior Change|Performance
title: Brief title
location: file:line (if applicable)
impact: |
  Detailed description of the impact on:
  - Existing CRs
  - Running operators
  - Upgrade scenarios
  - End users
evidence: |
  Code snippets or diff sections that support this finding
risk_scenario: |
  Specific scenario(s) where this issue will manifest
mitigation: |
  - Actionable step 1
  - Actionable step 2
  - Actionable step 3
test_scenarios: |
  - Test scenario 1
  - Test scenario 2
priority: 1-5 (1=must fix before merge, 5=consider for future)
```

## Special Focus Areas

Pay extra attention to:

1. **API versioning**: Are multiple versions served? Is there a conversion strategy?
2. **Defaulting vs Required**: Are new fields properly defaulted or will they break existing CRs?
3. **Condition semantics**: Do condition types or meanings change?
4. **Status subresource**: Are status updates backward compatible?
5. **Webhook logic**: Will validation reject previously valid CRs?
6. **RBAC**: Are new permissions needed that aren't granted?
7. **Managed resources**: Do changes affect deployed workloads?

## Analysis Style

- Be specific: Reference exact files, lines, and field names
- Be practical: Focus on real-world impact
- Be thorough: Consider edge cases and upgrade scenarios
- Be constructive: Always suggest mitigations
- Prioritize: Order findings by severity and impact
```

#### Step 3.3: Execute LLM Analysis

Send the prompt to Claude and parse the structured YAML output into findings.

---

### Phase 4: Discover Existing API Versions and CRs

To provide accurate recommendations, discover what versions and CRs currently exist.

```bash
# Find all API versions in the repo
find "$REPO_ROOT" -path '*/api/*' -name '*_types.go' -not -path '*/vendor/*' | \
  xargs grep -h "^// +groupName=" | sort -u

# Find CRD served versions
find "$REPO_ROOT" -name '*.yaml' -path '*/crd*' -not -path '*/vendor/*' | \
  xargs grep -A2 "kind: CustomResourceDefinition" | grep "versions:" -A5

# Check for conversion webhooks
find "$REPO_ROOT" -name '*.go' -not -path '*/vendor/*' | \
  xargs grep -l "ConvertTo\|ConvertFrom" | head -5
```

---

### Phase 5: Generate Regression Risk Report

Combine static analysis findings and LLM findings into a comprehensive markdown report.

#### Report Structure

```markdown
# Regression Risk Report: {repo_name}

**Generated**: {timestamp}
**Base Branch**: {base_branch}
**HEAD**: {head_commit_hash}
**Commits Analyzed**: {commit_count}
**Files Changed**: {files_changed} (+{insertions} -{deletions})

---

## Executive Summary

🔴 **{critical_count} Critical Issues Found**
🟠 **{high_count} High Risk Issues Found**
🟡 **{medium_count} Medium Risk Issues Found**
⚪ **{low_count} Low Risk Issues Found**

**Overall Risk Assessment**: {Critical|High|Medium|Low}

{brief_summary_paragraph}

---

## Quick Reference

| Finding ID | Severity | Category | Title |
|------------|----------|----------|-------|
| {id} | {severity} | {category} | {title} |
...

---

## Critical Findings

{For each CRITICAL finding, include detailed section with:
- Finding ID
- Title
- Severity badge
- Category
- Location
- Impact description
- Evidence (code snippets)
- Risk scenario
- Mitigation steps (numbered)
- Test scenarios (code blocks)
}

---

## High Risk Findings

{Same structure as Critical}

---

## Medium Risk Findings

{Same structure}

---

## Low Risk Findings

{Same structure, can be more concise}

---

## API Version Summary

{If multiple API versions exist:
- List all versions
- Conversion webhook status
- Served vs stored versions
- Deprecation status
}

---

## Recommended Actions

### Before Merge (BLOCKERS)
- [ ] {Critical issue 1}
- [ ] {Critical issue 2}
...

### Before Release
- [ ] {High priority issue 1}
- [ ] {High priority issue 2}
...

### Documentation Updates Needed
- [ ] Update upgrade guide with breaking changes
- [ ] Add migration steps for removed fields
- [ ] Document new required fields
...

### E2E Tests to Add
```go
// Test existing CR compatibility
It("should successfully reconcile CRs created with previous version", func() {
  // ...
})

// Test upgrade path
It("should handle upgrade from v1alpha1 to v1alpha2", func() {
  // ...
})
```

---

## Change Impact Matrix

| Change Type | Files Affected | Breaking | Upgrade Impact | Test Coverage |
|-------------|----------------|----------|----------------|---------------|
| API Types | {count} | {Yes/No} | {High/Medium/Low} | {Suggested tests} |
| CRD Schema | {count} | {Yes/No} | {High/Medium/Low} | {Suggested tests} |
| Controller | {count} | {No} | {Medium/Low} | {Suggested tests} |
| RBAC | {count} | {No} | {Low} | {Suggested tests} |

---

## Appendix: Full Diff Summary

```text
{git diff --stat output}
```

## Appendix: Analysis Methodology

This report was generated using:
1. **Static Analysis**: Rule-based detection of common breaking change patterns
2. **LLM Analysis**: Claude Sonnet 4.5 deep semantic analysis
3. **Repository Discovery**: Automated scanning of API types, CRDs, controllers
4. **Git Diff Analysis**: Detailed comparison of {base_branch}...HEAD

---

*Generated by OAPE Regression Predictor*
```

#### Write the Report

```bash
mkdir -p "$OUTPUT_DIR"
REPORT_FILE="$OUTPUT_DIR/regression-report.md"

# Write the generated report content to the file
echo "Report written to: $REPORT_FILE"
```

---

### Phase 6: Output Summary

```text
=== Regression Prediction Summary ===

Repository: {go_module}
Framework: {controller-runtime|library-go}
Base Branch: {base_branch}
Changes Analyzed: {N} commits, {M} files changed

Findings:
  🔴 Critical: {count}
  🟠 High:     {count}
  🟡 Medium:   {count}
  ⚪ Low:      {count}

Risk Assessment: {Critical|High|Medium|Low}

Report: {output_dir}/regression-report.md

{If critical issues found:}
⚠️  CRITICAL ISSUES DETECTED ⚠️
The following must be addressed before merge:
  - {issue 1}
  - {issue 2}

{If no critical issues:}
✓ No critical regressions detected.
{If high issues:}
⚠️  Review high-risk findings before release.
```

---

## Arguments

- **$1 (base-branch)**: Git branch or ref to diff against — e.g., `main`, `origin/main`, `release-4.18`. Required.
- **--output**: Output directory (optional). Default: `output`. Report written to `<output>/regression-report.md`.

## Examples

```shell
# Analyze changes since main
/oape:predict-regressions main

# Analyze changes since a release branch
/oape:predict-regressions origin/release-4.18

# Use custom output directory
/oape:predict-regressions main --output .reports
```

## Notes

- **Comprehensive**: Combines static analysis with LLM-powered semantic analysis
- **Actionable**: Every finding includes specific mitigation steps
- **Prioritized**: Findings ordered by severity and impact
- **Test-focused**: Suggests concrete e2e test scenarios for each issue
- **Framework-aware**: Detects controller-runtime vs library-go patterns
- **Upgrade-focused**: Specifically analyzes API version changes and migration paths

## Integration with Workflow

This command can be integrated into the OAPE workflow after API generation:

1. Generate API types
2. **→ Predict regressions** ← (this command)
3. Address critical issues
4. Generate tests
5. Create PR

## Behavioral Rules

1. **Stop on critical**: If critical issues are found, strongly recommend blocking the merge
2. **Never skip analysis**: All changed files must be analyzed, even if large
3. **Evidence-based**: Every finding must cite specific code or diff sections
4. **Constructive**: Always provide mitigation, never just criticize
5. **Test-driven**: Always suggest test scenarios to validate fixes
6. **Version-aware**: Pay special attention to multi-version API scenarios
