# Architecture: Components and Implementation Patterns

This document describes the Secrets Store CSI Driver Operator's internal architecture, repo layout, and verified implementation patterns.

## Repository Layout

```text
cmd/
└── secrets-store-csi-driver-operator/
    └── main.go                          # Entry point; creates cobra command + controller config

pkg/
├── operator/
│   ├── starter.go                       # RunOperator: creates clients, informers, CSI controller set
│   └── starter_test.go                  # Unit tests for getOperatorSyncState logic
├── version/
│   └── version.go                       # Build version info (ldflags); Prometheus gauge registration
└── dependencymagnet/
    └── dependencymagnet.go              # Build-tag-guarded import for build-machinery-go

assets/                                  # Embedded YAML manifests (go:embed)
├── assets.go                            # embed.FS + ReadFile wrapper
├── node.yaml                            # DaemonSet: CSI driver + sidecars
├── node_sa.yaml                         # ServiceAccount for DaemonSet
├── csidriver.yaml                       # CSIDriver resource
├── cabundle_cm.yaml                     # ConfigMap for trusted CA bundle
├── rbac/
│   ├── privileged_role.yaml             # ClusterRole: use privileged SCC
│   ├── node_privileged_binding.yaml     # ClusterRoleBinding: SA → privileged role
│   ├── secretproviderclasses_role.yaml  # ClusterRole: CRUD SecretProviderClass + PodStatus
│   └── secretproviderclasses_binding.yaml  # ClusterRoleBinding: SA → SecretProviderClass role
└── network-policy/
    └── allow-ingress-to-metrics-operand.yaml  # NetworkPolicy: allow metrics scraping

config/
├── manifests/
│   ├── secrets-store-csi-driver-operator.package.yaml  # OLM package manifest
│   ├── art.yaml                         # ART version substitution rules
│   └── stable/
│       ├── secrets-store-csi-driver-operator.clusterserviceversion.yaml  # OLM CSV
│       ├── secrets-store.csi.x-k8s.io_secretproviderclasses.yaml         # CRD
│       ├── secrets-store.csi.x-k8s.io_secretproviderclasspodstatuses.yaml  # CRD
│       ├── image-references             # ImageStream for release payload
│       └── sscsi-sample-*.yaml          # Sample SecretProviderClass manifests
└── metadata/
    └── annotations.yaml                 # OLM bundle metadata

hack/
├── e2e.sh                               # E2E test script (requires cluster)
├── update-metadata.sh                   # Bump OCP version across CSV, Makefile, README
└── create-bundle                        # Build OLM bundle + index images

Dockerfile.openshift                     # Multi-stage build for operator image
Dockerfile.mustgather                    # must-gather image
must-gather/gather                       # must-gather collection script
```

## Operator Startup Sequence

### 1. Entry Point (cmd/secrets-store-csi-driver-operator/main.go)

```text
main()
  └── NewOperatorCommand()  # Create cobra root command
      └── controllercmd.NewControllerCommandConfig(...).NewCommand()
          ├── operatorName: "secrets-store-csi-driver-operator"
          ├── version: version.Get()  # From ldflags
          ├── runFunc: operator.RunOperator
          └── clock: clock.RealClock{}
```

**Pattern**: Standard library-go operator pattern. No business logic in main.go — only CLI wiring.

### 2. RunOperator (pkg/operator/starter.go:40)

Creates clients, informers, and the CSI controller set:

```text
RunOperator(ctx, controllerConfig)
  ├── Create core clients
  │   ├── kubeClient (kubernetes.Clientset)
  │   ├── configClient (config.openshift.io)
  │   └── dynamicClient (dynamic.Interface)
  │
  ├── Create informers (scoped to operator namespace + cluster scope)
  │   ├── kubeInformersForNamespaces (operator namespace + "")
  │   ├── configInformers (cluster-scoped config.openshift.io)
  │   └── dynamicInformers (ClusterCSIDriver unstructured)
  │
  ├── Create GenericOperatorClient
  │   ├── GVR: operator.openshift.io/v1/clustercsidrivers
  │   ├── GVK: ClusterCSIDriver
  │   ├── configName: "secrets-store.csi.k8s.io"
  │   ├── extractOperatorSpec (convert unstructured → OperatorSpecApplyConfiguration)
  │   └── extractOperatorStatus (convert unstructured → OperatorStatusApplyConfiguration)
  │
  └── Build CSI controller set (method chaining)
      ├── NewCSIControllerSet(operatorClient, eventRecorder)
      ├── .WithLogLevelController()
      ├── .WithManagementStateController(operandName, true)  # true = removable
      ├── .WithConditionalStaticResourcesController(...)  # Static YAML assets
      ├── .WithCSIConfigObserverController(...)           # Cluster config observer
      └── .WithCSIDriverNodeService(...)                  # DaemonSet + CA bundle hook
```

## Controller Framework

### library-go CSI Controller Set

The operator uses `library-go/pkg/operator/csi/csicontrollerset` to compose 5 sub-controllers via method chaining. **All controllers share the same operator client and event recorder.**

| Controller                               | Purpose                                                                       | File List                    | Sync Condition                                 |
| ---------------------------------------- | ----------------------------------------------------------------------------- | ---------------------------- | ---------------------------------------------- |
| **LogLevelController**                   | Syncs log level from ClusterCSIDriver spec to operator                        | N/A                          | Always                                         |
| **ManagementStateController**            | Handles Managed/Unmanaged/Removed lifecycle                                   | N/A                          | Always                                         |
| **ConditionalStaticResourcesController** | Reconciles static YAML assets (RBAC, SA, CSIDriver, ConfigMap, NetworkPolicy) | See "Static Resources" below | `syncPredicate()` returns true (Managed state) |
| **CSIConfigObserverController**          | Observes cluster config (infrastructure, proxy, apiserver)                    | N/A                          | Always                                         |
| **CSIDriverNodeService**                 | Manages DaemonSet with CA bundle injection                                    | `node.yaml`                  | Managed state (implicit in library-go)         |

### Static Resources (ConditionalStaticResourcesController)

**Registered files** (pkg/operator/starter.go:85-93):
1. `node_sa.yaml` - ServiceAccount for DaemonSet
2. `csidriver.yaml` - CSIDriver resource (cluster-scoped)
3. `cabundle_cm.yaml` - ConfigMap for trusted CA bundle
4. `rbac/privileged_role.yaml` - ClusterRole for privileged SCC
5. `rbac/node_privileged_binding.yaml` - ClusterRoleBinding
6. `rbac/secretproviderclasses_role.yaml` - ClusterRole for SecretProviderClass CRUD
7. `rbac/secretproviderclasses_binding.yaml` - ClusterRoleBinding
8. `network-policy/allow-ingress-to-metrics-operand.yaml` - NetworkPolicy

**Sync predicates** (pkg/operator/starter.go:95-100):
- `syncPredicate`: Resources are **created/updated** when `getOperatorSyncState()` returns `Managed`
- `unsyncPredicate`: Resources are **deleted** when `getOperatorSyncState()` returns `Removed`

**Apply method**: `resourceapply` (library-go strategic merge) — NOT Server-Side Apply (SSA).

**Resource loading**: All files loaded via `replaceNamespaceFunc(operatorNamespace)` which:
1. Calls `assets.ReadFile(name)` (reads from embedded FS)
2. Replaces `${NAMESPACE}` with actual operator namespace

## Management State Logic

### getOperatorSyncState (pkg/operator/starter.go:150)

Determines whether to sync, skip, or delete conditional resources:

```go
func getOperatorSyncState(operatorClient) opv1.ManagementState {
    opSpec, _, _, err := operatorClient.GetOperatorState()
    if err != nil {
        klog.Errorf("Failed to get operator state: %v", err)
        return opv1.Unmanaged  // Fail-closed: skip sync on error
    }
    
    if opSpec.ManagementState != opv1.Managed {
        return opSpec.ManagementState  // Unmanaged or Removed
    }
    
    meta, err := operatorClient.GetObjectMeta()
    if err != nil {
        klog.Errorf("Failed to get operator object meta: %v", err)
        return opv1.Unmanaged  // Fail-closed
    }
    
    // Deletion timestamp treated as Removed (operator is removable)
    if management.IsOperatorRemovable() && meta.DeletionTimestamp != nil {
        klog.Infof("Operator deletion timestamp is set, removing conditional resources")
        return opv1.Removed
    }
    
    return opv1.Managed
}
```

**State transitions**:
- `Managed` → Sync conditional resources
- `Unmanaged` → Skip sync (fail-closed on errors)
- `Removed` OR `DeletionTimestamp != nil` → Delete conditional resources

**Removability**: Operator is marked removable (`management.IsOperatorRemovable()` returns true) because `WithManagementStateController` is called with `true` as its second (removable) argument (pkg/operator/starter.go:77-78).

## DaemonSet Management

### CSIDriverNodeService Controller

**Manifest**: `assets/node.yaml`

**Image substitution** (runtime environment variables):
- `${DRIVER_IMAGE}` → `DRIVER_IMAGE` env var (set by OLM from CSV)
- `${NODE_DRIVER_REGISTRAR_IMAGE}` → `NODE_DRIVER_REGISTRAR_IMAGE`
- `${LIVENESS_PROBE_IMAGE}` → `LIVENESS_PROBE_IMAGE`
- `${LOG_LEVEL}` → `LOG_LEVEL` (operator's log level)

**CA bundle injection** (pkg/operator/starter.go:111-115):
```go
csidrivernodeservicecontroller.WithCABundleDaemonSetHook(
    operatorNamespace,
    trustedCAConfigMap,  // "secrets-store-csi-driver-trusted-ca-bundle"
    configMapInformer,
)
```

**Hook behavior**: Mounts the trusted CA bundle ConfigMap into the DaemonSet at `/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem`. The library-go hook watches the ConfigMap and triggers DaemonSet update on changes.

**Update strategy** (assets/node.yaml:10-13):
```yaml
updateStrategy:
  type: RollingUpdate
  rollingUpdate:
    maxUnavailable: 10%
```

**Apply method**: library-go DaemonSet controller (not `resourceapply`) — uses strategic merge.

## Informer Scoping

**Namespace-scoped informers** (pkg/operator/starter.go:45):
```go
kubeInformersForNamespaces := v1helpers.NewKubeInformersForNamespaces(
    kubeClient,
    operatorNamespace,  // openshift-cluster-csi-drivers: scoped via WithNamespace()
    "",                 // unscoped factory, used here only for cluster-scoped resources
)
```

**Pattern**: The `operatorNamespace` entry is a namespace-scoped factory (`informers.WithNamespace`); the `""` entry is an unscoped factory that this operator only queries for cluster-scoped listers (ClusterRoles, CSIDriver, Nodes). **DO NOT request namespaced resource listers (Pods, Secrets, etc.) from the `""` factory** — that would cache them across every namespace in the cluster, which is the performance anti-pattern this scoping is meant to avoid.

**ConfigMap informer** (pkg/operator/starter.go:46):
```go
configMapInformer := kubeInformersForNamespaces.InformersFor(operatorNamespace).Core().V1().ConfigMaps()
```

Used by CA bundle hook to watch the trusted CA ConfigMap.

## Operator Client Pattern

### GenericOperatorClient (pkg/operator/starter.go:53-66)

```go
gvr := opv1.SchemeGroupVersion.WithResource("clustercsidrivers")
gvk := opv1.SchemeGroupVersion.WithKind("ClusterCSIDriver")

operatorClient, dynamicInformers, err := goc.NewClusterScopedOperatorClientWithConfigName(
    clock.RealClock{},
    controllerConfig.KubeConfig,
    gvr,
    gvk,
    providerName,  // "secrets-store.csi.k8s.io"
    extractOperatorSpec,
    extractOperatorStatus,
)
```

**Config name**: `providerName = "secrets-store.csi.k8s.io"` (pkg/operator/starter.go:35) — must match the ClusterCSIDriver resource name.

**Extractor functions** (pkg/operator/starter.go:173-201):
- `extractOperatorSpec`: Converts unstructured ClusterCSIDriver → OperatorSpecApplyConfiguration
- `extractOperatorStatus`: Converts unstructured ClusterCSIDriver → OperatorStatusApplyConfiguration

**Error handling**: Both extractors wrap errors with `fmt.Errorf(..., %w)` for context.

## Asset Management

### Embedded Assets (assets/assets.go:7-8)

```go
//go:embed *.yaml rbac/*.yaml network-policy/*.yaml
var f embed.FS
```

**CRITICAL**: When adding files to new subdirectories under `assets/`, update this directive. Missing glob → runtime panic in `ReadFile`.

### Namespace Substitution (pkg/operator/starter.go:131)

```go
func replaceNamespaceFunc(namespace string) resourceapply.AssetFunc {
    return func(name string) ([]byte, error) {
        content, err := assets.ReadFile(name)
        if err != nil {
            panic(err)  // Fail fast: missing asset is a build-time bug
        }
        return bytes.ReplaceAll(content, []byte(namespaceKey), []byte(namespace)), nil
    }
}
```

**Pattern**: Byte-level replacement of `${NAMESPACE}` (defined as const `namespaceKey` at pkg/operator/starter.go:36).

**Panic policy**: Missing embedded asset is treated as a build-time bug → panic is acceptable here. DO NOT panic in reconciliation loops.

## Image Resolution & OLM Integration

### CSV Environment Variables (config/manifests/stable/*.clusterserviceversion.yaml)

OLM injects images into the operator pod environment:

```yaml
env:
  - name: DRIVER_IMAGE
    value: quay.io/openshift/origin-secrets-store-csi-driver:latest
  - name: NODE_DRIVER_REGISTRAR_IMAGE
    value: quay.io/openshift/origin-csi-node-driver-registrar:latest
  - name: LIVENESS_PROBE_IMAGE
    value: quay.io/openshift/origin-csi-livenessprobe:latest
  - name: OPERATOR_NAME
    value: secrets-store-csi-driver-operator
```

**Runtime flow**:
1. OLM sets env vars on operator pod (values substituted from `relatedImages` via ART)
2. Operator reads env vars (library-go DaemonSet controller handles this internally)
3. library-go substitutes `${DRIVER_IMAGE}` etc. in `assets/node.yaml` at apply time

**CSV update checklist** (when adding new sidecars):
1. Add env var to CSV deployment spec
2. Add variable substitution in `assets/node.yaml`
3. Add to `relatedImages` list in CSV (for disconnected install)
4. Update ART rules in `config/manifests/art.yaml`

## Error Handling Patterns

### Error Wrapping (pkg/operator/starter.go:173-201)

```go
if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, castObj); err != nil {
    return nil, fmt.Errorf("unable to convert to ClusterCSIDriver: %w", err)
}
```

**Pattern**: Always wrap errors with context using `fmt.Errorf(..., %w)`. Error messages start with lowercase verb.

### Fail-Closed on Errors (pkg/operator/starter.go:150)

```go
if err != nil {
    klog.Errorf("Failed to get operator state: %v", err)
    return opv1.Unmanaged  // Fail-closed: do not sync if state is unknown
}
```

**Pattern**: When operator state is unknown (API errors), return `Unmanaged` to avoid syncing resources in an uncertain state.

## Testing Patterns

### Table-Driven Tests (pkg/operator/starter_test.go)

```go
cases := []struct {
    name          string
    operator      *FakeOperator
    expectedState opv1.ManagementState
}{
    {name: "...", operator: &FakeOperator{...}, expectedState: opv1.Managed},
    // ...
}

for _, tc := range cases {
    t.Run(tc.name, func(t *testing.T) {
        operatorClient := v1helpers.NewFakeOperatorClientWithObjectMeta(...)
        state := getOperatorSyncState(operatorClient)
        if state != tc.expectedState {
            t.Fatalf("expected %v, got %v", tc.expectedState, state)
        }
    })
}
```

**Pattern**: Use library-go fakes (`NewFakeOperatorClientWithObjectMeta`). No third-party mocking. Standard `if` checks + `t.Fatalf`.

## OpenShift Integrations

### Cluster Config Observer

The `CSIConfigObserverController` watches OpenShift cluster config resources:
- `config.openshift.io/v1/Infrastructure` - Cluster platform info
- `config.openshift.io/v1/Proxy` - Cluster-wide proxy settings
- `config.openshift.io/v1/APIServer` - API server TLS/auth config

**Purpose**: Observes cluster-level config and writes it to the `ClusterCSIDriver` status's `observedConfig` field. This operator does not currently read `observedConfig` to change the CSI driver operand (DaemonSet/assets) — the controller is wired in for future use.

### Trusted CA Bundle

The operator injects the cluster's trusted CA bundle into the DaemonSet:
1. OLM creates a ConfigMap labeled `config.openshift.io/inject-trusted-cabundle: "true"`
2. Cluster CA operator populates the ConfigMap with the cluster CA bundle
3. Operator's CA bundle hook mounts it into the DaemonSet

**ConfigMap name**: `secrets-store-csi-driver-trusted-ca-bundle` (pkg/operator/starter.go:34)  
**Mount path**: `/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem` (configured in library-go hook)

## Implementation Anti-Patterns

### DO NOT: Create all-namespace informers

```go
// WRONG
kubeInformers := kubeinformers.NewSharedInformerFactory(kubeClient, resync)
```

**Why**: Watches ALL namespaces → excessive memory + API load.

**Correct**: Use `NewKubeInformersForNamespaces` with explicit namespace list (pkg/operator/starter.go:45).

### DO NOT: Mix static and dynamic resource controllers

```go
// WRONG
.WithConditionalStaticResourcesController(..., []string{"node.yaml", ...})
```

**Why**: DaemonSet requires CA bundle hook and image substitution logic → use `WithCSIDriverNodeService`.

**Correct**: Static assets go to `WithConditionalStaticResourcesController`. DaemonSet goes to `WithCSIDriverNodeService` (pkg/operator/starter.go:79-116).

### DO NOT: Hardcode namespace in assets

```yaml
# WRONG
namespace: openshift-cluster-csi-drivers
```

**Why**: Breaks deployments in non-default namespaces (e.g., dev/test).

**Correct**: Use `${NAMESPACE}` placeholder (replaced at load time by `replaceNamespaceFunc`).

### DO NOT: Panic in reconciliation loops

```go
// WRONG (in controller Sync method)
if err != nil {
    panic(err)
}
```

**Why**: Crash-loops the operator pod → cluster-wide CSI driver outage.

**Correct**: Return errors from Sync methods. Library-go framework retries automatically. Panic is acceptable ONLY for build-time bugs (missing embedded assets).

---

**Source of Truth**: This document describes patterns observed in the code as of 2026-07-29. For current implementation, always verify by reading the source files referenced.
