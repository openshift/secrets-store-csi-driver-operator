# ADR-0001: Removable Operator with Conditional Resource Lifecycle

**Status**: Accepted  
**Date**: 2025-09-12 (inferred from implementation)  
**Deciders**: OpenShift Storage Team  
**Component**: Secrets Store CSI Driver Operator

## Context

The Secrets Store CSI Driver is an optional storage feature that users may install and uninstall as needed. Unlike core platform operators (e.g., kube-apiserver-operator), this operator must support clean removal without leaving orphaned resources or manual cleanup steps.

The operator must handle three management states:
1. **Managed** - Normal operation, resources actively reconciled
2. **Unmanaged** - Resources exist but operator doesn't touch them (for debugging/migration)
3. **Removed** - Explicit signal to delete all managed resources

Additionally, when the ClusterCSIDriver CR is deleted (DeletionTimestamp set), the operator must clean up before allowing finalizer removal.

**Scope**: This ADR is component-specific. For cross-repo operator lifecycle patterns, see [Platform ADRs](https://github.com/openshift/enhancements/tree/master/ai-docs/platform/decisions/).

## Decision

Mark the operator as **removable** (`removable=true` in `WithManagementStateController`) and treat both `ManagementState=Removed` AND `DeletionTimestamp != nil` as removal signals.

**Implementation** (pkg/operator/starter.go:77, 150-169):
```go
.WithManagementStateController(operandName, removable=true)

func getOperatorSyncState(operatorClient) opv1.ManagementState {
    if opSpec.ManagementState == opv1.Removed {
        return opv1.Removed
    }
    if meta.DeletionTimestamp != nil {
        return opv1.Removed  // Treat deletion as removal
    }
    return opSpec.ManagementState
}
```

Conditional resources (RBAC, ServiceAccount, CSIDriver, ConfigMap, NetworkPolicy) are deleted when `getOperatorSyncState()` returns `Removed` via the `unsyncPredicate` callback.

## Rationale

**Why:** Optional operators must support clean uninstall to avoid cluster resource bloat and to meet OpenShift's "no manual cleanup" principle. The Secrets Store CSI Driver has no persistent data (secrets are ephemeral from external stores), so full removal is safe.

**Why DeletionTimestamp = Removed:** Users expect that deleting the ClusterCSIDriver CR triggers full cleanup, not just operator pod termination. Without this mapping, deletion would leave DaemonSet + RBAC orphaned.

**How to apply:** When adding new managed resources:
1. If resource is **temporary** (only needed while operator is active), add to `WithConditionalStaticResourcesController` file list → auto-deleted on removal.
2. If resource is **persistent** (CRDs, user-created SecretProviderClass), do NOT add to conditional controller → users manage lifecycle.

## Consequences

### Positive
- Users can cleanly uninstall the operator by setting `ManagementState=Removed` OR deleting the ClusterCSIDriver CR
- No orphaned DaemonSets, RBAC, or NetworkPolicies after removal
- Meets OpenShift's operator removal requirements

### Negative
- **Deletion is immediate** - No grace period for pods using mounted secrets. Pods will fail to unmount volumes if CSI driver is deleted first. (Mitigation: Document that users must delete workload pods before removing the operator.)
- **No recovery from accidental removal** - Setting `ManagementState=Removed` triggers immediate resource deletion. Users cannot rollback mid-deletion. (Mitigation: Kubernetes garbage collection prevents re-creation conflicts.)

### Neutral
- CRDs (SecretProviderClass, SecretProviderClassPodStatus) are NOT deleted on removal (owned by OLM, not the operator's conditional controller)
- User-created SecretProviderClass resources persist after operator removal

## Alternatives Considered

### Alternative 1: Non-Removable Operator
**Description**: Set `removable=false`, require manual resource cleanup after operator deletion.  
**Rejected because**: Violates OpenShift operator best practices. Users expect `oc delete clustercsidrivers/secrets-store.csi.k8s.io` to clean up all managed resources.

### Alternative 2: Finalizer-Based Cleanup
**Description**: Add a finalizer to ClusterCSIDriver, block deletion until all resources are removed.  
**Rejected because**: library-go's `ManagementStateController` already provides removal logic. Adding a custom finalizer duplicates framework behavior and increases complexity.

### Alternative 3: Ignore DeletionTimestamp
**Description**: Only honor `ManagementState=Removed`, ignore DeletionTimestamp.  
**Rejected because**: Users expect CR deletion to trigger cleanup. Ignoring DeletionTimestamp creates confusion ("I deleted the CR, why is the DaemonSet still running?").

## References

- [pkg/operator/starter.go:77](../../../pkg/operator/starter.go) - `removable=true` registration
- [pkg/operator/starter.go:150-169](../../../pkg/operator/starter.go) - `getOperatorSyncState()` implementation
- [Management State Controller Documentation](https://github.com/openshift/library-go/tree/master/pkg/operator/managementstate)
- [Platform ADRs](https://github.com/openshift/enhancements/tree/master/ai-docs/platform/decisions/) for cross-repo operator patterns
