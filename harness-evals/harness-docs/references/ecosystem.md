# Platform Ecosystem References

This document links to generic OpenShift/Kubernetes patterns in the Platform ecosystem hub. The component inherits these platform-wide patterns and practices.

## Operator Patterns

**Location**: [openshift/enhancements/ai-docs/platform/operators/](https://github.com/openshift/enhancements/tree/master/ai-docs/platform/operators/)

- **Controller Runtime**: Reconciliation loops, event handling, client patterns
- **Status Conditions**: Available, Progressing, Degraded condition semantics
- **Management State**: Managed, Unmanaged, Removed lifecycle
- **Removable Operators**: Cleanup patterns for optional operators
- **RBAC**: Service account and permissions patterns

**Component Usage**:
- This operator uses library-go CSI controller framework (method chaining pattern)
- Implements removable operator pattern (see [ADR-0001](../decisions/adr-0001-removable-operator.md))
- Uses `ClusterCSIDriver` (operator.openshift.io/v1) as the primary CR

## Testing Practices

**Location**: [openshift/enhancements/ai-docs/platform/testing/](https://github.com/openshift/enhancements/tree/master/ai-docs/platform/testing/)

- **Test Pyramid**: Unit > Integration > E2E ratio
- **E2E Framework**: OpenShift E2E test patterns
- **Table-Driven Tests**: Go testing conventions

**Component Usage**:
- See [SECRETS_STORE_TESTING.md](../SECRETS_STORE_TESTING.md) for component-specific test suites
- Unit tests use library-go fakes (no third-party mocking frameworks)
- E2E tests run via `hack/e2e.sh` against live clusters

## Security Practices

**Location**: [openshift/enhancements/ai-docs/platform/security/](https://github.com/openshift/enhancements/tree/master/ai-docs/platform/security/)

- **RBAC Guidelines**: Role and ClusterRole design
- **SCC (Security Context Constraints)**: OpenShift security policies
- **TLS/Certificate Management**: Certificate rotation and trust

**Component Usage**:
- DaemonSet requires `privileged` SCC (host path mounts for CSI driver socket)
- Uses OpenShift's trusted CA bundle injection for provider TLS verification
- RBAC documented in `assets/rbac/` manifests

## Reliability Practices

**Location**: [openshift/enhancements/ai-docs/platform/reliability/](https://github.com/openshift/enhancements/tree/master/ai-docs/platform/reliability/)

- **SLO Framework**: Service Level Objectives
- **Observability**: Metrics, logging, tracing patterns
- **Resource Limits**: CPU/memory request/limit guidelines

**Component Usage**:
- DaemonSet exposes metrics on port 8095 (Prometheus scraping)
- NetworkPolicy allows ingress to metrics endpoint
- Resource requests: 50Mi memory, 10m CPU per sidecar (see `assets/node.yaml`)

## Kubernetes Fundamentals

**Location**: [openshift/enhancements/ai-docs/platform/kubernetes/](https://github.com/openshift/enhancements/tree/master/ai-docs/platform/kubernetes/)

- **CSI (Container Storage Interface)**: CSI driver architecture and lifecycle
- **DaemonSet**: Node-level workload patterns
- **CRDs**: CustomResourceDefinition design patterns

**Component Usage**:
- Implements CSI driver pattern (DaemonSet on every Linux node)
- Manages `SecretProviderClass` and `SecretProviderClassPodStatus` CRDs (defined upstream, bundled in OLM manifests)
- See [domain/](../domain/) for component-specific CRD documentation

## OpenShift Fundamentals

**Location**: [openshift/enhancements/ai-docs/platform/openshift/](https://github.com/openshift/enhancements/tree/master/ai-docs/platform/openshift/)

- **ClusterCSIDriver**: OpenShift CSI driver lifecycle API
- **OLM (Operator Lifecycle Manager)**: Operator packaging and versioning
- **Config Observers**: Watching cluster-wide configuration (Proxy, Infrastructure, APIServer)

**Component Usage**:
- Watches `ClusterCSIDriver` resource named `secrets-store.csi.k8s.io`
- Packaged as OLM bundle (see `config/manifests/`)
- Uses `CSIConfigObserverController` to propagate cluster config to operand

## Cross-Repository ADRs

**Location**: [openshift/enhancements/ai-docs/platform/decisions/](https://github.com/openshift/enhancements/tree/master/ai-docs/platform/decisions/)

Platform-wide architectural decisions (examples):
- **etcd Backend**: Kubernetes state storage
- **CVO Orchestration**: Cluster Version Operator upgrade patterns
- **Immutable Nodes**: RHCOS + rpm-ostree rationale

**Component-Specific ADRs**: See [decisions/](../decisions/) for this operator's architectural decisions.

## Library-go Framework

**Location**: [github.com/openshift/library-go](https://github.com/openshift/library-go)

- **CSI Controller Set**: `pkg/operator/csi/csicontrollerset` - composable CSI operator controllers
- **Resource Apply**: `pkg/operator/resource/resourceapply` - strategic merge apply helpers
- **Generic Operator Client**: `pkg/operator/genericoperatorclient` - unstructured CR client wrapper
- **v1helpers**: `pkg/operator/v1helpers` - informer scoping utilities

**Component Usage**:
- This operator is built entirely on library-go primitives (no controller-runtime)
- See [architecture/components.md](../architecture/components.md) for framework integration details

---

**Note**: These links point to Platform ecosystem hub documentation. Component-specific patterns and decisions are documented in the `harness-evals/harness-docs/` directory of this repository.

**Last Updated**: 2026-07-29
