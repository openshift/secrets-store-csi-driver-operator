---
name: /oape:api-generate-tests
id: oape-api-generate-tests
category: OAPE
description: Generate integration test suites (.testsuite.yaml) for OpenShift API type definitions
argument-hint: <path-to-types-file-or-api-directory>
---

## Name
oape:api-generate-tests

## Synopsis
```shell
/oape:api-generate-tests <path/to/types_file.go or path/to/api/directory>
```

## Description
The `oape:api-generate-tests` command generates `.testsuite.yaml` integration test files for
OpenShift API type definitions. It reads the Go type definitions, CRD manifests, and validation
markers to produce comprehensive test suites covering create, update, validation, and error
scenarios.

The generated tests use the YAML-based test suite format consumed by the envtest-based integration
test runner (Ginkgo + controller-runtime envtest).

**This command should be run AFTER API types and CRD manifests have been generated.**

## Skills

Read and follow **effective-go** (`.cursor/skills/effective-go/SKILL.md`) when generating or reviewing test-related artifacts.

## Implementation

### Phase 0: Prechecks

All prechecks must pass before proceeding. If ANY precheck fails, STOP immediately and report.

#### Precheck 1 — Verify Repository and Tools

```bash
if ! git rev-parse --is-inside-work-tree &> /dev/null 2>&1; then
  echo "PRECHECK FAILED: Not inside a git repository."
  exit 1
fi

REPO_ROOT=$(git rev-parse --show-toplevel)

if [ ! -f "$REPO_ROOT/go.mod" ]; then
  echo "PRECHECK FAILED: No go.mod found at repository root."
  exit 1
fi

GO_MODULE=$(head -1 "$REPO_ROOT/go.mod" | awk '{print $2}')
echo "Repository root: $REPO_ROOT"
echo "Go module: $GO_MODULE"
```

#### Precheck 2 — Identify Target API Types

The user MUST provide a path to a types file or API directory. If no argument is provided, STOP
and ask the user to specify one.

```bash
TARGET_PATH="$ARGUMENTS"

if [ -z "$TARGET_PATH" ]; then
  echo "PRECHECK FAILED: No target path provided."
  echo "Usage: /oape:api-generate-tests <path-to-types-file-or-api-directory>"
  exit 1
fi
```

```thinking
I need to determine which API types to generate tests for:
1. User provided a specific types file path → use it directly
2. User provided an API directory → find all types files in it

Once I have the target types file(s), I need to extract:
- The API group, version, kind, and resource (plural)
- All fields with their types, validation markers, and godoc
- Whether this is a new CRD or modifications to existing types
```

#### Precheck 3 — Verify CRD Manifests Exist

The test suite references CRD manifests. Verify they have been generated:

```bash
# For openshift/api repos: check zz_generated.crd-manifests/
find "$REPO_ROOT" -type d -name 'zz_generated.crd-manifests' -not -path '*/vendor/*' | head -5

# For operator repos: check config/crd/bases/
find "$REPO_ROOT" -type d -name 'bases' -path '*/crd/*' -not -path '*/vendor/*' | head -5
```

If no CRD manifests are found, warn the user:
```
WARNING: No CRD manifests found. Run 'make update' or 'make manifests' first to generate CRDs.
Test suites reference CRD manifests — tests will fail without them.
```

---

### Phase 1: Read API Types and CRD Manifests

Read the target Go types file(s) and extract all information needed for test generation:

```thinking
From the Go types, I need to extract every field, type, marker, and validation rule that could
produce a testable scenario. This includes but is not limited to:
1. Top-level CRD types (structs with +kubebuilder:object:root=true or +genclient)
2. For each CRD type: kind, API group, version, resource plural, scope, singleton constraints
3. Every spec and status field: name, type, optional/required, pointer semantics
4. All validation markers: enums, min/max, minLength/maxLength, minItems/maxItems, pattern,
   format, XValidation CEL rules, exclusiveMinimum/Maximum
5. Enum types and their allowed values
6. Discriminated unions: discriminator field, member types, required members per discriminator
7. Immutable fields (XValidation rules referencing oldSelf)
8. Default values and defaulting behavior
9. Feature gate annotations (+openshift:enable:FeatureGate)
10. Nested object validation (embedded structs, list item validation)
11. Map key/value constraints
12. Any other kubebuilder or OpenShift marker that implies validation behavior

I must read the FULL set of markers on every field — the list above is guidance, not exhaustive.
If a marker or annotation exists that I haven't seen before, I should still extract it and
generate appropriate test cases for it.
```

Also read the corresponding CRD manifest(s) to get:
- The full CRD name (`<plural>.<group>`)
- The OpenAPI v3 schema (for understanding the full validation tree)
- Feature set annotations (Default, TechPreviewNoUpgrade, etc.)

### Phase 2: Identify Test Directory and Existing Tests

Determine where test files should be placed based on the repository layout:

**openshift/api:**
```text
<group>/<version>/tests/<plural>.<group>/
```

**Operator repos:**
```text
api/<version>/tests/<plural>.<group>/
```
or
```text
api/<group>/<version>/tests/<plural>.<group>/
```

Check for existing test files in the target directory. If tests already exist, read them to
understand the existing coverage and avoid duplicating tests.

### Phase 3: Generate Test Suites

Generate `.testsuite.yaml` files covering the following categories. For each category, derive
the specific test cases from the types and validation rules read in Phase 1.

#### Category 1 — Minimal Valid Create

Every test suite MUST include at least one test that creates a minimal valid instance of the
resource with only required fields populated.

#### Category 2 — Valid Field Values

For each field in the spec:
- Test that valid values are accepted and persisted correctly
- For enum fields: test each allowed enum value
- For optional fields: test that the resource is valid both with and without the field
- For fields with defaults: verify the default is applied correctly

#### Category 3 — Invalid Field Values (Validation Failures)

For each field with validation rules:
- Enum fields: test a value not in the allowed set → `expectedError`
- Pattern fields: test a value that doesn't match the regex → `expectedError`
- Min/max constraints: test values at and beyond boundaries → `expectedError`
- Required fields: test omission → `expectedError`
- CEL validation rules: test inputs that violate each rule → `expectedError`

#### Category 4 — Update Scenarios

For fields that can be updated:
- Test valid updates (change field value) → `expected`
- For immutable fields: test that updates are rejected → `expectedError`
- For fields with update-specific validation: test boundary cases

#### Category 5 — Singleton Name Validation

If the CRD is a cluster-scoped singleton (name must be "cluster"):
- Test creation with `resourceName: cluster` → success
- Test creation with `resourceName: not-cluster` → `expectedError`

#### Category 6 — Discriminated Unions

If the type uses discriminated unions:
- Test each valid discriminator + corresponding member combination → `expected`
- Test mismatched discriminator + member → `expectedError`
- Test missing required member for a given discriminator → `expectedError`

#### Category 7 — Feature-Gated Fields

If fields are gated behind a FeatureGate:
- In the stable/default test suite: test that setting the gated field is rejected → `expectedError`
- In the techpreview test suite (if applicable): test that the gated field is accepted → `expected`

#### Category 8 — Status Subresource

If the type has a status subresource:
- Test valid status updates
- Test invalid status updates → `expectedStatusError`

#### Category 9 — Additional Coverage

```thinking
I have generated tests for the 8 standard categories above. Now I must re-examine every marker,
annotation, CEL rule, godoc comment, and structural detail extracted in Phase 1 and ask: is there
any validation behavior or edge case NOT already covered by Categories 1–8?

Examples of scenarios that may fall outside the standard categories:
- Cross-field dependencies (e.g., field B is required only when field A is set)
- Mutually exclusive fields outside of formal discriminated unions
- Nested object validation (deeply embedded structs with their own constraints)
- List item uniqueness constraints (listType=map, listMapKey)
- Map key or value constraints
- String format validations (IP, CIDR, DNS, URI, etc.)
- Complex CEL rules spanning multiple fields
- Defaulting interactions (e.g., a default on one field affecting validation of another)
- Metadata constraints (labels, annotations, finalizers if enforced)
- Edge cases around zero values vs nil for pointer fields
- Any custom OpenShift markers or annotations not covered above

For each uncovered scenario I find, I will generate appropriate test cases.
If everything is already covered, I will note that no additional tests are needed.
```

Generate test cases for any validation behavior, marker, or edge case discovered in the types
that does not fit neatly into Categories 1–8. This is a catch-all to ensure no testable scenario
is missed.

### Phase 4: Write Test Suite Files

Write the `.testsuite.yaml` file(s) following this format:

```yaml
apiVersion: apiextensions.k8s.io/v1
name: "<DisplayName>"
crdName: <plural>.<group>
tests:
  onCreate:
    - name: Should be able to create a minimal <Kind>
      initial: |
        apiVersion: <group>/<version>
        kind: <Kind>
        spec: {}
      expected: |
        apiVersion: <group>/<version>
        kind: <Kind>
        spec: {}
    - name: Should reject <Kind> with invalid <fieldName>
      initial: |
        apiVersion: <group>/<version>
        kind: <Kind>
        spec:
          <fieldName>: <invalidValue>
      expectedError: "<expected error substring>"
  onUpdate:
    - name: Should not allow changing immutable field <fieldName>
      initial: |
        apiVersion: <group>/<version>
        kind: <Kind>
        spec:
          <fieldName>: <value1>
      updated: |
        apiVersion: <group>/<version>
        kind: <Kind>
        spec:
          <fieldName>: <value2>
      expectedError: "<expected error substring>"
```

#### File Naming Conventions

Derive file names from existing patterns in the repository:

**openshift/api repos:**
- `stable.<kind>.testsuite.yaml` — tests for the default/stable CRD
- `techpreview.<kind>.testsuite.yaml` — tests for TechPreview-gated fields
- `stable.<kind>.<context>.testsuite.yaml` — platform-specific or scenario-specific tests

**Operator repos:**
- `<kind>.testsuite.yaml` — single test suite per kind (when no feature gating)

If the repo uses feature-gated CRD manifests (`zz_generated.featuregated-crd-manifests/`),
ensure each CRD variant has a corresponding test file.

### Phase 5: Output Summary

```text
=== API Test Generation Summary ===

Target API: <group>/<version> <Kind>
CRD Name: <plural>.<group>

Generated Test Files:
  - <path/to/testsuite.yaml> — <number of onCreate tests>, <number of onUpdate tests>

Test Coverage:
  onCreate:
    - Minimal valid create
    - <field>: valid values (<count> tests)
    - <field>: invalid values (<count> tests)
    - Singleton name validation
    - ...
  onUpdate:
    - Immutable field <field>: rejected
    - Valid update for <field>
    - ...

Next Steps:
  1. Review the generated test suites for correctness
  2. Run the integration tests
  3. Verify test coverage: make verify (runs hack/verify-integration-tests.sh)
  4. Add additional edge-case tests as needed
```

---

## Behavioral Rules

1. **Derive from source**: All test values, error messages, and validation expectations MUST be
   derived from the actual Go types, validation markers, and CRD manifests — never hardcode
   assumed validation behavior.
2. **Match existing style**: If the repo already has test suites, match their naming, formatting,
   and level of detail exactly.
3. **Comprehensive but focused**: Generate tests for every field and validation rule found in the
   types, but don't invent scenarios not supported by the schema.
4. **Error messages**: For `expectedError` fields, use substrings from the actual CRD validation
   rules. Read the CRD OpenAPI schema to determine the exact error message format.
5. **Minimal YAML**: In test `initial`/`expected`/`updated` blocks, include only the fields
   relevant to that specific test case. Don't include unrelated fields.
6. **Surgical additions**: When adding tests to an existing suite file, preserve all existing
   tests and only append new ones for newly added fields or types.

## Arguments

- `<path>`: Required path to a types file or API directory
  - Examples:
    - `api/v1alpha1/myresource_types.go`
    - `api/v1alpha1/`
    - `config/v1/types_infrastructure.go`

## Prerequisites

- API type definitions must already exist (run `/oape:api-generate` first if needed)
- CRD manifests should be generated (`make update` or `make manifests`)
- Must be run from within an OpenShift operator repository

## Exit Conditions

- **Success**: Test suite files generated with a coverage summary
- **Failure Scenarios**:
  - Not inside a valid repository
  - No API types found at the specified path
  - No CRD manifests found (warning, not fatal)
  - Cannot determine CRD name or resource plural from the types
