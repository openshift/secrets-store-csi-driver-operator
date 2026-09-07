# ADR-0004: gRPC Socket TLS/mTLS Scope

**Status**: Accepted (SME ratification pending — see Consequences/Negative)
**Date**: 2026-09-07
**Deciders**: SSCSI-264 implementation (default per technical_plan.md §8 Q2; not yet synchronously confirmed by a security/product SME)
**Component**: Secrets Store CSI Driver Operator

## Context

SSCSI-264 ("Centralized TLS Configuration for Secrets Store CSI Driver Operator and
Operand") scoped four TLS-relevant surfaces for this component: the operand metrics
endpoint (Phase 1, US-001), the operator's own management server (Phase 2, US-002),
the operand pprof endpoint (Phase 3, US-003), and the CSI gRPC Unix domain socket
(this phase, US-004). The originating spec (`specs.md`) left FR-007 marked
`[NEEDS CLARIFICATION]`: whether the gRPC socket requires additional TLS/mTLS
protection, or whether its existing access-control mechanism remains sufficient.

The socket in question is `unix:///csi/csi.sock` (`CSI_ENDPOINT` env var,
`assets/node.yaml`), exposed via a `plugin-dir` `hostPath` volume at
`/var/lib/kubelet/plugins/csi-secrets-store/` and mounted identically into the
`csi-driver`, `csi-node-driver-registrar`, and `csi-livenessprobe` containers of the
same DaemonSet pod. Kubernetes' `HostToContainer`/`Bidirectional` mount propagation
and standard Unix filesystem permissions on this hostPath are the socket's only
access-control mechanism today — this is a local, same-node, same-kubelet-managed
IPC channel between the kubelet's CSI plugin registration/RPC machinery and this
driver's own containers, not a network-exposed endpoint.

`repo_assessment.md` §3/§11 confirms **zero TLS/mTLS code exists for this socket
today**, and this change's Phase 4 (T4_1) confirmed that Phases 1–3's DaemonSet hook
work (metrics-endpoint and pprof-endpoint TLS scaffolding) never touched this
socket's env var, volume, volumeMount, or RBAC in any way — see
`openspec/changes/sscsi-264/implementation/T4_1-decision-note.md` for the full
byte-for-byte diff confirmation against this change's pinned baseline commit
(`58653d56123b2b66776285243cf2e3f2f6016126`).

**Scope**: This ADR is component-specific. For cross-repo decisions, see [Platform ADRs](https://github.com/openshift/enhancements/tree/master/ai-docs/).

## Decision

The gRPC Unix domain socket's existing filesystem-permission-based access control
**remains sufficient**. TLS/mTLS is **explicitly out of scope** for this socket in
SSCSI-264 (and, absent a superseding decision, for this component generally). No new
certificate delivery, no new Secret/Service, and no code change are introduced for
this socket by this change.

## Rationale

**Why:** The socket is not network-exposed — it exists only as a `hostPath`-mounted
file on the node's local filesystem, readable only by processes with filesystem
access to that path (effectively, containers sharing the same `plugin-dir` volume
mount within this DaemonSet's own pod, and the kubelet itself). `specs.md` frames
US-004 as the "lowest risk" of the four TLS-relevant surfaces in this change precisely
because of this local-only threat model — TLS/mTLS defends against network-level
interception and impersonation, neither of which applies to a same-node Unix domain
socket in the way it does to the metrics (`:8095`), pprof (`:6065`), or operator
management server (`:8443`) network listeners covered by Phases 1–3.

**How to apply:** Any future contributor proposing to add TLS/mTLS to this socket, or
to modify its access-control mechanism, should first read this ADR and confirm
whether the local-only threat model it documents has changed (e.g., if the socket
were ever exposed beyond the local node/pod boundary) before proceeding — and should
author a **superseding** ADR rather than silently reintroducing a decision this one
already closed.

## Consequences

### Positive
- No new crypto/cert-delivery complexity is introduced for this socket, consistent
  with its local-only, filesystem-permission-scoped threat model.
- Zero additional attack surface (no new Service, Secret, or listening TCP port) is
  added for this specific concern.
- Keeps this phase's implementation footprint at zero code changes, matching
  `technical_plan.md` §5 Phase 4's framing of this phase as documentation-only.

### Negative
- If a future security review (or a change to how this socket is exposed — e.g.,
  beyond the local node/pod boundary) determines mTLS is actually required, that
  would require a **superseding ADR** and new implementation work; this decision is
  not a permanent guarantee that the local-only threat model will never change.
- This ADR's `Deciders` reflects `technical_plan.md` §8 Q2's stated default
  assumption, not a synchronous SME (security/product owner, or downstream
  `openshift/secrets-store-csi-driver` maintainer) sign-off — see the open question
  below. Treat as ratified-by-default, not yet permanently closed, until that
  sign-off is obtained.

### Neutral
- No change to this component's CI/build/test surface for this socket — no new
  `_test.go` files, no new Makefile target, no new manifest to validate.
- `harness-evals/harness-docs/domain/` is unchanged; if a future contributor adds a
  domain doc describing the gRPC socket's access-control model, it should
  cross-reference this ADR (per `technical_plan.md` §5 Phase 4's target-files note,
  authoring that doc was out of this task's scope).

## Alternatives Considered

### Alternative 1: Add mTLS to the gRPC socket
**Description**: Require mutual TLS authentication for gRPC calls over
`unix:///csi/csi.sock`, mirroring the certificate-delivery pattern already
established for the metrics (Phase 1) and pprof (Phase 3) endpoints.
**Rejected because**:
- The socket is already local-filesystem-scoped, not network-exposed — mTLS defends
  against a threat (network interception/impersonation) this socket's access
  pattern does not present.
- No existing precedent or library for gRPC-over-mTLS on a Unix domain socket was
  found anywhere in this repo (`repo_assessment.md` §2/§5) — building this from
  scratch would be new, unreviewed cryptographic infrastructure for a socket whose
  current access-control mechanism has no documented incident or gap.
- Would meaningfully expand this component's scope (new cert lifecycle, new failure
  modes for socket-level RPCs that today only fail on standard filesystem-permission
  errors) without a confirmed threat-model justification.

### Alternative 2: Defer the decision (leave FR-007 as `[NEEDS CLARIFICATION]`)
**Description**: Do not resolve FR-007 in this change; leave the socket's TLS/mTLS
posture as an open question for a future change.
**Rejected because**:
- `technical_plan.md` explicitly scoped Phase 4 as the vehicle to close this
  specific `[NEEDS CLARIFICATION]` marker via a documented default assumption
  (§8 Q2), consistent with how Phases 1 and 3 closed their own analogous open
  questions (FR-003, FR-009) via documented defaults rather than leaving them open.
  Leaving FR-007 unresolved would leave US-004 without any deliverable at all.

## References

- `openspec/changes/sscsi-264/specs.md` — US-004, FR-007
- `technical_plan.md` §5 Phase 4, §8 Q2
- `repo_assessment.md` §3, §11
- `openspec/changes/sscsi-264/implementation/T4_1-decision-note.md` — confirmed-unaffected finding this ADR's Context/Decision rely on
- [Platform ADRs](https://github.com/openshift/enhancements/tree/master/ai-docs/) for cross-repo decisions
