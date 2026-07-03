This file provides openspec-workflow-specific guidance for AI agents working with the **secrets-store-csi-driver-operator** repository. It supplements the operator's `AGENTS.md` (which covers architecture, build, testing, and code conventions) with the routing tables and stage hints required by the openspec pipeline.

**Primary reference**: Read `AGENTS.md` in the target repository first. This file adds only what `AGENTS.md` intentionally omits (openspec pipeline specifics).

---

## Per-task Testing During `/opsx-apply` (code generation eval gate)

During implementation, each code generation task is verified with **real command execution** (not agent assertions).

| Task type | Verification | Test strategy |
|-----------|-------------|--------------|
| Static asset YAML | `go build ./assets/...` | Embed compile check — catches missing `//go:embed` glob |
| Controller logic (`pkg/operator/`) | `go build ./...` && `go vet ./...` | Co-generated `_test.go` + `go test ./pkg/... ./cmd/...` |
| OLM/bundle/metadata | `make metadata && go build ./...` | Build check |
| E2E test authoring | `go build -tags e2e ./...` | Compile-only (no live cluster locally) |

---

## Execution Agent Routing

Use these **Assigned Agent** IDs in `tasks.md` §3 when **`AgentRoutingMode: PROVIDED`**. Each task gets exactly one primary agent. Map work to paths below; split mixed tasks.

| Agent ID | Scope | Route when task touches | OAPE / execution |
|----------|-------|------------------------|------------------|
| **OperatorController_Agent** | Reconciliation, CSIControllerSet wiring, operator bootstrap, informers | `pkg/operator/starter.go`, controller hooks | `api-implement` |
| **Assets_Agent** | Static YAML manifests, RBAC, NetworkPolicy | `assets/*.yaml`, `assets/rbac/`, `assets/network-policy/` | Manual — edit YAML directly |
| **OLMRelease_Agent** | OLM bundle, CSV, package.yaml, OCP version bumps | `config/manifests/`, `config/metadata/`, `hack/update-metadata.sh` | Manual — `make metadata` |
| **Testing_Agent** | E2E and unit test authoring | `pkg/operator/*_test.go`, `hack/e2e.sh` | `e2e-generate` for e2e tasks |
| **Docs_Agent** | User-facing docs | `README.md`, `must-gather/` | Manual |

### Routing rules

- All operator reconciliation logic routes to `OperatorController_Agent`.
- Asset-only changes (new YAML, RBAC tweak) → `Assets_Agent`.
- OLM/CSV/bundle/version changes → `OLMRelease_Agent`.
- Unit test changes accompany controller changes (pair `OperatorController_Agent` + `Testing_Agent`).
- E2E test authoring → `Testing_Agent` standalone task.

### Verification pairing

- Controller changes → `go test ./pkg/... ./cmd/...`
- Asset additions → `go build ./...` (embed compile check)
- OLM/bundle → `make metadata` and manual CSV review

---

## Stage-Specific Agent Guidance

### Repo-Assessment Stage Hints

Required sections and their operator-specific content:

- **§1 Architecture**: Single-controller library-go CSI operator. No dual patterns, no addon controllers, no controller-runtime. All reconciliation flows through `csicontrollerset.CSIControllerSet`.
- **§1.3 Dead code / do not edit**: `vendor/` — never hand-edit. Image variable substitution in `assets/node.yaml` (`${DRIVER_IMAGE}`, `${NODE_DRIVER_REGISTRAR_IMAGE}`, `${LIVENESS_PROBE_IMAGE}`, `${LOG_LEVEL}`) is performed by the library-go DaemonSet controller at runtime — do not substitute manually.
- **§4.2 Controller pipeline**: Document the `CSIControllerSet` chain in exact order: LogLevel → ManagementState → ConditionalStaticResources → CSIConfigObserver → CSIDriverNodeService.
- **§5 Reuse guidelines**: Reuse `replaceNamespaceFunc` for any new namespace-substituted asset. Reuse `getOperatorSyncState` for any management-state-conditional behavior.
- **§5 Platform Network Policy Hook**: DaemonSet pods in `assets/node.yaml` carry label `openshift.storage.network-policy.api-server: allow`. This opts pods into CSO-managed egress-to-api-server network policies. **Do NOT** create standalone egress-to-api-server NetworkPolicy objects — document this hook in §5 Reusable Assets and §10.2. (Evidence: `assets/node.yaml`, commit 796a110a)
- **§8.2 Test commands**: `go test ./pkg/... ./cmd/...` (unit), `hack/e2e.sh` (e2e, needs cluster).
- **§10.5 Console Resources**: For features involving ConsoleQuickStart or ConsoleYAMLSample — enumerate `qs-nav-*` navigation tokens used in existing manifests, note their OCP version coupling, and flag that tokens can change across releases (e.g., `qs-nav-operator-hub` was renamed to `qs-nav-ecosystem` in OCP 4.18+). Verify the current token against the target release branch.
- **§10.6 OLM Bundle Annotations**: For any resource type added to `config/manifests/stable/`, document the required deployment-profile annotations: `capability.openshift.io/name`, `include.release.openshift.io/ibm-cloud-managed`, `include.release.openshift.io/self-managed-high-availability`, `include.release.openshift.io/single-node-developer`.
- **§11.1 Unverified**: No custom CRD types, no feature gates, no addon controllers, no controller-runtime.

### Planning Stage Hints

Prefer operator-native thinking:
- `ClusterCSIDriver` CR configuration surface (managementState, logLevel, operatorLogLevel)
- CSIControllerSet hook additions vs. new static assets
- DaemonSet configuration (CA bundle, node selector, tolerations, image env vars)
- RBAC blast radius for privileged SCC and CSI secrets access
- OLM/CSV update requirements for new RBAC or image references
- E2E impact: does the change require a running CSI provider plugin?

### Validation Stage Hints

When evaluating a spec for this project, assess:
- Managed resource semantics (`Managed`/`Unmanaged`/`Removed` honor)
- CSI driver registration lifecycle (CSIDriver object create/delete)
- DaemonSet node deployment (all nodes vs. node selector, tolerations)
- RBAC scope (privileged SCC, secrets access, cluster-wide reads)
- CA bundle propagation (trusted CA injection into DaemonSet pods)
- Platform matrix (OpenShift versions, MicroShift if mentioned)
- OLM upgrade edge behavior
- Image reference management (env vars in CSV vs. hardcoded images)
- **OLM bundle resources**: if spec mentions adding resources to OLM bundle, check for explicit deployment-profile annotation requirements (`capability.openshift.io/name`, `include.release.openshift.io/*`)
- **OLM bundle placement**: if spec mentions ConsoleQuickStart or similar resources, verify the spec explicitly decides which resources go in bundle vs. demo/ and that install-type guides are NOT bundled (circular dependency)

---

## Platform Integration Hooks

### CSO Network Policy Hook
DaemonSet pods in `assets/node.yaml` carry label `openshift.storage.network-policy.api-server: allow`.
This opts pods into CSO-managed egress-to-api-server network policies.

**Rule:** Do NOT create standalone `allow-egress-to-api-server-*.yaml` NetworkPolicy objects.
Use this label to hook into CSO-managed shared policies. Only create operator-specific ingress
policies that CSO does not manage (e.g., `allow-ingress-to-metrics-operand.yaml`).
**Evidence:** `assets/node.yaml`, commit 796a110a.

---

## Console Resources (ConsoleQuickStart, ConsoleYAMLSample)

Manifests: `config/manifests/stable/` (OLM bundle) and `demo/console/` (standalone apply).

### Navigation Tokens
Navigation highlight tokens in QuickStart tasks are OCP version-dependent:
- Current token (OCP 4.18+): `{{highlight qs-nav-ecosystem}}` (Ecosystem section)
- Former token (pre-4.18): `{{highlight qs-nav-operator-hub}}` (Operators section)
**Rule:** Before implementing QuickStart tasks, verify the correct `qs-nav-*` token for the target
OCP release. Do not copy tokens from older examples without checking.

### OLM Bundle Annotations (required for all ConsoleQuickStart in config/manifests/stable/)
```yaml
metadata:
  annotations:
    capability.openshift.io/name: "Console"
    include.release.openshift.io/ibm-cloud-managed: "true"
    include.release.openshift.io/self-managed-high-availability: "true"
    include.release.openshift.io/single-node-developer: "true"
```

### Bundle Placement Rule
- `sscsi-example-quickstart.yaml` → OLM bundle (`config/manifests/stable/`) + full annotations
- `sscsi-install-quickstart.yaml` → `demo/console/` ONLY — **not** in OLM bundle
  - Rationale: bundling an install QuickStart is circular (resource appears after OLM completes
    install, which is what the QuickStart was trying to guide)
- `sscsi-sample-secretproviderclass-*.yaml` → OLM bundle + annotations

---

## OLM Bundle Resource Conventions

All resources in `config/manifests/stable/` that should appear on managed/HA/SNO deployments
MUST carry:
```yaml
capability.openshift.io/name: "<capability-name>"
include.release.openshift.io/ibm-cloud-managed: "true"
include.release.openshift.io/self-managed-high-availability: "true"
include.release.openshift.io/single-node-developer: "true"
```
Capability name by resource type:
- `ConsoleQuickStart`, `ConsoleYAMLSample` → `"Console"`
- Storage controller Services → `"Storage"`
Verify against existing bundle resources (`config/manifests/stable/*.yaml`) for the current set.
