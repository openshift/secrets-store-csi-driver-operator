---
name: /oape:api-implement
id: oape-api-implement
category: OAPE
description: Generate OpenShift controller/reconciler implementation code from an enhancement proposal PR and/or design document, following controller-runtime and operator-sdk conventions
argument-hint: [<enhancement-pr-url>] [--design-doc <gist-url-or-local-path>]
---

## Name
oape:api-implement

## Synopsis
```shell
# Both EP and design document
/oape:api-implement <https://github.com/openshift/enhancements/pull/NNNN> --design-doc <https://gist.github.com/user/gist_id>

# EP only (original behavior)
/oape:api-implement <https://github.com/openshift/enhancements/pull/NNNN>

# Design document only
/oape:api-implement --design-doc <https://gist.github.com/user/gist_id>
```

## Description
The `oape:api-implement` command reads an OpenShift enhancement proposal PR and/or a design document (GitHub Gist), extracts the required implementation logic, and generates complete controller/reconciler code in the correct paths of the current OpenShift operator repository.

**Input Sources:**
- **Enhancement Proposal (EP)**: High-level requirements, constraints, and context from an openshift/enhancements PR
- **Design Document (Gist)**: Detailed implementation specifications including reconciliation workflow, dependent resources, and controller behavior

When both sources are provided, the design document takes precedence for implementation details while the EP provides high-level context.

This command generates **production-ready code with zero TODOs** by:
1. Parsing the input sources for explicit business logic requirements
2. Detecting the operator framework in use (controller-runtime, operator-sdk, library-go)
3. Generating actual reconciliation logic, not placeholders
4. Creating dependent resource builders and reconcilers
5. Implementing cleanup/finalizer logic
6. Setting up watches for external resources

**You MUST follow ALL conventions strictly. If any precheck fails, you MUST stop immediately and report the failure.**

## Skills

Read and follow **effective-go** (`.cursor/skills/effective-go/SKILL.md`) for all generated Go controller code.

---

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
  echo "  /oape:api-implement <EP_URL> [--design-doc <GIST_URL>]"
  echo "  /oape:api-implement --design-doc <GIST_URL>"
  echo ""
  echo "Examples:"
  echo "  /oape:api-implement https://github.com/openshift/enhancements/pull/1234"
  echo "  /oape:api-implement https://github.com/openshift/enhancements/pull/1234 --design-doc https://gist.github.com/user/abc123"
  echo "  /oape:api-implement --design-doc https://gist.github.com/user/abc123"
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

# Check make
if ! command -v make &> /dev/null; then
  MISSING_TOOLS="$MISSING_TOOLS make"
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

#### Precheck 3 — Verify Current Repository is a Valid Operator Repo

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

  PR_TITLE=$(gh pr view "$ENHANCEMENT_PR_NUMBER" --repo openshift/enhancements --json title --jq '.title')
  echo "Enhancement title: $PR_TITLE"
else
  echo "Skipping Enhancement PR validation (not provided)."
fi
```

#### Precheck 5 — Verify Design Document is Accessible (if provided)

```bash
GIST_ID=""

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

#### Precheck 6 — Verify API Types Exist

```bash
echo "Checking if API types exist in the repository..."

API_TYPES=$(find "$REPO_ROOT" -type f \( -name 'types*.go' -o -name '*_types.go' \) -not -path '*/vendor/*' -not -path '*/_output/*' -not -path '*/zz_generated*' | head -20)

if [ -z "$API_TYPES" ]; then
  echo "PRECHECK FAILED: No API type definitions found in the repository."
  echo "You MUST run /oape:api-generate first to create the API types."
  echo "The controller needs types to reconcile."
  exit 1
fi

echo "Found API types:"
echo "$API_TYPES" | head -10
```

#### Precheck 7 — Verify Clean Working Tree (Warning)

```bash
if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "WARNING: Uncommitted changes detected in the working tree."
  echo "It is recommended to commit or stash changes before generating controller code."
  echo "Proceeding anyway..."
  git status --short
else
  echo "Working tree is clean."
fi
```

**If ALL prechecks above passed, proceed to Phase 1.**
**If ANY precheck FAILED (exit 1), STOP. Do NOT proceed further. Report the failure to the user.**

---

### Phase 1: Detect Operator Framework Type

Before generating code, determine which operator framework this repository uses. This affects code generation patterns.

```bash
echo "Detecting operator framework type..."

# Check for operator-sdk PROJECT file (kubebuilder/operator-sdk marker)
if [ -f "$REPO_ROOT/PROJECT" ]; then
  echo "Found PROJECT file - this is an operator-sdk/kubebuilder project"
  cat "$REPO_ROOT/PROJECT"
fi

# Check go.mod for framework indicators
echo "Checking go.mod for framework dependencies..."

# Check for library-go (OpenShift operators)
if grep -q "github.com/openshift/library-go" "$REPO_ROOT/go.mod"; then
  echo "Found: github.com/openshift/library-go"
fi

# Check for controller-runtime
if grep -q "sigs.k8s.io/controller-runtime" "$REPO_ROOT/go.mod"; then
  echo "Found: sigs.k8s.io/controller-runtime"
fi
```

```thinking
Based on the dependency analysis, I must determine the OPERATOR_TYPE:

**Type 1: controller-runtime based (kubebuilder/operator-sdk v1+)**
Indicators:
- Has `sigs.k8s.io/controller-runtime` in go.mod
- May have PROJECT file with `layout: go.kubebuilder.io`
- Uses `ctrl.Manager`, `Reconciler` pattern
- File structure: `controllers/` or `internal/controller/`

**Type 2: library-go based (OpenShift core operators)**
Indicators:
- Has `github.com/openshift/library-go` in go.mod
- Uses `pkg/operator/` structure
- Uses `factory.Controller` and `SyncFunc` pattern
- Has `starter.go` for controller registration
- More imperative style, less declarative

I will set OPERATOR_TYPE to one of:
- "controller-runtime" (most common, default)
- "library-go" (OpenShift core operators)

If neither framework is detected, STOP and ask user for clarification.

The generated code patterns will differ based on this type.
```

#### Framework Detection Rules

Apply these rules in order:

1. **If `github.com/openshift/library-go` is in go.mod AND `pkg/operator/` directory exists:**
   - OPERATOR_TYPE = "library-go"
   - Use SyncFunc pattern, factory.Controller

2. **Else if `sigs.k8s.io/controller-runtime` is in go.mod:**
   - OPERATOR_TYPE = "controller-runtime"
   - Use Reconciler pattern, ctrl.Manager

3. **Else:**
   - STOP and ask user for clarification
   - Only controller-runtime and library-go frameworks are supported

---

### Phase 2: Refresh Knowledge — Fetch Latest Operator Conventions

Fetch and read these documents based on OPERATOR_TYPE:

**For controller-runtime:**
1. `https://book.kubebuilder.io/cronjob-tutorial/controller-implementation`
2. `https://pkg.go.dev/sigs.k8s.io/controller-runtime`

**For library-go:**
1. `https://github.com/openshift/library-go/tree/master/pkg/controller`
2. `https://github.com/openshift/library-go/blob/master/pkg/operator/events/recorder.go`

**For all types:**
1. `https://raw.githubusercontent.com/openshift/enhancements/master/dev-guide/operator.md`

```thinking
Based on OPERATOR_TYPE, I must internalize the correct patterns:

**For controller-runtime:**
- Reconcile(ctx, req) returns (ctrl.Result, error)
- Use client.Client for API operations
- Use controllerutil for finalizers, owner refs
- Use ctrl.Log or log.FromContext for logging
- Use mgr.GetEventRecorderFor for events

**For library-go:**
- SyncFunc(ctx, syncContext) error pattern
- Use factory.Controller with Informers
- Use events.Recorder (library-go's recorder)
- Use klog for logging
- Explicit informer cache management
- Use resourceapply for resource creation

I will apply the correct patterns based on detected type.
```

---

### Phase 3: Fetch and Parse Input Sources

Fetch content from all provided input sources (Enhancement Proposal and/or Design Document).

#### 3.1 Fetch Enhancement Proposal (if provided)

```bash
if [ -n "$ENHANCEMENT_PR_NUMBER" ]; then
  echo "Fetching files changed in enhancement PR #$ENHANCEMENT_PR_NUMBER..."
  gh pr view "$ENHANCEMENT_PR_NUMBER" --repo openshift/enhancements --json files --jq '.files[].path'
fi
```

Fetch the full content of each proposal file:

```bash
# For each enhancement .md file, fetch content
gh api "repos/openshift/enhancements/contents/<path-to-file>?ref=refs/pull/$ENHANCEMENT_PR_NUMBER/head" --jq '.content' | base64 -d
```

Fallback methods if above fails:

```bash
# Fallback 1: Raw content
curl -sL "https://raw.githubusercontent.com/openshift/enhancements/refs/pull/$ENHANCEMENT_PR_NUMBER/head/<path-to-file>"

# Fallback 2: PR diff
gh pr diff "$ENHANCEMENT_PR_NUMBER" --repo openshift/enhancements
```

#### 3.2 Fetch Design Document (if provided)

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

#### 3.3 Extract Structured Requirements

```thinking
I MUST extract structured information from the input source(s). The approach depends on what was provided:

**If BOTH Enhancement Proposal AND Design Document are provided:**
- The EP provides high-level context: motivation, constraints, affected components
- The Design Document provides implementation details: reconciliation workflow, dependent resources, controller behavior
- When both specify the same information, the Design Document takes precedence
- Extract from EP: component context, FeatureGate requirements, general constraints
- Extract from Design Document: exact reconciliation steps, dependent resources, status updates, events

**If only Enhancement Proposal is provided:**
- Extract all requirements from the EP (original behavior)

**If only Design Document is provided:**
- The Design Document must be comprehensive enough to generate controller code
- It should specify: API details, reconciliation workflow, dependent resources, status conditions

From the combined sources, I will extract the following. For each item, I will search for specific sections, keywords, and patterns.

## EXTRACTION CHECKLIST

### A. API Information (Required)
Search sections: "API", "CRD", "Custom Resource", "API Extensions"
Extract:
- [ ] API Group (e.g., config.openshift.io, operator.openshift.io)
- [ ] API Version (v1, v1alpha1, v1beta1)
- [ ] Kind name (PascalCase, e.g., IngressController)
- [ ] Resource name (plural, lowercase, e.g., ingresscontrollers)
- [ ] Scope (Cluster or Namespaced)

### B. Spec Fields → Controller Actions (Required)
Search sections: "Spec", "Configuration", "API" tables/structs
For EACH spec field, determine:
- [ ] Field name and type
- [ ] What controller action this field triggers
- [ ] Validation to perform
- [ ] Default behavior if not set

### C. Reconciliation Workflow (Required)
Search sections: "Implementation", "Proposal", "Workflow", "Reconciliation", "Controller"
Extract ordered steps:
- [ ] Step 1: What to do first
- [ ] Step 2: What to do next
- [ ] ... continue for all steps
- [ ] What triggers re-reconciliation

### D. Dependent Resources (Required for complete code)
Search sections: "Implementation", "Resources", "Managed Resources"
Keywords: "create", "deploy", "manage", "ConfigMap", "Secret", "Deployment", "Service"
For EACH dependent resource:
- [ ] Resource type (ConfigMap, Secret, Deployment, Service, etc.)
- [ ] Resource name pattern
- [ ] Namespace (same as CR, or specific namespace, or cluster-scoped)
- [ ] Content/spec to set
- [ ] When to create/update/delete

### E. External Resources / Integrations (If applicable)
Search sections: "External", "Integration", "Cloud", "API calls"
Keywords: "AWS", "Azure", "GCP", "HTTP", "API", "external service"
For EACH external integration:
- [ ] External system name
- [ ] API/SDK to use
- [ ] Operations to perform
- [ ] Credentials handling

### F. Status Conditions (Required)
Search sections: "Status", "Conditions", "Reporting"
Standard OpenShift conditions to consider:
- [ ] Available (true when functioning)
- [ ] Progressing (true when changes in progress)
- [ ] Degraded (true when errors)
For EACH condition:
- [ ] Condition Type name
- [ ] When to set True
- [ ] When to set False
- [ ] Reason codes
- [ ] Message templates

### G. Status Fields (Beyond conditions)
Search sections: "Status", "Observed"
For EACH status field:
- [ ] Field name
- [ ] What it represents
- [ ] How to compute/observe it

### H. Events to Record (Required)
Search sections: "Events", "Notifications", "Audit"
Keywords: "event", "notify", "record", "log"
For EACH event:
- [ ] Event type (Normal/Warning)
- [ ] Reason (short CamelCase)
- [ ] When to emit
- [ ] Message template

### I. Error Handling (Required)
Search sections: "Errors", "Failure", "Retry", "Edge Cases"
Keywords: "error", "fail", "retry", "backoff", "timeout"
Extract:
- [ ] Transient errors (retry with backoff)
- [ ] Permanent errors (don't retry, set Degraded)
- [ ] Specific error conditions and handling

### J. Cleanup / Deletion (Required if external resources)
Search sections: "Cleanup", "Deletion", "Finalizer", "Garbage Collection"
Keywords: "delete", "cleanup", "remove", "finalizer"
Extract:
- [ ] What to clean up on CR deletion
- [ ] Order of cleanup
- [ ] Finalizer name to use

### K. Watches / Triggers (Required for reactive behavior)
Search sections: "Watch", "React", "Trigger", "Events"
Keywords: "watch", "react to", "when X changes", "trigger"
For EACH watch:
- [ ] Resource type to watch
- [ ] Filter/predicate (which resources)
- [ ] How to map to primary resource

### L. Feature Gate (If applicable)
Search sections: "Feature Gate", "TechPreview", "Graduation"
Keywords: "feature gate", "tech preview", "alpha", "beta"
Extract:
- [ ] Feature gate name
- [ ] Behavior when disabled

### M. RBAC Requirements (Derived)
Based on all above, compute:
- [ ] Primary resource: get, list, watch, update (status)
- [ ] Dependent resources: get, list, watch, create, update, patch, delete
- [ ] External resources watched: get, list, watch
- [ ] Events: create, patch

**Source Merging Rules:**
When both EP and Design Document are provided:
1. Design Document takes precedence for implementation-specific details
2. EP provides context and constraints
3. If there are conflicts, prefer Design Document specifics
4. Document any significant conflicts in the output summary

If ANY required section (A, B, C, F, I) is missing or ambiguous across ALL provided sources, I MUST stop and ask the user for clarification. I will NOT guess.
```

---

### Phase 4: Identify Target Paths for Controller Code

Explore the current repo to determine the layout pattern:

```bash
# Find existing controller files
find "$REPO_ROOT" -type f -name '*controller*.go' -not -path '*/vendor/*' -not -path '*/_output/*' | head -30

# Find main.go or manager setup
find "$REPO_ROOT" -type f -name 'main.go' -not -path '*/vendor/*' -not -path '*/_output/*' | head -10

# Find existing reconcilers
grep -r "func.*Reconcile\|func.*Sync" "$REPO_ROOT" --include='*.go' -l | grep -v vendor | grep -v _output | head -20

# Find controller registration
grep -r "SetupWithManager\|AddToManager\|NewController\|factory.New" "$REPO_ROOT" --include='*.go' -l | grep -v vendor | head -20

# For library-go, find starter.go
find "$REPO_ROOT" -type f -name 'starter.go' -not -path '*/vendor/*' | head -5
```

#### Layout Patterns by OPERATOR_TYPE

**controller-runtime layouts:**

| Pattern | Controller Location | Registration |
|---------|---------------------|--------------|
| Standard | `controllers/<resource>_controller.go` | `main.go` |
| Internal | `internal/controller/<resource>_controller.go` | `cmd/main.go` |
| Nested | `internal/controller/<resource>/controller.go` | `internal/controller/setup.go` |

**library-go layouts:**

| Pattern | Controller Location | Registration |
|---------|---------------------|--------------|
| Standard | `pkg/operator/<resource>/<resource>_controller.go` | `pkg/operator/starter.go` |
| Flat | `pkg/operator/<resource>_controller.go` | `pkg/operator/operator.go` |

---

### Phase 5: Read Existing Controller Code for Context

#### 5.1 Find existing controller for the target resource

```bash
# Search for an existing controller that already reconciles the target resource type.
# Check Reconcile/Sync methods, SetupWithManager, and For() calls for the target Kind.
grep -r "For(&.*<TargetKind>{}\|<TargetResources>()\|Reconcile.*<TargetKind>" "$REPO_ROOT" --include='*.go' -l | grep -v vendor | grep -v _output | head -10
```

```thinking
CRITICAL: If an existing controller already reconciles this resource type, I MUST modify it
rather than create a new controller file. I will:
1. Read ALL files in the existing controller's package (controller.go, constants.go, helpers, resource builders)
2. Add new logic into the existing Reconcile/Sync flow
3. Add constants to existing constant files, not new ones
4. Add resource builders to existing builder files, not new ones

I only create a NEW controller file when NO existing controller handles this resource type.
```

#### 5.2 Read existing controller patterns

```bash
# Find sample controller based on OPERATOR_TYPE
if [ "$OPERATOR_TYPE" = "library-go" ]; then
  SAMPLE_CONTROLLER=$(find "$REPO_ROOT/pkg" -type f -name '*controller*.go' -not -path '*/vendor/*' -not -name '*_test.go' | head -1)
else
  SAMPLE_CONTROLLER=$(find "$REPO_ROOT" -type f -name '*controller*.go' -not -path '*/vendor/*' -not -path '*/_output/*' -not -name '*_test.go' | head -1)
fi

if [ -n "$SAMPLE_CONTROLLER" ]; then
  echo "Reading sample controller: $SAMPLE_CONTROLLER"
fi
```

```thinking
I MUST read existing controller(s) and extract these EXACT patterns to replicate:

1. **Package name** - What package are controllers in?
2. **Import organization** - How are imports grouped? Aliases used?
3. **Struct fields** - What fields does the reconciler/controller struct have?
4. **Constructor pattern** - New<Resource>Controller() or direct struct init?
5. **Reconcile/Sync signature** - Exact method signature used
6. **Logging** - log.FromContext? klog? What format?
7. **Event recording** - How events are recorded
8. **Status updates** - Pattern for updating status
9. **Condition helpers** - Existing helper functions for conditions
10. **Resource creation** - How dependent resources are created
11. **Error handling** - How errors are wrapped and returned
12. **Constants** - Where constants are defined (same file, separate file)
13. **Resource builder files** - Are there template.go / builder files that construct Jobs, Deployments, etc.? Read ALL files in the controller package, not just the main controller file.
14. **Function signatures** - Do builder functions accept individual params or whole spec structs? Match their style when adding new parameters.

I will replicate these patterns EXACTLY in generated code.

**CRITICAL pre-generation checklist:**
- For each new spec field, find the existing file where its parent resource is built/used. Modify THAT file — do not create a new file.
- If constants already exist in a constants file, add new constants there.
- If the controller already has a builder function for the resource type (e.g., getJobTemplate, buildDeployment), add parameters to that function.
- Only create new files for genuinely new resource types or controllers that have no existing equivalent.
```

---

### Phase 6: Generate Controller Code

Based on OPERATOR_TYPE and extracted requirements, generate the appropriate controller.

---

#### 6.1 For controller-runtime Based Operators

Generate file: `<target-path>/<resource>_controller.go`

```go
/*
Copyright <year> Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
...
*/

package <package>

import (
    "context"
    "fmt"
    "time"

    corev1 "k8s.io/api/core/v1"
    "k8s.io/apimachinery/pkg/api/errors"
    "k8s.io/apimachinery/pkg/api/meta"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/types"
    "k8s.io/client-go/tools/record"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
    "sigs.k8s.io/controller-runtime/pkg/log"
    "sigs.k8s.io/controller-runtime/pkg/predicate"

    <apigroupversion> "<module>/api/<version>"
)

const (
    // <Resource>Finalizer is the finalizer for <Resource> cleanup
    <Resource>Finalizer = "<group>.<resource>-finalizer"

    // Condition type constants
    ConditionTypeAvailable   = "Available"
    ConditionTypeProgressing = "Progressing"
    ConditionTypeDegraded    = "Degraded"

    // Event reason constants
    ReasonReconciling       = "Reconciling"
    ReasonReconcileComplete = "ReconcileComplete"
    ReasonReconcileFailed   = "ReconcileFailed"
    ReasonCreated           = "Created"
    ReasonUpdated           = "Updated"
    ReasonDeleted           = "Deleted"

    // Requeue intervals
    DefaultRequeueInterval = 30 * time.Second
    ErrorRequeueInterval   = 5 * time.Second
)
```

**Generate RBAC markers dynamically based on extracted requirements:**

```go
// RBAC for primary resource
//+kubebuilder:rbac:groups=<group>,resources=<resources>,verbs=get;list;watch
//+kubebuilder:rbac:groups=<group>,resources=<resources>/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=<group>,resources=<resources>/finalizers,verbs=update

// RBAC for dependent resources (generated per-resource from extraction)
// Example: If enhancement requires creating ConfigMaps:
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete

// Example: If enhancement requires creating Deployments:
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// RBAC for events
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
```

**Generate Reconciler struct:**

```go
// <Resource>Reconciler reconciles a <Resource> object
type <Resource>Reconciler struct {
    client.Client
    Scheme   *runtime.Scheme
    Recorder record.EventRecorder
}
```

**Generate Reconcile method with ACTUAL logic (no TODOs):**

```go
// Reconcile performs reconciliation for <Resource>
func (r *<Resource>Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    logger := log.FromContext(ctx).WithValues("<resource>", req.NamespacedName)
    logger.Info("Starting reconciliation")

    // Step 1: Fetch the <Resource> instance
    instance := &<apigroupversion>.<Resource>{}
    if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
        if errors.IsNotFound(err) {
            logger.Info("<Resource> not found, likely deleted")
            return ctrl.Result{}, nil
        }
        logger.Error(err, "Failed to get <Resource>")
        return ctrl.Result{}, err
    }

    // Step 2: Check for deletion
    if !instance.DeletionTimestamp.IsZero() {
        logger.Info("Handling deletion")
        return r.reconcileDelete(ctx, instance)
    }

    // Step 3: Add finalizer if needed
    if !controllerutil.ContainsFinalizer(instance, <Resource>Finalizer) {
        logger.Info("Adding finalizer")
        controllerutil.AddFinalizer(instance, <Resource>Finalizer)
        if err := r.Update(ctx, instance); err != nil {
            logger.Error(err, "Failed to add finalizer")
            return ctrl.Result{}, err
        }
        return ctrl.Result{Requeue: true}, nil
    }

    // Step 4: Set Progressing condition
    r.setCondition(instance, ConditionTypeProgressing, metav1.ConditionTrue,
        ReasonReconciling, "Reconciliation in progress")
    r.setCondition(instance, ConditionTypeDegraded, metav1.ConditionFalse,
        ReasonReconciling, "")

    // Step 5: Main reconciliation logic
    if err := r.reconcile(ctx, instance); err != nil {
        logger.Error(err, "Reconciliation failed")

        // Set degraded condition
        r.setCondition(instance, ConditionTypeDegraded, metav1.ConditionTrue,
            ReasonReconcileFailed, err.Error())
        r.setCondition(instance, ConditionTypeAvailable, metav1.ConditionFalse,
            ReasonReconcileFailed, "Reconciliation failed")
        r.setCondition(instance, ConditionTypeProgressing, metav1.ConditionFalse,
            ReasonReconcileFailed, "")

        r.Recorder.Event(instance, corev1.EventTypeWarning, ReasonReconcileFailed, err.Error())

        if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
            logger.Error(statusErr, "Failed to update status")
        }

        // Requeue with backoff for transient errors
        return ctrl.Result{RequeueAfter: ErrorRequeueInterval}, err
    }

    // Step 6: Set success conditions
    r.setCondition(instance, ConditionTypeAvailable, metav1.ConditionTrue,
        ReasonReconcileComplete, "Successfully reconciled")
    r.setCondition(instance, ConditionTypeProgressing, metav1.ConditionFalse,
        ReasonReconcileComplete, "")
    r.setCondition(instance, ConditionTypeDegraded, metav1.ConditionFalse,
        ReasonReconcileComplete, "")

    instance.Status.ObservedGeneration = instance.Generation

    if err := r.Status().Update(ctx, instance); err != nil {
        logger.Error(err, "Failed to update status")
        return ctrl.Result{}, err
    }

    r.Recorder.Event(instance, corev1.EventTypeNormal, ReasonReconcileComplete,
        "Successfully reconciled <Resource>")

    logger.Info("Reconciliation complete")
    return ctrl.Result{RequeueAfter: DefaultRequeueInterval}, nil
}
```

**Generate actual reconcile() method based on EP extraction:**

```go
// reconcile performs the core reconciliation logic
// This method is generated based on the enhancement proposal requirements
func (r *<Resource>Reconciler) reconcile(ctx context.Context, instance *<apigroupversion>.<Resource>) error {
    logger := log.FromContext(ctx)

    // ============================================================
    // STEP 1: Validate Spec
    // (Generated based on validation requirements from EP)
    // ============================================================
    if err := r.validateSpec(instance); err != nil {
        return fmt.Errorf("spec validation failed: %w", err)
    }

    // ============================================================
    // STEP 2: Reconcile Dependent Resources
    // (Generated for EACH dependent resource extracted from EP)
    // ============================================================

    // Example: If EP requires a ConfigMap
    if err := r.reconcileConfigMap(ctx, instance); err != nil {
        return fmt.Errorf("failed to reconcile ConfigMap: %w", err)
    }

    // Example: If EP requires a Secret
    if err := r.reconcileSecret(ctx, instance); err != nil {
        return fmt.Errorf("failed to reconcile Secret: %w", err)
    }

    // Example: If EP requires a Deployment
    if err := r.reconcileDeployment(ctx, instance); err != nil {
        return fmt.Errorf("failed to reconcile Deployment: %w", err)
    }

    // Example: If EP requires a Service
    if err := r.reconcileService(ctx, instance); err != nil {
        return fmt.Errorf("failed to reconcile Service: %w", err)
    }

    // ============================================================
    // STEP 3: Check/Update External State
    // (Generated if EP has external integrations)
    // ============================================================

    // Example: If EP requires external API calls
    // if err := r.syncExternalState(ctx, instance); err != nil {
    //     return fmt.Errorf("failed to sync external state: %w", err)
    // }

    // ============================================================
    // STEP 4: Update Status with Observed State
    // (Generated based on status fields from EP)
    // ============================================================
    if err := r.updateObservedStatus(ctx, instance); err != nil {
        return fmt.Errorf("failed to update observed status: %w", err)
    }

    logger.V(1).Info("All reconciliation steps completed successfully")
    return nil
}
```

**Generate validateSpec based on EP:**

```go
// validateSpec validates the <Resource> spec
func (r *<Resource>Reconciler) validateSpec(instance *<apigroupversion>.<Resource>) error {
    // Generated validation based on EP requirements
    // Example validations:

    // Required field validation
    // if instance.Spec.RequiredField == "" {
    //     return fmt.Errorf("spec.requiredField is required")
    // }

    // Enum validation
    // validValues := []string{"Value1", "Value2", "Value3"}
    // if !slices.Contains(validValues, string(instance.Spec.EnumField)) {
    //     return fmt.Errorf("spec.enumField must be one of: %v", validValues)
    // }

    // Cross-field validation
    // if instance.Spec.FieldA != "" && instance.Spec.FieldB == "" {
    //     return fmt.Errorf("spec.fieldB is required when spec.fieldA is set")
    // }

    return nil
}
```

**Generate dependent resource reconcilers (for EACH resource from EP):**

```go
// reconcileConfigMap ensures the ConfigMap exists with correct data
func (r *<Resource>Reconciler) reconcileConfigMap(ctx context.Context, instance *<apigroupversion>.<Resource>) error {
    logger := log.FromContext(ctx)

    desired := r.buildConfigMap(instance)

    existing := &corev1.ConfigMap{}
    err := r.Get(ctx, types.NamespacedName{
        Name:      desired.Name,
        Namespace: desired.Namespace,
    }, existing)

    if errors.IsNotFound(err) {
        // Create new ConfigMap
        if err := controllerutil.SetControllerReference(instance, desired, r.Scheme); err != nil {
            return fmt.Errorf("failed to set owner reference: %w", err)
        }

        logger.Info("Creating ConfigMap", "name", desired.Name)
        if err := r.Create(ctx, desired); err != nil {
            return fmt.Errorf("failed to create ConfigMap: %w", err)
        }

        r.Recorder.Event(instance, corev1.EventTypeNormal, ReasonCreated,
            fmt.Sprintf("Created ConfigMap %s", desired.Name))
        return nil
    } else if err != nil {
        return fmt.Errorf("failed to get ConfigMap: %w", err)
    }

    // Update if changed
    if r.configMapNeedsUpdate(existing, desired) {
        existing.Data = desired.Data
        existing.BinaryData = desired.BinaryData

        logger.Info("Updating ConfigMap", "name", desired.Name)
        if err := r.Update(ctx, existing); err != nil {
            return fmt.Errorf("failed to update ConfigMap: %w", err)
        }

        r.Recorder.Event(instance, corev1.EventTypeNormal, ReasonUpdated,
            fmt.Sprintf("Updated ConfigMap %s", desired.Name))
    }

    return nil
}

// buildConfigMap constructs the desired ConfigMap
func (r *<Resource>Reconciler) buildConfigMap(instance *<apigroupversion>.<Resource>) *corev1.ConfigMap {
    return &corev1.ConfigMap{
        ObjectMeta: metav1.ObjectMeta{
            Name:      fmt.Sprintf("%s-config", instance.Name),
            Namespace: instance.Namespace,
            Labels: map[string]string{
                "app.kubernetes.io/name":       "<resource>",
                "app.kubernetes.io/instance":   instance.Name,
                "app.kubernetes.io/managed-by": "<resource>-controller",
            },
        },
        Data: map[string]string{
            // Generated based on EP requirements
            // Map spec fields to config data
        },
    }
}

// configMapNeedsUpdate checks if the ConfigMap needs updating
func (r *<Resource>Reconciler) configMapNeedsUpdate(existing, desired *corev1.ConfigMap) bool {
    // Compare relevant fields
    // Use reflect.DeepEqual or field-by-field comparison
    return !reflect.DeepEqual(existing.Data, desired.Data) ||
           !reflect.DeepEqual(existing.BinaryData, desired.BinaryData)
}
```

**Generate similar methods for each dependent resource type (Deployment, Service, Secret, etc.)**

**Generate deletion handler:**

```go
// reconcileDelete handles cleanup when <Resource> is being deleted
func (r *<Resource>Reconciler) reconcileDelete(ctx context.Context, instance *<apigroupversion>.<Resource>) (ctrl.Result, error) {
    logger := log.FromContext(ctx)

    r.Recorder.Event(instance, corev1.EventTypeNormal, ReasonDeleted, "Cleaning up resources")

    // ============================================================
    // Cleanup external resources (generated from EP)
    // Owner references handle Kubernetes resources automatically
    // ============================================================

    // Example: If EP requires external cleanup
    // if err := r.cleanupExternalResources(ctx, instance); err != nil {
    //     logger.Error(err, "Failed to cleanup external resources")
    //     // Don't remove finalizer - will retry
    //     return ctrl.Result{RequeueAfter: ErrorRequeueInterval}, err
    // }

    // Remove finalizer
    logger.Info("Removing finalizer")
    controllerutil.RemoveFinalizer(instance, <Resource>Finalizer)
    if err := r.Update(ctx, instance); err != nil {
        logger.Error(err, "Failed to remove finalizer")
        return ctrl.Result{}, err
    }

    logger.Info("Cleanup complete")
    return ctrl.Result{}, nil
}
```

**Generate status update helper:**

```go
// updateObservedStatus updates status fields based on observed state
func (r *<Resource>Reconciler) updateObservedStatus(ctx context.Context, instance *<apigroupversion>.<Resource>) error {
    // Generated based on status fields from EP

    // Example: Count ready replicas
    // instance.Status.ReadyReplicas = r.countReadyReplicas(ctx, instance)

    // Example: Collect endpoint addresses
    // instance.Status.Endpoints = r.collectEndpoints(ctx, instance)

    return nil
}

// setCondition sets a condition on the status
func (r *<Resource>Reconciler) setCondition(instance *<apigroupversion>.<Resource>, conditionType string, status metav1.ConditionStatus, reason, message string) {
    meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
        Type:               conditionType,
        Status:             status,
        ObservedGeneration: instance.Generation,
        LastTransitionTime: metav1.Now(),
        Reason:             reason,
        Message:            message,
    })
}
```

**Generate SetupWithManager with watches:**

```go
// SetupWithManager sets up the controller with the Manager
func (r *<Resource>Reconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&<apigroupversion>.<Resource>{}).
        // Owned resources (generated from dependent resources in EP)
        Owns(&corev1.ConfigMap{}).
        Owns(&corev1.Secret{}).
        Owns(&appsv1.Deployment{}).
        Owns(&corev1.Service{}).
        // External watches (generated from EP watch requirements)
        // Watches(
        //     &corev1.Secret{},
        //     handler.EnqueueRequestsFromMapFunc(r.mapSecretTo<Resource>),
        //     builder.WithPredicates(r.secretPredicate()),
        // ).
        WithEventFilter(predicate.GenerationChangedPredicate{}).
        Complete(r)
}
```

---

#### 6.2 For library-go Based Operators

Generate file: `pkg/operator/<resource>/<resource>_controller.go`

```go
package <resource>

import (
    "context"
    "fmt"
    "time"

    "k8s.io/apimachinery/pkg/api/errors"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
    "k8s.io/klog/v2"

    operatorv1 "github.com/openshift/api/operator/v1"
    "github.com/openshift/library-go/pkg/controller/factory"
    "github.com/openshift/library-go/pkg/operator/events"
    "github.com/openshift/library-go/pkg/operator/v1helpers"

    <client> "<module>/pkg/generated/clientset/versioned"
    <informers> "<module>/pkg/generated/informers/externalversions"
)

// <Resource>Controller manages <Resource> resources
type <Resource>Controller struct {
    client          <client>.Interface
    kubeClient      kubernetes.Interface
    operatorClient  v1helpers.OperatorClient
    eventRecorder   events.Recorder
}

// New<Resource>Controller creates a new controller
func New<Resource>Controller(
    client <client>.Interface,
    kubeClient kubernetes.Interface,
    operatorClient v1helpers.OperatorClient,
    informers <informers>.SharedInformerFactory,
    eventRecorder events.Recorder,
) factory.Controller {
    c := &<Resource>Controller{
        client:         client,
        kubeClient:     kubeClient,
        operatorClient: operatorClient,
        eventRecorder:  eventRecorder,
    }

    return factory.New().
        WithSync(c.sync).
        WithInformers(
            informers.<Group>().V1().<Resources>().Informer(),
            // Add more informers based on EP requirements
        ).
        ToController("<Resource>Controller", eventRecorder.WithComponentSuffix("<resource>-controller"))
}

// sync is the main synchronization function
func (c *<Resource>Controller) sync(ctx context.Context, syncCtx factory.SyncContext) error {
    klog.V(4).InfoS("Starting sync", "controller", "<Resource>Controller")

    // Get the resource
    instance, err := c.client.<Group>V1().<Resources>(<namespace>).Get(ctx, <name>, metav1.GetOptions{})
    if errors.IsNotFound(err) {
        klog.V(2).InfoS("Resource not found, skipping", "controller", "<Resource>Controller")
        return nil
    }
    if err != nil {
        return fmt.Errorf("failed to get <Resource>: %w", err)
    }

    // Perform reconciliation
    // Generated based on EP requirements

    // Update status conditions
    _, _, err = v1helpers.UpdateStatus(ctx, c.operatorClient, v1helpers.UpdateConditionFn(
        operatorv1.OperatorCondition{
            Type:    "<Resource>Available",
            Status:  operatorv1.ConditionTrue,
            Reason:  "SyncComplete",
            Message: "Successfully synchronized",
        },
    ))
    if err != nil {
        return fmt.Errorf("failed to update status: %w", err)
    }

    klog.V(4).InfoS("Sync complete", "controller", "<Resource>Controller")
    return nil
}
```

---

### Phase 7: Register Controller with Manager

#### 7.1 For controller-runtime

Locate `main.go` or `cmd/main.go` and add:

```go
// Add import
import (
    "<module>/internal/controller"  // or controllers package
)

// In main(), after manager creation:
if err = (&controller.<Resource>Reconciler{
    Client:   mgr.GetClient(),
    Scheme:   mgr.GetScheme(),
    Recorder: mgr.GetEventRecorderFor("<resource>-controller"),
}).SetupWithManager(mgr); err != nil {
    setupLog.Error(err, "unable to create controller", "controller", "<Resource>")
    os.Exit(1)
}
```

#### 7.2 For library-go

Locate `pkg/operator/starter.go` and add:

```go
// Add import
import (
    "<module>/pkg/operator/<resource>"
)

// In RunOperator() or similar:
<resource>Controller := <resource>.New<Resource>Controller(
    client,
    kubeClient,
    operatorClient,
    informers,
    eventRecorder,
)
```

---

### Phase 8: Generate Feature Gate Check (if applicable)

If the enhancement specifies a FeatureGate, add at the start of Reconcile/Sync:

**For controller-runtime:**

```go
func (r *<Resource>Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // Check feature gate
    if !features.FeatureGates.Enabled(features.<FeatureGateName>) {
        log.FromContext(ctx).V(2).Info("Feature gate disabled, skipping reconciliation",
            "featureGate", "<FeatureGateName>")
        return ctrl.Result{}, nil
    }
    // ... rest of reconcile
}
```

**For library-go:**

```go
func (c *<Resource>Controller) sync(ctx context.Context, syncCtx factory.SyncContext) error {
    if !featuregates.CurrentFeatureGates().Enabled(features.<FeatureGateName>) {
        klog.V(2).InfoS("Feature gate disabled, skipping sync", "featureGate", "<FeatureGateName>")
        return nil
    }
    // ... rest of sync
}
```

---

### Phase 9: Output Summary

After generating all files, provide a comprehensive summary:

```text
=== Controller Implementation Summary ===

Input Sources:
  Enhancement PR: <url> (if provided)
  Design Document: <gist-url> (if provided)
  Enhancement Title: <title> (if EP provided)
Operator Type: <controller-runtime | library-go>

Generated Files:
  - <path/to/controller.go> — Main controller implementation
  - <path/to/main.go> — Updated with controller registration

Controller Details:
  Package: <package name>
  API Group: <group.openshift.io>
  API Version: <version>
  Kind: <KindName>
  Controller Name: <Resource>Reconciler (or <Resource>Controller)

Reconciliation Workflow:
  1. Validate spec
  2. <step from EP>
  3. <step from EP>
  ...
  N. Update status

Dependent Resources Managed:
  - ConfigMap: <name-pattern> — <purpose>
  - Secret: <name-pattern> — <purpose>
  - Deployment: <name-pattern> — <purpose>
  - Service: <name-pattern> — <purpose>

Status Conditions:
  - Available: Set True when <criteria>
  - Progressing: Set True during reconciliation
  - Degraded: Set True on errors

Events Recorded:
  - Normal/Created: When resources are created
  - Normal/Updated: When resources are updated
  - Normal/ReconcileComplete: On successful reconciliation
  - Warning/ReconcileFailed: On errors

RBAC Permissions Generated:
  - <group>/<resource>: get, list, watch
  - <group>/<resource>/status: get, update, patch
  - <group>/<resource>/finalizers: update
  - core/configmaps: get, list, watch, create, update, patch, delete
  - core/secrets: get, list, watch, create, update, patch, delete
  - apps/deployments: get, list, watch, create, update, patch, delete
  - core/services: get, list, watch, create, update, patch, delete
  - core/events: create, patch

Watches Configured:
  - Primary: <Kind>
  - Owned: ConfigMap, Secret, Deployment, Service
  - External: <if any>

Cleanup on Deletion:
  - Kubernetes resources: Handled by owner references
  - External resources: <if any>

Feature Gate: <FeatureGateName> (if applicable)

Source Conflicts Resolved: (if both EP and design doc provided)
  - <item>: Used design doc specification (<reason>)

Next Steps:
  1. Review the generated controller code
  2. Run 'make generate' to update generated code
  3. Run 'make manifests' to update RBAC/CRD manifests
  4. Run 'make build' to verify compilation
  5. Run 'make test' to run tests
  6. Run 'make lint' to check for issues
```

---

## Critical Failure Conditions

The command MUST FAIL and STOP immediately if ANY of the following are true:

1. **No input provided**: Neither an enhancement PR URL nor a design document URL was provided
2. **Invalid PR URL**: The provided EP URL is not a valid `openshift/enhancements` PR
3. **Invalid gist URL**: The provided design document URL is not a valid GitHub Gist
4. **Missing tools**: `gh`, `go`, `git`, or `make` not installed
5. **Not authenticated**: `gh` not authenticated
6. **Not an operator repo**: No go.mod or not a recognized operator type
7. **No API types**: API types don't exist (run `/oape:api-generate` first)
8. **Input not accessible**: Enhancement PR or design document cannot be fetched
9. **No implementation requirements**: Input sources don't describe controller behavior
10. **Ambiguous requirements**: Cannot determine reconciliation workflow from input sources
11. **Unsupported framework**: Repository does not use controller-runtime or library-go

## Behavioral Rules

1. **Never guess**: If input sources are ambiguous, STOP and ask the user for clarification
2. **Design document precedence**: When both EP and design document are provided, the design document takes precedence for implementation details
3. **Zero TODOs**: Generate actual implementation code, not placeholders
4. **Convention over proposal**: Apply framework best practices even if input sources differ
5. **Match existing patterns**: Replicate patterns from existing controllers in the repo
6. **Idempotent reconciliation**: Generated Reconcile() must be idempotent
7. **Minimal changes**: Only generate what the input sources require
8. **Surgical edits**: Preserve unrelated code when modifying files
9. **Status-first**: Always use Status().Update() for status changes
10. **Finalizer safety**: Add before external resources, remove after cleanup
11. **Event recording**: Record events for user-visible state changes
12. **Integrate, don't duplicate**: Most EPs add functionality to EXISTING controllers. Before creating any new controller file, search for an existing controller that already reconciles the same resource type. If one exists, modify it to add the new behavior — add logic to existing Reconcile/Sync methods, add helpers to existing resource-builder files, add constants to existing constant files. Only create a NEW controller when no existing controller handles the target resource type, or the EP explicitly specifies a new controller.
13. **No invented logic**: Do NOT add controller behavior (constants, helper files, resource builders) that the input sources do not describe. If the EP only adds API fields, only wire those fields into the existing reconciliation — do not create standalone feature files.
14. **Wire new fields into existing flows**: When the EP adds fields to an existing type, find the existing code that builds or uses that type (e.g., Job templates, Deployment specs, container env vars) and wire the new fields there. Do not create a separate file per new field — add the logic inline where the resource is already being constructed.
15. **Update scheme and imports**: When new API types are introduced (e.g., `routev1`, `networkingv1`), update `cmd/*/main.go` or equivalent entrypoints to register the new scheme and add the import. Also update any `pkg/client/` setup files that initialize shared schemes.
16. **Update existing resource builders**: If the repo has template/builder files that construct Jobs, Deployments, or other resources, modify those files to pass through new spec fields (e.g., add parameters to existing builder functions, add env vars to existing container specs). Do not create parallel builder files.

## Arguments

- `<enhancement-pr-url>` (optional if design-doc provided): GitHub PR URL to the OpenShift enhancement proposal
  - Format: `https://github.com/openshift/enhancements/pull/<number>`

- `--design-doc <gist-url>` (optional if EP provided): GitHub Gist URL containing detailed implementation specifications
  - Supported formats:
    - `https://gist.github.com/username/gist_id`
    - `https://gist.github.com/gist_id`
    - `https://gist.githubusercontent.com/username/gist_id/raw/...`

**At least one input source (EP or design document) must be provided.**

## Design Document Expected Format

When using a design document for controller implementation, it should contain:

```markdown
# Design Document: Feature Name

## API Specification
- Group: config.openshift.io
- Version: v1
- Kind: FeatureName

## Reconciliation Workflow
1. Validate spec
2. Create/update ConfigMap with configuration
3. Deploy application Deployment
4. Expose via Service
5. Update status with observed state

## Dependent Resources
- ConfigMap: <name>-config — stores configuration
- Deployment: <name>-deployment — runs the workload
- Service: <name>-service — exposes the workload

## Status Conditions
- Available: true when Deployment is ready
- Progressing: true during reconciliation
- Degraded: true on errors

## Events
- ReconcileComplete: successful reconciliation
- ReconcileFailed: reconciliation error
- Created/Updated: resource lifecycle events

## Cleanup
- External resources: describe what to clean up
- Finalizer: <group>.<resource>-finalizer
```

## Prerequisites

- **gh** (GitHub CLI) — installed and authenticated
- **go** — Go toolchain installed
- **git** — Git installed
- **make** — Make installed
- Must be run from within an OpenShift operator repository
- API types MUST exist (run `/oape:api-generate` first)

## Exit Conditions

- **Success**: Complete controller code generated with summary
- **Failure**: Clear error message with fix instructions
