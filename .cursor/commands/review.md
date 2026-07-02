---
name: /oape:review
id: oape-review
category: OAPE
description: Production-grade OpenShift code reviewer that validates logic, safety, OLM, and build consistency against Jira requirements
argument-hint: <ticket_id> [base_ref]
---

## Name
oape:review

## Synopsis
```shell
/oape:review <ticket_id> [base_ref]
```

## Description

The `oape:review` command performs a "Principal Engineer" level code review. It verifies that the code **actually solves the Jira problem** (Logic) and follows OpenShift safety standards.

The review covers five modules:
- **Golang Logic & Safety**: Intent matching, execution traces, edge cases, context usage, concurrency, error handling, scheme registration, namespace hardcoding, status handling, event recording
- **Bash Scripts**: Safety patterns, variable quoting, temp file handling
- **Operator Metadata (OLM)**: RBAC updates, RBAC three-way consistency, finalizer handling
- **Build Consistency**: Generation drift detection for types and CRDs, dependency completeness
- **Context-Adaptive Review**: Open-ended analysis tailored to the specific PR (owner references, proxy awareness, API deprecation, and other PR-specific concerns)

## Arguments

- `$1` (ticket_id): The Jira Ticket ID (e.g., OCPBUGS-12345). **Required.**
- `$2` (base_ref): The base git ref to diff against. Defaults to `origin/master`. **Optional.**


## Implementation

### Step 1: Determine Base Ref
- If `$2` (base_ref) is provided, use it
- If NOT provided, use `origin/master`

```bash
BASE_REF="${2:-origin/master}"
```

### Step 2: Fetch Context
1. **Jira Issue**: Fetch the Jira issue details using curl:
   ```bash
   curl -s "https://issues.redhat.com/browse/$1"
   ```
   Focus on Acceptance Criteria as the primary validation source.

2. **Git Diff**: Get the code changes:
   ```bash
   git diff ${BASE_REF}...HEAD --stat -p
   ```

3. **File List**: Get list of changed files:
   ```bash
   git diff ${BASE_REF}...HEAD --name-only
   ```

### Step 3: Analyze Code Changes

Apply **all** of the following review criteria. Modules A–D are **mandatory** — every check must be evaluated on every review, regardless of PR size. Module E is an adaptive pass that extends the review based on what the PR actually does.

#### Module A: Golang (Logic & Safety)

**Logic Verification (The "Mental Sandbox")**:
- **Intent Match:** Does the code implementation match the Jira Acceptance Criteria? Quote the Jira line that justifies the change.
- **Execution Trace:** Mentally simulate the function.
    - *Happy Path:* Does it succeed as expected?
    - *Error Path:* If the API fails, does it retry or return an error?
- **Edge Cases:**
    - **Nil/Empty:** Does it handle `nil` pointers or empty slices?
    - **State:** Does it handle resources that are `Deleting` or `Pending`?

**Safety & Patterns**:
- **Context:** REJECT `context.TODO()` in production paths. Must use `context.WithTimeout`.
- **Concurrency:** `go func` must be tracked (WaitGroup/ErrGroup). No race conditions.
- **Errors:** Must use `fmt.Errorf("... %w", err)`. No capitalized error strings.
- **Complexity:** Flag functions > 50 lines or > 3 nesting levels.

**Idiomatic Clean Code (via Golang-Skills):**
- **Slices/Maps:** Ensure slices are pre-allocated with `make` if the length is known. Avoid unnecessary `nil` slice vs. `empty` slice confusion.
- **Interfaces:** Reject "Interface Pollution" (defining interfaces before they are actually used by multiple implementations).
- **Naming:** Follow Go conventions (e.g., `url` not `URL` in mixed-case, `id` not `ID` for local vars, no `Get` prefix).
- **Receiver Types:** Check for consistency in pointer vs. value receivers.

**Scheme Registration** *(Severity: CRITICAL)*:
- For every `client.Get()`, `client.List()`, `client.Create()`, `client.Update()`, `client.Delete()` call in changed Go files, identify the GVK of the object being operated on.
- Read `main.go` or any file matching `*scheme*.go`. Look for `AddToScheme` or `SchemeBuilder.Register` calls.
- Every external type (not in the operator's own API group — e.g., `corev1`, `routev1`, `configv1`) used as a client call target **must** have a corresponding `AddToScheme` in scheme setup.
- Flag any external type used in client calls but missing from scheme registration.

**Namespace Hardcoding** *(Severity: WARNING)*:
- In changed Go files under `controllers/`, `pkg/controller/`, or any reconciler file, scan for string literals matching `"openshift-*"`, `"kube-*"`, or `"default"` used as namespace values.
- These should use constants, environment variables, or config/options structs.
- Ignore test files (`*_test.go`) and string literals inside comments or log messages.

**Status Handling (Infinite Requeue Prevention)** *(Severity: WARNING)*:
- In `Reconcile()` functions, flag patterns where a terminal/validation error causes `return ctrl.Result{}, err` (infinite requeue).
- Terminal failures (spec validation, type assertion, config parsing — NOT API call errors) should instead set a Degraded condition and return `ctrl.Result{}, nil`.

**Event Recording** *(Severity: INFO)*:
- Check if reconciler structs embed or reference a `record.EventRecorder`.
- If the reconciler performs significant state transitions (create, update, delete, degrade) without calling `recorder.Event()` or `recorder.Eventf()`, flag it.

#### Module B: Bash (Scripts)
- **Safety:** Must start with `set -euo pipefail`.
- **Quoting:** Variables in `oc`/`kubectl` commands MUST be quoted (`"$VAR"`).
- **Tmp Files:** Must use `mktemp`, never hardcoded paths like `/tmp/data`.

#### Module C: Operator Metadata (OLM)
- **RBAC:** If new K8s APIs are used in Go, check if `config/rbac/role.yaml` is updated.
- **RBAC Three-Way Consistency** *(Severity: CRITICAL)*:
    - Cross-reference three sources of RBAC truth and flag inconsistencies:
        1. **Kubebuilder markers**: `// +kubebuilder:rbac:groups=...,resources=...,verbs=...` in controller Go files.
        2. **ClusterRole manifest**: `config/rbac/role.yaml` (or `config/rbac/clusterrole.yaml`).
        3. **CSV permissions**: `bundle/manifests/*clusterserviceversion.yaml` — `spec.install.spec.clusterPermissions` and `spec.install.spec.permissions`.
    - All three must declare the same API groups, resources, and verbs.
    - Common drift: marker added but `role.yaml` not regenerated (missing `make manifests`); `role.yaml` updated but CSV not rebuilt (missing `make bundle`).
    - Also verify CSV is updated when API version, description, installModes, or new CRD entries change.
- **Finalizers:** If logic deletes resources, ensure Finalizers are handled to prevent hanging.

#### Module D: Build Consistency (The "Gotchas")
- **Generation Drift:**
    - IF `types.go` is modified, AND `zz_generated.deepcopy.go` is NOT in the file list -> **CRITICAL FAIL**.
    - IF `types.go` is modified, AND `config/crd/bases/...yaml` is NOT in the file list -> **CRITICAL FAIL**.
- **Dependency Completeness** *(Severity: WARNING)*:
    - If changed Go files introduce new import paths, verify they exist in `go.mod` (direct or indirect).
    - If a `vendor/` directory exists and `go.mod` is in the changed file list but `vendor/modules.txt` is not, flag that `go mod vendor` may need to be re-run.
    - Flag any import of a package that does not resolve to a module declared in `go.mod`.

#### Module E: Context-Adaptive Review

After completing the mandatory checks above, perform an open-ended review pass tailored to this specific PR. Analyze what the code **actually does** and flag issues that the checklist does not cover. Focus areas to consider based on the PR content:

- **OwnerReferences / Garbage Collection:** If the PR creates child resources (Deployments, ConfigMaps, Services, etc.) via `client.Create()`, verify that `metav1.OwnerReference` is set so child resources are cleaned up when the parent CR is deleted. *(Severity: CRITICAL)*
- **Proxy / Disconnected Environment:** If the PR makes outbound HTTP calls (`http.Get`, `http.NewRequest`, `http.Client`), verify it respects `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` environment variables. OpenShift clusters behind proxies will fail silently without this. *(Severity: WARNING)*
- **API Deprecation:** If the PR imports API packages, check for deprecated versions (`v1beta1` when `v1` exists, `policy/v1beta1`, `extensions/v1beta1`). *(Severity: WARNING)*
- **Watch Predicates:** If the PR adds or modifies `Watches()`, `For()`, or `Owns()` calls, check if filtering predicates are used to avoid excessive reconciliation. *(Severity: INFO)*
- **Resource Requests/Limits:** If the PR creates Pod specs (Deployments, StatefulSets, Jobs), check if resource requests and limits are set. *(Severity: INFO)*
- **Leader Election Safety:** If the PR modifies cluster-scoped resources or runs background goroutines, verify leader election is configured to prevent split-brain in HA. *(Severity: WARNING)*

These are starting points, not an exhaustive list. Use your judgment as a principal engineer to flag any additional correctness, safety, or operational concern specific to this PR that is not covered by Modules A–D.

Report adaptive findings in the `issues` array using `"module": "Adaptive"` and the appropriate severity level.

### Step 4: Generate Report
Generate a structured JSON report based on the analysis.

### Step 5: Apply Fixes Automatically

After the report is generated, if the `issues` array is non-empty, automatically apply the suggested fixes by following the procedure in `implement-review-fixes.md`, passing the review report produced in Step 4 as input.

This step is skipped when the verdict is `"Approved"` and there are no issues.

## Return Value

Returns a JSON report with the following structure, followed by an automatic fix summary if issues were found:

```json
{
  "summary": {
    "verdict": "Approved | Changes Requested",
    "rating": "1-10",
    "simplicity_score": "1-10"
  },
  "logic_verification": {
    "jira_intent_met": true,
    "missing_edge_cases": ["List handled edge cases or gaps (e.g., 'Does not handle pod deletion')"]
  },
  "issues": [
    {
      "severity": "CRITICAL",
      "module": "Logic",
      "file": "pkg/controller/gather.go",
      "line": 45,
      "description": "Logic Error: Jira asks to 'retry on failure', but code returns 'nil' immediately.",
      "fix_prompt": "Update the error handling to use the retry logic..."
    },
    {
      "severity": "CRITICAL",
      "module": "Logic",
      "file": "pkg/controller/cert_controller.go",
      "line": 112,
      "description": "Scheme Registration: client.Get() targets routev1.Route but routev1.AddToScheme is not called in main.go.",
      "fix_prompt": "Add routev1.AddToScheme(scheme) to the scheme registration block in main.go..."
    },
    {
      "severity": "CRITICAL",
      "module": "OLM",
      "file": "controllers/mycontroller_controller.go",
      "line": 28,
      "description": "RBAC Consistency: kubebuilder marker grants 'get;list;watch' on 'routes' but config/rbac/role.yaml does not include this rule.",
      "fix_prompt": "Run 'make manifests' to regenerate RBAC from kubebuilder markers, then 'make bundle' to update CSV..."
    },
    {
      "severity": "WARNING",
      "module": "Adaptive",
      "file": "pkg/controller/cert_controller.go",
      "line": 87,
      "description": "Proxy Awareness: http.Get() call does not respect HTTP_PROXY/HTTPS_PROXY env vars. Will fail in disconnected clusters.",
      "fix_prompt": "Use net/http.ProxyFromEnvironment in the http.Transport to respect cluster proxy settings..."
    }
  ]
}
```

When issues are present, the fixes are applied automatically and a fix summary is appended (see `implement-review-fixes.md` for the summary format).

## Examples

1. **Review changes against origin/master**:
   ```shell
   /oape:review OCPBUGS-12345
   ```

2. **Review changes against a specific branch**:
   ```shell
   /oape:review OCPBUGS-12345 origin/release-4.15
   ```

3. **Review changes against a specific commit**:
   ```shell
   /oape:review OCPBUGS-12345 abc123def
   ```