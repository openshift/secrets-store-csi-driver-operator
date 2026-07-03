# Design Bundle — SSCSI-254 / T0_1
**Task:** T0_1 — Update go.mod and vendor tree for openshift/api + client-go
**Phase:** 0 (Upstream Vendor Update)
**Prepared:** 2026-07-03

---

## Constitution Excerpts (Relevant)

From `constitution.md`:
- **No type invention**: Use upstream `openshift/api` types only; do not define local equivalents.
- **Vendor-only changes in isolation**: T0_1 and T0_2 should land in a separate, minimal PR to keep implementation PRs cleanly reviewable.
- **Go modules**: All changes must keep `go build ./...` and `make test` green.

---

## Specs Excerpts (Relevant)

From `specs.md`:
- **FR-001/FR-002**: `SecretsStoreSecretRotation.RotationPollIntervalSeconds` is the current vendored field name (PR #2906 renaming it to `minimumRefreshAge` is still open). Implementation uses the current `rotationPollIntervalSeconds` field (Assumption A-001).
- **FR-004**: `SecretsStoreTokenRequests.Audiences *[]SecretsStoreTokenRequest` is the vendor field for WIF configuration.
- **FR-006**: CEL validation rules in the API enforce `SecretsStore` driver-type constraint.

---

## Repo-Assessment Excerpts (Relevant)

From `repo-assessment.md §2`:
- `openshift/api` was pinned to `v0.0.0-20260302174620-dcac36b908db` (March 2026) — prior to PR #2846 merge date (2026-06-24).
- All three modules (`api`, `client-go`, `library-go`) must be bumped together due to inter-dependency constraints.

---

## Plan Excerpts (Relevant — Phase 0)

From `plan.md §4.0`:
- **T0_1 objective**: Bump `openshift/api`, `openshift/client-go`, and `openshift/library-go` to post-2026-06-24 pseudo-versions.
- **Verification**: `go build ./...` clean; `make test` green; `grep SecretsStoreCSIDriverConfigSpec vendor/...` resolves.

---

## Task Payload — T0_1

**Objective:** Update `go.mod` so that `SecretsStoreCSIDriverConfigSpec`, `SecretsStoreSecretRotation`, and `SecretsStoreTokenRequests` are present in the vendored `openshift/api` types.

**Target files:**
- `go.mod` (openshift/api, openshift/client-go, openshift/library-go bumped)
- `go.sum` (updated checksums)
- `vendor/` tree (re-vendored via `go mod vendor`)

**Non-goals:**
- Do NOT change any operator Go source files in this task.
- Do NOT introduce new Kubernetes API types beyond what is vendored.

**Implementation notes:**
1. `go get github.com/openshift/api@v0.0.0-20260702202555-ef71f942ef6c`
2. `go get github.com/openshift/client-go@latest` (→ `v0.0.0-20260703082747-24d059aea27a`)
3. `go get github.com/openshift/library-go@latest` (→ `v0.0.0-20260703081820-c6cd1a243d2d`)
4. `go mod tidy && go mod vendor`

**Acceptance criteria:**
- `grep SecretsStoreCSIDriverConfigSpec vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go` returns a hit.
- `go build ./...` exits 0.
- `make test` exits 0 (`ok github.com/.../pkg/operator`).
- A-001: current field name is `rotationPollIntervalSeconds`; note in ADR if/when PR #2906 merges.
