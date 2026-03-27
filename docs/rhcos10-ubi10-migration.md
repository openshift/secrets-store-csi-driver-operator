# RHCOS10 UBI10 Base Image Migration

## Summary

Migrate all OpenShift Dockerfile base images from the OCP CI registry (RHEL9-based) to
`registry.redhat.io` UBI10 for native RHCOS10 compatibility.

| Dockerfile | Builder: Before | Builder: After | Runtime: Before | Runtime: After |
|------------|----------------|----------------|-----------------|----------------|
| `Dockerfile.openshift` | `ocp/builder:rhel-9-golang-1.25-openshift-4.22` | `ubi10/go-toolset:10.1` | `ocp/4.22:base-rhel9` | `ubi10-minimal:10.1` |
| `Dockerfile.mustgather` | n/a | n/a | `ocp/4.22:must-gather` | unchanged |

All images move from `registry.ci.openshift.org` → `registry.redhat.io`.

## Files Changed

| File | Change |
|------|--------|
| `Dockerfile.openshift` | Builder: `ocp/builder:rhel-9-golang-1.24-openshift-4.22` → `ubi10/go-toolset:10.1`; adds `USER 0` (required by go-toolset); Runtime: `ocp/4.22:base-rhel9` → `ubi10-minimal:10.1` |

## Unchanged Files

| File | Reason |
|------|--------|
| `Dockerfile.mustgather` | Depends on `ocp/4.22:must-gather` which is an OCP-managed image; migration tracked separately |
| `.ci-operator.yaml` | `build_root_image` uses a Prow CI imagestream tag; no `rhel-10` equivalent exists yet for the CI build root — tracked separately |

## Prerequisite

PR1 (`rhcos10-ubi9-compat-test`) should pass CI on RHCOS10 nodes before merging this.

## Test Matrix

| Cluster | Test Suite | Expected |
|---------|-----------|---------|
| RHCOS10 | e2e-azure-rhcos10 | Pass |
| RHCOS10 | e2e-azure-rhcos10-fips | Pass |
