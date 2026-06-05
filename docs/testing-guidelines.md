# Testing Guidelines

## Test Structure

- Use table-driven tests with named test cases as the default pattern. Each test case is a struct with a descriptive `name` field.
- Run subtests with `t.Run(tc.name, func(t *testing.T) { ... })` so each case appears as a separate subtest in output and can be run individually.
- Name test cases descriptively to explain the scenario being tested, e.g., `"operator state is Removed when ClusterCSIDriver does not exist"`.

## Test Organization

- Place unit tests in `_test.go` files alongside the code they test in the same package.
- The main test file is `pkg/operator/starter_test.go` — match this pattern for new packages.
- E2E tests are invoked via `hack/e2e.sh`.

## Fakes and Mocks

- Use `library-go`'s `FakeOperatorClient` (`v1helpers.NewFakeOperatorClientWithObjectMeta`) for unit testing operator logic without a real cluster.
- Use the `FakeOperatorClient` pattern to set up operator spec, status, and object metadata for testing sync behavior.
- Do not use third-party mocking frameworks — prefer hand-written fakes and the standard library's testing utilities.

## Test Assertions

- Use standard `if` checks with `t.Errorf` or `t.Fatalf` — no assertion libraries.
- Use `t.Fatalf` when a failure makes the rest of the test meaningless (e.g., setup failure, nil pointer).
- Use `t.Errorf` when subsequent assertions may still provide useful diagnostic information.
- Compare expected values explicitly: `if got != want { t.Fatalf("expected sync state to be %v, got %v", want, got) }`.

## Test Data and Fixtures

- Operator manifests (YAML assets) are embedded in the binary via `//go:embed` in `assets/assets.go` and loaded via `assets.ReadFile()`. Tests that depend on these assets can use the same loading mechanism.
- Test data for operator state is constructed inline in test cases using struct literals — no external fixture files for unit tests.

## Makefile Test Targets

- `make test-unit` — runs unit tests via `go test`.
- `make test-e2e` — runs end-to-end tests via `hack/e2e.sh`.
- `make verify` — runs code verification (formatting, vetting, Go version checks).
- `make test` — runs `test-unit` (the default test target).
- Run `make verify` before submitting changes to catch formatting and vet issues.

## E2E Testing

- E2E tests require a running OpenShift cluster and are executed via `hack/e2e.sh`.
- The e2e script handles test setup, execution, and teardown including artifact collection.
- The e2e script creates an ephemeral namespace (`secrets-store-test-ns-<random>`) and cleans it up via `test_teardown`.
- E2E tests validate: CSIDriver resource existence, provider pod readiness, SecretProviderClass creation, and secret volume mounting.
- E2E tests are run in CI via Prow jobs — they are not expected to run locally in most development workflows.

## Code Verification

- `make verify` checks:
  - `go vet ./...` for common Go issues.
  - `gofmt` for code formatting.
  - Go version consistency across `go.mod` and Dockerfile.
- Run `make verify` locally before pushing to catch issues that would fail in CI.
- Verification checks are implemented in vendored `build-machinery-go` makefiles.
- Fix formatting violations with `make update-gofmt`.

## CI Integration

- Tests run in OpenShift CI (Prow) on every pull request.
- The CI configuration lives in the `openshift/release` repository, not in this repo.
- CI runs `make test-unit` and `make verify` on every PR.
- E2E tests run as periodic or pre-submit Prow jobs against a real cluster.
- CI builds use FIPS-compliant build flags (`CGO_ENABLED=1 GOEXPERIMENT=strictfipsruntime` with `-tags strictfipsruntime,openssl`).

## Adding New Tests

1. Place the test file next to the source file it tests, using the same package name.
2. Use table-driven tests with descriptive `name` fields.
3. Use `t.Fatalf` for fatal assertions and `t.Errorf` for non-fatal assertions; do not import external assertion libraries.
4. Use `library-go` fakes (`v1helpers.NewFakeOperatorClientWithObjectMeta`) for operator client mocking.
5. If the test needs new assets, add the YAML to `assets/` and update the embed directive if a new subdirectory is introduced.
6. Run `make verify && make test-unit` locally before submitting a PR.
