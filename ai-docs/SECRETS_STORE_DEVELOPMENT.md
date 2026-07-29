# Secrets Store CSI Driver Operator Development Guide

This guide covers development workflows specific to the Secrets Store CSI Driver Operator.

**For generic OpenShift operator patterns**, see [Platform Operator Patterns](https://github.com/openshift/enhancements/tree/master/ai-docs/platform/operators/).

## Prerequisites

- **Go**: 1.25.0+ (matching `go.mod`)
- **Make**: GNU Make
- **oc CLI**: For E2E tests and cluster interactions
- **OpenShift cluster**: Required for E2E tests (not needed for unit tests)
- **FIPS-capable Go** (CI/production only): `GOEXPERIMENT=strictfipsruntime` support

**Quick check**:
```bash
go version  # Should be 1.25.0+
make check  # Runs verify + unit tests
```

## Build

### Standard Build

```bash
make                # Build operator binary → ./secrets-store-csi-driver-operator
make build          # Alias for 'make'
```

**Output**: `./secrets-store-csi-driver-operator` binary

**Build flags**: See Makefile:36-43 for FIPS detection logic.

### FIPS Build (CI/Production)

```bash
GOEXPERIMENT=strictfipsruntime make
```

**Requirements**:
- Go compiler with `GOEXPERIMENT=strictfipsruntime` support
- `CGO_ENABLED=1` (enabled automatically by Makefile)

**Local builds without FIPS**: The Makefile emits a warning but builds successfully. Such builds are **not valid for CI or production**.

## Testing

### Unit Tests

```bash
make test-unit      # Run unit tests (./pkg/... ./cmd/...)
```

**Coverage**: Tests in `pkg/operator/starter_test.go` cover `getOperatorSyncState` logic (management state transitions).

**Patterns**:
- Table-driven tests (see [testing-guidelines.md](../docs/testing-guidelines.md))
- library-go fakes (`v1helpers.NewFakeOperatorClientWithObjectMeta`)
- No third-party mocking frameworks

### E2E Tests

```bash
make test-e2e       # Run E2E tests via hack/e2e.sh (requires cluster)
```

**Requirements**:
- Running OpenShift cluster
- `oc` CLI authenticated to the cluster

**What it tests**:
- Operator deployment via OLM
- DaemonSet reconciliation
- Management state transitions (Managed → Unmanaged → Removed)

**Note**: E2E tests are NOT expected to pass locally (require specific cluster configuration). Run in CI via Prow.

### Verification

```bash
make verify         # Run go vet + gofmt check + Go version consistency
make check          # Run verify + test-unit
```

**Before submitting PRs**: Always run `make check`.

## Common Development Tasks

### Task: Add a New Static Resource (RBAC, ConfigMap, etc.)

**Complexity**: Low

**Steps**:
1. Create YAML file in `assets/` or subdirectory (e.g., `assets/rbac/new-role.yaml`)
2. If creating a **new subdirectory** under `assets/`:
   - Update `//go:embed` directive in `assets/assets.go`:
     ```go
     //go:embed *.yaml rbac/*.yaml network-policy/*.yaml new-subdir/*.yaml
     ```
3. Use `${NAMESPACE}` for namespace fields (replaced at runtime)
4. Register file in `pkg/operator/starter.go` (add to `WithConditionalStaticResourcesController` file list):
   ```go
   []string{
       "node_sa.yaml",
       // ...
       "new-subdir/new-role.yaml",  // Add here
   }
   ```
5. Run `make verify && make test-unit`
6. Test locally or in CI

**Common mistakes**:
- Forgetting step 2 → runtime panic: `open new-subdir/new-role.yaml: file does not exist`
- Hardcoding namespace → breaks non-default namespace deployments

### Task: Add a New Sidecar to the DaemonSet

**Complexity**: Medium

**Steps**:
1. Edit `assets/node.yaml`:
   - Add container spec under `spec.template.spec.containers`
   - Use `${NEW_IMAGE}` for image field
   - Add volume mounts if needed
2. Update CSV (`config/manifests/stable/secrets-store-csi-driver-operator.clusterserviceversion.yaml`):
   - Add `NEW_IMAGE` env var to operator deployment spec:
     ```yaml
     - name: NEW_IMAGE
       value: quay.io/openshift/origin-new-sidecar:latest
     ```
   - Add to `relatedImages` list (for disconnected install):
     ```yaml
     relatedImages:
       - name: new-sidecar
         image: quay.io/openshift/origin-new-sidecar:latest
     ```
3. Update `config/manifests/art.yaml` (ART version substitution rules):
   ```yaml
   - name: NEW_IMAGE
     from:
       kind: ImageStreamTag
       name: new-sidecar:latest
     ```
4. Run `make verify`
5. Test: Deploy operator, verify sidecar appears in DaemonSet

**Files modified**: `assets/node.yaml`, CSV, `art.yaml`

### Task: Change Management State Behavior

**Complexity**: High

**Files**:
- `pkg/operator/starter.go` (`getOperatorSyncState` function)
- `pkg/operator/starter_test.go` (add test cases)

**Steps**:
1. Modify `getOperatorSyncState` logic (pkg/operator/starter.go:150)
2. Add table-driven test cases to `starter_test.go`
3. Run `make test-unit`
4. Document decision in an ADR (see [decisions/](./decisions/))

**Example change**: Add a new condition for skipping sync (e.g., "skip if operator is paused").

### Task: Update OCP Version

**Complexity**: Low

```bash
make metadata VERSION=4.20.0
```

**What it updates**:
- `config/manifests/secrets-store-csi-driver-operator.package.yaml`
- CSV files in `config/manifests/stable/`
- Makefile image tags
- README.md registry references

**Script**: `hack/update-metadata.sh`

### Task: Test Management State Transitions Locally

**Complexity**: Medium

**Prerequisites**: OpenShift cluster with operator installed

**Steps**:
1. Deploy operator: `oc create -f config/manifests/stable/` (or via OLM)
2. Check initial state:
   ```bash
   oc get clustercsidrivers/secrets-store.csi.k8s.io -o yaml
   oc get daemonset -n openshift-cluster-csi-drivers
   ```
3. Set to Unmanaged:
   ```bash
   oc patch clustercsidrivers/secrets-store.csi.k8s.io --type=merge -p '{"spec":{"managementState":"Unmanaged"}}'
   ```
   - Expect: Operator stops reconciling (DaemonSet remains but is not updated)
4. Set to Removed:
   ```bash
   oc patch clustercsidrivers/secrets-store.csi.k8s.io --type=merge -p '{"spec":{"managementState":"Removed"}}'
   ```
   - Expect: Conditional resources deleted (RBAC, ServiceAccount, DaemonSet)
5. Delete CR:
   ```bash
   oc delete clustercsidrivers/secrets-store.csi.k8s.io
   ```
   - Expect: Same as Removed (operator cleans up before allowing deletion)

## Directory Structure for Development

**Do not modify** (already documented in architecture):
- See [architecture/components.md](./architecture/components.md#repository-layout)

**When adding new code**:
- **Controllers**: Should use library-go CSI controller set (not standalone controllers)
- **Utilities**: Add to `pkg/operator/` (no separate `pkg/util/` package)
- **Constants**: Define in `pkg/operator/starter.go` (co-located with usage)

## Dependency Management

### Updating Dependencies

```bash
go get github.com/openshift/library-go@latest  # Update specific dependency
go mod tidy                                    # Remove unused deps
go mod vendor                                  # Commit vendored code
make verify                                    # Validate vendor/ matches go.mod
```

**Pattern**: All dependencies are vendored. A `go.sum`-only dependency fails CI.

### Key Dependencies

See [architecture/components.md](./architecture/components.md#key-dependencies) for the dependency table.

### Dependency Magnet

`pkg/dependencymagnet/dependencymagnet.go` imports `build-machinery-go` under a `tools` build tag. **Do not delete this file** — it keeps build-machinery-go in `go.mod` and `vendor/` without pulling it into the binary.

## Image Building

### Operator Image

**Dockerfile**: `Dockerfile.openshift`

```bash
podman build -f Dockerfile.openshift -t quay.io/myuser/secrets-store-csi-driver-operator:dev .
podman push quay.io/myuser/secrets-store-csi-driver-operator:dev
```

**Multi-stage build**: Uses FIPS-compliant base image in CI.

### must-gather Image

**Dockerfile**: `Dockerfile.mustgather`

```bash
podman build -f Dockerfile.mustgather -t quay.io/myuser/secrets-store-must-gather:dev .
```

**Script**: `must-gather/gather` collects operator logs, DaemonSet state, SecretProviderClass resources.

## OLM Integration

### Building OLM Bundle

```bash
./hack/create-bundle
```

**Output**: OLM bundle image (catalog source)

**What it builds**:
- Bundle metadata (`config/metadata/`)
- CSV (`config/manifests/stable/*.clusterserviceversion.yaml`)
- CRDs (`config/manifests/stable/*.crd.yaml`)

### Testing OLM Bundle Locally

1. Build bundle: `./hack/create-bundle`
2. Create CatalogSource in cluster:
   ```yaml
   apiVersion: operators.coreos.com/v1alpha1
   kind: CatalogSource
   metadata:
     name: secrets-store-dev
     namespace: openshift-marketplace
   spec:
     sourceType: grpc
     image: quay.io/myuser/secrets-store-csi-driver-operator-bundle:dev
   ```
3. Create Subscription via OperatorHub UI or YAML

## Debugging

### Operator Logs

```bash
oc logs -n openshift-cluster-csi-drivers deployment/secrets-store-csi-driver-operator -f
```

### DaemonSet Logs

```bash
# CSI driver container
oc logs -n openshift-cluster-csi-drivers daemonset/secrets-store-csi-driver-node -c csi-driver -f

# Node driver registrar
oc logs -n openshift-cluster-csi-drivers daemonset/secrets-store-csi-driver-node -c csi-node-driver-registrar -f
```

### Check Operator Status

```bash
oc get clustercsidrivers/secrets-store.csi.k8s.io -o yaml
# Look for status.conditions (Available, Progressing, Degraded)
```

### Check DaemonSet Rollout

```bash
oc rollout status daemonset/secrets-store-csi-driver-node -n openshift-cluster-csi-drivers
```

## Common Mistakes

1. **Modifying assets without updating //go:embed** → runtime panic  
   Fix: Update `assets/assets.go` directive

2. **Hardcoding operator namespace in assets** → breaks deployments  
   Fix: Use `${NAMESPACE}` placeholder

3. **Adding DaemonSet to ConditionalStaticResourcesController** → CA bundle hook not applied  
   Fix: Use `WithCSIDriverNodeService` for DaemonSet

4. **Creating all-namespace informers** → memory bloat  
   Fix: Use `NewKubeInformersForNamespaces` (see [ADR-0003](./decisions/adr-0003-scoped-informers.md))

5. **Forgetting to update CSV when adding sidecars** → OLM doesn't inject image env vars  
   Fix: Add env var, relatedImages, and ART rules

6. **Panicking in reconciliation loops** → operator crash-loop  
   Fix: Return errors from Sync methods (see [ADR-0002](./decisions/adr-0002-embedded-assets-panic-policy.md))

## References

- [Component Architecture](./architecture/components.md)
- [Testing Guidelines](../docs/testing-guidelines.md)
- [Error Handling Guidelines](../docs/error-handling-guidelines.md)
- [Security Guidelines](../docs/security-guidelines.md)
- [Platform Operator Patterns](https://github.com/openshift/enhancements/tree/master/ai-docs/platform/operators/)
