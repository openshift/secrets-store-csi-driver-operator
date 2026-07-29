# ADR-0002: Panic on Missing Embedded Assets

**Status**: Accepted  
**Date**: 2025-09-12 (inferred from implementation)  
**Deciders**: OpenShift Storage Team  
**Component**: Secrets Store CSI Driver Operator

## Context

The operator embeds YAML manifests (DaemonSet, RBAC, ServiceAccount, etc.) into the binary using Go's `embed` package. At runtime, these assets are loaded via `assets.ReadFile(name)` and applied to the cluster.

**Possible failure modes**:
1. **Build-time**: Asset file missing from `assets/` directory → embed fails → build error (caught by CI)
2. **Build-time**: Asset file exists but not covered by `//go:embed` directive → compiles successfully, but `ReadFile` returns error at runtime
3. **Runtime**: Asset file exists in embed but has invalid YAML → apply fails → reconciliation error

The question: How should the operator handle missing embedded assets at runtime (failure mode #2)?

**Scope**: This ADR is component-specific. For cross-repo error handling patterns, see [Platform ADRs](https://github.com/openshift/enhancements/tree/master/ai-docs/platform/decisions/).

## Decision

**Panic immediately** when `assets.ReadFile()` fails in the asset loading function (`replaceNamespaceFunc`).

**Implementation** (pkg/operator/starter.go:131-139):
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

**Why panic is acceptable here:**
- Missing embedded asset is a **build-time bug**, not a transient runtime error
- This code path is executed during controller setup (before reconciliation starts)
- Panic causes operator pod to crash-loop → immediate visibility in OLM and cluster monitoring
- The error cannot be recovered without redeploying a fixed operator binary

## Rationale

**Why:** A missing embedded asset means the operator cannot fulfill its contract (e.g., cannot create the DaemonSet because `node.yaml` is missing). Continuing with degraded state would:
1. Produce confusing partial deployments (some resources created, others missing)
2. Hide the bug behind "Available=False" status (less visible than crash-loop)
3. Waste cluster resources retrying an unrecoverable error

**Why panic vs return error:** Returning an error from `replaceNamespaceFunc` would propagate to `resourceapply` → logged as "sync failed" → operator pod stays running but stuck in retry loop. Panic makes the failure **loud** (crash-loop) → forces immediate attention.

**How to apply:**
- **DO panic** in build-time invariant violations (missing embedded asset, malformed constants)
- **DO NOT panic** in reconciliation loops (API errors, invalid user input, transient failures) → return errors, let framework retry

## Consequences

### Positive
- **Fail-fast detection** - Missing asset causes immediate crash-loop, visible in `oc get pods -n openshift-cluster-csi-drivers`
- **Clear root cause** - Panic message includes asset name: `panic: open node.yaml: file does not exist`
- **Prevents partial state** - Operator never reaches reconciliation if assets are missing → no orphaned resources

### Negative
- **Operator unavailable during bug** - Crash-loop means no reconciliation, no status updates, no cleanup. Cluster's CSI driver state is frozen.
- **Requires redeployment** - Cannot be fixed at runtime (e.g., via ConfigMap patch). Must rebuild and redeploy operator image.

### Neutral
- Only affects build-time bugs (e.g., developer adds asset file but forgets to update `//go:embed`). Does not affect runtime errors (API failures, YAML schema changes).

## Alternatives Considered

### Alternative 1: Return Error from replaceNamespaceFunc
**Description**: Return error from `ReadFile`, propagate to `resourceapply`, set `Available=False` status.  
**Rejected because**:
- Hides build-time bug behind generic "sync failed" status
- Operator pod stays running but stuck (less visible than crash-loop)
- No actionable signal for cluster admins (status message: "failed to sync resources")

### Alternative 2: Pre-Validate Assets at Startup
**Description**: Call `ReadFile` for all registered assets during `RunOperator` startup, panic if any missing.  
**Rejected because**:
- Duplicates validation (assets are loaded lazily by library-go controllers anyway)
- Adds startup latency
- Same outcome (panic) with extra code

### Alternative 3: Degrade to Default Manifest
**Description**: If embedded asset is missing, fall back to a default manifest (e.g., minimal DaemonSet).  
**Rejected because**:
- Masks build-time bugs → operator appears healthy but deploys wrong configuration
- Violates principle of least surprise (user expects operator to deploy `assets/node.yaml`, not a fallback)
- Increases complexity (must maintain both embedded assets and fallback manifests)

## References

- [pkg/operator/starter.go:131-139](../../pkg/operator/starter.go) - `replaceNamespaceFunc` implementation
- [assets/assets.go:7-13](../../assets/assets.go) - `//go:embed` directive and `ReadFile`
- [Error Handling Guidelines](../guidelines/error-handling-guidelines.md) - Component conventions for error vs panic
- [Platform ADRs](https://github.com/openshift/enhancements/tree/master/ai-docs/platform/decisions/) for cross-repo error handling patterns
