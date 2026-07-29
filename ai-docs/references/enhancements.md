# Enhancement Proposals & Design Documents

This document catalogs design documentation for the Secrets Store CSI Driver Operator.

## Enhancement Proposals (openshift/enhancements)

### Implemented

- **[Secrets Store CSI Driver Support](https://github.com/openshift/enhancements/blob/master/enhancements/storage/csi-secrets-store.md)** (storage/csi-secrets-store.md)  
  Status: Implemented  
  Summary: Introduces support for mounting secrets from external secret stores (Azure Key Vault, GCP Secret Manager, HashiCorp Vault) via the Secrets Store CSI Driver. Defines the operator architecture, OLM packaging, RBAC model, and integration with OpenShift storage.

## Local Design Documents

### Component Guidelines (ai-docs/guidelines/)

The following guidelines are component-specific operational conventions, not enhancement proposals:

- **[Security Guidelines](../guidelines/security-guidelines.md)** - RBAC principles, SCC usage, container security contexts, TLS/cert management, host path security, image reference patterns
- **[Performance Guidelines](../guidelines/performance-guidelines.md)** - Informer scoping, resource requests/limits, metrics, liveness probes, leader election, static resource management, DaemonSet update strategy
- **[Error Handling Guidelines](../guidelines/error-handling-guidelines.md)** - Error wrapping, klog usage, operator status conditions, Sync return patterns, fatal error policy, test error handling
- **[Testing Guidelines](../guidelines/testing-guidelines.md)** - Table-driven test patterns, test organization, library-go fakes, assertion style, Makefile targets, E2E testing

**Note**: These are operational conventions for code contributions, not architectural decisions. For architectural decisions, see [ADRs](../decisions/).

## Upstream Design Documents

- **[Secrets Store CSI Driver Design](https://github.com/kubernetes-sigs/secrets-store-csi-driver/blob/main/docs/book/src/README.md)** - Upstream CSI driver architecture and provider interface specification
- **[Provider Interface](https://github.com/kubernetes-sigs/secrets-store-csi-driver/blob/main/docs/book/src/providers.md)** - gRPC interface specification for provider plugins

## Related Cross-Component Enhancements

- **[CSI Driver Framework](https://github.com/openshift/enhancements/tree/master/enhancements/storage)** - General CSI driver support in OpenShift
- **[OLM Integration](https://github.com/openshift/enhancements/tree/master/enhancements/olm)** - Operator Lifecycle Manager patterns used by this operator

---

**Note**: This catalog is a pointer to design documents, not a replacement for them. Always consult the source document for full context.
