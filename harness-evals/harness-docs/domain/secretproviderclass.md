# SecretProviderClass

**API Group**: `secrets-store.csi.x-k8s.io/v1`  
**Kind**: `SecretProviderClass`  
**Scope**: Namespaced

**API Definition**: [CRD](../../../config/manifests/stable/secrets-store.csi.x-k8s.io_secretproviderclasses.yaml)  
**Upstream Documentation**: [secrets-store-csi-driver](https://secrets-store-csi-driver.sigs.k8s.io/)

## Purpose

Defines how the Secrets Store CSI Driver should mount secrets from external secret stores (Azure Key Vault, GCP Secret Manager, HashiCorp Vault) into pod volumes. Each SecretProviderClass specifies the provider, provider-specific configuration, and optional Kubernetes Secret synchronization.

**Key Principle**: Provider-agnostic specification that delegates actual secret fetching to provider-specific binaries running on nodes.

## Spec Structure

```go
type SecretProviderClassSpec struct {
    Provider      string                         // Provider name (e.g., "azure", "gcp", "vault")
    Parameters    map[string]string              // Provider-specific configuration (opaque to CSI driver)
    SecretObjects []SecretObject                 // Optional: K8s Secrets to sync from mounted secrets
}

type SecretObject struct {
    SecretName  string                           // Name of the K8s secret object
    Type        string                           // Type of K8s secret (e.g., "Opaque", "kubernetes.io/tls")
    Labels      map[string]string                // Labels to apply to the K8s secret
    Annotations map[string]string                // Annotations to apply to the K8s secret
    Data        []SecretObjectData               // Mapping from provider secret to K8s secret data field
}

type SecretObjectData struct {
    ObjectName string                            // Name of the object to sync from mounted volume
    Key        string                            // Data field to populate in the K8s secret
}
```

## Key Concepts

### Provider Selection

The `provider` field determines which provider binary the CSI driver invokes. The operator deploys the CSI driver with two provider search paths:
- `/var/run/secrets-store-csi-providers` (primary)
- `/etc/kubernetes/secrets-store-csi-providers` (additional)

Provider binaries must implement the [provider gRPC interface](https://secrets-store-csi-driver.sigs.k8s.io/providers.html). Common providers:
- `azure` - Azure Key Vault Provider
- `gcp` - Google Cloud Secret Manager Provider
- `vault` - HashiCorp Vault Provider

### Provider Parameters

The `parameters` map is opaque to the CSI driver and passed directly to the provider binary. Each provider defines its own schema. Examples:

**Azure**:
```yaml
parameters:
  usePodIdentity: "true"
  keyvaultName: "my-vault"
  objects: |
    array:
      - objectName: "my-secret"
        objectType: "secret"
```

**GCP**:
```yaml
parameters:
  secrets: |
    - resourceName: "projects/123/secrets/my-secret/versions/latest"
      path: "my-secret.txt"
```

**Vault**:
```yaml
parameters:
  vaultAddress: "https://vault.example.com"
  roleName: "my-role"
  objects: |
    - objectName: "my-secret"
      secretPath: "secret/data/my-secret"
      secretKey: "password"
```

### Secret Objects (Optional Sync)

`secretObjects` enables automatic synchronization of mounted secrets into Kubernetes Secret resources. The CSI driver:
1. Mounts secrets as files in the pod's volume
2. Reads specified files from the mount
3. Creates/updates a K8s Secret in the pod's namespace with the content

**Use case**: A separate consumer — another pod, a Deployment using `envFrom`/`secretKeyRef`, or a TLS/Ingress resource — that reads the synced K8s Secret. Because the Secret is only created after this pod's CSI volume is mounted, it cannot bootstrap this same pod's own environment variables at startup; use it to expose the secret to a pre-existing Secret consumer or a different workload instead.

**Lifecycle**: Secrets are synced when the volume is mounted and updated based on `rotation-poll-interval` (default: 2m, configured in DaemonSet).

## Lifecycle

1. **Creation**: SecretProviderClass is namespace-scoped. Create it in the same namespace as pods that will reference it.
2. **Mount**: When a pod references the SecretProviderClass via a CSI volume, the CSI driver:
   - Calls the provider binary with `parameters`
   - Mounts provider-returned secrets as files
   - (Optional) Syncs to K8s Secrets if `secretObjects` is defined
3. **Update**: Without secret auto-rotation (`--enable-secret-rotation=true`), changes to SecretProviderClass do NOT affect existing mounts — pods must be recreated to pick up changes. With auto-rotation enabled, changes to `parameters`/`secretObjects` are applied to existing mounts and synced Secrets on the next rotation interval (see [Rotation Support](#rotation-support)).
4. **Deletion**: SecretProviderClass can be deleted when no pods reference it. The CSI driver does NOT enforce this; K8s finalizers may be added by users.

## Example: Azure Key Vault with Secret Sync

```yaml
apiVersion: secrets-store.csi.x-k8s.io/v1
kind: SecretProviderClass
metadata:
  name: azure-vault-example
  namespace: my-app
spec:
  provider: azure
  parameters:
    usePodIdentity: "true"
    keyvaultName: "my-vault"
    objects: |
      array:
        - objectName: "db-password"
          objectType: "secret"
  secretObjects:
    - secretName: db-credentials
      type: Opaque
      data:
        - objectName: db-password
          key: password
```

**Use case**: Pod mounts Azure Key Vault secret `db-password` as a file AND syncs it to a K8s Secret `db-credentials` for use in environment variables.

## Component-Specific Behavior

### Rotation Support

The CSI driver polls for secret updates every `rotation-poll-interval` (assets/node.yaml:46, default 2m). When secrets change in the external store:
- **Mounted files**: Updated automatically
- **Synced K8s Secrets**: Updated automatically
- **Pod environment variables**: NOT updated (requires pod restart)

### SecretProviderClassPodStatus

For each pod that mounts a SecretProviderClass, the CSI driver creates a `SecretProviderClassPodStatus` resource tracking mount state. See [secretproviderclasspodstatus.md](./secretproviderclasspodstatus.md).

### RBAC Requirements

The CSI driver node SA requires:
- `get`, `list`, `watch` on `secretproviderclasses` (read SecretProviderClass spec)
- `create`, `delete`, `get`, `list`, `patch`, `update`, `watch` on `secretproviderclasspodstatuses` (track mount state)
- `get`, `patch`, `update` on `secretproviderclasspodstatuses/status`

Configured in assets/rbac/secretproviderclasses_role.yaml.

### Provider Installation

**The operator does NOT install provider binaries**. Users must deploy provider DaemonSets separately. Common provider charts:
- [Azure Provider](https://github.com/Azure/secrets-store-csi-driver-provider-azure)
- [GCP Provider](https://github.com/GoogleCloudPlatform/secrets-store-csi-driver-provider-gcp)
- [Vault Provider](https://github.com/hashicorp/secrets-store-csi-driver-provider-vault)

## Common Mistakes

1. **Creating SecretProviderClass in wrong namespace** - Must be in the same namespace as the pod. Cross-namespace references are not supported.

2. **Expecting pod env vars to update on rotation** - Only mounted files and synced K8s Secrets update. Pod env vars are fixed at pod start.

3. **Changing SecretProviderClass and expecting existing pods to see changes** - Pods must be recreated. The CSI driver does not watch SecretProviderClass for changes.

4. **Forgetting to install provider binary** - The operator deploys the CSI driver, not provider binaries. `provider: azure` fails if the Azure provider DaemonSet is not installed.

5. **Mixing v1alpha1 and v1** - v1alpha1 is deprecated. Always use v1 (served since secrets-store-csi-driver 1.0).

## Related Concepts

- [SecretProviderClassPodStatus](./secretproviderclasspodstatus.md) - Mount status tracking
