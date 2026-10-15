package e2e

import (
	"context"
	"crypto/tls"
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/util/wait"
)

// dialTLS intentionally disables certificate verification (curl -k)
// because the OpenShift service-CA is not mounted in the operator pod's
// default filesystem. Kept in this file so Snyk can exclude just this helper.
//
// It runs curl inside the operator pod itself (via execInOperatorPod),
// targeting localhost:8443. This bypasses NetworkPolicy restrictions that
// would otherwise block traffic from external pods to the operator's
// metrics endpoint -- the default NetworkPolicy only allows ingress from
// openshift-monitoring, not from test pods. Using localhost also avoids
// any Service/DNS resolution or OVN ACL convergence issues.

// curlTLSVersionFlags maps a tls.VersionTLS* min/max pair to curl's
// --tlsv1.x/--tls-max flags, mirroring the crypto/tls MinVersion/MaxVersion
// pair these checks used when dialing directly.
func curlTLSVersionFlags(minVersion, maxVersion uint16) ([]string, error) {
	names := map[uint16]string{
		tls.VersionTLS12: "1.2",
		tls.VersionTLS13: "1.3",
	}
	minStr, ok := names[minVersion]
	if !ok {
		return nil, fmt.Errorf("unsupported curl min TLS version 0x%x", minVersion)
	}
	maxStr, ok := names[maxVersion]
	if !ok {
		return nil, fmt.Errorf("unsupported curl max TLS version 0x%x", maxVersion)
	}
	return []string{"--tlsv" + minStr, "--tls-max", maxStr}, nil
}

// dialTLS execs curl inside the operator pod to verify the TLS endpoint.
// Using localhost:8443 bypasses NetworkPolicy restrictions.
func dialTLS(ctx context.Context, podName string, minVersion, maxVersion uint16) error {
	verFlags, err := curlTLSVersionFlags(minVersion, maxVersion)
	if err != nil {
		return err
	}
	args := append([]string{
		"curl", "-sS", "-k",
		"--max-time", strconv.Itoa(int(wireDialTimeout.Seconds())),
	}, verFlags...)
	// -o discards the body and -w prints just the status code: this check
	// only cares that the TLS handshake completed, not the HTTP outcome
	// (curl exits 0 on any completed HTTP response, 401/403 included).
	// Use localhost:8443 to bypass NetworkPolicy restrictions.
	args = append(args, "-o", "/dev/null", "-w", "%{http_code}", "https://localhost:8443/metrics")

	stdout, stderr, err := execInOperatorPod(ctx, podName, args)
	if err != nil {
		return fmt.Errorf("curl https://localhost:8443/metrics in pod %s failed (exit=%d) stdout=%q stderr=%q: %w", podName, curlExitCode(err), stdout, stderr, err)
	}
	return nil
}

// waitForDialTLS retries dialTLS for wireRetryTimeout. A freshly restarted
// operator pod may take a moment for its metrics server to start serving;
// this retry loop tolerates that startup delay.
func waitForDialTLS(ctx context.Context, podName string, minVersion, maxVersion uint16) error {
	var lastErr error
	err := wait.PollUntilContextTimeout(ctx, pollInterval, wireRetryTimeout, true, func(ctx context.Context) (bool, error) {
		if lastErr = dialTLS(ctx, podName, minVersion, maxVersion); lastErr != nil {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to dial localhost:8443 in pod %s within %s: %w", podName, wireRetryTimeout, lastErr)
	}
	return nil
}
