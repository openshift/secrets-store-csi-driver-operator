package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

// The Ginkgo process running this suite is a ci-operator TestStep
// container on the CI build farm, not a workload on the cluster under
// test -- it only has $KUBECONFIG credentials to the target cluster's API
// server, no L3 route to that cluster's pod/Service network, and no
// access to its in-cluster DNS. Wire checks that dialed a pod IP or a
// cluster-internal Service FQDN directly from Ginkgo could therefore
// never succeed, no matter how long they retried.
//
// execClientPodName is a small, long-lived helper pod created once (per
// TLS suite run) in operatorNamespace purely so curl can run *from inside
// the cluster's real pod network*: exec-ing into it and running curl
// exercises the exact path (CNI/OVN routing, NetworkPolicy, cluster DNS)
// that a real client such as Prometheus would use, which a direct dial
// from Ginkgo never could.
const (
	execClientPodName       = "sscsi-e2e-tls-client"
	execClientContainerName = "client"
	// execClientImageEnv optionally overrides execClientDefaultImage, e.g.
	// on a disconnected/restricted-network cluster where this default
	// (a public registry.access.redhat.com pull) isn't reachable.
	//
	// The operand images already running in-cluster (DaemonSet in
	// assets/node.yaml: csi-driver, csi-node-driver-registrar,
	// csi-liveness-probe) were considered as an in-cluster-resolvable
	// alternative, but all three are minimal single-purpose sidecar images
	// with no shell or curl, so they can't run the wire checks below --
	// hence the explicit override rather than sourcing this from the
	// DaemonSet.
	execClientImageEnv = "E2E_TLS_CLIENT_IMAGE"
	// execClientDefaultImage needs a curl new enough to support
	// --tlsv1.x/--tls-max (curl >=7.54); ubi9-minimal's is. Pinned by
	// digest (rather than :latest) for reproducibility.
	execClientDefaultImage = "registry.access.redhat.com/ubi9/ubi-minimal@sha256:8eb2830d0936237fc13a1f2f7e45aecf90d69043380ad167fad0343632937f41"
	execPodReadyTimeout    = 3 * time.Minute
)

func execClientImage() string {
	if img := os.Getenv(execClientImageEnv); img != "" {
		return img
	}
	return execClientDefaultImage
}

// ensureExecClientPod creates the exec-client pod if it doesn't already
// exist and waits for it to be Ready. Safe to call multiple times (e.g.
// from a BeforeAll that may run more than once across retried suites).
//
// An existing pod found in any phase other than Running is deleted and
// recreated rather than waited on: with RestartPolicyNever, a pod that has
// gone Succeeded/Failed/Unknown will never come back on its own, and
// waiting on its Ready condition would just spin until execPodReadyTimeout
// on every remaining call for the rest of the suite.
func ensureExecClientPod(ctx context.Context) error {
	pod, err := kubeClient.CoreV1().Pods(operatorNamespace).Get(ctx, execClientPodName, metav1.GetOptions{})
	switch {
	case err == nil:
		if pod.Status.Phase == corev1.PodRunning {
			return waitForExecClientPodReady(ctx)
		}
		if err := kubeClient.CoreV1().Pods(operatorNamespace).Delete(ctx, execClientPodName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete non-Running exec-client pod %s/%s (phase=%s): %w", operatorNamespace, execClientPodName, pod.Status.Phase, err)
		}
	case !apierrors.IsNotFound(err):
		return fmt.Errorf("failed to check for existing exec-client pod %s/%s: %w", operatorNamespace, execClientPodName, err)
	}

	newPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      execClientPodName,
			Namespace: operatorNamespace,
			Labels:    map[string]string{"app": execClientPodName},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:  execClientContainerName,
				Image: execClientImage(),
				// "infinity" (GNU coreutils, present on ubi-minimal) rather
				// than a fixed duration: this pod must outlive the whole
				// suite, which can run well past an hour once kubelet's
				// per-container restart backoff (see operatorRestartTimeout)
				// stacks up across the scenario matrix's many operator
				// restarts. deleteExecClientPod (AfterAll) tears it down.
				Command: []string{"sleep", "infinity"},
			}},
		},
	}
	if _, err := kubeClient.CoreV1().Pods(operatorNamespace).Create(ctx, newPod, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("failed to create exec-client pod %s/%s: %w", operatorNamespace, execClientPodName, err)
	}
	return waitForExecClientPodReady(ctx)
}

func waitForExecClientPodReady(ctx context.Context) error {
	err := wait.PollUntilContextTimeout(ctx, pollInterval, execPodReadyTimeout, true, func(ctx context.Context) (bool, error) {
		pod, err := kubeClient.CoreV1().Pods(operatorNamespace).Get(ctx, execClientPodName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		for _, c := range pod.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("exec-client pod %s/%s did not become Ready within %s: %w", operatorNamespace, execClientPodName, execPodReadyTimeout, err)
	}
	return nil
}

// deleteExecClientPod tears down the pod created by ensureExecClientPod.
// Safe to call even if the pod was never created.
func deleteExecClientPod(ctx context.Context) {
	_ = kubeClient.CoreV1().Pods(operatorNamespace).Delete(ctx, execClientPodName, metav1.DeleteOptions{})
}

// execInClientPod runs command inside the exec-client pod via the
// Kubernetes exec subresource -- the same mechanism `oc exec` uses --
// returning the command's stdout/stderr. A nonzero remote exit code comes
// back as a *utilexec.CodeExitError wrapped in err; use curlExitCode to
// pull the exit code back out.
//
// It re-ensures the pod exists before every call (a cheap Get in the
// common case where it's already Ready) rather than assuming
// ensureExecClientPod's one BeforeAll call is enough for the whole suite:
// on constrained CI lanes (e.g. compact/FIPS clusters where masters double
// as workers) a bare, controller-less Pod like this one can be lost mid-run
// to node-pressure eviction, and nothing would otherwise recreate it --
// every subsequent wire check would then fail with "pods ... not found"
// for the rest of the suite instead of just this one attempt.
func execInClientPod(ctx context.Context, command []string) (stdout, stderr string, err error) {
	if err := ensureExecClientPod(ctx); err != nil {
		return "", "", fmt.Errorf("failed to ensure exec-client pod before exec: %w", err)
	}

	req := kubeClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(execClientPodName).
		Namespace(operatorNamespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: execClientContainerName,
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return "", "", fmt.Errorf("failed to build SPDY executor for exec-client pod: %w", err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdoutBuf,
		Stderr: &stderrBuf,
	})
	return stdoutBuf.String(), stderrBuf.String(), err
}

// curlExitCode extracts curl's remote exit code from an execInClientPod
// error, or -1 if err isn't a remote exit-code error (e.g. a transport
// failure reaching the exec-client pod itself, as opposed to curl running
// and failing inside it).
func curlExitCode(err error) int {
	var exitErr utilexec.CodeExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitStatus()
	}
	return -1
}
