# AGENTS.md

## Project Overview

This is the **OpenShift Secrets Store CSI Driver Operator** -- a Go-based Kubernetes operator that manages the lifecycle of the [Secrets Store CSI Driver](https://github.com/openshift/secrets-store-csi-driver) on OpenShift clusters. It is built on the `library-go` controller framework and deployed via OLM (Operator Lifecycle Manager).

The operator watches a `ClusterCSIDriver` custom resource (`secrets-store.csi.k8s.io`) and reconciles a DaemonSet that runs the CSI driver on every Linux node, along with associated RBAC, ServiceAccounts, ConfigMaps, NetworkPolicies, and the CSIDriver object.

## Detailed Guidelines

The following documents cover domain-specific conventions in depth. Read these before working in their respective areas:

- **[docs/security-guidelines.md](docs/security-guidelines.md)** -- RBAC principles, SCC usage, container security contexts, TLS/cert management, host path security, and image reference patterns.
- **[docs/performance-guidelines.md](docs/performance-guidelines.md)** -- Informer scoping, resource requests/limits, metrics, liveness probes, leader election, static resource management, and DaemonSet update strategy.
- **[docs/error-handling-guidelines.md](docs/error-handling-guidelines.md)** -- Error wrapping, klog usage, operator status conditions, Sync return patterns, fatal error policy, and test error handling.
- **[docs/testing-guidelines.md](docs/testing-guidelines.md)** -- Table-driven test patterns, test organization, library-go fakes, assertion style, Makefile targets, E2E testing via `hack/e2e.sh`, and CI integration.

## Repository Layout

```
cmd/secrets-store-csi-driver-operator/main.go  -- Entry point; wires cobra command + library-go controller
pkg/operator/starter.go                        -- RunOperator: creates clients, informers, CSI controller set
pkg/operator/starter_test.go                   -- Unit tests for operator sync state logic
pkg/version/version.go                         -- Build version info via ldflags; registers Prometheus gauge
pkg/dependencymagnet/dependencymagnet.go       -- Build-tag-guarded import to keep build-machinery-go vendored
assets/                                        -- Embedded YAML manifests (go:embed)
  assets.go                                    -- embed.FS declaration and ReadFile wrapper
  node.yaml                                    -- DaemonSet for the CSI driver + sidecars
  node_sa.yaml, csidriver.yaml, cabundle_cm.yaml
  rbac/                                        -- ClusterRoles and ClusterRoleBindings
  network-policy/                              -- NetworkPolicy for metrics ingress
config/manifests/                              -- OLM bundle manifests (CSV, CRDs, package.yaml)
  art.yaml                                     -- ART version update rules
  stable/                                      -- Channel-specific manifests
    image-references                           -- ImageStream for release payload injection
hack/
  e2e.sh                                       -- E2E test script (requires running cluster)
  update-metadata.sh                           -- Bump OCP version across CSV, Makefile, README
  create-bundle                                -- Build OLM bundle + index images
must-gather/gather                             -- must-gather collection script
Dockerfile.openshift                           -- Multi-stage build for the operator image
Dockerfile.mustgather                          -- must-gather image build
```

## Architecture

### Controller Framework

The operator uses `library-go`'s `csicontrollerset` to compose multiple sub-controllers into a single controller set. All controllers are configured in `pkg/operator/starter.go:RunOperator`. The controller set includes:

1. **LogLevelController** -- Syncs log level from the `ClusterCSIDriver` spec to the operator.
2. **ManagementStateController** -- Handles `Managed`/`Unmanaged`/`Removed` lifecycle. This operator is marked **removable** (`true` passed to `WithManagementStateController`).
3. **ConditionalStaticResourcesController** -- Reconciles static YAML assets (RBAC, ServiceAccount, CSIDriver, ConfigMap, NetworkPolicy) based on management state. Resources are created when `Managed`, deleted when `Removed`.
4. **CSIConfigObserverController** -- Observes cluster config (infrastructure, proxy, apiserver) and propagates to the operand.
5. **CSIDriverNodeService** -- Manages the node DaemonSet, including CA bundle injection.

### Operator Client

The operator uses `goc.NewClusterScopedOperatorClientWithConfigName` to create a generic operator client for `ClusterCSIDriver`. Two extractor functions (`extractOperatorSpec`, `extractOperatorStatus`) convert between unstructured objects and typed apply configurations.

### Management State Logic

The `getOperatorSyncState` function determines whether to sync, skip, or delete resources:
- `Managed` -- normal reconciliation.
- `Unmanaged` -- skip sync (also the fallback on errors).
- `Removed` -- delete conditional resources. A `DeletionTimestamp` on the CR is treated as `Removed`.

## Build and Development

### Prerequisites

- Go 1.25+ (matching `go.mod`).
- `make` (GNU Make).
- Access to an OpenShift cluster for E2E tests.
- `oc` CLI for E2E and must-gather testing.

### Key Make Targets

| Target | Purpose |
|--------|---------|
| `make` / `make build` | Build the operator binary |
| `make test-unit` | Run unit tests (`./pkg/... ./cmd/...`) |
| `make verify` | Run `go vet`, `gofmt` check, Go version consistency |
| `make test-e2e` | Run E2E tests via `hack/e2e.sh` (requires cluster) |
| `make check` | Run `verify` then `test-unit` |
| `make update-gofmt` | Auto-fix formatting |
| `make clean` | Remove binary and yq tool |

### FIPS Build

The Makefile auto-detects `GOEXPERIMENT=strictfipsruntime` support. In CI, builds use `CGO_ENABLED=1 GOEXPERIMENT=strictfipsruntime` with `-tags strictfipsruntime,openssl`. Local builds without FIPS-capable Go will emit a warning and build without FIPS -- such builds are not valid for CI or production.

### Validation Before Submitting

Always run before pushing:
```
make verify && make test-unit
```

## Dependency Management

### Vendoring

Dependencies are committed in `vendor/`. This is the standard pattern for OpenShift operators.

- Run `go mod tidy && go mod vendor` to update dependencies.
- The `verify-deps` target (from `build-machinery-go`) validates that `vendor/` matches `go.mod`.

### Key Dependencies

| Dependency | Purpose |
|------------|---------|
| `github.com/openshift/library-go` | Controller framework, CSI controller set, resource apply, operator client |
| `github.com/openshift/api` | OpenShift API types (`ClusterCSIDriver`, `operator/v1`) |
| `github.com/openshift/client-go` | OpenShift typed clients and informer factories |
| `github.com/openshift/build-machinery-go` | Shared Makefile targets for building, testing, vendoring |
| `k8s.io/client-go` | Kubernetes client, informers, REST config |
| `k8s.io/apimachinery` | Kubernetes API machinery (unstructured, runtime) |
| `github.com/spf13/cobra` | CLI command structure |
| `k8s.io/klog/v2` | Logging |

### Dependency Magnet

`pkg/dependencymagnet/dependencymagnet.go` imports `build-machinery-go` under a `tools` build tag to keep it in `go.mod` and `vendor/` without pulling it into the compiled binary. Do not remove this file.

## Asset Management

### Embedded Assets Pattern

All Kubernetes manifests deployed by the operator live in the `assets/` directory and are compiled into the binary using Go's `embed` package:

```go
//go:embed *.yaml rbac/*.yaml network-policy/*.yaml
var f embed.FS
```

The `assets.ReadFile(name)` function wraps `f.ReadFile`. When adding new assets:
- Place YAML files under `assets/` or its subdirectories (`rbac/`, `network-policy/`).
- If adding a new subdirectory, update the `//go:embed` directive in `assets/assets.go` to include the new glob.
- Register the new file in the appropriate controller in `pkg/operator/starter.go` (e.g., add it to the `WithConditionalStaticResourcesController` file list).

### Namespace Substitution

Assets use `${NAMESPACE}` as a placeholder for the operator namespace. The `replaceNamespaceFunc` in `starter.go` performs byte-level replacement at load time. Use `${NAMESPACE}` (not hardcoded namespace names) in any new asset that references the operator namespace.

### Image Variable Substitution

Container images in `assets/node.yaml` use variables like `${DRIVER_IMAGE}`, `${NODE_DRIVER_REGISTRAR_IMAGE}`, `${LIVENESS_PROBE_IMAGE}`, and `${LOG_LEVEL}`. These are substituted by the library-go DaemonSet controller at runtime from environment variables set on the operator pod.

## Code Conventions

### Package Structure

- `cmd/` -- One sub-package per binary. Contains only CLI wiring (cobra commands, controller config). No business logic.
- `pkg/operator/` -- Operator startup and controller composition. This is where `RunOperator` lives.
- `pkg/version/` -- Build version info. Injected via ldflags.
- `pkg/dependencymagnet/` -- Build-tag-guarded imports for tool dependencies.
- `assets/` -- Embedded YAML manifests. The `assets.go` file provides the `ReadFile` API.

### Naming Conventions

- **Package names**: lowercase, single-word where possible (`operator`, `version`, `assets`).
- **File names**: lowercase with underscores (`starter.go`, `starter_test.go`, `node_sa.yaml`).
- **Constants**: camelCase, unexported unless needed externally (`operatorName`, `providerName`, `namespaceKey`).
- **Functions**: exported for cross-package use (`RunOperator`, `ReadFile`), unexported for internal helpers (`replaceNamespaceFunc`, `getOperatorSyncState`, `extractOperatorSpec`).
- **YAML asset names**: descriptive, matching the Kubernetes resource they define (`csidriver.yaml`, `node.yaml`, `privileged_role.yaml`).

### Code Style

- Standard `gofmt` formatting. No custom linter configuration beyond `go vet`.
- Imports grouped in standard Go convention: stdlib, external, internal -- separated by blank lines.
- No assertion libraries. Use standard `if` checks with `t.Fatalf` or `t.Errorf`.
- No third-party mocking frameworks. Use `library-go` fakes.
- Error messages start with a lowercase verb: `"unable to convert..."`, `"failed to get..."`.

### Operator Pattern Conventions

- The operator manages a **single binary** that contains all controllers.
- Controllers are composed via `csicontrollerset` method chaining, not registered individually.
- The operator is **cluster-scoped** -- it watches a cluster-scoped `ClusterCSIDriver` CR.
- The operator is **removable** -- it supports the `Removed` management state and cleans up resources on deletion.
- Static resources (RBAC, SA, ConfigMap, CSIDriver, NetworkPolicy) use `WithConditionalStaticResourcesController`. The DaemonSet uses `WithCSIDriverNodeService`. Do not mix these patterns.
- Informers are scoped to the operator namespace and cluster scope (`""`) -- not all namespaces.

## OLM and Release

### Version Bumping

When the OCP version changes, run:
```
./hack/update-metadata.sh X.Y
```
This updates the package manifest, CSV, Makefile image tags, and README registry references.

### OLM Bundle

The operator is packaged as an OLM bundle. Key files:
- `config/manifests/secrets-store-csi-driver-operator.package.yaml` -- Package name and channel.
- `config/manifests/stable/*.clusterserviceversion.yaml` -- CSV with deployment spec, RBAC, and owned CRDs.
- `config/manifests/stable/image-references` -- Image stream mapping for release payload.
- `config/metadata/annotations.yaml` -- OLM bundle metadata.
- `config/manifests/art.yaml` -- ART (Automated Release Tooling) version substitution rules.

### Operator Namespace

The operator runs in `openshift-cluster-csi-drivers` (set via OLM's `suggested-namespace` annotation). All namespace-scoped resources are created in this namespace.

## CI/CD

- CI runs in OpenShift Prow. Configuration lives in `openshift/release`, not this repo.
- `.ci-operator.yaml` specifies the build root image.
- Every PR runs: `make verify` and `make test-unit`.
- E2E tests run as Prow jobs against real clusters.
- CI builds enforce FIPS compliance via the `strictfipsruntime` build experiment.

## Common Pitfalls

- **Missing embed directive update**: When adding a new subdirectory under `assets/`, you must update the `//go:embed` directive in `assets/assets.go`. A missing glob will cause a `panic` at runtime.
- **Hardcoded namespaces in assets**: Always use `${NAMESPACE}` in YAML assets for namespace fields. Hardcoding a namespace will break deployments in non-default namespaces.
- **Wrong controller for new resources**: Static resources go in `WithConditionalStaticResourcesController`. The DaemonSet goes in `WithCSIDriverNodeService`. Mixing these causes incorrect lifecycle management.
- **Cluster-wide informers**: Do not create informers that watch all namespaces. Use `NewKubeInformersForNamespaces` with explicit namespace lists.
- **Non-vendored dependencies**: All dependencies must be vendored. A `go.sum`-only dependency will fail CI.
- **FIPS build failures locally**: If your local Go does not support `GOEXPERIMENT=strictfipsruntime`, the build will succeed with a warning but produce a non-FIPS binary. This is fine for local development.
- **Panics in reconciliation**: `panic` is only acceptable for build-time bugs (missing embedded assets). Return errors in reconciliation loops and let the framework retry.

## Behavioral Preferences

- Always run `make verify && make test-unit` before suggesting a PR is ready.
- After modifying Go files, run `make verify` to catch formatting or vet issues early.
- After modifying files under `assets/`, verify the `//go:embed` directive in `assets/assets.go` covers the new paths.
- E2E tests (`make test-e2e`) require a live OpenShift cluster and are not expected to pass locally.
