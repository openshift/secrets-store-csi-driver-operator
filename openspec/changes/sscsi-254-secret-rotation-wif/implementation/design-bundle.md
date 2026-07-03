# Design Bundle — SSCSI-254 / T0_2
**Task:** T0_2 — Verify new API types compile and confirm field name (A-001)
**Phase:** 0 — Upstream Vendor Update
**Prepared:** 2026-07-03

---

## Constitution Excerpts
- No type invention: read vendored types as-is; document findings verbatim.
- Verification tasks produce documentation only; no source edits permitted.

---

## Task Payload — T0_2 (verification only)

**Objective:** Confirm vendored `SecretsStoreCSIDriverConfigSpec` compiles, resolve A-001 (field name), confirm Q4 CEL immutability, verify A-002 RBAC.

**Non-goals:** Do not modify any vendored or operator source files.

**Acceptance criteria (all must be met):**
1. Exact field path for rotation interval recorded (A-001)
2. CEL immutability for `tokenRequests.type` confirmed (Q4)
3. RBAC for `csidrivers` create/get/list/watch/update/delete confirmed (A-002)
4. `make build` green (inherited from T0_1)
