# RHCOS10 UBI9 Compatibility Test

## Summary

Validate that the existing UBI9/RHEL9-based images run correctly on RHCOS10 cluster
nodes without any changes.

RHCOS10 ships with RHEL10 as its host OS. This PR triggers CI against an RHCOS10 cluster
to confirm RHEL9-based containers remain compatible before committing to a base image
migration.

**No Dockerfile or manifest changes.** This is a baseline/smoke test only.

## Images Under Test

| Image | Current Base |
|-------|-------------|
| `secrets-store-csi-driver-operator` | `registry.ci.openshift.org/ocp/builder:rhel-9-golang-1.24-openshift-4.22` (builder) / `registry.ci.openshift.org/ocp/4.22:base-rhel9` (runtime) |
| `secrets-store-csi-driver-mustgather` | `registry.ci.openshift.org/ocp/4.22:must-gather` |

CI build root: `rhel-9-release-golang-1.24-openshift-4.22`

## Test Matrix

| Cluster | Test Suite | Expected |
|---------|-----------|---------|
| RHCOS10 | e2e-azure-rhcos10 | Pass |
| RHCOS10 | e2e-azure-rhcos10-fips | Pass |

## Expected Outcome

- All existing e2e tests pass on RHCOS10 nodes with UBI9-based images.
- No runtime compatibility issues between RHEL9 containers and RHCOS10 hosts.

## Follow-up

If CI passes → PR2 ([rhcos10-ubi10-migration](../)) migrates all base images from
UBI9/RHEL9 to UBI10/RHEL10.
