# Template Gaps — agents.md
**Round:** 1 | **Source issues:** SSCSI-235-BUG-1 (I-003), network-policy-redesign (I-005),
                                   SSCSI-235-GAP-2 (I-001)

---

## Gap AG-1: Missing CSO Network Policy Label Hook
**Severity:** patchable
**Pattern:** platform-pattern-reinvention (I-005)

**Current state:**
agents.md documents the operator's CSIControllerSet architecture, assets embedding, ClusterCSIDriver
singleton, and test patterns. It does not document the CSO-managed network policy hook.

**Gap:**
The operator's pods (node DaemonSet in assets/node.yaml) carry the label:
  `openshift.storage.network-policy.api-server: allow`
This label opts pods into CSO-managed egress-to-api-server network policies, avoiding the need
to create custom standalone policies. This is a non-obvious platform hook that implementation
agents must know about to avoid reimplementing what CSO already provides.

**Recommended addition (patchable):**
Add to agents.md Architecture Patterns or a new "Platform Integration Hooks" section:
```
## Platform Integration Hooks
- **CSO network policy hook**: DaemonSet pods in assets/node.yaml carry label
  `openshift.storage.network-policy.api-server: allow`. This opts pods into
  CSO-managed egress network policies. Do NOT create standalone egress-to-api-server
  NetworkPolicy objects — use this label instead.
  Evidence: assets/node.yaml, commit 796a110a.
```

---

## Gap AG-2: Missing ConsoleQuickStart Navigation Token Guidance
**Severity:** patchable
**Pattern:** console-api-token-version-coupling (I-003)

**Current state:**
agents.md has no section on ConsoleQuickStart resources or console navigation tokens.

**Gap:**
The operator ships ConsoleQuickStart resources that use navigation highlight tokens (e.g.,
`{{highlight qs-nav-ecosystem}}`). These tokens are version-dependent and changed between OCP
releases (e.g., "Operators" → "Ecosystem" rename). Implementation agents must know to verify
the current token against the target OCP release, not copy from older examples.

**Recommended addition (patchable):**
Add to agents.md OLM/Bundle section (or create a Console Resources section):
```
## Console Resources (ConsoleQuickStart, ConsoleYAMLSample)
- Manifests live in config/manifests/stable/ (OLM bundle) and demo/console/ (standalone)
- Navigation tokens ({{highlight qs-nav-*}}) are OCP version-dependent
  - Current token as of OCP 4.18+: qs-nav-ecosystem (was: qs-nav-operator-hub before rename)
  - Verify token against target OCP release before implementation
- Bundle annotations required for all ConsoleQuickStart in config/manifests/stable/:
    capability.openshift.io/name: "Console"
    include.release.openshift.io/ibm-cloud-managed: "true"
    include.release.openshift.io/self-managed-high-availability: "true"
    include.release.openshift.io/single-node-developer: "true"
```

---

## Gap AG-3: Missing OLM Bundle Annotation Convention
**Severity:** patchable
**Pattern:** OLM-bundle-annotation-omission (I-001)

**Current state:**
agents.md has no section explicitly listing required annotations for OLM bundle resources.

**Gap:**
Any resource added to `config/manifests/stable/` requires the full set of deployment-profile
annotations. Without this being documented in agents.md, task agents producing bundle resources
will omit them (as happened in SSCSI-235 PR #94).

**Recommended addition (patchable):**
Add to agents.md OLM bundle section:
```
## OLM Bundle Resource Conventions
All resources in config/manifests/stable/ that should appear on managed/HA/SNO deployments
MUST carry:
  capability.openshift.io/name: "<capability name>"
  include.release.openshift.io/ibm-cloud-managed: "true"
  include.release.openshift.io/self-managed-high-availability: "true"
  include.release.openshift.io/single-node-developer: "true"
The capability name depends on resource type: "Console" for QuickStarts, "Storage" for
storage controllers. Verify against existing bundle resources.
```

---

## Gap AG-4: No Install QuickStart Exclusion Note
**Severity:** patchable (low — operator-specific design decision)
**Pattern:** OLM-bundle-placement-decision (I-002)

**Current state:**
agents.md has no note about why the install QuickStart does NOT live in the OLM bundle.

**Gap:**
Future agents might add an install QuickStart to the OLM bundle without understanding the circular
dependency problem (OLM installs the operator, but the QuickStart explains how to install it via
OLM — it appears only after the install is already complete).

**Recommended addition (patchable):**
Add to agents.md Console Resources section:
```
- sscsi-install-quickstart.yaml lives in demo/console/ ONLY — not in the OLM bundle.
  Rationale: bundling an install QuickStart creates circular dependency (resource appears
  after OLM completes install, which is what the QuickStart was trying to guide).
  Only sscsi-example-quickstart.yaml and YAML samples live in config/manifests/stable/.
```

---

## No gaps found in: Per-task testing strategy, execution agent routing format, stage-specific hints structure
