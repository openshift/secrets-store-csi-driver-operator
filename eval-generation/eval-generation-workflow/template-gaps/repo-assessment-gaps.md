# Template Gaps — repo-assessment-template.md
**Round:** 1 | **Source issues:** SSCSI-235-BUG-1 (I-003), network-policy-redesign (I-005)

---

## Gap RA-1: §10.5 Console/UI Integration — No Navigation Token Version Guidance
**Severity:** patchable
**Pattern:** console-api-token-version-coupling (I-003)

**Current state:**
§10.5 lists "Console plugins, YAML samples, quickstarts; CLI tooling, dashboards" in one line.
It does not require the agent to document console navigation token names, their stability, or
version coupling.

**Gap:**
For operators that ship ConsoleQuickStart resources, the repo-assessment agent must document:
- Which `qs-nav-*` tokens are in use in existing manifests (e.g., `qs-nav-ecosystem`)
- That these tokens are version-coupled to the OpenShift Console and may change across OCP releases
- How to verify the current token for the target OCP version (search console-operator source or OCP docs)

Without this, implementors copy tokens from examples that may already be outdated.

**Recommended addition (patchable):**
Add to §10.5 guidance: "For ConsoleQuickStart resources: enumerate all `qs-nav-*` navigation
tokens used or referenced, note which OCP version introduced them, and call out that tokens are
version-coupled. Mark any token used in the repo as verified/unverified against the target
release branch."

---

## Gap RA-2: §5 Reusable Assets + §10.2 Network Config — No CSO Network Policy Hook
**Severity:** patchable
**Pattern:** platform-pattern-reinvention (I-005)

**Current state:**
§5 says "Existing functions, components, or utilities the Planner MUST use instead of
reimplementing" with a generic per-asset format. §10.2 covers proxy settings and trusted CA
injection. Neither section prompts the agent to survey platform-level network policy hooks.

**Gap:**
OpenShift storage operators can opt into CSO-managed egress network policies by adding a pod
label. If the repo-assessment does not document this, the planner will design standalone
network policies and miss the platform hook (as happened in the network-policy feature).

**Recommended addition (patchable):**
Add to §10.2 guidance: "For OpenShift storage operators: check whether CSO (cluster-storage-
operator) manages shared network policies for storage workloads. If so, document the pod label
keys that opt pods into those policies. List them in §5 as reusable assets with exact label
key/values."

---

## Gap RA-3: §10.6 Packaging/OLM — No Annotation Requirements for Bundle Resources
**Severity:** patchable (secondary to RA-1)
**Pattern:** OLM-bundle-annotation-omission (I-001)

**Current state:**
§10.6 covers CSV structure, upgrade path, channels, installModes. It does not list required
annotations for individual bundle resources (ConsoleQuickStart, ConsoleYAMLSample, etc.).

**Gap:**
The repo-assessment should enumerate which annotation keys are required on every resource that
enters the OLM bundle. Without this, task agents don't know to add them.

**Recommended addition (patchable):**
Add to §10.6: "For each resource type shipped in the OLM bundle, document required metadata
annotations (e.g., capability.openshift.io/name, include.release.openshift.io/*) and their
target values for this operator's deployment profiles (HA, IBM Cloud Managed, SNO)."

---

## No gaps found in: §1–§4 core architecture sections, §7 change cascade format, §8 test structure
