//go:build e2e
// +build e2e

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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	pollInterval     = 2 * time.Second
	operatorTimeout  = 5 * time.Minute
	wireDialTimeout  = 15 * time.Second
	stableWaitWindow = 20 * time.Second
)

type operatorPod struct {
	Name string
	UID  string
	IP   string
}

func waitForOperatorReady(ctx context.Context) (*operatorPod, error) {
	var ready *operatorPod
	err := wait.PollUntilContextTimeout(ctx, pollInterval, operatorTimeout, true, func(ctx context.Context) (bool, error) {
		pod, err := getOperatorPod(ctx)
		if err != nil {
			return false, nil
		}
		if pod.IP == "" {
			return false, nil
		}
		ready = pod
		return true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to wait for operator pod: %w", err)
	}
	return ready, nil
}

func getOperatorPod(ctx context.Context) (*operatorPod, error) {
	pods, err := kubeClient.CoreV1().Pods(operatorNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=" + operatorDeploymentName,
	})
	if err != nil {
		return nil, err
	}
	var candidates []corev1.Pod
	for _, p := range pods.Items {
		if p.DeletionTimestamp != nil {
			continue
		}
		if p.Status.Phase != corev1.PodRunning {
			continue
		}
		ready := false
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}
		if ready {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("failed to find ready operator pod in %s", operatorNamespace)
	}
	// Prefer the newest ready pod (UID changes after restart).
	newest := candidates[0]
	for _, p := range candidates[1:] {
		if p.CreationTimestamp.After(newest.CreationTimestamp.Time) {
			newest = p
		}
	}
	return &operatorPod{
		Name: newest.Name,
		UID:  string(newest.UID),
		IP:   newest.Status.PodIP,
	}, nil
}

func waitForOperatorRestart(ctx context.Context, previousUID string) (*operatorPod, error) {
	var ready *operatorPod
	err := wait.PollUntilContextTimeout(ctx, pollInterval, operatorTimeout, true, func(ctx context.Context) (bool, error) {
		pod, err := getOperatorPod(ctx)
		if err != nil {
			return false, nil
		}
		if previousUID != "" && pod.UID == previousUID {
			return false, nil
		}
		if pod.IP == "" {
			return false, nil
		}
		ready = pod
		return true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to wait for operator restart (still uid=%s): %w", previousUID, err)
	}
	return ready, nil
}

func assertOperatorUIDStable(ctx context.Context, t *testing.T, uid string) {
	t.Helper()
	deadline := time.Now().Add(stableWaitWindow)
	for time.Now().Before(deadline) {
		pod, err := getOperatorPod(ctx)
		if err == nil && pod.UID != uid {
			t.Fatalf("operator restarted unexpectedly: old uid=%s new uid=%s name=%s", uid, pod.UID, pod.Name)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("context canceled while asserting stability: %v", ctx.Err())
		case <-time.After(pollInterval):
		}
	}
}

func operatorLogs(ctx context.Context, podName string) (content string, err error) {
	req := kubeClient.CoreV1().Pods(operatorNamespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: operatorContainerName,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := stream.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("failed to close log stream: %w", cerr)
		}
	}()
	b, err := io.ReadAll(stream)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func waitForOperatorLogContains(ctx context.Context, podName string, substrings ...string) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, operatorTimeout, true, func(ctx context.Context) (bool, error) {
		logs, err := operatorLogs(ctx, podName)
		if err != nil {
			return false, nil
		}
		for _, s := range substrings {
			if !strings.Contains(logs, s) {
				return false, nil
			}
		}
		return true, nil
	})
}

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

func assertPlaintextHTTP(podIP string, port int) (err error) {
	addr := net.JoinHostPort(podIP, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, wireDialTimeout)
	if err != nil {
		return fmt.Errorf("failed to dial tcp %s: %w", addr, err)
	}
	defer func() {
		if cerr := conn.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("failed to close connection: %w", cerr)
		}
	}()
	if err := conn.SetDeadline(time.Now().Add(wireDialTimeout)); err != nil {
		return fmt.Errorf("failed to set connection deadline: %w", err)
	}
	if _, err := conn.Write([]byte("GET /metrics HTTP/1.0\r\nHost: localhost\r\n\r\n")); err != nil {
		return err
	}
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil && err != io.EOF {
		return err
	}
	resp := string(buf[:n])
	if strings.HasPrefix(resp, "HTTP/") {
		return nil
	}
	// TLS would typically start with 0x16 0x03; treat non-HTTP as failure.
	return fmt.Errorf("failed to verify plaintext HTTP on %s, got %q", addr, truncate(resp, 80))
}

func getReadyOperandPodIP(ctx context.Context) (string, error) {
	pods, err := kubeClient.CoreV1().Pods(operatorNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=" + operandDaemonSetName,
	})
	if err != nil {
		return "", err
	}
	for _, p := range pods.Items {
		if p.DeletionTimestamp != nil || p.Status.Phase != corev1.PodRunning || p.Status.PodIP == "" {
			continue
		}
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				return p.Status.PodIP, nil
			}
		}
	}
	return "", fmt.Errorf("failed to find ready operand pod for DaemonSet %s", operandDaemonSetName)
}

func daemonSetHasContainerPort(ctx context.Context, port int32) (bool, error) {
	ds, err := kubeClient.AppsV1().DaemonSets(operatorNamespace).Get(ctx, operandDaemonSetName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	for _, c := range ds.Spec.Template.Spec.Containers {
		for _, p := range c.Ports {
			if p.ContainerPort == port {
				return true, nil
			}
		}
		for _, arg := range c.Args {
			if strings.Contains(arg, fmt.Sprintf(":%d", port)) {
				return true, nil
			}
		}
	}
	return false, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
