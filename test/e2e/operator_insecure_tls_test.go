package e2e

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/util/wait"
)

// dialTLS and scrapeOperatorMetrics intentionally disable certificate
// verification because the OpenShift service-CA is not mounted in the e2e
// process. Kept in this file so Snyk can exclude just these helpers.

func dialTLS(addr string, minVersion, maxVersion uint16) error {
	cfg := &tls.Config{
		InsecureSkipVerify: true, // service-CA not mounted in the test process
		MinVersion:         minVersion,
		MaxVersion:         maxVersion,
	}
	dialer := &net.Dialer{Timeout: wireDialTimeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, cfg)
	if err != nil {
		return err
	}
	if err := conn.Close(); err != nil {
		return fmt.Errorf("failed to close TLS connection: %w", err)
	}
	return nil
}

// waitForDialTLS retries dialTLS for wireRetryTimeout. A freshly created (or
// just-restarted) pod's NetworkPolicy ACLs can take a moment to converge on
// OVN-Kubernetes, which manifests as a transient "i/o timeout" rather than
// "connection refused" on the very first dial attempt right after an
// install/update. Every other cluster-state wait in this suite already
// retries (see pollInterval/pollTimeout in helpers_test.go); the raw wire
// checks are wrapped the same way here for consistency and to avoid flaking
// on that convergence window.
func waitForDialTLS(ctx context.Context, addr string, minVersion, maxVersion uint16) error {
	var lastErr error
	err := wait.PollUntilContextTimeout(ctx, pollInterval, wireRetryTimeout, true, func(context.Context) (bool, error) {
		if lastErr = dialTLS(addr, minVersion, maxVersion); lastErr != nil {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to dial %s within %s: %w", addr, wireRetryTimeout, lastErr)
	}
	return nil
}

func scrapeOperatorMetrics(ctx context.Context, podIP string) (err error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr, Timeout: wireDialTimeout}
	url := fmt.Sprintf("https://%s/metrics", net.JoinHostPort(podIP, fmt.Sprintf("%d", operatorMetricsPort)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	token, err := metricsBearerToken()
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := resp.Body.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("failed to close metrics response body: %w", cerr)
		}
	}()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to check metrics response status: got %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	if !strings.Contains(string(body), "# HELP") && !strings.Contains(string(body), "# TYPE") {
		return fmt.Errorf("failed to find prometheus markers in metrics body: %s", truncate(string(body), 200))
	}
	return nil
}

// waitForScrapeOperatorMetrics retries scrapeOperatorMetrics for
// wireRetryTimeout; see waitForDialTLS for why raw wire checks need to
// tolerate a brief NetworkPolicy/OVN convergence window instead of failing
// on one attempt.
func waitForScrapeOperatorMetrics(ctx context.Context, podIP string) error {
	var lastErr error
	err := wait.PollUntilContextTimeout(ctx, pollInterval, wireRetryTimeout, true, func(ctx context.Context) (bool, error) {
		if lastErr = scrapeOperatorMetrics(ctx, podIP); lastErr != nil {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to scrape operator metrics on %s within %s: %w", podIP, wireRetryTimeout, lastErr)
	}
	return nil
}

// metricsBearerToken returns the bearer token from the e2e restConfig used for
// API access (BearerToken, or BearerTokenFile for in-cluster).
func metricsBearerToken() (string, error) {
	if restConfig == nil {
		return "", fmt.Errorf("rest config not initialized")
	}
	if token := restConfig.BearerToken; token != "" {
		return token, nil
	}
	if restConfig.BearerTokenFile == "" {
		return "", fmt.Errorf("no bearer token available for metrics scrape")
	}
	b, err := os.ReadFile(restConfig.BearerTokenFile)
	if err != nil {
		return "", fmt.Errorf("read bearer token for metrics scrape: %w", err)
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return "", fmt.Errorf("empty bearer token file %q", restConfig.BearerTokenFile)
	}
	return token, nil
}
