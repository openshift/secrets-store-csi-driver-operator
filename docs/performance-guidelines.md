# Performance Guidelines

## Informer and Controller Patterns

- This operator uses the `library-go` controller framework via `NewKubeInformersForNamespaces`.
- The config informer factory uses a 20-minute resync period (`resync = 20 * time.Minute` in `pkg/operator/starter.go`). Kube informers via `NewKubeInformersForNamespaces` use a 10-minute resync (library-go default).
- Scope informers to the specific namespaces needed. The operator watches the operator namespace and cluster-scoped resources via `NewKubeInformersForNamespaces` rather than cluster-wide informers, reducing API server load.
- When adding new watchers, scope to the minimum required namespace set instead of watching all namespaces.

## Resource Requests and Limits

- DaemonSet containers in `assets/node.yaml` set resource requests but no limits:
  - `csi-driver` container: `cpu: 10m`, `memory: 50Mi`
  - `csi-node-driver-registrar`: `cpu: 10m`, `memory: 50Mi`
  - `csi-liveness-probe`: `cpu: 10m`, `memory: 50Mi`
- Follow this pattern: set requests for scheduling guarantees, omit limits to allow burst usage.
- Keep requests conservative — the node plugin runs on every node as a DaemonSet, so per-pod resource requests multiply across the cluster.

## Volume and Mount Configuration

- The DaemonSet uses `hostPath` volumes with explicit `type` (`DirectoryOrCreate` or `Directory`) for CSI plugin directories and provider paths.
- Mount secrets-store provider paths as `hostPath` with `DirectoryOrCreate` — the paths are `/var/run/secrets-store-csi-providers` and `/etc/kubernetes/secrets-store-csi-providers`.

## Metrics and Monitoring

- The CSI driver exposes Prometheus metrics on port 8095, configured via the `--metrics-addr=:8095` flag in the driver container.
- Metrics are served behind TLS using auto-provisioned certificates from OpenShift's service-ca.
- When adding new metrics, register them in the driver startup and ensure they have bounded cardinality — avoid labels with unbounded values (pod names, UIDs).

## Liveness and Readiness Probes

- The `csi-driver` container has a liveness probe that checks the CSI driver's health via HTTP:
  - `httpGet` on port 9808 (named `healthz`), path `/healthz`
  - `failureThreshold: 5`, `initialDelaySeconds: 30`, `timeoutSeconds: 10`, `periodSeconds: 15`
- The `csi-liveness-probe` sidecar provides the health endpoint at port 9808 by connecting to the Unix socket at `/csi/csi.sock`.
- These values balance responsiveness with tolerance for transient failures — keep the 5-failure threshold to avoid unnecessary restarts during brief node pressure.

## Leader Election

- The operator uses `library-go`'s built-in leader election via `NewControllerCommandConfig` — a single leader runs the control loops.
- The operator binary accepts standard leader-election flags from the controller framework. Do not implement custom leader election.

## Operator Static Resource Management

- The operator uses `library-go`'s `WithConditionalStaticResourcesController` to reconcile static assets (ServiceAccounts, ClusterRoles, ClusterRoleBindings, ConfigMaps, CSIDriver) from the `assets/` directory.
- Static resources are re-synced periodically by the controller — avoid expensive operations in asset rendering (template expansion).
- Assets are loaded via `assets.ReadFile()` using Go's `embed.FS` — they are compiled into the binary at build time and cached.

## Build and Binary Size

- The Makefile uses `GO_BUILD_FLAGS` for build optimization, including `-trimpath` and optional FIPS tags.
- The operator builds a single binary via `cmd/secrets-store-csi-driver-operator/main.go` — keep the binary footprint small by not pulling unnecessary dependencies.

## DaemonSet Update Strategy

- The node DaemonSet uses `RollingUpdate` strategy with `maxUnavailable: 10%` — this prevents all nodes from restarting simultaneously during updates.
- Keep `maxUnavailable` at or below 10% for DaemonSets that run on every node.
