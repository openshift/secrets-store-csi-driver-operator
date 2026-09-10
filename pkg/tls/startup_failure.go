package tls

import "fmt"

// StartupBlockedError indicates the operator cannot safely start its HTTPS
// metrics server because cluster TLS settings could not be resolved or applied.
// Administrators observe this as a pod startup failure (CrashLoopBackOff) with
// a clear error in logs and a non-ready operator Deployment — satisfying FR-003
// fail-hard semantics without serving non-compliant TLS settings.
type StartupBlockedError struct {
	Cause error
}

func (e StartupBlockedError) Error() string {
	return fmt.Sprintf("failed to resolve cluster TLS security profile: %v", e.Cause)
}

func (e StartupBlockedError) Unwrap() error {
	return e.Cause
}

// StartupBlockedError wraps cause when startup must abort due to TLS resolution.
func NewStartupBlockedError(cause error) error {
	if cause == nil {
		return nil
	}
	return StartupBlockedError{Cause: cause}
}
