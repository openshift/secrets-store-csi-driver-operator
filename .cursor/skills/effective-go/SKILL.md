# effective-go

Ensures all generated Go code follows best practices from the official Effective Go documentation and Go community standards.

## Purpose

This skill provides guidelines for writing idiomatic, clean, and maintainable Go code. It is applied whenever generating Go code (types, controllers, tests) to ensure consistency and quality.

## When This Skill Applies

- Generating API type definitions (`generate-types`)
- Generating controller/reconciler code (`generate-controller`)
- Generating tests (`generate-tests`)
- Any Go code generation or modification

## Guidelines

### 1. Formatting

**Rule:** Always format code with `gofmt` standards.

```go
// GOOD: Proper formatting
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    logger := log.FromContext(ctx)
    logger.Info("Starting reconciliation")
    return ctrl.Result{}, nil
}

// BAD: Inconsistent formatting
func (r *Reconciler) Reconcile(ctx context.Context,req ctrl.Request) (ctrl.Result,error) {
logger := log.FromContext(ctx)
    logger.Info("Starting reconciliation")
return ctrl.Result{},nil
}
```

### 2. Naming Conventions

**Rules:**
- Use `MixedCaps` for exported identifiers (public)
- Use `mixedCaps` for unexported identifiers (private)
- Never use underscores in names
- Acronyms should be consistent case: `HTTP`, `URL`, `ID` (not `Http`, `Url`, `Id`)

```go
// GOOD: Proper naming
type IngressController struct {}           // Exported, MixedCaps
type ingressConfig struct {}               // Unexported, mixedCaps
func (r *Reconciler) GetHTTPClient() {}    // Acronym all caps
var userID string                          // ID not Id

// BAD: Improper naming
type Ingress_Controller struct {}          // No underscores
type IngressConfig struct {}               // Should be unexported if internal
func (r *Reconciler) GetHttpClient() {}    // Http should be HTTP
var userId string                          // Id should be ID
```

### 3. Error Handling

**Rules:**
- Always check errors explicitly
- Return errors, don't panic (except for truly unrecoverable situations)
- Wrap errors with context using `fmt.Errorf("context: %w", err)`
- Don't ignore errors with `_`

```go
// GOOD: Proper error handling
func (r *Reconciler) reconcile(ctx context.Context, obj *v1.Resource) error {
    if err := r.validateSpec(obj); err != nil {
        return fmt.Errorf("spec validation failed: %w", err)
    }

    if err := r.createConfigMap(ctx, obj); err != nil {
        return fmt.Errorf("failed to create ConfigMap: %w", err)
    }

    return nil
}

// BAD: Poor error handling
func (r *Reconciler) reconcile(ctx context.Context, obj *v1.Resource) {
    r.validateSpec(obj)           // Error ignored!

    err := r.createConfigMap(ctx, obj)
    if err != nil {
        panic(err)                // Don't panic!
    }
}
```

### 4. Error Messages

**Rules:**
- Start with lowercase (errors are often chained)
- Don't end with punctuation
- Be specific about what failed

```go
// GOOD: Proper error messages
return fmt.Errorf("failed to create ConfigMap %s: %w", name, err)
return fmt.Errorf("spec.replicas must be positive, got %d", replicas)

// BAD: Poor error messages
return fmt.Errorf("Error creating ConfigMap.")   // Uppercase, punctuation
return fmt.Errorf("failed")                       // Not specific
return errors.New("something went wrong")         // Vague
```

### 5. Documentation

**Rules:**
- Document all exported functions, types, and constants
- Start comments with the name of the thing being documented
- Use complete sentences

```go
// GOOD: Proper documentation
// Reconciler manages the lifecycle of Foo resources.
// It creates and updates dependent resources based on the Foo spec.
type Reconciler struct {
    client.Client
    Scheme *runtime.Scheme
}

// Reconcile performs a single reconciliation loop for a Foo resource.
// It returns an error if the reconciliation fails.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // ...
}

// DefaultRequeueInterval is the default interval between reconciliations.
const DefaultRequeueInterval = 30 * time.Second

// BAD: Poor or missing documentation
type Reconciler struct {        // No documentation
    client.Client
}

// reconciles foo                // Doesn't start with name, incomplete sentence
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
```

### 6. Interfaces

**Rules:**
- Keep interfaces small (1-3 methods ideally)
- Accept interfaces, return concrete types
- Define interfaces where they're used, not where they're implemented
- Name single-method interfaces with `-er` suffix

```go
// GOOD: Small, focused interface
type StatusUpdater interface {
    UpdateStatus(ctx context.Context, obj client.Object) error
}

// GOOD: Accept interface, return concrete
func NewReconciler(client client.Client) *Reconciler {
    return &Reconciler{Client: client}
}

// BAD: Large interface
type ResourceManager interface {
    Create(ctx context.Context, obj client.Object) error
    Update(ctx context.Context, obj client.Object) error
    Delete(ctx context.Context, obj client.Object) error
    Get(ctx context.Context, key types.NamespacedName, obj client.Object) error
    List(ctx context.Context, list client.ObjectList) error
    Patch(ctx context.Context, obj client.Object, patch client.Patch) error
    // ... too many methods
}
```

### 7. Concurrency

**Rules:**
- Share memory by communicating (use channels)
- Don't communicate by sharing memory
- Use `sync.Mutex` only when channels are impractical
- Always handle context cancellation

```go
// GOOD: Respect context cancellation
func (r *Reconciler) reconcile(ctx context.Context, obj *v1.Resource) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }

    // Continue with reconciliation
    return r.doWork(ctx, obj)
}

// GOOD: Use channels for coordination
results := make(chan Result, len(items))
for _, item := range items {
    go func(item Item) {
        results <- process(item)
    }(item)
}
```

### 8. Package Organization

**Rules:**
- Package names should be short, lowercase, single-word
- Avoid `util`, `common`, `misc` package names
- Group related functionality together

```go
// GOOD: Clear package names
package controller
package reconciler
package status

// BAD: Poor package names
package controller_utils    // No underscores
package common              // Too vague
package myPackage           // No mixed case
```

### 9. Imports

**Rules:**
- Group imports: standard library, external, internal
- Use blank lines to separate groups
- Use aliases only when necessary (conflicts, clarity)

```go
// GOOD: Properly organized imports
import (
    "context"
    "fmt"
    "time"

    corev1 "k8s.io/api/core/v1"
    "k8s.io/apimachinery/pkg/api/errors"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"

    configv1 "github.com/openshift/api/config/v1"
    "github.com/myorg/myoperator/internal/controller"
)

// BAD: Unorganized imports
import (
    "github.com/myorg/myoperator/internal/controller"
    "context"
    corev1 "k8s.io/api/core/v1"
    "fmt"
    "sigs.k8s.io/controller-runtime/pkg/client"
)
```

### 10. Variable Declarations

**Rules:**
- Use short variable declarations (`:=`) inside functions
- Use `var` for package-level variables or zero values
- Group related declarations

```go
// GOOD: Appropriate declarations
const (
    DefaultTimeout  = 30 * time.Second
    MaxRetries      = 3
)

var (
    ErrNotFound     = errors.New("resource not found")
    ErrInvalidSpec  = errors.New("invalid spec")
)

func (r *Reconciler) reconcile(ctx context.Context) error {
    logger := log.FromContext(ctx)        // Short declaration
    instance := &v1.Resource{}            // Short declaration

    var result ctrl.Result                // Zero value needed
    return nil
}

// BAD: Inconsistent declarations
func (r *Reconciler) reconcile(ctx context.Context) error {
    var logger = log.FromContext(ctx)     // Use := instead
    instance := new(v1.Resource)          // Use &v1.Resource{} instead
}
```

### 11. Receiver Names

**Rules:**
- Use short, consistent receiver names (1-2 letters)
- Use the same receiver name throughout the type's methods
- Don't use generic names like `this` or `self`

```go
// GOOD: Short, consistent receivers
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {}
func (r *Reconciler) reconcileDelete(ctx context.Context, obj *v1.Resource) error {}
func (r *Reconciler) setCondition(obj *v1.Resource, condition metav1.Condition) {}

// BAD: Inconsistent or long receivers
func (reconciler *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) {}
func (r *Reconciler) reconcileDelete(ctx context.Context, obj *v1.Resource) {}
func (this *Reconciler) setCondition(obj *v1.Resource, condition metav1.Condition) {}
```

### 12. Zero Values

**Rules:**
- Leverage zero values for initialization
- Design types so zero value is useful

```go
// GOOD: Zero value is useful
type Config struct {
    Timeout  time.Duration  // Zero means no timeout
    Replicas int            // Zero means default
}

cfg := Config{}  // Usable immediately

// GOOD: Check for zero value
if cfg.Timeout == 0 {
    cfg.Timeout = DefaultTimeout
}
```

## References

- [Effective Go](https://go.dev/doc/effective_go) - Official guide
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments) - Common review feedback
- [Go Proverbs](https://go-proverbs.github.io/) - Wisdom from Rob Pike
- [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md) - Industry practices

## Usage by Other Skills

This skill is referenced by:
- `generate-types` - When generating API type definitions
- `generate-controller` - When generating controller code
- `generate-tests` - When generating test code

All Go code generation MUST follow these guidelines.
