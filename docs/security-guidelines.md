# Security Guidelines

## RBAC Principles

- Follow least-privilege: each component gets its own `ClusterRole` scoped to exactly the verbs and resources it needs.
- The operator defines two ClusterRoles in `assets/rbac/`:
  - **`privileged_role.yaml`** — grants `use` on the `privileged` SecurityContextConstraint (SCC). Only the node plugin DaemonSet ServiceAccount binds this (via `node_privileged_binding.yaml`).
  - **`secretproviderclasses_role.yaml`** — node-plugin permissions: `get`/`list`/`watch` on `secrets`, `pods`, `secretproviderclasses`, `csidrivers`; full CRUD on `secrets` (for rotation/syncing) and `secretproviderclasspodstatuses`; `create`/`patch` on `events`; `create` on `serviceaccounts/token` (bound token minting). Bound via `secretproviderclasses_binding.yaml`.
- Prefer keeping per-component RBAC separation so a compromised component cannot escalate.
- When adding a new resource permission, add it to the correct component role, not a shared one.

## SecurityContextConstraints (SCC)

- The node plugin DaemonSet runs under the `privileged` SCC because CSI node drivers require host-level access (mount propagation, `/var/lib/kubelet` access).
- The `privileged_role.yaml` + `node_privileged_binding.yaml` pair grants this — avoid adding `privileged` SCC access to other components without justification.
- Prefer `readOnlyRootFilesystem: true` on all containers. The node-registrar sidecar already sets this.

## Container Security Contexts

- The `csi-driver` container in the DaemonSet (`assets/node.yaml`) runs with `privileged: true`, required for CSI mount operations.
- The `csi-node-driver-registrar` sidecar runs with `privileged: true` and `readOnlyRootFilesystem: true`.
- The `csi-liveness-probe` sidecar runs non-privileged with `readOnlyRootFilesystem: true`.
- When adding new containers, default to non-privileged with `readOnlyRootFilesystem: true`. Only escalate with a documented justification.

## Service Account Token Handling

- The node plugin uses `serviceaccounts/token` create permission to mint short-lived bound tokens for pod identity.
- Projected service account tokens should be mounted via `serviceAccountToken` volume projections with explicit `expirationSeconds` and `audience` when applicable.

## TLS and Certificate Management

- The operator creates a `Service` for metrics/healthz endpoints with `service.beta.openshift.io/serving-cert-secret-name` annotation — OpenShift's service-ca-operator auto-generates and rotates the TLS certificate.
- CA bundles are injected via ConfigMaps annotated with `config.openshift.io/inject-trusted-cabundle: "true"` (see `assets/cabundle_cm.yaml`).
- Prefer annotation-based auto-provisioning over hard-coding certificates or keys in manifests.
- The operator mounts the CA bundle and serving cert into the operand as volumes — see `assets/node.yaml` for the volume/volumeMount pattern.

## Image References

- Container images use variable substitution (`${NODE_DRIVER_REGISTRAR_IMAGE}`, `${LIVENESS_PROBE_IMAGE}`, etc.) for deploy-time image injection.
- Prefer the substitution pattern so the operator can pin images from the cluster's release payload rather than hard-coding image tags or digests.

## Secrets Handling in Code

- The operator reads ClusterCSIDriver and related config objects to determine desired state — it does not directly handle user secrets.
- The CSI driver itself (upstream, not this operator) handles secret volume mounts. This operator only manages the lifecycle of the driver deployment.
- Avoid logging secret values. The operator uses `klog` — ensure no secret content reaches log statements.

## Host Path Security

- The DaemonSet mounts several host paths: `/var/lib/kubelet/pods` (for CSI), `/var/lib/kubelet/plugins`, `/run/secrets-store-csi`.
- Host path mounts should use explicit `type: DirectoryOrCreate` or `type: Directory`.
- `mountPropagation: Bidirectional` is set on the kubelet pods mount so CSI-mounted volumes are visible to the host. Do not use Bidirectional propagation on other mounts without justification.

## Network and Port Security

- Metrics are served on container port 8095, exposed via a `Service` with TLS (auto-provisioned cert).
- The CSI driver endpoint listens on a Unix socket at `/csi/csi.sock` — not a TCP port. Prefer Unix sockets for driver endpoints to avoid network exposure.
- Liveness probes use HTTP on port 9808, served by the `csi-liveness-probe` sidecar container.
