package e2e

import (
	"context"
	"crypto/tls"
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/util/wait"
)

// dialTLS intentionally disables certificate verification (curl -k)
// because the OpenShift service-CA is not mounted in the exec-client pod.
// Kept in this file so Snyk can exclude just this helper.
//
// It runs curl inside execClientPodName (see exec_client_test.go) rather
// than dialing from the Ginkgo process directly, since Ginkgo has no route
// to the target cluster's pod/Service network.

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

func dialTLS(ctx context.Context, addr string, minVersion, maxVersion uint16) error {
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
	args = append(args, "-o", "/dev/null", "-w", "%{http_code}", "https://"+addr+"/metrics")

	stdout, stderr, err := execInClientPod(ctx, args)
	if err != nil {
		return fmt.Errorf("curl https://%s failed (exit=%d) stdout=%q stderr=%q: %w", addr, curlExitCode(err), stdout, stderr, err)
	}
	return nil
}

// waitForDialTLS retries dialTLS for wireRetryTimeout. A freshly created (or
// just-restarted) pod's NetworkPolicy ingress ACLs can take a moment to
// converge on OVN-Kubernetes, which can otherwise show up as a flaky
// failure on the very first attempt. Every other cluster-state wait in
// this suite already retries (see pollInterval/pollTimeout in
// helpers_test.go); this wire check is wrapped the same way for
// consistency and to tolerate that convergence window.
func waitForDialTLS(ctx context.Context, addr string, minVersion, maxVersion uint16) error {
	var lastErr error
	err := wait.PollUntilContextTimeout(ctx, pollInterval, wireRetryTimeout, true, func(ctx context.Context) (bool, error) {
		if lastErr = dialTLS(ctx, addr, minVersion, maxVersion); lastErr != nil {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to dial %s within %s: %w", addr, wireRetryTimeout, lastErr)
	}
	return nil
}
