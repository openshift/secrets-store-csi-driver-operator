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
	// operatorRestartTimeout bounds waitForOperatorRestart specifically. The
	// operator reconfigures its HTTPS metrics listener by exiting the
	// process (RequestShutdown/os.Exit) and relying on kubelet to restart
	// the container under restartPolicy: Always. Kubelet does not
	// distinguish this graceful exit from a crash, so it applies its usual
	// per-container restart backoff (10s, 20s, 40s, ... capped at 300s --
	// confirmed via "Back-off restarting failed container" pod events),
	// throttling how soon the container comes back up. In a suite that
	// flips the TLS profile/adherence repeatedly within a few minutes (the
	// scenario matrix below, plus D1-D5), later restarts reliably ride the
	// full 300s cap. A timeout here now fails the scenario outright (no
	// silent fallback -- see runTLSScenario), so this needs real headroom
	// above that 300s floor, not just enough to barely clear it: the
	// operator's own watch/reconcile latency plus container init/Ready on
	// a loaded or compact/FIPS-style CI node still has to fit in what's
	// left after the mandatory backoff wait.
	operatorRestartTimeout = 10 * time.Minute
	wireDialTimeout        = 15 * time.Second
	// wireRetryTimeout bounds retries of raw TCP/TLS wire checks (dialTLS,
	// assertPlaintextHTTP) against a just-installed or just-restarted pod,
	// tolerating transient NetworkPolicy/OVN ACL convergence rather than
	// failing on a single dial attempt.
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

// operatorMetricsFQDN returns the in-cluster DNS name for the operator's
// metrics Service (e.g.
// secrets-store-csi-driver-operator-metrics.openshift-cluster-csi-drivers.svc).
// Wire checks dial this instead of the pod IP directly: Service traffic is
// resolved via cluster DNS and load-balanced by kube-proxy/OVN through the
// Service's ClusterIP+port (operatorMetricsServicePort), a different network
// path than a raw pod-IP dial -- useful for telling apart a problem specific
// to routing/ACLs on that one pod IP from a problem with the operator's TLS
// listener itself.
func operatorMetricsFQDN() string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", operatorMetricsServiceName, operatorNamespace)
}

type operatorPod struct {
	Name string
	UID  string
	IP   string
	// RestartKey identifies a specific instance of the operator container.
	// The operator restarts via graceful shutdown (RequestShutdown/os.Exit),
	// which kubelet handles as an in-place container restart -- the owning
	// Pod object (and its UID) is unchanged, only ContainerStatuses[i].
	// ContainerID and RestartCount change. Callers that need to detect a
	// restart must key off RestartKey, not UID.
	RestartKey string
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
	var restartKey string
	for _, cs := range newest.Status.ContainerStatuses {
		if cs.Name != operatorContainerName {
			continue
		}
		// ContainerID is unique per container instance and changes on every
		// restart, unlike the Pod UID. Fall back to a UID+RestartCount
		// composite if ContainerID isn't populated yet (e.g. container still
		// starting), so early polls don't spuriously match a later restart.
		if cs.ContainerID != "" {
			restartKey = cs.ContainerID
		} else {
			restartKey = fmt.Sprintf("%s/%d", newest.UID, cs.RestartCount)
		}
		break
	}
	return &operatorPod{
		Name:       newest.Name,
		UID:        string(newest.UID),
		IP:         newest.Status.PodIP,
		RestartKey: restartKey,
	}, nil
}

func waitForOperatorRestart(ctx context.Context, previousRestartKey string) (*operatorPod, error) {
	var ready *operatorPod
	err := wait.PollUntilContextTimeout(ctx, pollInterval, operatorRestartTimeout, true, func(ctx context.Context) (bool, error) {
		pod, err := getOperatorPod(ctx)
		if err != nil {
			return false, nil
		}
		if pod.RestartKey == "" || (previousRestartKey != "" && pod.RestartKey == previousRestartKey) {
			return false, nil
		}
		if pod.IP == "" {
			return false, nil
		}
		ready = pod
		return true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to wait for operator restart (still restartKey=%s): %w", previousRestartKey, err)
	}
	return ready, nil
}

// assertOperatorRestartKeyStable asserts the operator container does not
// restart for stableWaitWindow. It must key off RestartKey rather than Pod
// UID: the operator's graceful-shutdown restart keeps the same Pod object
// (and UID), so a UID comparison here would never catch an unwanted restart.
func assertOperatorRestartKeyStable(ctx context.Context, restartKey string) {
	deadline := time.Now().Add(stableWaitWindow)
	for time.Now().Before(deadline) {
		pod, err := getOperatorPod(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.RestartKey).To(Equal(restartKey), "operator restarted unexpectedly: old restartKey=%s new restartKey=%s name=%s", restartKey, pod.RestartKey, pod.Name)
		select {
		case <-ctx.Done():
			Fail(fmt.Sprintf("context canceled while asserting stability: %v", ctx.Err()))
		case <-time.After(pollInterval):
		}
	}
}

// dumpNetworkDiagnostics logs the NetworkPolicies in effect in
// operatorNamespace, pod's node placement, its recent events, and the
// operator container's own logs. Call this right before failing a wire check
// (dialTLS/assertPlaintextHTTP) that exhausted its
// retry budget, so the resulting artifacts can distinguish a slow-converging
// ACL (would eventually pass with a longer budget, and diagnostics here look
// unremarkable) from a hard block (diagnostics will show a NetworkPolicy
// whose ingress rules don't cover the caller, or a pod stuck
// NotReady/misplaced) from an application-level delay (the logs won't yet
// show the metrics server having started serving).
func dumpNetworkDiagnostics(ctx context.Context, pod *operatorPod) {
	// Logs are scoped to the current container instance (kubelet doesn't
	// return --previous logs here), so the first bytes cover the startup
	// sequence since the last restart -- exactly what's needed to tell
	// whether the metrics server had started serving by the time the dial
	// gave up.
	if logs, err := operatorLogs(ctx, pod.Name); err == nil {
		GinkgoWriter.Printf("[diagnostics] operator logs for pod %s (first 4000 bytes since last restart):\n%s\n",
			pod.Name, truncate(logs, 4000))
	} else {
		GinkgoWriter.Printf("[diagnostics] failed to get operator logs for pod %s: %v\n", pod.Name, err)
	}

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

// assertPlaintextHTTP execs curl inside execClientPodName (see
// exec_client_test.go) to confirm addr speaks plaintext HTTP: requesting
// http://addr against a TLS listener fails curl's HTTP response parsing
// (nonzero exit) rather than returning a 200, so a clean 2xx/3xx already
// proves this wasn't silently upgraded to TLS.
func assertPlaintextHTTP(ctx context.Context, podIP string, port int) error {
	addr := net.JoinHostPort(podIP, fmt.Sprintf("%d", port))
	args := []string{
		"curl", "-sS", "-o", "/dev/null", "-w", "%{http_code}",
		"--max-time", fmt.Sprintf("%d", int(wireDialTimeout.Seconds())),
		"http://" + addr + "/metrics",
	}
	stdout, stderr, err := execInClientPod(ctx, args)
	if err != nil {
		return fmt.Errorf("curl http://%s/metrics failed (exit=%d) stdout=%q stderr=%q: %w", addr, curlExitCode(err), stdout, stderr, err)
	}
	return nil
}

// waitForPlaintextHTTP retries assertPlaintextHTTP for wireRetryTimeout; see
// waitForDialTLS for why raw wire checks need to tolerate a brief
// NetworkPolicy/OVN convergence window instead of failing on one attempt.
func waitForPlaintextHTTP(ctx context.Context, podIP string, port int) error {
	var lastErr error
	err := wait.PollUntilContextTimeout(ctx, pollInterval, wireRetryTimeout, true, func(ctx context.Context) (bool, error) {
		if lastErr = assertPlaintextHTTP(ctx, podIP, port); lastErr != nil {
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
