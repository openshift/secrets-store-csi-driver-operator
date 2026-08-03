# Secrets Store CSI Driver Operator - Agentic Documentation

**Component**: OpenShift Secrets Store CSI Driver Operator  
**Repository**: openshift/secrets-store-csi-driver-operator  

> **For AI Agents**: Start with `harness-evals/harness-docs/domain/` to understand SecretProviderClass APIs, then read `harness-evals/harness-docs/architecture/components.md` for implementation patterns. Check `harness-evals/harness-docs/decisions/` for architectural constraints before proposing changes. Use `harness-evals/harness-docs/SECRETS_STORE_DEVELOPMENT.md` for common tasks.
>
> **Generic Platform Patterns**: See Platform documentation (openshift/enhancements/ai-docs/) for operator patterns, testing practices, security guidelines, and cross-repo ADRs.

## What is Secrets Store CSI Driver Operator?

Manages the lifecycle of the Secrets Store CSI Driver on OpenShift clusters, enabling pods to mount secrets from external secret stores (Azure Key Vault, GCP Secret Manager, HashiCorp Vault) as volumes.

**Key Principle**: Cluster-scoped, removable operator using library-go CSI controller framework with static resource management.

## Core Components

**Operator**: library-go CSI controller set (pkg/operator/starter.go) | **Operand**: DaemonSet running CSI driver + sidecars on every Linux node | **API**: ClusterCSIDriver CR (secrets-store.csi.k8s.io)

**Quick Start**: `oc describe clustercsidrivers/secrets-store.csi.k8s.io` | `oc get daemonset -n openshift-cluster-csi-drivers secrets-store-csi-driver-node`

## Critical Patterns

**1. NEVER edit assets without updating embed directive**  
When adding files to `assets/` subdirectories, update `//go:embed` in `assets/assets.go`. Missing glob → runtime panic.

**2. Static resources via ConditionalStaticResourcesController**  
RBAC, ServiceAccount, CSIDriver, ConfigMap, NetworkPolicy use `WithConditionalStaticResourcesController`. DaemonSet uses `WithCSIDriverNodeService`. DO NOT mix.

**3. Management state determines resource lifecycle**  
`Managed` → sync resources | `Unmanaged` → skip sync | `Removed` OR `DeletionTimestamp != nil` → delete conditional resources (pkg/operator/starter.go:150)

## Documentation Structure

```text
harness-evals/harness-docs/
├── domain/                           # SecretProviderClass + PodStatus CRDs
├── architecture/
│   └── components.md                 # Repo layout, controller set, apply patterns
├── decisions/                        # Component ADRs
├── exec-plans/                       # Feature planning
├── guidelines/                       # Security, performance, error-handling, testing conventions
├── references/
│   ├── ecosystem.md                  # Links to Platform
│   └── enhancements.md               # Enhancement proposals catalog
├── SECRETS_STORE_DEVELOPMENT.md      # Dev workflows
└── SECRETS_STORE_TESTING.md          # Test suites
```

**Exec-Plans**: Use `active/` for new features. See Platform Exec-Plans Guide.

**Platform Patterns**: [Operator](https://github.com/openshift/enhancements/tree/master/ai-docs/platform/operator-patterns/) | [Testing & Security](https://github.com/openshift/enhancements/tree/master/ai-docs/) (dedicated sections not yet published upstream)

**Retrieval Strategy**: For implementation tasks, always check `harness-evals/harness-docs/architecture/components.md` for verified patterns (apply methods, informer scoping, controller framework). For API questions, see `harness-evals/harness-docs/domain/`. For "why" questions, see `harness-evals/harness-docs/decisions/`.

**Common Pitfalls** (verified in code):

| Anti-Pattern | Fix |
|-------------|-----|
| Add asset subdirectory without updating `//go:embed` | Update `assets/assets.go:7` directive |
| Use all-namespace informers | Use `NewKubeInformersForNamespaces` (ADR-0003) |
| Add DaemonSet to ConditionalStaticResourcesController | Use `WithCSIDriverNodeService` instead |
| Hardcode namespace in YAML | Use `${NAMESPACE}` placeholder |
| Panic in reconciliation | Return errors (ADR-0002: panic only for build-time bugs) |

## External References

- [Product Docs](https://docs.openshift.com/container-platform/latest/storage/container_storage_interface/persistent-storage-csi-secrets-store.html)
- [Upstream CSI Driver](https://github.com/kubernetes-sigs/secrets-store-csi-driver)
- [OpenShift Fork](https://github.com/openshift/secrets-store-csi-driver)

---

**Platform Documentation**: openshift/enhancements/ai-docs/
