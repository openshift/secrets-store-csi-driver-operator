# SecretProviderClassPodStatus

**API Group**: `secrets-store.csi.x-k8s.io/v1`  
**Kind**: `SecretProviderClassPodStatus`  
**Scope**: Namespaced

**API Definition**: [CRD](../../../config/manifests/stable/secrets-store.csi.x-k8s.io_secretproviderclasspodstatuses.yaml)  
**Upstream Documentation**: [secrets-store-csi-driver](https://secrets-store-csi-driver.sigs.k8s.io/)

## Purpose

Tracks the mount status of a SecretProviderClass for a specific pod. Created automatically by the CSI driver when a pod mounts a volume backed by a SecretProviderClass. Used for observability and cleanup.

**Key Principle**: Internal status resource managed entirely by the CSI driver. Users should NOT create, update, or delete these resources manually.

## Status Structure

```go
type SecretProviderClassPodStatusStatus struct {
    PodName                  string                          // Name of the pod that mounted the volume
    SecretProviderClassName  string                          // Name of the SecretProviderClass used
    TargetPath               string                          // Host path where secrets are mounted
    Mounted                  bool                            // Whether the volume is currently mounted
    Objects                  []SecretProviderClassObject     // List of objects fetched from the provider
}

type SecretProviderClassObject struct {
    ID      string                                           // Object identifier from the provider
    Version string                                           // Object version (provider-specific)
}
```

## Key Concepts

### Automatic Lifecycle

The CSI driver manages SecretProviderClassPodStatus resources automatically:

1. **Creation**: When a pod's CSI volume is mounted, the driver creates a SecretProviderClassPodStatus in the pod's namespace.
2. **Update**: The `objects` field is populated with metadata returned by the provider during mount.
3. **Deletion**: When the pod is deleted, the CSI driver's cleanup logic removes the SecretProviderClassPodStatus.

**Naming**: The resource name follows the pattern `<pod-name>-<namespace>-<secretproviderclass-name>`.

### Mount State Tracking

The `mounted` field indicates whether the volume is currently mounted:
- `true` - Volume successfully mounted, secrets available to the pod
- `false` - Volume mount failed or has been unmounted

### Provider Object Metadata

The `objects` array contains metadata about secrets fetched from the external store. Each entry includes:
- `id` - Provider-specific object identifier (e.g., secret name, resource ID)
- `version` - Provider-specific version (e.g., version number, timestamp)

**Purpose**: Enables observability into which secrets were mounted and their versions. Useful for:
- Auditing which secrets a pod accessed
- Debugging mount failures
- Tracking secret rotation

## Lifecycle

1. **Creation**: CSI driver creates when `NodePublishVolume` is called (pod starts and kubelet requests volume mount).
2. **Population**: CSI driver populates `objects` after successfully fetching secrets from the provider.
3. **Update**: CSI driver may update `objects` if secret rotation occurs and new versions are fetched.
4. **Deletion**: CSI driver deletes when `NodeUnpublishVolume` is called (pod is deleted or volume is unmounted).

## Example

```yaml
apiVersion: secrets-store.csi.x-k8s.io/v1
kind: SecretProviderClassPodStatus
metadata:
  name: my-app-pod-default-azure-vault-example
  namespace: default
status:
  podName: my-app-pod
  secretProviderClassName: azure-vault-example
  targetPath: /var/lib/kubelet/pods/abc-123/volumes/kubernetes.io~csi/secrets-store-inline/mount
  mounted: true
  objects:
    - id: db-password
      version: "1234567890"
```

**Interpretation**: Pod `my-app-pod` successfully mounted SecretProviderClass `azure-vault-example`. The provider returned one object (`db-password` version `1234567890`).

## Component-Specific Behavior

### RBAC Requirements

The CSI driver node SA requires full access to SecretProviderClassPodStatus resources:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: secretproviderclasses-role
rules:
  - apiGroups: ["secrets-store.csi.x-k8s.io"]
    resources: ["secretproviderclasspodstatuses"]
    verbs: ["create", "update", "patch", "delete", "get", "list"]
```

Configured in assets/rbac/secretproviderclasses_role.yaml.

### Observability

Users can inspect SecretProviderClassPodStatus for debugging:

```bash
# List all mount statuses in a namespace
oc get secretproviderclasspodstatuses -n my-app

# Check mount status for a specific pod
oc get secretproviderclasspodstatuses -n my-app my-app-pod-my-app-azure-vault-example -o yaml
```

**Common debugging patterns**:
- `mounted: false` - Volume mount failed; check pod events and CSI driver logs
- Empty `objects` array - Provider returned no secrets; check provider logs and SecretProviderClass parameters
- Missing resource - Pod never attempted mount, or CSI driver failed before creating status

### Cleanup Behavior

SecretProviderClassPodStatus resources are deleted when:
- Pod is deleted (normal cleanup)
- Volume is unmounted before pod deletion (rare)

**Orphaned resources**: If the CSI driver crashes during cleanup, SecretProviderClassPodStatus resources may be orphaned. The CSI driver does NOT have a garbage collection loop. Users must manually delete orphaned resources or implement custom cleanup.

### Version API

The resource supports both v1 and v1alpha1 API versions:
- `v1` (storage version, served: true) - Current API
- `v1alpha1` (served: true, deprecated: true) - Legacy API with deprecation warning

Always use v1. v1alpha1 support may be removed in future releases.

## Common Mistakes

1. **Manually creating/deleting SecretProviderClassPodStatus** - The CSI driver owns these resources. Manual changes are overwritten or cause reconciliation issues.

2. **Using SecretProviderClassPodStatus for pod health checks** - This resource tracks CSI mount state, not pod readiness. A `mounted: true` status does NOT mean the pod is healthy.

3. **Expecting immediate deletion** - SecretProviderClassPodStatus is deleted asynchronously when the pod terminates. A brief delay is normal.

4. **Relying on `objects` for secret content** - The `objects` field contains metadata only (ID and version), not secret values. Secret content is in the mounted volume files.

5. **Cross-namespace queries** - SecretProviderClassPodStatus is namespace-scoped. Use `-A` flag to list across all namespaces: `oc get secretproviderclasspodstatuses -A`.

## Related Concepts

- [SecretProviderClass](./secretproviderclass.md) - Volume specification
