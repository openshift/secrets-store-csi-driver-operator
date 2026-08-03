# Secrets Store CSI Driver Operator Testing Guide

This guide covers testing practices specific to the Secrets Store CSI Driver Operator.

**For generic OpenShift testing patterns**, see [Platform Testing Practices](https://github.com/openshift/enhancements/tree/master/ai-docs/).

## Testing Strategy

The operator follows a standard test pyramid:
- **Unit tests** (~60%): Fast, isolated, no external dependencies
- **Integration tests** (~30%): Currently minimal (future: test operator + fake Kubernetes API)
- **E2E tests** (~10%): Full cluster deployment, real resources

**Current state**: Primarily unit tests + E2E tests (integration tests are a future enhancement).

## Unit Tests

### Running Unit Tests

```bash
make test-unit      # Run all unit tests (./pkg/... ./cmd/...)
```

**Test files**: `pkg/operator/starter_test.go`

### Test Structure: Table-Driven Pattern

**Example** (from `starter_test.go:17-72`):

```go
func TestGetOperatorSyncState(t *testing.T) {
    deletionTimestamp := metav1.Now()

    cases := []struct {
        name          string
        operator      *FakeOperator
        expectedState opv1.ManagementState
    }{
        {
            name: "should return managed when the operator state is managed",
            operator: &FakeOperator{
                ObjectMeta: metav1.ObjectMeta{Name: providerName},
                Spec:       opv1.OperatorSpec{ManagementState: opv1.Managed},
            },
            expectedState: opv1.Managed,
        },
        // More cases...
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            operatorClient := v1helpers.NewFakeOperatorClientWithObjectMeta(
                &tc.operator.ObjectMeta,
                &tc.operator.Spec,
                &tc.operator.Status,
                nil,
            )
            state := getOperatorSyncState(operatorClient)
            if state != tc.expectedState {
                t.Fatalf("expected %v, got %v", tc.expectedState, state)
            }
        })
    }
}
```

**Conventions**:
- Use `t.Run(tc.name, ...)` for subtests (clear failure messages)
- Use `t.Fatalf` for assertion failures (stops subtest immediately)
- No assertion libraries (plain `if` checks)

### Test Fakes

**library-go fakes** (NOT third-party mocking frameworks):

```go
import (
    "github.com/openshift/library-go/pkg/operator/v1helpers"
)

operatorClient := v1helpers.NewFakeOperatorClientWithObjectMeta(
    &metav1.ObjectMeta{Name: "test", DeletionTimestamp: &now},
    &opv1.OperatorSpec{ManagementState: opv1.Managed},
    &opv1.OperatorStatus{},
    nil,  // error to return (nil = success)
)
```

**Why fakes over mocks**: Fakes are real implementations with in-memory storage. More reliable than reflection-based mocks.

### What to Test

**Tested** (starter_test.go):
- ✅ `getOperatorSyncState` management state transitions
- ✅ `DeletionTimestamp != nil` → `Removed` mapping
- ✅ Error handling (API errors → `Unmanaged`)

**Not tested** (future enhancement):
- Controller reconciliation loops (would require fake Kubernetes API server)
- Asset loading (tested implicitly via E2E)
- Image substitution (tested implicitly via E2E)

### Adding New Unit Tests

**When to add**:
- New logic in `pkg/operator/` (e.g., new helper functions)
- Changes to `getOperatorSyncState` behavior
- New extractor functions (`extractOperatorSpec`, `extractOperatorStatus`)

**Pattern** (add to `starter_test.go` or new `*_test.go` file):

```go
func TestNewFeature(t *testing.T) {
    cases := []struct {
        name     string
        input    InputType
        expected OutputType
    }{
        {name: "case 1", input: ..., expected: ...},
        {name: "case 2", input: ..., expected: ...},
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            result := NewFeature(tc.input)
            if result != tc.expected {
                t.Fatalf("expected %v, got %v", tc.expected, result)
            }
        })
    }
}
```

## E2E Tests

### Running E2E Tests

```bash
make test-e2e       # Runs hack/e2e.sh
```

**Requirements**:
- OpenShift cluster (4.12+)
- `oc` CLI authenticated to cluster
- Operator deployed (via OLM or manual YAML apply)

**Script**: `hack/e2e.sh`

### What E2E Tests Cover

1. **Operator deployment** - OLM installs operator, pod starts
2. **DaemonSet reconciliation** - DaemonSet created on all Linux nodes
3. **Management state transitions**:
   - `Managed` → DaemonSet running
   - `Unmanaged` → Operator stops reconciling
   - `Removed` → Resources deleted
4. **Resource cleanup** - CR deletion triggers full cleanup

**Note**: E2E tests are **cluster-specific** (require proper OLM catalog, image pull credentials). Not expected to pass locally.

### E2E Test Structure

**Location**: `hack/e2e.sh` (shell script, not Go test)

**Pattern**:
```bash
# 1. Deploy operator
oc create -f config/manifests/stable/

# 2. Wait for DaemonSet
oc wait --for=condition=Ready daemonset/secrets-store-csi-driver-node -n openshift-cluster-csi-drivers --timeout=5m

# 3. Test management state
oc patch clustercsidrivers/secrets-store.csi.k8s.io --type=merge -p '{"spec":{"managementState":"Unmanaged"}}'
# Verify DaemonSet not updated...

# 4. Cleanup
oc delete clustercsidrivers/secrets-store.csi.k8s.io
```

### Adding New E2E Tests

**When to add**:
- New operator features (e.g., new static resources)
- Management state behavior changes
- Integration with new OpenShift subsystems (proxy, trusted CA)

**Pattern** (add to `hack/e2e.sh`):
```bash
echo "Testing new feature..."
oc create -f test-manifests/new-feature.yaml
oc wait --for=condition=... resource/name --timeout=2m
# Verify behavior
oc delete -f test-manifests/new-feature.yaml
```

## Component-Specific Test Scenarios

### Scenario: Management State Transitions

**Goal**: Verify operator honors Managed/Unmanaged/Removed states.

**Unit test** (starter_test.go):
```go
{
    name: "should return removed when the operator state is removed",
    operator: &FakeOperator{
        Spec: opv1.OperatorSpec{ManagementState: opv1.Removed},
    },
    expectedState: opv1.Removed,
}
```

**E2E test** (hack/e2e.sh):
```bash
# Set to Removed
oc patch clustercsidrivers/secrets-store.csi.k8s.io --type=merge -p '{"spec":{"managementState":"Removed"}}'

# Verify DaemonSet deleted
oc wait --for=delete daemonset/secrets-store-csi-driver-node -n openshift-cluster-csi-drivers --timeout=2m
```

### Scenario: DeletionTimestamp Triggers Cleanup

**Goal**: Verify CR deletion cleans up resources.

**Unit test** (starter_test.go:51-60):
```go
{
    name: "should return removed when the deletion timestamp is set",
    operator: &FakeOperator{
        ObjectMeta: metav1.ObjectMeta{
            Name:              providerName,
            DeletionTimestamp: &deletionTimestamp,
        },
        Spec: opv1.OperatorSpec{ManagementState: opv1.Managed},
    },
    expectedState: opv1.Removed,
}
```

**E2E test** (hack/e2e.sh):
```bash
# Delete CR
oc delete clustercsidrivers/secrets-store.csi.k8s.io

# Verify resources cleaned up
oc wait --for=delete daemonset/secrets-store-csi-driver-node -n openshift-cluster-csi-drivers --timeout=2m
oc get clusterrole secrets-store-privileged-role 2>&1 | grep "NotFound"  # Should fail
```

### Scenario: Embedded Asset Loading

**Goal**: Verify operator panics on missing embedded asset (build-time bug detection).

**Unit test** (not directly testable — panic is intentional):
- Indirectly tested: If `//go:embed` is wrong, operator build succeeds but pod crash-loops.
- E2E test catches this: Operator pod would never become Ready.

**Manual test**:
1. Remove a file from `assets/` (e.g., delete `node.yaml`)
2. Build: `make build` (succeeds — Go compiler doesn't validate embed at build time)
3. Run: `./secrets-store-csi-driver-operator start --kubeconfig=...`
4. Expected: Panic with `open node.yaml: file does not exist`

### Scenario: CA Bundle Injection

**Goal**: Verify trusted CA ConfigMap is mounted into DaemonSet.

**E2E test** (manual verification):
```bash
# Check ConfigMap exists
oc get configmap -n openshift-cluster-csi-drivers secrets-store-csi-driver-trusted-ca-bundle

# Check DaemonSet volume mount
oc get daemonset -n openshift-cluster-csi-drivers secrets-store-csi-driver-node -o yaml | grep -A5 "ca-bundle"
```

**Expected**: Volume mount at `/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem`.

## CI Integration

### Prow Jobs

**CI system**: OpenShift Prow (configuration in `openshift/release` repo, not this repo)

**Jobs**:
- `pull-ci-...-verify` - Runs `make verify`
- `pull-ci-...-unit` - Runs `make test-unit`
- `pull-ci-...-e2e` - Runs `make test-e2e` on real cluster

**FIPS enforcement**: CI builds use `GOEXPERIMENT=strictfipsruntime`. Local non-FIPS builds fail CI.

### Pre-Submit Checklist

Before opening a PR, run locally:

```bash
make verify         # go vet, gofmt, verify-deps
make test-unit      # unit tests
```

**Do NOT run `make test-e2e` locally** — it requires cluster-specific setup. Let CI handle it.

## Test Coverage

### Current Coverage (pkg/operator/)

- ✅ `getOperatorSyncState` - 100% (all branches covered by table-driven tests)
- ✅ `extractOperatorSpec` - Indirectly via E2E
- ✅ `extractOperatorStatus` - Indirectly via E2E
- ❌ `RunOperator` - Not unit-tested (controller setup is integration-tested in E2E)

### Coverage Gaps (Future Enhancements)

1. **Integration tests** - Test operator with fake Kubernetes API server (library-go provides test fixtures)
2. **Asset substitution tests** - Unit test `replaceNamespaceFunc` (currently only E2E-tested)
3. **Error handling paths** - Test API error scenarios (e.g., informer failures, apply failures)

## Common Testing Mistakes

1. **Using third-party mocking frameworks** (e.g., gomock, testify/mock)  
   ❌ Wrong: `mock := NewMockOperatorClient(ctrl)`  
   ✅ Correct: `v1helpers.NewFakeOperatorClientWithObjectMeta(...)`

2. **Not using table-driven tests**  
   ❌ Wrong: One test function per case (`TestManagedState`, `TestUnmanagedState`, ...)  
   ✅ Correct: One test function with table of cases (`TestGetOperatorSyncState`)

3. **Using `t.Error` instead of `t.Fatal` for assertion failures**  
   ❌ Wrong: `t.Errorf("expected %v, got %v", ...)` (test continues, confusing output)  
   ✅ Correct: `t.Fatalf("expected %v, got %v", ...)` (stops immediately)

4. **Expecting E2E tests to pass locally**  
   ❌ Wrong: Debugging E2E failures locally without cluster setup  
   ✅ Correct: Run E2E in CI or on a properly configured cluster

5. **Not testing error paths**  
   ❌ Wrong: Only testing happy path (`ManagementState=Managed`)  
   ✅ Correct: Test errors too (`operatorClient.GetOperatorState()` returns error → expect `Unmanaged`)

## Debugging Test Failures

### Unit Test Failures

**Symptom**: `make test-unit` fails

**Debug**:
```bash
go test -v ./pkg/operator/...  # Verbose output
go test -run TestGetOperatorSyncState/should_return_removed ./pkg/operator/  # Run specific subtest
```

**Common causes**:
- Logic change in `getOperatorSyncState` without updating test expectations
- New `ManagementState` enum value not covered in test cases

### E2E Test Failures

**Symptom**: `make test-e2e` fails in CI

**Debug**:
1. Check Prow job logs (link in GitHub PR checks)
2. Look for operator pod logs:
   ```
   oc logs -n openshift-cluster-csi-drivers deployment/secrets-store-csi-driver-operator
   ```
3. Check DaemonSet status:
   ```
   oc get daemonset -n openshift-cluster-csi-drivers secrets-store-csi-driver-node -o yaml
   ```
4. Check ClusterCSIDriver status:
   ```
   oc get clustercsidrivers/secrets-store.csi.k8s.io -o yaml
   ```

**Common causes**:
- Image pull failures (wrong image reference in CSV)
- RBAC missing (new permission not added to ClusterRole)
- Asset missing (forgot to update `//go:embed`)

## References

- [Testing Guidelines](./guidelines/testing-guidelines.md) - Component-specific test conventions
- [Component Architecture](./architecture/components.md) - What to test
- [Error Handling Guidelines](./guidelines/error-handling-guidelines.md) - Error path testing
- [Platform Testing Practices](https://github.com/openshift/enhancements/tree/master/ai-docs/) - Cross-repo patterns
