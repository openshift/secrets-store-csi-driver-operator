# Important E2E Scenarios (Generic Operator)

Use this list when suggesting or generating e2e tests for any OpenShift operator. These scenarios should be **highly checked** for operator and operand health. Adapt Kind names, namespaces, and conditions to the actual operator being tested.

## Operator

| # | Scenario | What to verify |
|---|----------|----------------|
| 1 | Operator install | All managed CRDs Established; operator Deployment/Pod Available in operator namespace. |
| 2 | Operator recovery | After force-deleting operator pod(s), new pod(s) Running and Deployment Available again. |
| 3 | Operator log level | If configurable (via Subscription env, CR field, or command-line flag), verify propagation to deployment and container args/env. |

## Manager CR (if the operator has a top-level manager/config CR)

| # | Scenario | What to verify |
|---|----------|----------------|
| 4 | Manager CR created | Create manager CR with required fields; no error; conditions progress to Available/Ready. |
| 5 | Operand aggregation | If manager CR aggregates operand status, verify all operands reported with expected state. |
| 6 | Management state | If the CR supports managementState (Managed/Unmanaged/Removed), verify each state behaves correctly. |

## Operand CRs (for each CR kind the operator manages)

| # | Scenario | What to verify |
|---|----------|----------------|
| 7 | Operand CR lifecycle | Create operand CR; conditions become True/Ready; backing workload (Deployment/StatefulSet/DaemonSet) ready. |
| 8 | Operand deletion | Delete operand CR; backing workload and managed resources cleaned up. |
| 9 | Operand update | Patch operand CR fields; backing workload updated (rolling update observed). |

## OperatorCondition (if applicable)

| # | Scenario | What to verify |
|---|----------|----------------|
| 10 | Upgradeable when healthy | OperatorCondition Upgradeable status True. |
| 11 | Upgradeable degraded and recovery | Delete an operand pod; Upgradeable becomes False; after operand recovers, Upgradeable returns to True. |

## CR-Driven Configuration (for operands with configurable fields)

| # | Scenario | What to verify |
|---|----------|----------------|
| 12 | Resource limits/requests | Patch CR with resources (limits/requests); backing workload pods have expected resources. |
| 13 | Scheduling (nodeSelector, tolerations, affinity) | Patch CR with scheduling fields; pods rescheduled as expected. |
| 14 | Configuration propagation | Patch CR spec fields; verify ConfigMaps, Secrets, or container args/env updated accordingly. |
| 15 | Log level | Patch CR log level; verify container args or env reflect the change. |

## Validation (negative tests)

| # | Scenario | What to verify |
|---|----------|----------------|
| 16 | Invalid CR rejected | Apply CR with invalid field values (out-of-range, wrong type, missing required); expect admission error or degraded condition. |
| 17 | Immutable field enforcement | If fields are marked immutable, attempt to change them after creation; expect error. |

## Diff-Specific Guidance

When generating tests from a git diff, focus on:

- **API/CRD changes** (new or modified `*_types.go`, CRD YAML): Generate CR create/update tests for new fields, condition checks for new conditions.
- **Controller changes** (`*controller*.go`, `*reconcile*.go`): Generate reconciliation, recovery, and lifecycle tests for the affected CR kinds.
- **RBAC changes** (`config/rbac/*.yaml`): Verify operator has required permissions by exercising the affected operations.
- **Sample changes** (`config/samples/*.yaml`): Validate updated samples apply successfully and produce expected state.
- **Validation/webhook changes**: Generate negative tests with invalid values.
