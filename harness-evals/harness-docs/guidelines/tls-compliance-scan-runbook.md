# TLS Compliance-Scan Verification Runbook

**Scope:** Verifying that the Secrets Store CSI Driver Operator's own HTTPS metrics endpoint
(`:8443`) honors the cluster's centrally-configured `TLSSecurityProfile`
(`apiserver.config.openshift.io/cluster`). This covers **US-003 / FR-007 / SC-001** of SSCSI-264
("Centralized & enforced TLS configuration").

## Scope decision: pod-IP-direct scanning (no `Service` change)

This runbook assumes the external TLS compliance-scan mechanism (referred to only as
`tls-scanner` in Jira comments for SSCSI-264, and intentionally not consulted for this change's
design — see `plan.md` §1) can reach the operator pod's IP directly on port `8443`, **without**
requiring a real, traffic-routing `Service` in front of it. Today's
`secrets-store-csi-driver-operator-metrics-service.yaml` is explicitly a "fake port" (per its own
inline comment) that does not route traffic — this runbook does **not** change that.

This is `plan.md` §8 Open Question #1's own pre-authorized **default assumption**, applied
because no SME/CI-config answer was available before Task Creation for this change's Phase 3. If
a future investigation into the actual `tls-scanner`/`openshift/release` CI wiring determines that
a real `Service` is required (branch (b) in `plan.md` §5 Phase 3), **this runbook alone does not
resolve that** — Phase 3 would need to be reopened with a task that changes
`config/manifests/stable/secrets-store-csi-driver-operator-metrics-service.yaml`, not just this
document.

## Prerequisite

The mechanism this runbook verifies is delivered by SSCSI-264 Phases 1–2:
`pkg/operator/tls_serving_config.go`'s `ResolveTLSServingProfile`/`WriteTLSServingConfigFile`,
wired into `cmd/secrets-store-csi-driver-operator/main.go` (bootstrap) and
`pkg/operator/starter.go` (live `APIServer` informer watch), with the operator's CSV `args`
passing `--config=/tmp/tls-serving-config.yaml`. Confirm the operator is running with these
changes deployed before using this runbook — a pre-Phase-1 operator always serves a static
Intermediate-equivalent default regardless of the cluster's actual `TLSSecurityProfile`, and a
scan against it will not reflect the current profile.

## Step 1 — Identify the operator pod's IP

```bash
oc get pods -n openshift-cluster-csi-drivers -l app=secrets-store-csi-driver-operator \
  -o custom-columns=NAME:.metadata.name,IP:.status.podIP,READY:.status.containerStatuses[0].ready
```

Confirm `READY` is `true` before proceeding — see the restart-window caveat below.

## Step 2 — Confirm the cluster's currently configured profile

```bash
oc get apiserver cluster -o jsonpath='{.spec.tlsSecurityProfile}{"\n"}'
```

An empty/absent result means the cluster is using the platform default (`Intermediate`).

## Step 3 — Manual handshake spot-check with `openssl s_client`

Run these directly against the pod IP from Step 1 (substitute `<pod-ip>`):

**Expected-pass case** — a protocol version within the resolved profile (e.g. `Intermediate`
permits TLS 1.2 and 1.3):

```bash
openssl s_client -connect <pod-ip>:8443 -tls1_2 </dev/null
openssl s_client -connect <pod-ip>:8443 -tls1_3 </dev/null
```

Both should complete the handshake (look for `Verify return code` and a negotiated `Cipher` line
in the output, not a connection/handshake error).

**Expected-fail case** — a protocol version outside the resolved profile. For any profile at or
stricter than `Intermediate` (the platform default), TLS 1.1 must be rejected:

```bash
openssl s_client -connect <pod-ip>:8443 -tls1_1 </dev/null
```

This should fail the handshake (e.g. `ssl3 alert handshake failure` or a similar TLS-alert
error) — a **successful** handshake here would indicate the resolved profile is not actually
being enforced by the server, and is a compliance/regression finding.

To check that a specific cipher outside a `Custom` profile's list is rejected, add `-cipher`
with an out-of-list OpenSSL cipher name to either command above and confirm the handshake fails.

## Step 4 — Automated compliance scan

Point your available scanning tool directly at `<pod-ip>:8443`. Two common options:

```bash
# Generic, widely available nmap script — reports offered protocols/ciphers for manual review
nmap --script ssl-enum-ciphers -p 8443 <pod-ip>

# Or, if you have access to the openshift/release "tls-scanner" tooling referenced in
# SSCSI-264's Jira comments: invoke it per its own documentation, targeting <pod-ip>:8443
# directly. This runbook does not have visibility into that tool's exact invocation —
# consistent with plan.md's explicit non-consultation of it for this change's design.
```

**Pass criterion (SC-001):** the scan reports zero findings for protocols or cipher suites
outside the profile confirmed in Step 2. Any offered protocol/cipher not present in
`configv1.TLSProfiles[<profile-type>]` (or, for a `Custom` profile, not in
`spec.tlsSecurityProfile.custom.ciphers`) is a compliance finding and should be investigated
before considering this change SC-001-complete for that cluster.

## Caveat: restart-on-profile-change window

Per `plan.md` §7's documented risk, this operator's design (Phase 1/T1_2) picks up a
`TLSSecurityProfile` change by writing a new config file and relying on the existing
restart-on-file-change mechanism to gracefully restart the operator process. A scan run
**immediately** after changing `apiserver.config.openshift.io/cluster`'s `spec.tlsSecurityProfile`
may transiently hit a pod that is mid-restart. If a scan run in this window reports a connection
refusal or an unexpected timeout (rather than a protocol/cipher finding), re-confirm the pod's
`Ready` status (Step 1) and retry the scan once the pod is ready again, rather than treating the
transient failure itself as a compliance finding.

## References

- `plan.md` §5 Phase 3, §6 (verification matrix), §8 Open Question #1 — source of this runbook's
  scope decision.
- `implementation/task-reports/T1_2.md` — the original manual-verification step this runbook
  generalizes for an external auditor.
- [Component Architecture](../architecture/components.md)
