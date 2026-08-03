# ADR-0003: Namespace-Scoped Informers

**Status**: Accepted  
**Date**: 2025-09-12 (inferred from implementation)  
**Deciders**: OpenShift Storage Team  
**Component**: Secrets Store CSI Driver Operator

## Context

Kubernetes informers watch API resources and maintain a local cache for efficient reconciliation. Informer scope determines which resources are cached:
- **All-namespace informers**: Watch every resource of a type across ALL namespaces (e.g., all Pods in the cluster)
- **Single-namespace informers**: Watch resources in ONE namespace (e.g., Pods in `openshift-cluster-csi-drivers`)
- **Multi-namespace informers**: Watch resources in a SPECIFIED LIST of namespaces

The Secrets Store CSI Driver Operator needs:
1. Namespace-scoped resources in `openshift-cluster-csi-drivers` (DaemonSet, ServiceAccount, ConfigMaps)
2. Cluster-scoped resources (ClusterCSIDriver, CSIDriver, ClusterRoles)

**Problem**: Using all-namespace informers would cache every Pod, ConfigMap, and Secret in the cluster → excessive memory usage and API load for resources the operator never uses.

**Scope**: This ADR is component-specific. For cross-repo informer patterns, see [Platform Performance Guidelines](https://github.com/openshift/enhancements/tree/master/ai-docs/platform/performance/).

## Decision

Use `v1helpers.NewKubeInformersForNamespaces` with an explicit namespace list: `[operatorNamespace, ""]`.

**Implementation** (pkg/operator/starter.go:45):
```go
kubeInformersForNamespaces := v1helpers.NewKubeInformersForNamespaces(
    kubeClient,
    operatorNamespace,  // "openshift-cluster-csi-drivers"
    "",                 // Cluster-scoped resources (no namespace filter)
)
```

**Behavior**:
- `operatorNamespace` → watches namespace-scoped resources in `openshift-cluster-csi-drivers`
- `""` → watches cluster-scoped resources (ClusterRoles, CSIDriver, nodes, etc.)
- All other namespaces are **ignored**

## Rationale

**Why:** All-namespace informers cause memory bloat and unnecessary API server load. For example:
- A 1000-node cluster may have 10,000+ Pods. Caching all Pods costs ~50-100MB RAM.
- This operator only needs Pods in `openshift-cluster-csi-drivers` (the DaemonSet pods).
- Caching 9,900+ irrelevant Pods wastes memory and CPU (reflector updates, cache indexing).

**Why `""` (cluster-scoped):** Some resources (CSIDriver, ClusterCSIDriver, nodes) have no namespace. The empty string `""` is library-go's convention for cluster-scoped resources.

**How to apply:** When creating new informers in this operator:
1. **DO** use `NewKubeInformersForNamespaces(client, operatorNamespace, "")`
2. **DO NOT** use `kubeinformers.NewSharedInformerFactory(client, resync)` (watches all namespaces)
3. If a new controller needs resources from a different namespace (rare), add that namespace to the list: `NewKubeInformersForNamespaces(client, operatorNamespace, otherNamespace, "")`

## Consequences

### Positive
- **Reduced memory usage** - Operator only caches resources it actually uses (~5-10MB vs ~100MB for all-namespace)
- **Reduced API load** - Fewer watch connections, less network traffic, less etcd load
- **Faster startup** - Smaller cache to populate during informer sync

### Negative
- **Cannot watch resources in other namespaces** - If a future feature needs to watch user workloads in arbitrary namespaces, this pattern won't work. (Mitigation: For user workloads, use label selectors or field selectors, not namespace scoping.)
- **Slightly more complex setup** - Developers must explicitly list namespaces instead of defaulting to "all".

### Neutral
- Does not affect controller logic (controllers receive the same events, just filtered to relevant namespaces)

## Alternatives Considered

### Alternative 1: All-Namespace Informers
**Description**: Use `kubeinformers.NewSharedInformerFactory(client, resync)` to watch all namespaces.  
**Rejected because**:
- Wastes memory and API bandwidth on resources the operator never uses
- Violates OpenShift operator performance guidelines
- No benefit (operator does not manage resources outside its own namespace)

### Alternative 2: Dynamic Namespace Discovery
**Description**: Watch all namespaces at startup, dynamically add informers for namespaces containing SecretProviderClass resources.  
**Rejected because**:
- SecretProviderClass is user-managed (not owned by the operator), so watching them is unnecessary
- The operator only needs to watch its own operand (DaemonSet in operator namespace), not user workloads
- Adds complexity (dynamic informer registration, cache invalidation) for no gain

### Alternative 3: Single-Namespace Informers Only
**Description**: Watch only `operatorNamespace`, skip cluster-scoped resources.  
**Rejected because**:
- Operator needs cluster-scoped informers for ClusterCSIDriver (the CR it reconciles)
- Library-go CSI framework requires config informers (Infrastructure, Proxy, APIServer) which are cluster-scoped

## References

- [pkg/operator/starter.go:45](../../../pkg/operator/starter.go) - Informer creation
- [library-go v1helpers.NewKubeInformersForNamespaces](https://github.com/openshift/library-go/blob/master/pkg/operator/v1helpers/helpers.go)
- [Performance Guidelines](../guidelines/performance-guidelines.md) - Component-specific performance patterns
- [Platform Performance Guidelines](https://github.com/openshift/enhancements/tree/master/ai-docs/platform/performance/) - Cross-repo informer best practices
