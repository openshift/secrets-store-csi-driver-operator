# Template Gaps — code-generation-template.md
**Round:** 1 | **Source issues:** SSCSI-235-GAP-2 (I-001), network-policy-redesign (I-005)

---

## Gap CG-1: No Annotation Verification Step for OLM Bundle Manifests
**Severity:** patchable
**Pattern:** OLM-bundle-annotation-omission (I-001)

**Current state:**
The code-generation template instructs agents to produce code/manifests from task payloads.
For manifest-only tasks, there is no generated checkpoint to verify OLM bundle annotations.

**Gap:**
When a code-generation task produces or modifies files in `config/manifests/stable/`, the
agent should be instructed to verify (before outputting the final manifest) that all required
deployment-profile annotations are present. This is a pre-output validation step.

**Recommended addition (patchable):**
Add to the code-generation template: "For tasks producing OLM bundle resources: before
finalizing output, self-check that the manifest includes: capability.openshift.io/name,
include.release.openshift.io/ibm-cloud-managed, include.release.openshift.io/self-managed-
high-availability, include.release.openshift.io/single-node-developer. If task payload does
not specify these values, flag as UNVERIFIED rather than omitting them."

---

## Gap CG-2: No Platform-Pattern Survey Instruction for Infrastructure Tasks
**Severity:** eval-only
**Pattern:** platform-pattern-reinvention (I-005)

**Current state:**
Code-generation tasks for new infrastructure (network policies, RBAC) proceed directly from
task payload to implementation without a platform-survey step.

**Gap:**
For tasks that add network policies, the code-generation agent should check whether the task
payload includes a "platform pattern reuse" note. If missing, it should not silently implement
standalone policies.

**Recommended addition (eval-only):**
This is best enforced at the task payload level (tasks-template, TK-1) rather than in code
generation. Deferred to eval case for tasks stage.

---

## No gaps found in: oape_command tagging, multi-command routing, eval schema structure
