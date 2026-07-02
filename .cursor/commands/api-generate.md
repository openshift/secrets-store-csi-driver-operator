---
name: /oape:api-generate
id: oape-api-generate
category: OAPE
description: Generate OpenShift API type definitions from an enhancement proposal PR and/or design document, following OpenShift and Kubernetes API conventions
argument-hint: [<enhancement-pr-url>] [--design-doc <gist-url-or-local-path>]
---

## Name
oape:api-generate

## Synopsis
```shell
# Both EP and design document
/oape:api-generate <https://github.com/openshift/enhancements/pull/NNNN> --design-doc <https://gist.github.com/user/gist_id>

# EP only (original behavior)
/oape:api-generate <https://github.com/openshift/enhancements/pull/NNNN>

# Design document only
/oape:api-generate --design-doc <https://gist.github.com/user/gist_id>
```

## Description
The `oape:api-generate` command reads an OpenShift enhancement proposal PR and/or a design document (GitHub Gist), extracts the required API changes, and generates compliant Go type definitions in the correct paths of the current OpenShift operator repository.

**Input Sources:**
- **Enhancement Proposal (EP)**: High-level requirements, constraints, and context from an openshift/enhancements PR
- **Design Document (Gist)**: Detailed implementation specifications including exact field definitions, validation rules, and code structure

When both sources are provided, the design document takes precedence for implementation details while the EP provides high-level context.

It refreshes its knowledge of API conventions from the authoritative sources on every run, analyzes the input sources, and generates or modifies Go types that strictly follow both OpenShift and Kubernetes API conventions.

**You MUST follow ALL conventions strictly. If any precheck fails, you MUST stop immediately and report the failure.**

## Skills

Read and follow **effective-go** (`.cursor/skills/effective-go/SKILL.md`) for all generated Go code.

## Implementation

### Phase 0: Prechecks

All prechecks must pass before proceeding. If ANY precheck fails, STOP immediately and report the failure.

#### Precheck 1 — Parse and Validate Input Arguments

The command accepts an Enhancement Proposal URL and/or a design document (gist) URL. At least one must be provided.

```bash
ARGS="$ARGUMENTS"
ENHANCEMENT_PR=""
DESIGN_DOC_URL=""
ENHANCEMENT_PR_NUMBER=""

# Extract --design-doc argument if present
if echo "$ARGS" | grep -q '\-\-design-doc'; then
  DESIGN_DOC_URL=$(echo "$ARGS" | sed -n 's/.*--design-doc[[:space:]]\+\([^[:space:]]\+\).*/\1/p')
  # Remove --design-doc and its value from ARGS to get EP URL
  ENHANCEMENT_PR=$(echo "$ARGS" | sed 's/--design-doc[[:space:]]\+[^[:space:]]\+//' | xargs)
else
  ENHANCEMENT_PR="$ARGS"
fi

# Validate at least one input is provided
if [ -z "$ENHANCEMENT_PR" ] && [ -z "$DESIGN_DOC_URL" ]; then
  echo "PRECHECK FAILED: No input provided."
  echo "Usage:"
  echo "  /oape:api-generate <EP_URL> [--design-doc <GIST_URL>]"
  echo "  /oape:api-generate --design-doc <GIST_URL>"
  echo ""
  echo "Examples:"
  echo "  /oape:api-generate https://github.com/openshift/enhancements/pull/1234"
  echo "  /oape:api-generate https://github.com/openshift/enhancements/pull/1234 --design-doc https://gist.github.com/user/abc123"
  echo "  /oape:api-generate --design-doc https://gist.github.com/user/abc123"
  exit 1
fi

# Validate Enhancement PR URL if provided
if [ -n "$ENHANCEMENT_PR" ]; then
  if ! echo "$ENHANCEMENT_PR" | grep -qE '^https://github\.com/openshift/enhancements/pull/[0-9]+/?$'; then
    echo "PRECHECK FAILED: Invalid enhancement PR URL."
    echo "Expected format: https://github.com/openshift/enhancements/pull/<number>"
    echo "Got: $ENHANCEMENT_PR"
    exit 1
  fi
  ENHANCEMENT_PR_NUMBER=$(echo "$ENHANCEMENT_PR" | grep -oE '[0-9]+$')
  echo "Enhancement PR #$ENHANCEMENT_PR_NUMBER validated."
else
  echo "No Enhancement PR provided. Using design document only."
fi

# Validate Design Document if provided (Gist URL or local file — OpenSpec design-bundle.md)
DESIGN_DOC_LOCAL=false
if [ -n "$DESIGN_DOC_URL" ]; then
  if [ -f "$DESIGN_DOC_URL" ]; then
    DESIGN_DOC_LOCAL=true
    echo "Design document (local file) validated: $DESIGN_DOC_URL"
  elif echo "$DESIGN_DOC_URL" | grep -qE '^https://gist\.github(usercontent)?\.com/'; then
    echo "Design document URL validated: $DESIGN_DOC_URL"
  else
    echo "PRECHECK FAILED: Invalid design document path or URL."
    echo "Expected: local file path OR https://gist.github.com/[username/]<gist_id>"
    echo "Got: $DESIGN_DOC_URL"
    exit 1
  fi
else
  echo "No design document provided. Using Enhancement PR only."
fi

echo ""
echo "=== Input Sources ==="
[ -n "$ENHANCEMENT_PR" ] && echo "  Enhancement PR: $ENHANCEMENT_PR"
[ -n "$DESIGN_DOC_URL" ] && echo "  Design Document: $DESIGN_DOC_URL"
echo "====================="
```

#### Precheck 2 — Verify Required Tools

```bash
MISSING_TOOLS=""

# Check gh CLI
if ! command -v gh &> /dev/null; then
  MISSING_TOOLS="$MISSING_TOOLS gh(GitHub CLI)"
fi

# Check Go
if ! command -v go &> /dev/null; then
  MISSING_TOOLS="$MISSING_TOOLS go"
fi

# Check git
if ! command -v git &> /dev/null; then
  MISSING_TOOLS="$MISSING_TOOLS git"
fi

if [ -n "$MISSING_TOOLS" ]; then
  echo "PRECHECK FAILED: Missing required tools:$MISSING_TOOLS"
  echo "Please install the missing tools and try again."
  exit 1
fi

# Check gh auth status
if ! gh auth status &> /dev/null 2>&1; then
  echo "PRECHECK FAILED: GitHub CLI is not authenticated."
  echo "Run 'gh auth login' to authenticate."
  exit 1
fi

echo "All required tools are available and authenticated."
```

#### Precheck 3 — Verify Current Repository is a Valid OpenShift Operator Repo

```bash
# Must be in a git repository
if ! git rev-parse --is-inside-work-tree &> /dev/null 2>&1; then
  echo "PRECHECK FAILED: Not inside a git repository."
  echo "This command must be run from within an OpenShift operator repository."
  exit 1
fi

REPO_ROOT=$(git rev-parse --show-toplevel)
echo "Repository root: $REPO_ROOT"

# Must have a go.mod file
if [ ! -f "$REPO_ROOT/go.mod" ]; then
  echo "PRECHECK FAILED: No go.mod found at repository root."
  echo "This command must be run from within a Go-based OpenShift operator repository."
  exit 1
fi

# Identify the Go module name
GO_MODULE=$(head -1 "$REPO_ROOT/go.mod" | awk '{print $2}')
echo "Go module: $GO_MODULE"

# Check if this repo vendors or imports openshift/api
if grep -q "github.com/openshift/api" "$REPO_ROOT/go.mod"; then
  echo "Confirmed: Repository depends on github.com/openshift/api"
elif echo "$GO_MODULE" | grep -q "github.com/openshift/api"; then
  echo "Confirmed: This IS the openshift/api repository."
else
  echo "PRECHECK FAILED: This repository does not appear to be an OpenShift operator repository."
  echo "go.mod does not reference github.com/openshift/api."
  echo "Module: $GO_MODULE"
  exit 1
fi
```

#### Precheck 4 — Verify Enhancement PR is Accessible (if provided)

```bash
PR_TITLE=""
PR_STATE=""

if [ -n "$ENHANCEMENT_PR_NUMBER" ]; then
  echo "Fetching enhancement PR #$ENHANCEMENT_PR_NUMBER details..."

  PR_STATE=$(gh pr view "$ENHANCEMENT_PR_NUMBER" --repo openshift/enhancements --json state --jq '.state' 2>/dev/null)

  if [ -z "$PR_STATE" ]; then
    echo "PRECHECK FAILED: Unable to access enhancement PR #$ENHANCEMENT_PR_NUMBER."
    echo "Ensure the PR exists and you have access to the openshift/enhancements repository."
    exit 1
  fi

  echo "Enhancement PR #$ENHANCEMENT_PR_NUMBER state: $PR_STATE"

  # Get the PR title and body for context
  PR_TITLE=$(gh pr view "$ENHANCEMENT_PR_NUMBER" --repo openshift/enhancements --json title --jq '.title')
  echo "Enhancement title: $PR_TITLE"
else
  echo "Skipping Enhancement PR validation (not provided)."
fi
```

#### Precheck 5 — Verify Design Document is Accessible (if provided)

```bash
if [ -n "$DESIGN_DOC_URL" ] && [ "$DESIGN_DOC_LOCAL" = true ]; then
  echo "Local design document ready (skip gist verification)."
elif [ -n "$DESIGN_DOC_URL" ]; then
  echo "Verifying design document accessibility..."

  # Extract gist ID from URL (handles various formats)
  GIST_ID=$(echo "$DESIGN_DOC_URL" | grep -oE '[a-f0-9]{32}' | head -1)
  
  if [ -z "$GIST_ID" ]; then
    # Try extracting from end of URL for short gist IDs
    GIST_ID=$(echo "$DESIGN_DOC_URL" | sed 's|.*/||' | sed 's|[?#].*||')
  fi

  if [ -z "$GIST_ID" ]; then
    echo "PRECHECK FAILED: Could not extract gist ID from URL."
    echo "URL: $DESIGN_DOC_URL"
    exit 1
  fi

  # Verify gist is accessible
  GIST_INFO=$(gh api "gists/$GIST_ID" --jq '.description // "Untitled"' 2>/dev/null)
  
  if [ -z "$GIST_INFO" ]; then
    echo "PRECHECK FAILED: Unable to access design document gist."
    echo "Gist ID: $GIST_ID"
    echo "Ensure the gist exists and is public (or you have access)."
    exit 1
  fi

  echo "Design document gist verified: $GIST_INFO"
  echo "Gist ID: $GIST_ID"
else
  echo "Skipping design document validation (not provided)."
fi
```

#### Precheck 6 — Verify Clean Working Tree (Warning)

```bash
if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "WARNING: Uncommitted changes detected in the working tree."
  echo "It is recommended to commit or stash changes before generating API types."
  echo "Proceeding anyway..."
  git status --short
else
  echo "Working tree is clean."
fi
```

**If ALL prechecks above passed, proceed to Phase 1.**
**If ANY precheck FAILED (exit 1), STOP. Do NOT proceed further. Report the failure to the user.**

---

### Phase 1: Refresh Knowledge — Fetch Latest API Conventions

You MUST fetch and read both of these documents in full BEFORE analyzing the enhancement proposal
or generating any code. Never rely on cached knowledge — the freshly fetched versions are the
single source of truth.

1. **OpenShift API Conventions**: `https://raw.githubusercontent.com/openshift/enhancements/master/dev-guide/api-conventions.md`
2. **Kubernetes API Conventions**: `https://raw.githubusercontent.com/kubernetes/community/master/contributors/devel/sig-architecture/api-conventions.md`

```thinking
I must now read both fetched convention documents in full and extract every rule that applies to
API type generation — field markers, naming, documentation, validation, pointers, unions, enums,
TechPreview gating, etc. I will NOT rely on any pre-built checklist; the fetched documents are the
single source of truth. If the conventions have been updated since this command was written, the
freshly fetched versions take precedence. I will carry all extracted rules forward into the code
generation steps.
```

### Phase 2: Fetch and Analyze Input Sources

Fetch content from all provided input sources (Enhancement Proposal and/or Design Document).

#### 2.1 Fetch Enhancement Proposal (if provided)

```bash
if [ -n "$ENHANCEMENT_PR_NUMBER" ]; then
  echo "Fetching files changed in enhancement PR #$ENHANCEMENT_PR_NUMBER..."
  gh pr view "$ENHANCEMENT_PR_NUMBER" --repo openshift/enhancements --json files --jq '.files[].path'
fi
```

Fetch the full content of each proposal file. Use the PR ref (`refs/pull/<number>/head`) which
GitHub always maintains, even if the fork branch has been deleted:

```bash
# For each enhancement .md file found in the file list above, fetch its full content:
gh api "repos/openshift/enhancements/contents/<path-to-file>?ref=refs/pull/$ENHANCEMENT_PR_NUMBER/head" --jq '.content' | base64 -d
```

If the above fails, try fetching the raw file via curl:

```bash
curl -sL "https://raw.githubusercontent.com/openshift/enhancements/refs/pull/$ENHANCEMENT_PR_NUMBER/head/<path-to-file>"
```

As a last resort, fall back to reading the diff which contains the full proposed content:

```bash
gh pr diff "$ENHANCEMENT_PR_NUMBER" --repo openshift/enhancements
```

#### 2.2 Fetch Design Document (if provided)

```bash
if [ -n "$DESIGN_DOC_URL" ] && [ "$DESIGN_DOC_LOCAL" = true ]; then
  echo "Reading local design document: $DESIGN_DOC_URL"
  cat "$DESIGN_DOC_URL"
elif [ -n "$GIST_ID" ]; then
  echo "Fetching design document from gist $GIST_ID..."
  gh api "gists/$GIST_ID" --jq '.files | to_entries[] | "=== FILE: \(.key) ===\n\(.value.content)\n"'
fi
```

If the gist `gh api` command fails, try fetching via curl:

```bash
curl -sL "https://api.github.com/gists/$GIST_ID" | jq -r '.files | to_entries[] | "=== FILE: \(.key) ===\n\(.value.content)\n"'
```

#### 2.3 Analyze and Merge Requirements

```thinking
I need to analyze the input source(s) and extract API requirements. The approach depends on what was provided:

**If BOTH Enhancement Proposal AND Design Document are provided:**
- The EP provides high-level context: motivation, constraints, affected components
- The Design Document provides implementation details: exact field definitions, types, validation
- When both specify the same information, the Design Document takes precedence
- Extract from EP: operator/component context, FeatureGate requirements, general constraints
- Extract from Design Document: exact API fields, types, validation rules, code structure

**If only Enhancement Proposal is provided:**
- Extract all requirements from the EP (original behavior)

**If only Design Document is provided:**
- The Design Document must be comprehensive enough to generate API types
- It should specify: API group, version, kind, all fields with types and validation

From the combined sources, I must extract:
   a. Which OpenShift operator/component is being modified
   b. The API group and version (e.g., config.openshift.io/v1, operator.openshift.io/v1)
   c. Whether this is a NEW CRD or modifications to an EXISTING CRD
   d. Whether this is a Configuration API or Workload API
   e. The specific fields/types being added or modified
   f. Validation requirements (enums, patterns, min/max, cross-field)
   g. Whether fields should be TechPreview-gated
   h. Any discriminated unions
   i. Defaulting behavior
   j. Immutability requirements
   k. Status fields and conditions
   l. The FeatureGate name to use

If there are conflicts between sources, I will:
1. Prefer Design Document specifics over EP generalizations
2. Document any conflicts in my analysis
3. Ask the user for clarification if conflicts are ambiguous
```

### Phase 3: Identify Target API Paths in Current Repository

Different OpenShift repositories organize API types differently. Explore the current repo to
determine which layout pattern is in use, then map the enhancement proposal's API changes to
the correct file paths.

#### Known Layout Patterns

**Pattern 1 — openshift/api repository:**
```text
<group>/<version>/types_<resource>.go
<group>/<version>/doc.go
<group>/<version>/register.go
<group>/<version>/tests/<crd-name>/*.testsuite.yaml
features/features.go
```
- File naming: `types_<resource>.go`
- Registration: `doc.go` + `register.go`
- FeatureGates: `features/features.go`

**Pattern 2 — Operator repo with group subdirectory (e.g., cert-manager-operator):**
```text
api/<group>/<version>/<resource>_types.go
api/<group>/<version>/groupversion_info.go
api/<group>/<version>/doc.go
api/<group>/<version>/zz_generated.deepcopy.go
```
- File naming: `<resource>_types.go`
- Registration: `groupversion_info.go` with `SchemeBuilder`
- Each types file has `init()` calling `SchemeBuilder.Register()`

**Pattern 3 — Operator repo with flat version directory (e.g., external-secrets-operator):**
```text
api/<version>/<resource>_types.go
api/<version>/groupversion_info.go
api/<version>/tests/<resource>/*.testsuite.yaml
api/<version>/zz_generated.deepcopy.go
```
- File naming: `<resource>_types.go`
- Registration: `groupversion_info.go` with `SchemeBuilder`

#### Detect the Pattern

Run these commands to identify which layout the current repo uses:

```bash
# Find type definition files
find "$REPO_ROOT" -type f \( -name 'types*.go' -o -name '*_types.go' \) -not -path '*/vendor/*' -not -path '*/_output/*' -not -path '*/zz_generated*' | head -40

# Find registration files
find "$REPO_ROOT" -type f \( -name 'doc.go' -o -name 'register.go' -o -name 'groupversion_info.go' \) -not -path '*/vendor/*' -not -path '*/_output/*' | head -40

# Find CRD manifests
find "$REPO_ROOT" -type f -name '*.crd.yaml' -not -path '*/vendor/*' | head -20

# Find test suites
find "$REPO_ROOT" -type f -name '*.testsuite.yaml' -not -path '*/vendor/*' | head -20

# Find feature gate definitions
find "$REPO_ROOT" -type f -name 'features.go' -not -path '*/vendor/*' | head -10
```

### Phase 4: Read Existing API Types for Context

Before generating new code, read the existing types in the target API package to understand:
- The existing struct layout and naming patterns
- Import conventions used
- Existing markers and annotations
- How other fields in the same struct are documented

```thinking
I must read the existing types file(s) in the target package to:
1. Match the coding style exactly
2. Understand existing struct hierarchy
3. Know where to insert new fields or add new types
4. Identify existing fields/types that need to be modified (e.g., adding new enum values,
   updating validation rules, changing godoc, adding new fields to existing structs)
5. Identify existing imports that may be reused
6. See how feature gates are applied to existing fields
7. Understand the existing validation patterns
```

### Phase 5: Generate or Modify API Type Definitions

Generate or modify Go type definitions based on the enhancement proposal. This may include new
types, new fields, modifications to existing fields, enum types, discriminated unions, or type
registration.

#### Pre-generation traceability check

Before writing any code, list every field/type being added, modified, or removed. For each one,
cite the specific sentence or section in the input sources that requires it. If a change cannot
be traced to a specific requirement, do NOT make it.

#### File scope guard

Only create or modify files under `api/` or type-definition directories (e.g., `features/`).
If you are about to write a file under `controllers/`, `pkg/controller/`, `internal/controller/`,
`pkg/operator/`, `cmd/`, or `bindata/`, STOP — that belongs in `api-implement`, not here.

**Hard deny-list — NEVER create or modify files matching ANY of these patterns:**
- `controllers/**` or `pkg/controller/**` or `internal/controller/**` (controller logic)
- `pkg/operator/**` (operator logic)
- `cmd/**` or `main.go` (entrypoints and scheme registration)
- `bindata/**` (static resource manifests)
- `**/constant.go` or `**/constants.go` outside `api/` (controller constants)
- `**/networkpolicies.go`, `**/federation.go`, `**/template.go` (implementation files)

Even if new API fields imply controller behavior (e.g., new fields need env vars, new types need
resource builders), do NOT generate those files. Only generate the types and note the implied
controller work in the summary under "Deferred to api-implement".

#### Generation rules

For every marker, tag, or convention applied: derive it from the fetched convention documents
(Phase 1) or the existing code (Phase 4). Conventions take precedence when both differ. Existing
patterns not covered by conventions (e.g., mechanical code-gen markers) should be replicated for
consistency.

Determine from the enhancement proposal whether this is a **Configuration API** or **Workload API**,
as the conventions define different rules for each.

After generating, review every changed line against the conventions. If any violation has a
convention-compliant alternative, apply it and note the deviation in the Phase 7 summary.

### Phase 6: Add FeatureGate Registration (if applicable)

If the repository contains a `features.go` file (found in Phase 3), read it to learn the existing
FeatureGate registration pattern, then add a new FeatureGate following that pattern.

If no `features.go` exists and the enhancement requires a FeatureGate, note this in the summary
and advise the user on where to register it.

### Phase 7: Output Summary

After generating all files, provide a summary:

```text
=== API Generation Summary ===

Input Sources:
  Enhancement PR: <url> (if provided)
  Design Document: <gist-url> (if provided)
  Enhancement Title: <title> (if EP provided)

Generated/Modified Files:
  - <path/to/types_resource.go> — <description of changes>
  - <path/to/features/features.go> — <FeatureGate added> (if applicable)

API Group: <group.openshift.io>
API Version: <version>
Kind: <KindName>
Resource: <resourcename>
Scope: <Cluster|Namespaced>
FeatureGate: <FeatureGateName>

New Types Added:
  - <TypeName> — <description>

New Fields Added:
  - <ParentType>.<fieldName> (<type>) — <description>

Modified Fields/Types:
  - <ParentType>.<fieldName> — <what changed and why>

Validation Rules:
  - <field>: <rule description>

Source Conflicts Resolved: (if both EP and design doc provided)
  - <field>: Used design doc specification (<reason>)

Next Steps:
  1. Review the generated code for correctness
  2. Run 'make update' to regenerate CRDs and deep copy functions
  3. Run 'make verify' to validate all generated code
  4. Run 'make lint' to check for kube-api-linter issues
  5. If FeatureGate was added, verify it appears in the feature gate list
```

---

## Critical Failure Conditions

The command MUST FAIL and STOP immediately if ANY of the following are true:

1. **No input provided**: Neither an enhancement PR URL nor a design document URL was provided
2. **Invalid PR URL**: The provided EP URL is not a valid `openshift/enhancements` PR
3. **Invalid gist URL**: The provided design document URL is not a valid GitHub Gist
4. **Missing tools**: `gh`, `go`, or `git` are not installed or `gh` is not authenticated
5. **Not an operator repo**: The current directory is not a Git repository with a Go module that references `openshift/api`
6. **Input not accessible**: The enhancement PR or design document cannot be fetched (permissions, doesn't exist, etc.)
7. **No API changes found**: The input source(s) do not describe any API changes
8. **Ambiguous API target**: Cannot determine the target API group, version, or kind from the input sources

When failing, provide a clear error message explaining:
- Which precheck failed
- What the expected state is
- How to fix the issue

## Behavioral Rules

1. **Never guess**: If the input sources are ambiguous about API details, STOP and ask the user for clarification rather than guessing.
2. **Design document precedence**: When both EP and design document are provided, the design document takes precedence for implementation details.
3. **Convention over proposal**: If the input sources suggest an API design that violates conventions (e.g., using a Boolean), generate the convention-compliant alternative and document the deviation.
4. **TechPreview when specified**: If the input sources indicate TechPreview gating, generate the appropriate FeatureGate markers. Follow whatever is specified regarding API maturity level.
5. **Idempotent**: Running this command multiple times with the same inputs should produce the same result (though it should warn if files already exist).
6. **Minimal changes**: Only generate what the input sources specify. Do not add extra fields, types, or features not described.
7. **Surgical edits**: When modifying existing files, only change what the input sources require. Preserve all unrelated code, comments, and formatting. For modifications to existing fields, clearly document what changed and why in the output summary.
8. **API types only — no controller code**: This command MUST only create or modify files in API-layer directories (`api/`, `features/`, type definition files). Do NOT create or modify files in controller directories (`controllers/`, `pkg/controller/`, `internal/controller/`, `pkg/operator/`, `cmd/`, `bindata/`). If the EP describes controller behavior, note it in the summary under "Deferred to api-implement" but generate zero controller code.
9. **No invented fields**: Do NOT add fields, types, or enum values that the input sources do not explicitly specify. If the EP adds field X, only add field X — do not also add a related field Y you think "should" exist.
10. **No restructuring**: Do NOT rename existing fields, change pointer-vs-value semantics on existing fields, or move fields between structs unless the input sources explicitly require it. When the EP says "remove field X", only remove field X. Do NOT modernize, reformat, or "improve" existing comments, type names, or field ordering on untouched fields.
11. **No controller-layer files**: Do NOT create files under `controllers/`, `pkg/controller/`, `bindata/`, `cmd/`, or `pkg/operator/`. This includes constants files, helper files, and resource builders that serve controller logic. If new API fields imply controller wiring, list the implied work in the summary — do not generate it.

## Arguments

- `<enhancement-pr-url>` (optional if design-doc provided): GitHub PR URL to the OpenShift enhancement proposal
  - Format: `https://github.com/openshift/enhancements/pull/<number>`

- `--design-doc <gist-url>` (optional if EP provided): GitHub Gist URL containing detailed API specifications
  - Supported formats:
    - `https://gist.github.com/username/gist_id`
    - `https://gist.github.com/gist_id`
    - `https://gist.githubusercontent.com/username/gist_id/raw/...`

**At least one input source (EP or design document) must be provided.**

## Design Document Expected Format

When using a design document, it should contain structured implementation details:

```markdown
# Design Document: Feature Name

## API Specification
- Group: config.openshift.io (or operator.openshift.io, etc.)
- Version: v1 (or v1alpha1, v1beta1)
- Kind: FeatureName
- Scope: Cluster (or Namespaced)

## Spec Fields
- `fieldName` (type): Description
  - Validation: required, enum values, min/max, pattern
  - Default: default value if any
  - Immutable: yes/no

## Status Fields
- `conditions`: Standard OpenShift conditions
- `observedGeneration`: int64

## FeatureGate
- Name: FeatureGateName
- Stage: TechPreviewNoUpgrade / Default
```

## Prerequisites

- **gh** (GitHub CLI) — installed and authenticated (`gh auth login`)
- **go** — Go toolchain installed
- **git** — Git installed
- Must be run from within an OpenShift operator repository (Go module that references `github.com/openshift/api`)

## Exit Conditions

- **Success**: API type definitions generated/modified with a summary of all changes
- **Failure Scenarios**:
  - No input provided (neither EP nor design document)
  - Invalid enhancement PR URL or gist URL
  - Missing required tools or unauthenticated GitHub CLI
  - Not inside a valid OpenShift operator repository
  - Input source(s) inaccessible
  - No API changes found in the input sources
  - Ambiguous API target (asks for clarification instead of guessing)
