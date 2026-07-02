<!-- Companion artifact: repo-assessment.md (target files, reusable assets, risks) -->
# Cert Manager Operator Constitution

**AgentRoutingMode:** PROVIDED
<!-- PROVIDED — AGENTS.md exists in repo and has been parsed -->

**Version**: 1.0.0 | **Ratified**: 2025-07-15 | **Last Amended**: 2025-07-15

<!--
  QUALITY TARGET: ≥90% against Stage 2 constitution rubric.
  Self-check (all must pass):
  - Every principle cites observable repo evidence (file path or pattern), not generic best practices.
  - No file inventories, hook tables, or risk analysis — those belong in repo-assessment.md only.
  - No implementation sequencing — that belongs in plan.md (Stage 3).
  - AgentRoutingMode matches whether AGENTS.md was found and parsed.
  - Upstream operand vs Open: separate principles where the repo embeds upstream workloads.
  - Addon controllers: note controller-runtime exception if repo uses library-go for core + runtime for addons.
-->

## Core Principles

### I. Upstream Operand Separation — Do Not Fork Upstream Logic

The operator deploys and manages upstream cert-manager (and addon operands like Istio CSR, trust-manager) via **embedded manifests in `bindata/`**. The operator **never** reimplements upstream controller logic (ACME, certificate issuance, trust bundle reconciliation). Operator packages reconcile CRs and deploy/configure operand workloads only. New operand integrations (e.g., trust-manager) MUST follow this boundary: operator deploys manifests, upstream operand provides domain behavior.

**Evidence:** `bindata/cert-manager-deployment/`, `bindata/istio-csr/` — all operand YAML is vendored upstream manifests; `pkg/controller/deployment/` reconciles deployments but contains zero ACME/issuance logic. `README.md` explicitly states: "uses upstream deployment manifests."

### II. Follow Existing Controller Patterns — library-go for Core, controller-runtime for Addons

The core cert-manager operand lifecycle uses **`library-go`** patterns: `controllercmd` entrypoint (`pkg/cmd/operator/cmd.go`), informer-based controllers wired in `pkg/operator/starter.go`, `OperatorClient` in `pkg/operator/operatorclient/`, and `ClusterOperator`-style status reporting. Addon controllers (Istio CSR, and by extension trust-manager) use **`sigs.k8s.io/controller-runtime`** (`pkg/operator/setup_manager.go`) with the manager pattern. New addon operand controllers MUST use the controller-runtime addon pattern, not the library-go core pattern.

**Evidence:** `pkg/operator/starter.go` — library-go `controllercmd` wiring for core; `pkg/operator/setup_manager.go` — controller-runtime manager setup for addons; `pkg/controller/istiocsr/controller.go` — uses controller-runtime reconciler. `go.mod` imports both `github.com/openshift/library-go` and `sigs.k8s.io/controller-runtime v0.19.0`.

### III. Singleton CR Convention — Name `cluster`, One Per Kind

All operator-level CRs are **singletons named `cluster`**. The operator auto-creates a default `CertManager` CR named `cluster` if missing. Addon CRs (IstioCSR, TrustManager) follow the same singleton pattern. Validation MUST reject duplicate instances. The `TargetNamespace` for the core operand is hardcoded to `cert-manager`; the operator runs in `cert-manager-operator`.

**Evidence:** `README.md` — "automatically deploys a cluster-scoped CertManager object named cluster"; `pkg/operator/operatorclient/` — `TargetNamespace = cert-manager`; `api/operator/v1alpha1/certmanager_types.go` — singleton `cluster`; `deploy/examples/cluster-cert-manager.yaml`.

### IV. Feature Gate Discipline — TechPreview Gating via `features.go`

Addon operands are gated behind **feature gates** defined in `api/operator/v1alpha1/features.go`. Each feature gate has an explicit default and release level (e.g., TechPreview). New features MUST be registered in `features.go` with appropriate gating. Feature gate checks determine whether addon CRs are accepted and whether addon controllers are started.

**Evidence:** `api/operator/v1alpha1/features.go` — defines `IstioCSR`, `TrustManager` feature gates with defaults and links to OpenShift enhancements. `AGENTS.md` confirms: "Always read features.go for current defaults and links to OpenShift enhancements."

### V. Bindata / Manifest Regeneration — Never Hand-Edit, Always `make update`

Operand manifests under `bindata/` and generated code (`zz_generated.deepcopy.go`, client-gen output, CRD YAML under `config/crd/bases/`) are **generated artifacts**. They MUST NOT be hand-edited long-term. Changes to APIs or upstream operand versions require running `make update` (which chains `make manifests generate` plus `hack/update-*.sh` scripts). CI verification (`make verify`) will fail if generated outputs are stale.

**Evidence:** `bindata/` — generated from upstream via `hack/update-cert-manager-manifests.sh`, `hack/update-istio-csr-manifests.sh`; `api/operator/v1alpha1/zz_generated.deepcopy.go` — code-generated; `hack/verify-deepcopy.sh`, `hack/verify-crds.sh`, `hack/verify-clientgen.sh` — verify scripts detect drift; `AGENTS.md` — "Do not hand-edit vendor/ or long-term bindata/; use make update."

### VI. Verification-First Development — `make verify && make lint && make test`

All changes MUST pass the pre-merge verification loop: `make verify` (runs `hack/verify-*.sh` scripts for CRDs, deepcopy, client-gen, deps, bundle, protobuf, swagger, types), `make lint` (golangci-lint with `.golangci.yaml`), and `make test` (which runs `make manifests generate vet test-apis test-unit`). E2E tests (`make test-e2e`, build tag `e2e`) require a running cluster with stable operands. Unit tests exclude `test/e2e`, `test/apis`, `test/utils` directories.

**Evidence:** `Makefile` — `verify`, `lint`, `test`, `test-unit`, `test-e2e` targets; `hack/verify-*.sh` — 10+ verification scripts; `.golangci.yaml` — linter config with `e2e` build tag; `AGENTS.md` Testing instructions — explicit pre-merge loop.

### VII. RBAC Least Privilege — Scoped Permissions, Explicit Manifests

RBAC manifests are explicit and scoped. The operator's own RBAC is defined in `config/rbac/`. Operand RBAC (cert-manager controller, webhook, cainjector, Istio CSR) is defined in `bindata/` with separate ClusterRole/ClusterRoleBinding per component. New operand integrations MUST define explicit, minimal RBAC manifests rather than relying on broad cluster-admin grants. Dynamic RBAC for trust-manager bundle distribution aligns with FR-012 (minimum permissions for target namespaces).

**Evidence:** `config/rbac/role.yaml`, `config/rbac/role_binding.yaml` — operator RBAC; `bindata/cert-manager-deployment/controller/cert-manager-controller-certificates-cr.yaml` — per-function ClusterRoles; `bindata/istio-csr/cert-manager-istio-csr-clusterrole.yaml` — addon-specific RBAC.

### VIII. OLM Bundle and Release Conventions

The operator ships via OLM with bundle artifacts under `bundle/` and generation config under `config/manifests/`. The CSV (`bundle/manifests/cert-manager-operator.clusterserviceversion.yaml`) is generated via `make bundle`. Operand versions are pinned in the `Makefile` (`CERT_MANAGER_VERSION`, `ISTIO_CSR_VERSION`). Related images are managed via `RELATED_IMAGE_*` environment variables resolved in `pkg/controller/deployment/related_images.go`. New operands MUST add their version variable to `Makefile` and related image references to the CSV generation pipeline.

**Evidence:** `Makefile` — `BUNDLE_VERSION ?= 1.18.1`, `CERT_MANAGER_VERSION ?= "v1.18.4"`, `ISTIO_CSR_VERSION ?= "v0.14.2"`, `CHANNELS ?= "stable-v1,stable-v1.18"`; `bundle/manifests/cert-manager-operator.clusterserviceversion.yaml`; `pkg/controller/deployment/related_images.go`.

### IX. OpenShift API Integration — `operator.openshift.io` Group, OperatorSpec Embedding

Operator CRDs use the `operator.openshift.io` API group. The `CertManager` type embeds `github.com/openshift/api/operator/v1.OperatorSpec` which provides `managementState`, `unsupportedConfigOverrides`, and standard OpenShift operator lifecycle semantics (Managed/Unmanaged/Removed). Addon CRs live in `operator.openshift.io/v1alpha1`. New CRs MUST follow this API group and embed the appropriate OpenShift operator spec types.

**Evidence:** `api/operator/v1alpha1/certmanager_types.go` — embeds `operatorv1.OperatorSpec`; `config/crd/bases/operator.openshift.io_certmanagers.yaml`, `operator.openshift.io_istiocsrs.yaml` — API group; `go.mod` — `github.com/openshift/api v0.0.0-20250710004639-926605d3338b`.

### X. Network Policy Management — Default-Deny with Explicit Allow Rules

The operator manages network policies for operand namespaces following a default-deny pattern with explicit ingress/egress allow rules per component. Network policy manifests are stored in `bindata/networkpolicies/`. Changes are reconciled through the deployment controller.

**Evidence:** `bindata/networkpolicies/cert-manager-deny-all-networkpolicy.yaml` — default deny; `bindata/networkpolicies/cert-manager-allow-egress-to-api-server-networkpolicy.yaml` — explicit allow; `pkg/controller/deployment/cert_manager_networkpolicy.go` — reconciliation logic; git log `08326d92` — "Updates cert-manager NP support only for CoreController."

## Additional Constraints

- **Go version**: Match the `go.mod` directive — currently `go 1.24.4`. — **Evidence:** `go.mod` line `go 1.24.4`
- **Vendor mode**: Dependencies are vendored; use `modules-download-mode: vendor`. Do not modify `vendor/` directly; use `make update-vendor`. — **Evidence:** `.golangci.yaml` `modules-download-mode: vendor`; `AGENTS.md` — "Do not hand-edit vendor/"
- **Container base image**: Production images use `ubi9-minimal:9.2`; operator binary runs as non-root (`USER 65532:65532`). — **Evidence:** `Dockerfile` — `FROM registry.access.redhat.com/ubi9-minimal:9.2`, `USER 65532:65532`
- **Import ordering**: Local imports (`github.com/openshift/cert-manager-operator`) placed after third-party imports. — **Evidence:** `.golangci.yaml` `goimports.local-prefixes: github.com/openshift/cert-manager-operator`
- **Linter set**: Explicit allowlist — `errcheck`, `gofmt`, `goimports`, `gosec`, `gosimple`, `govet`, `ineffassign`, `misspell`, `staticcheck`, `typecheck`, `unused`. No additional linters without `.golangci.yaml` update. — **Evidence:** `.golangci.yaml` `linters.enable` list
- **Namespace hardcoding**: Operator namespace = `cert-manager-operator`; operand namespace = `cert-manager`. Both are hardcoded. — **Evidence:** `README.md` — "Both those namespaces are hardcoded"; `pkg/controller/common/` constants
- **Operand version pinning**: Operand versions are pinned in `Makefile` variables, not in Go code. Version bumps require updating `Makefile` and running `make update` to regenerate bindata and bundle. — **Evidence:** `Makefile` — `CERT_MANAGER_VERSION`, `ISTIO_CSR_VERSION`; git log `1b63525f` — "Updates operator, operand version to 1.18.1, 1.18.4"
- **Test framework**: Ginkgo v2 + Gomega for e2e and API tests; standard `testing` + `testify` for unit tests. — **Evidence:** `go.mod` — `github.com/onsi/ginkgo/v2`, `github.com/onsi/gomega`, `github.com/stretchr/testify`; `test/e2e/suite_test.go`
- **E2E build tag**: E2E tests are gated behind the `e2e` build tag. They are excluded from `make test-unit`. — **Evidence:** `.golangci.yaml` `build-tags: [e2e]`; `AGENTS.md` — "E2E (test/e2e/, tag e2e)"
- **CI system**: Prow via `openshift/release`; this repo does not ship `.github/workflows`. — **Evidence:** `AGENTS.md` — "jobs often live in openshift/release (Prow); this repo may not ship .github/workflows"

## Development Workflow

| Activity | Requirement | Evidence |
|----------|-------------|----------|
| Local unit tests | `make test` (chains `manifests`, `generate`, `vet`, `test-apis`, `test-unit`) | `Makefile` `test` target |
| Full verify | `make verify` (runs all `hack/verify-*.sh` scripts) | `hack/verify-*.sh` (10+ scripts) |
| Lint | `make lint` (golangci-lint with `.golangci.yaml`) | `Makefile` `lint` target, `.golangci.yaml` |
| Codegen refresh | `make update` after any API type change, operand version bump, or dependency update | `hack/update-*.sh`, `AGENTS.md` "After API edits" |
| E2E tests | `make test-e2e` (requires running cluster with stable operands); narrow with `TEST=` or `E2E_GINKGO_LABEL_FILTER=` | `Makefile` `test-e2e` target |
| Bundle generation | `make bundle` after CSV or CRD changes | `Makefile` `bundle` target, `hack/verify-bundle.sh` |
| Image build | `make image-build image-push` with `IMG`, `CONTAINER_ENGINE` env vars | `Makefile`, `Dockerfile` |
| Local dev run | `make deploy && oc scale --replicas=0 deploy --all -n cert-manager-operator && make local-run` | `README.md`, `hack/local-run-config.yaml` |
| PR pre-merge | `make verify && make lint && make test`; commit all generated outputs | `AGENTS.md` PR instructions |
| PR scope | Small diffs; follow existing library-go / controller-runtime patterns; update docs for user-visible changes | `AGENTS.md` PR instructions |

## Agent Routing

| Agent ID | Scope | When to route |
|----------|-------|---------------|
| Tier 1 Hub (openshift/enhancements ai-docs) | Generic OpenShift operator patterns, testing guidance, security practices | Cross-cutting platform questions not specific to cert-manager-operator |
| This repo AGENTS.md | Component-specific controller map, Make targets, test tags, PR hygiene | All cert-manager-operator development tasks |
| README.md | Human quick-start, install, upgrade, local run | Setup and onboarding tasks |
| docs/ (proxy.md, operand_metrics.md, cloud_credentials.md) | Platform integration details | Proxy, metrics, or cloud credential tasks |
| api/operator/v1alpha1/ | API types, feature gates, conditions | API design, feature gating, status reporting tasks |
| pkg/controller/{deployment,istiocsr,trustmanager}/ | Controller implementation patterns | New controller or reconciliation tasks — follow existing sibling pattern |

## Governance

- This constitution supersedes ad-hoc conventions for downstream Planning, Task Creation, and Code Generation agents.
- **Amendments:** require documented evidence of repo change; bump Version and Last Amended date.
- **Conflicts:** if spec contradicts constitution, escalate in plan.md §8 — do not silently override. For example, if the spec requests behavior that would require forking upstream cert-manager logic into the operator, this must be flagged as a constitution violation.
- **Companion docs:**
  - **AGENTS.md** takes precedence for agent routing, controller map, Make target documentation, and PR/testing instructions.
  - **README.md** takes precedence for human-facing install/upgrade/local-run procedures.
  - **This constitution** takes precedence for architectural principles and non-negotiable guardrails that downstream agents must follow.
  - **docs/** takes precedence for domain-specific platform integration details (proxy, metrics, cloud credentials).
- **Complexity:** new patterns must justify deviation from existing repo conventions with explicit rationale. Adding a new controller framework (beyond library-go for core or controller-runtime for addons) requires constitution amendment.
- **Addon operand pattern:** Trust-manager integration MUST follow the established Istio CSR addon pattern: feature gate in `features.go`, CR in `api/operator/v1alpha1/`, controller under `pkg/controller/trustmanager/`, manifests under `bindata/`, controller-runtime reconciler wired via `setup_manager.go`. Deviation from this pattern requires explicit justification.