package e2e

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	operatorTimeout = 5 * time.Minute
	wireDialTimeout = 15 * time.Second
	// wireRetryTimeout bounds retries of raw TCP/TLS wire checks (dialTLS,
	// assertPlaintextHTTP, scrapeOperatorMetrics) against a just-installed or
	// just-restarted pod, tolerating transient NetworkPolicy/OVN ACL
	// convergence rather than failing on a single dial attempt.
	//
	// The "TLS profile adherence" suite (tls_profile_test.go) only runs
	// under the TLSAdherence feature gate, which today only the
	// operator-e2e-fips CI job enables (CustomNoUpgrade FeatureSet). That
	// job's first wire check (scenario A1) dials the operator pod within
	// ~1s of it clearing readiness after BeforeAll's apiserver mutation, so
	// there's very little cushion for ACL convergence on that lane before
	// this budget was widened -- see dumpNetworkDiagnostics for how a
	// repeat timeout here should be triaged.
	wireRetryTimeout = 3 * time.Minute
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

func assertOperatorUIDStable(ctx context.Context, uid string) {
	deadline := time.Now().Add(stableWaitWindow)
	for time.Now().Before(deadline) {
		pod, err := getOperatorPod(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.UID).To(Equal(uid), "operator restarted unexpectedly: old uid=%s new uid=%s name=%s", uid, pod.UID, pod.Name)
		select {
		case <-ctx.Done():
			Fail(fmt.Sprintf("context canceled while asserting stability: %v", ctx.Err()))
		case <-time.After(pollInterval):
		}
	}
}

// dumpNetworkDiagnostics logs the NetworkPolicies in effect in
// operatorNamespace, pod's node placement, and its recent events. Call this
// right before failing a wire check (dialTLS/scrapeOperatorMetrics/
// assertPlaintextHTTP) that exhausted its retry budget, so the resulting
// artifacts can distinguish a slow-converging ACL (would eventually pass
// with a longer budget, and diagnostics here look unremarkable) from a hard
// block (diagnostics will show a NetworkPolicy whose ingress rules don't
// cover the caller, or a pod stuck NotReady/misplaced).
func dumpNetworkDiagnostics(ctx context.Context, pod *operatorPod) {
	if nps, err := kubeClient.NetworkingV1().NetworkPolicies(operatorNamespace).List(ctx, metav1.ListOptions{}); err == nil {
		for _, np := range nps.Items {
			GinkgoWriter.Printf("[diagnostics] NetworkPolicy %s/%s podSelector=%v ingress=%+v\n",
				np.Namespace, np.Name, np.Spec.PodSelector.MatchLabels, np.Spec.Ingress)
		}
	} else {
		GinkgoWriter.Printf("[diagnostics] failed to list NetworkPolicies in %s: %v\n", operatorNamespace, err)
	}

	if p, err := kubeClient.CoreV1().Pods(operatorNamespace).Get(ctx, pod.Name, metav1.GetOptions{}); err == nil {
		GinkgoWriter.Printf("[diagnostics] pod %s node=%s hostNetwork=%t phase=%s\n",
			pod.Name, p.Spec.NodeName, p.Spec.HostNetwork, p.Status.Phase)
	} else {
		GinkgoWriter.Printf("[diagnostics] failed to get pod %s: %v\n", pod.Name, err)
	}

	events, err := kubeClient.CoreV1().Events(operatorNamespace).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s", pod.Name),
	})
	if err != nil {
		GinkgoWriter.Printf("[diagnostics] failed to list events for pod %s: %v\n", pod.Name, err)
		return
	}
	for _, e := range events.Items {
		GinkgoWriter.Printf("[diagnostics] event pod=%s reason=%s lastSeen=%s: %s\n",
			pod.Name, e.Reason, e.LastTimestamp, e.Message)
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

func assertPlaintextHTTP(podIP string, port int) (err error) {
	addr := net.JoinHostPort(podIP, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, wireDialTimeout)
	if err != nil {
		return fmt.Errorf("failed to dial tcp %s: %w", addr, err)
	}
	defer func() {
		if cerr := conn.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("failed to close connection: %w", err)
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

// waitForPlaintextHTTP retries assertPlaintextHTTP for wireRetryTimeout; see
// waitForDialTLS for why raw wire checks need to tolerate a brief
// NetworkPolicy/OVN convergence window instead of failing on one attempt.
func waitForPlaintextHTTP(ctx context.Context, podIP string, port int) error {
	var lastErr error
	err := wait.PollUntilContextTimeout(ctx, pollInterval, wireRetryTimeout, true, func(context.Context) (bool, error) {
		if lastErr = assertPlaintextHTTP(podIP, port); lastErr != nil {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to verify plaintext HTTP on %s within %s: %w",
			net.JoinHostPort(podIP, fmt.Sprintf("%d", port)), wireRetryTimeout, lastErr)
	}
	return nil
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
