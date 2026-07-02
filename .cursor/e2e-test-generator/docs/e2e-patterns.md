# Generic E2E Test Patterns for OpenShift Operators

This document describes common e2e test patterns across OpenShift operator repositories so the plugin can discover and generate compatible e2e tests for any operator.

## Framework Detection

| Framework | go.mod Indicator | Code Pattern | E2E Style |
|---|---|---|---|
| **controller-runtime** | `sigs.k8s.io/controller-runtime` | `Reconcile(ctx, req) (Result, error)` | Ginkgo v2 Go tests |
| **library-go** | `github.com/openshift/library-go` (without controller-runtime) | `sync(ctx, syncCtx) error` | Bash scripts with `oc` commands |

## Controller-Runtime E2E Structure

Typical file layout for controller-runtime based operators:

```
test/e2e/
  e2e_suite_test.go     # Suite setup: kubeconfig, scheme registration, clients
  e2e_test.go           # Main e2e specs: Describe/Context/It
  utils/
    constants.go        # Namespace, deployment names, label selectors, timeouts
    utils.go            # Helpers: WaitFor*, condition checkers, resource getters
```

### Conventions

- **Package**: `e2e`
- **Framework**: Ginkgo v2 (`Describe`, `Context`, `It`, `BeforeAll`, `BeforeEach`, `By`, `DeferCleanup`, `Eventually`)
- **Clients** (set in suite): `k8sClient` (controller-runtime), `clientset` (kubernetes.Interface), optionally `apiextClient`, `configClient`
- **Per-test context**: `testCtx` from `context.WithTimeout` in `BeforeEach`, with `DeferCleanup(cancel)`
- **Constants**: Use `utils.*` (e.g., `utils.OperatorNamespace`, `utils.DefaultTimeout`)
- **Assertions**: Gomega (`Expect`, `Eventually`, `Consistently`, `HaveLen`, `BeTrue`, etc.)

### Suite Setup Pattern

```go
var (
    k8sClient  client.Client
    clientset  kubernetes.Interface
    testCtx    context.Context
)

func TestE2E(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "E2E Suite")
}

var _ = BeforeSuite(func() {
    cfg, err := config.GetConfig()
    Expect(err).NotTo(HaveOccurred())

    scheme := runtime.NewScheme()
    // Register scheme types...

    k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
    Expect(err).NotTo(HaveOccurred())

    clientset, err = kubernetes.NewForConfig(cfg)
    Expect(err).NotTo(HaveOccurred())
})
```

## Library-Go E2E Structure

Typical file layout for library-go based operators:

```
test/e2e/
  e2e_test.sh           # Main e2e script (or hack/e2e.sh)
  framework/
    helpers.sh           # Optional helper functions
```

Or commonly:

```
hack/
  e2e.sh                # E2E test script
```

### Conventions

- **Shell**: `#!/usr/bin/env bash` with `set -euo pipefail`
- **Test structure**: Functions named `test_<scenario>()`
- **Assertions**: `oc` commands with exit code checks, `grep`, `jq` for JSON parsing
- **Timeouts**: `oc wait --for=condition=... --timeout=...`
- **Cleanup**: `trap` or explicit cleanup function at end
- **Namespace**: Typically uses a fixed operator namespace or creates a test namespace

### Bash Test Pattern

```bash
#!/usr/bin/env bash
set -euo pipefail

OPERATOR_NAMESPACE="${OPERATOR_NAMESPACE:-openshift-cluster-csi-drivers}"
TEST_NAMESPACE="e2e-test-$(head -c 4 /dev/urandom | xxd -p)"
TIMEOUT="120s"

setup() {
    oc create namespace "$TEST_NAMESPACE"
    # Apply test resources...
}

test_cr_lifecycle() {
    oc apply -f config/samples/sample-cr.yaml -n "$TEST_NAMESPACE"
    oc wait --for=condition=Ready <kind>/<name> -n "$TEST_NAMESPACE" --timeout="$TIMEOUT"
    # Verify expected state...
}

cleanup() {
    oc delete namespace "$TEST_NAMESPACE" --ignore-not-found
}

trap cleanup EXIT
setup
test_cr_lifecycle
echo "All e2e tests passed"
```

## Common Patterns (Both Frameworks)

### OLM Install Verification

```bash
# Check CRDs are established
oc wait --for=condition=Established crd/<crd-name> --timeout=60s

# Check CSV is succeeded
oc wait --for=jsonpath='{.status.phase}'=Succeeded csv -l <label> -n <namespace> --timeout=300s

# Check operator deployment is available
oc wait --for=condition=Available deployment/<name> -n <namespace> --timeout=300s
```

### CR Lifecycle Testing

1. **Create**: Apply CR from sample or inline YAML
2. **Verify conditions**: `oc wait --for=condition=Ready` or poll via client
3. **Update**: Patch CR fields, verify propagation to backing workloads
4. **Delete**: Remove CR, verify cleanup of managed resources

### Condition Polling (Ginkgo)

```go
Eventually(func(g Gomega) {
    cr := &v1alpha1.MyResource{}
    err := k8sClient.Get(testCtx, client.ObjectKey{Name: "cluster", Namespace: ns}, cr)
    g.Expect(err).NotTo(HaveOccurred())
    g.Expect(cr.Status.Conditions).To(ContainElement(
        HaveField("Type", Equal("Ready")),
    ))
}, utils.DefaultTimeout, 5*time.Second).Should(Succeed())
```

### Cleanup Ordering

Always clean up in reverse dependency order:
1. Operand/user CRs (reverse creation order)
2. Operator subscription / CSV
3. OperatorGroup
4. Namespace

### Pod Recovery Testing

```go
// Delete operator pod
pods := &corev1.PodList{}
k8sClient.List(testCtx, pods, client.InNamespace(ns), client.MatchingLabels{...})
for _, pod := range pods.Items {
    k8sClient.Delete(testCtx, &pod)
}
// Wait for new pod to be ready
Eventually(func(g Gomega) {
    dep := &appsv1.Deployment{}
    k8sClient.Get(testCtx, client.ObjectKey{Name: depName, Namespace: ns}, dep)
    g.Expect(dep.Status.AvailableReplicas).To(Equal(int32(1)))
}, timeout, poll).Should(Succeed())
```

## Discovery Checklist

When analyzing an operator repo, discover these in order:

| # | Target | Where to Look | What to Extract |
|---|--------|--------------|-----------------|
| 1 | Framework | `go.mod` | controller-runtime or library-go |
| 2 | API types | `api/**/*_types.go`, `types_*.go` | Kind, group, version, fields, conditions |
| 3 | CRDs | `config/crd/**/*.yaml`, `config/manifests/**/*.yaml` | Kind, group, plural, scope |
| 4 | Existing e2e | `test/e2e/`, `hack/e2e.sh` | Framework (Ginkgo/bash), package, imports, clients, helpers |
| 5 | Install mechanism | `config/manifests/`, `bundle/`, `deploy/` | OLM (Subscription, CSV) or manual (Deployment) |
| 6 | Samples | `config/samples/`, `examples/` | Sample CR manifests with default values |
| 7 | Namespace | e2e constants, deploy manifests, CSV | Operator namespace |
| 8 | Controllers | `*controller*.go`, `*reconcile*.go`, `pkg/operator/` | Reconciliation targets, managed resources |
