# Error Handling Guidelines

## Error Wrapping

- Wrap errors with `fmt.Errorf("context: %w", err)` to preserve the error chain. This is the standard pattern used throughout the codebase (see `pkg/operator/starter.go`).
- Keep context messages short and descriptive: `"unable to convert to ClusterCSIDriver: %w"`, not `"an error occurred while trying to convert the object to a ClusterCSIDriver type: %w"`.
- Use `%w` (not `%v`) so callers can use `errors.Is` and `errors.As` on the wrapped error.

## Logging

- Use `klog` for all logging — the operator framework and Kubernetes ecosystem standardize on it.
- Use `klog.Errorf` for errors that indicate operator malfunction: `klog.Errorf("unable to get operator state: %v", err)`.
- Use `klog.Infof` for normal operational events.
- Use `klog.Warningf` sparingly — only for situations that are recoverable but unexpected.
- Format log messages starting with a lowercase verb: `"unable to get..."`, `"syncing..."`, `"skipping..."`.
- Avoid logging secret values, tokens, or credentials. Log resource names and namespaces, not resource contents.
- Use `klog.V(n)` for debug-level messages. The operator supports standard klog verbosity flags.

## Operator Status and Conditions

- The operator uses `library-go`'s generic operator client (via `goc.NewClusterScopedOperatorClientWithConfigName`) to manage `ClusterCSIDriver` status.
- The CSI controller set framework manages standard operator status conditions:
  - `Available` — the operator and its operand are functioning correctly.
  - `Progressing` — the operator is actively working toward a desired state.
  - `Degraded` — the operator encountered an error that prevents normal operation.
- Set `Degraded=True` when the operator cannot reconcile desired state after retries, not on every transient error.

## Error Return Patterns

- Controller `Sync` methods return `error` — the framework handles requeueing on error.
- Return `nil` from `Sync` when the reconciliation succeeded, even if some optional work was skipped.
- Return an error from `Sync` only when the primary reconciliation objective failed — this triggers a requeue.
- Do not return errors for expected states like "resource not found during initial setup" — handle those inline.

## Fatal Errors at Startup

- Use `klog.Fatal` or `os.Exit(1)` only during operator startup (`cmd/secrets-store-csi-driver-operator/main.go`) when initialization fails irrecoverably.
- The operator uses `panic(err)` in `replaceNamespaceFunc` (`pkg/operator/starter.go`) when asset loading fails — a missing embedded asset is a build-time bug, not a runtime error.
- Avoid using `Fatal` or `panic` in reconciliation loops — return an error and let the framework retry.

## Container Termination Messages

- DaemonSet containers set `terminationMessagePolicy: FallbackToLogsOnError` so Kubernetes captures the last log lines as the termination message when a container crashes.
- Keep this policy on all containers — it aids debugging without requiring log aggregation access.

## Error Handling in Tests

- Use `t.Fatalf` for setup failures that make the rest of the test meaningless.
- Use `t.Errorf` for assertion failures where subsequent checks may still provide useful information.
- In table-driven tests, use `t.Run(tc.name, ...)` within each subtest so all cases run even if one fails. Note: the existing test in `pkg/operator/starter_test.go` uses `t.Fatalf` for assertions in subtests.

## Configuration Validation

- Validate operator configuration at startup before entering the reconciliation loop.
- The `RunOperator` function in `pkg/operator/starter.go` validates required clients and informers before starting controllers.
- Fail fast with a clear error message if required configuration is missing, rather than entering a degraded reconciliation loop.
