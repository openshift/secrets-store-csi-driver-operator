---
name: /oape:e2e-generate
id: oape-e2e-generate
category: OAPE
description: Generate e2e test artifacts (test cases, execution steps, and test code) for any OpenShift operator based on git diff from a base branch
argument-hint: "<base-branch> [--output <path>]"
---

## Name
oape:e2e-generate

## Synopsis
```
/oape:e2e-generate <base-branch> [--output <path>]
```

## Description

Analyzes the current OpenShift operator repository and generates **all e2e test artifacts** based on the diff between `<base-branch>` and HEAD:

1. **test-cases.md** — Test scenarios with operator context, prerequisites, install steps, CR deployment, diff-specific test cases, verification, and cleanup.
2. **execution-steps.md** — Step-by-step procedure with executable `oc` commands.
3. **e2e test code** — Go (Ginkgo) or bash script, matching the repo's existing e2e pattern.
4. **e2e-suggestions.md** — Which scenarios apply to this diff, with recommendations.

All files are written into **one output directory**: `<output-dir>/e2e_<repo-name>/`. Default `<output-dir>` is `output` (create if missing).

- **Generic**: Works with any OpenShift operator repository (controller-runtime or library-go).
- **Discovery-based**: All operator structure (API types, CRDs, namespaces, install mechanism, e2e patterns) is discovered from the repo. Nothing is hardcoded.
- **Diff-driven**: Tests are focused on what changed between the base branch and HEAD.

## Skills

Read and follow **e2e-test-generator** (`.cursor/skills/e2e-test-generator/SKILL.md`).
Fixture templates live under `.cursor/e2e-test-generator/fixtures/` (linked as `../e2e-test-generator/fixtures/` from this command file).

## Implementation

### Phase 0: Prechecks

All prechecks must pass before proceeding. If ANY precheck fails, STOP immediately and report the failure.

#### Precheck 1 — Validate Arguments

```bash
BASE_BRANCH="$1"

if [ -z "$BASE_BRANCH" ]; then
  echo "PRECHECK FAILED: No base branch provided."
  echo "Usage: /oape:e2e-generate <base-branch> [--output <path>]"
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

# oc is recommended but not required for generation
if ! command -v oc &> /dev/null; then
  echo "WARNING: oc not found. Generated execution steps require oc to run."
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

if [ ! -f "$REPO_ROOT/go.mod" ]; then
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

#### Precheck 5 — Verify Clean Working Tree (Warning)

```bash
if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "WARNING: Uncommitted changes detected in the working tree."
  echo "Generated tests will be based on committed changes only (diff between $BASE_BRANCH and HEAD)."
  echo "Proceeding anyway..."
else
  echo "Working tree is clean."
fi
```

**If ALL prechecks above passed, proceed to Phase 1.**
**If ANY precheck FAILED (exit 1), STOP. Do NOT proceed further.**

---

### Phase 1: Framework Detection and Repository Discovery

This phase discovers the operator's structure. All subsequent phases use this discovered information — never hardcoded values.

#### Step 1.1: Discover API Types

```bash
find "$REPO_ROOT" -type f \( -name '*_types.go' -o -name 'types_*.go' \) \
  -not -path '*/vendor/*' -not -path '*/_output/*' -not -path '*/zz_generated*' | head -40
```

For each types file found, read it and extract:
- API group and version (from `// +groupName=...` marker or `GroupVersion` var)
- Kind names (struct names with `metav1.TypeMeta` embedded)
- Spec and Status field names
- Condition types (from constants or status field types)
- Whether CRs are cluster-scoped or namespaced (from `// +kubebuilder:resource:scope=...` marker)

If **no types files found** in the repo (common with library-go operators like `secrets-store-csi-driver-operator`):
- Check `go.mod` for `github.com/openshift/api` dependency
- Note that API types are external
- Look in vendor directory or CRD manifests to understand the managed kinds

```thinking
I must build a complete picture of the API types this operator manages, whether defined in-repo
or imported from external modules. This is critical for generating accurate test code that
references the correct types, fields, and conditions.
```

#### Step 1.2: Discover CRDs

```bash
find "$REPO_ROOT" -type f -name '*.yaml' \( -path '*/crd/*' -o -path '*/crds/*' -o -path '*/manifests/*' \) \
  -not -path '*/vendor/*' | head -30
```

For each CRD file, extract: Kind, group, plural resource name, scope (Cluster/Namespaced), served versions.

#### Step 1.3: Discover Existing E2E Test Patterns

```bash
# Look for Go-based e2e tests
find "$REPO_ROOT" -type f -name '*_test.go' -path '*/e2e/*' -not -path '*/vendor/*' | head -20

# Look for bash-based e2e tests
find "$REPO_ROOT" -type f -name '*.sh' \( -path '*/e2e/*' -o -path '*/hack/e2e*' \) -not -path '*/vendor/*' | head -10
```

If Go test files found with Ginkgo imports (`onsi/ginkgo`):
- Read 1-2 existing e2e test files to understand: package name, import paths, client variable names, helper utilities used, assertion patterns, test structure
- Read `utils/constants.go` (if exists) for namespace, deployment names, timeouts
- Read `utils/utils.go` (if exists) for helper function signatures

If bash scripts found:
- Read the script to understand: namespace usage, test structure (functions vs sequential), assertion patterns, cleanup approach

If **no existing e2e tests found**:
- Default to Ginkgo for controller-runtime repos, bash for library-go repos
- Use the plugin's fixture examples as templates:
  - [fixtures/e2e-sample-controller-runtime_test.go.example](../e2e-test-generator/fixtures/e2e-sample-controller-runtime_test.go.example) for Ginkgo
  - [fixtures/e2e-sample-library-go_test.sh.example](../e2e-test-generator/fixtures/e2e-sample-library-go_test.sh.example) for bash

#### Step 1.4: Discover Install Mechanism

```bash
# Look for OLM manifests
find "$REPO_ROOT" -type f -name '*.yaml' \
  \( -path '*/config/manifests/*' -o -path '*/bundle/*' \) \
  -not -path '*/vendor/*' | head -20

# Look for deployment manifests
find "$REPO_ROOT" -type f -name '*.yaml' \
  \( -path '*/config/default/*' -o -path '*/deploy/*' \) \
  -not -path '*/vendor/*' | head -20
```

From OLM manifests, extract:
- Package name (from `*.package.yaml`)
- Channel name (stable, alpha, etc.)
- CSV name and version
- Install namespace
- Subscription details

If no OLM manifests found, note that install is manual (deployment-based).

#### Step 1.5: Discover Sample CRs

```bash
find "$REPO_ROOT" -type f -name '*.yaml' \
  \( -path '*/config/samples/*' -o -path '*/examples/*' \) \
  -not -path '*/vendor/*' | head -20
```

Read sample CR files to understand: which CR kinds have samples, default field values, required environment variables (e.g., `${APP_DOMAIN}`).

#### Step 1.6: Discover Operator Namespace

Search for namespace in this order:
1. E2E constants file (`utils/constants.go` or similar): look for `Namespace` const or var
2. CSV manifest: look for `metadata.annotations["operatorframework.io/suggested-namespace"]`
3. Namespace YAML in config/manifests or deploy/
4. If not found, note as `<operator-namespace>` placeholder

#### Step 1.7: Discover Controllers

```bash
find "$REPO_ROOT" -type f -name '*.go' \
  \( -name '*controller*' -o -name '*reconcile*' -o -name 'starter.go' \) \
  -not -path '*/vendor/*' -not -name '*_test.go' | head -20
```

Read controller files to understand: which CRs are reconciled, what resources are managed (Deployments, StatefulSets, DaemonSets, ConfigMaps), condition update logic.

#### Step 1.8: Build Repo Profile

```thinking
I now have a complete repo profile. I will summarize:
- Framework: controller-runtime or library-go
- Go module: <module path>
- Repo name: <basename>
- API types: list of {Kind, group, version, scope, key fields, conditions}
- Types location: in-repo (api/) or external (openshift/api)
- CRDs: list of {Kind, group, plural, scope}
- E2E pattern: Ginkgo (package name, imports, clients, helpers) or bash (script path, test structure)
- Install mechanism: OLM (package, channel, CSV, namespace) or manual
- Samples: list of sample CR files with their kinds
- Operator namespace: discovered or placeholder
- Controllers: list of reconciled CRs and managed resources
- Managed workloads: Deployments, StatefulSets, DaemonSets managed by the operator

This profile drives all subsequent generation phases. I will NOT use any hardcoded values.
```

---

### Phase 2: Analyze Git Diff

```bash
# Get the diff stat
git diff "$BASE_BRANCH"...HEAD --stat

# Get the full diff
git diff "$BASE_BRANCH"...HEAD -p

# Get the commit log
git log "$BASE_BRANCH"...HEAD --oneline
```

Categorize each changed file:

| File Pattern | Category | Test Focus |
|---|---|---|
| `api/**/*_types.go`, `types_*.go` | API Types | New/changed fields, validation, conditions |
| `config/crd/**/*.yaml` | CRD Changes | Schema updates, new versions |
| `*controller*.go`, `*reconcile*.go`, `starter.go` | Controller | Reconciliation logic, conditions, managed resources |
| `config/rbac/*.yaml` | RBAC | Permission changes |
| `config/samples/*.yaml` | Samples | Example CR usage, default values |
| `test/e2e/**` | E2E Tests | Existing test patterns (do not duplicate) |
| `assets/**` | Embedded Assets | Resource deployment changes |
| `Makefile`, `Dockerfile*` | Build | Build/deploy changes (no direct test) |

For each changed file in the API Types and Controller categories, read the diff hunks to understand the specific changes: new fields, modified conditions, new reconciliation logic, changed status updates.

```thinking
I must map each meaningful change to a specific test scenario. New API fields need CR create/update
tests. New conditions need condition-check tests. Controller changes need reconciliation and
recovery tests. I will focus tests on what actually changed, not generate tests for unchanged code.
```

---

### Phase 3: Generate test-cases.md

Write **test-cases.md** into the output directory. Content must be based entirely on discovered repo profile and diff analysis.

**Structure:**

```markdown
# E2E Test Cases: <repo-name>

## Operator Information
- **Repository**: <go-module>
- **Framework**: <controller-runtime|library-go>
- **API Group**: <discovered-api-group>
- **Managed CRDs**: <list of Kind names>
- **Operator Namespace**: <discovered-namespace>
- **Changes Analyzed**: git diff <base-branch>...HEAD

## Prerequisites
- OpenShift cluster with admin access
- `oc` CLI installed and authenticated
- <any env vars discovered from samples, e.g., APP_DOMAIN>

## Installation
<Based on discovered install mechanism: OLM subscription or manual deployment>
<Include oc apply, oc wait commands using actual discovered values>

## CR Deployment
<Based on discovered sample CRs>
<Include oc apply commands for each sample CR>

## Test Cases

### <Category from diff: e.g., "API Type Changes">
<For each change detected, describe the test scenario>
- **Test**: <what to test>
- **Steps**: <oc commands or programmatic steps>
- **Expected**: <expected outcome>

### <Category: e.g., "Controller Changes">
...

## Verification
<oc get, oc wait, oc describe commands for all managed CRs>
<oc logs for operator namespace>

## Cleanup
<Reverse order deletion of CRs>
<OLM cleanup if applicable: subscription, CSV, operatorgroup, namespace>
```

---

### Phase 4: Generate execution-steps.md

Write **execution-steps.md** into the output directory.

**Structure:**

```markdown
# E2E Execution Steps: <repo-name>

## Prerequisites

```bash
which oc
oc version
oc whoami
oc get nodes
oc get clusterversion
<packagemanifests check if OLM install>
```

## Environment Variables

```bash
<Any env vars discovered from samples or e2e constants>
<e.g., export APP_DOMAIN=apps.$(oc get dns cluster -o jsonpath='{.spec.baseDomain}')>
```

## Step 1: Install Operator

```bash
<oc apply or OLM subscription commands using discovered values>
<oc wait for CSV, deployment>
<oc get pods -n <namespace>>
```

## Step 2: Deploy CRs

```bash
<oc apply for each sample CR, with envsubst if needed>
```

## Step 3: Verify Installation

```bash
<oc get for each managed CR kind>
<oc wait for conditions>
```

## Step 4: Diff-Specific Tests

```bash
<Specific oc commands to exercise changes detected in the diff>
```

## Step 5: Cleanup

```bash
<oc delete for CRs in reverse order>
<oc delete subscription, csv, operatorgroup if OLM>
<oc delete namespace>
```
```

---

### Phase 5: Generate E2E Test Code

Generate test code that matches the repo's existing e2e pattern.

#### Path A — Ginkgo (controller-runtime repos)

Generate a file named **`e2e_test.go`** in the output directory.

Requirements:
- **Package**: Same as discovered existing e2e package (usually `e2e`)
- **Imports**: Match existing e2e import style. Include only needed imports. Use the actual operator API import path (discovered in Phase 1).
- **Clients**: Use the same client variables as existing tests (`k8sClient`, `clientset`, etc.). Do not redefine them.
- **Helpers**: Use discovered helper utilities (`utils.WaitFor*`, `utils.OperatorNamespace`, etc.). If helpers don't exist for the needed operations, write inline test logic.
- **Structure**: `Describe`/`Context`/`It` with `By("...")` steps.
- **No suite logic**: Do not include `BeforeSuite`, `TestE2E`, or client setup — only test blocks.
- **Comments**: Each `It` block prefixed with `// Diff-suggested: <reason based on diff>` for pick-and-choose.
- **Content**: Include both (a) important scenarios relevant to the diff (see [fixtures/e2e-important-scenarios.md](../e2e-test-generator/fixtures/e2e-important-scenarios.md)) and (b) diff-specific tests derived from Phase 2 analysis.

For code style reference, see [fixtures/e2e-sample-controller-runtime_test.go.example](../e2e-test-generator/fixtures/e2e-sample-controller-runtime_test.go.example).

#### Path B — Bash (library-go repos)

Generate a file named **`e2e_test.sh`** in the output directory.

Requirements:
- **Header**: `#!/usr/bin/env bash` with `set -euo pipefail`
- **Configuration**: Variables for namespace, deployment name, labels, timeout — all using discovered values
- **Test functions**: `test_<scenario>()` for each test case
- **Assertions**: `oc` commands with exit code checks
- **Cleanup**: `trap cleanup EXIT` function
- **Comments**: Each test function prefixed with `# Diff-suggested: <reason>`
- **Content**: Include operator install verification, CR lifecycle, diff-specific tests, cleanup

For code style reference, see [fixtures/e2e-sample-library-go_test.sh.example](../e2e-test-generator/fixtures/e2e-sample-library-go_test.sh.example).

---

### Phase 6: Generate e2e-suggestions.md

Write **e2e-suggestions.md** into the output directory.

**Content:**

- Summary of detected operator structure (framework, managed CRDs, e2e pattern)
- List of changes detected in the diff
- For each change, which e2e scenarios are **highly recommended**
- Which additional scenarios are **optional/nice-to-have**
- Any gaps: areas of the diff that are hard to test automatically

---

### Phase 7: Output Summary

After generating all files, confirm output:

```
=== E2E Test Generation Summary ===

Repository: <go-module>
Framework: <controller-runtime|library-go>
Base Branch: <base-branch>
Changes Analyzed: <N files changed, M insertions, K deletions>

Generated Files:
  - <output-dir>/e2e_<repo-name>/test-cases.md
  - <output-dir>/e2e_<repo-name>/execution-steps.md
  - <output-dir>/e2e_<repo-name>/e2e_test.go (or e2e_test.sh)
  - <output-dir>/e2e_<repo-name>/e2e-suggestions.md

Repo Profile:
  API Types: <list of Kind names or "external (openshift/api)">
  CRDs: <list of CRD names>
  E2E Pattern: <Ginkgo|bash|none (generated from template)>
  Operator Namespace: <namespace>
  Install Mechanism: <OLM|manual>

Next Steps:
  1. Review generated test cases and suggestions
  2. Copy e2e test code into the repo's test/e2e/ directory (or hack/)
  3. Adjust placeholder values if any remain
  4. Run tests against a live cluster
```

## Arguments

- **$1 (base-branch)**: Git branch or ref to diff against — e.g., `main`, `origin/main`, `release-4.18`, `ai-staging`. Required.
- **--output**: Output base directory (optional). Default: `output`. Generated files go in `<output>/e2e_<repo-name>/`.

## Examples

```
# From within an operator repo, generate e2e tests for changes since main
/oape:e2e-generate main

# Use a specific base branch and custom output directory
/oape:e2e-generate origin/release-4.18 --output .work

# For repos with ai-staging branches (from repos.txt)
/oape:e2e-generate ai-staging

# With a remote tracking branch
/oape:e2e-generate origin/ai-staging-release-1.1 --output test-output
```

## Notes

- **Generic**: Works with any OpenShift operator repository. No operator-specific knowledge is hardcoded.
- **Discovery-based**: All operator structure is discovered from the repository. If something cannot be discovered, it is noted as a placeholder in the generated output.
- **Diff-focused**: Only generates tests relevant to the changes between the base branch and HEAD. Does not generate tests for unchanged code.
- **Framework-aware**: Detects controller-runtime vs library-go and generates the appropriate test format.
- **No browser tools**: Works entirely locally with git and file reads. No `gh` CLI or browser navigation required.
- **Pick-and-choose**: Generated test code is commented so the user can copy only the blocks they need.
- **Existing patterns respected**: If the repo has existing e2e tests, the generated code follows the same style, imports, and conventions.

## Behavioral Rules

1. **Never hardcode**: All operator-specific values (namespaces, CR kinds, API groups, conditions) must be discovered from the repo. Never use hardcoded values from any specific operator.
2. **Match existing style**: If the repo has existing e2e tests, generated code must match the same package name, import style, client variables, helper usage, and assertion patterns.
3. **Diff-driven focus**: Only generate tests for code that changed. Do not generate tests for unchanged functionality.
4. **Fail on ambiguity**: If the repo structure is ambiguous (e.g., cannot determine framework, no API types and no CRDs found), STOP and ask the user for clarification.
5. **Minimal placeholders**: Replace as many placeholders as possible with discovered values. Only leave placeholders for values that genuinely cannot be determined from the repo.
6. **No duplicate suite logic**: For Ginkgo tests, do not generate BeforeSuite, TestE2E, or client setup. Only generate test blocks (Describe/Context/It).
7. **Correct cleanup order**: Always generate cleanup in reverse dependency order — CRs first, then OLM resources, then namespace.
