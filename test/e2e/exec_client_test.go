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

// The test process is a ci-operator step on the build farm, not a pod on
// the cluster. It has kubeconfig for the API server only: no route to pod
// or Service IPs, and no in-cluster DNS. Curling a pod IP or a Service
// DNS name from the test process fails no matter how long we retry.
//
// execClientPodName is a small pod created in operatorNamespace and left
// running for the "TLS profile adherence" Describe (BeforeAll through
// AfterAll in tls_profile_test.go). That Describe is part of the single
// e2e suite registered by TestE2E. The checks exec into the pod and run
// curl there, on the same path a client such as Prometheus uses: cluster
// routing, NetworkPolicy, and in-cluster DNS.
const (
	execClientPodName       = "sscsi-e2e-tls-client"
	execClientContainerName = "client"
	// Set E2E_TLS_CLIENT_IMAGE to override execClientDefaultImage. A
	// disconnected cluster often cannot pull registry.access.redhat.com.
	//
	// The operand images already on the cluster (csi-driver,
	// csi-node-driver-registrar, and csi-liveness-probe in assets/node.yaml)
	// are too small to use here: none of them has a shell or curl. The image
	// has to be set explicitly instead of taken from the DaemonSet.
	execClientImageEnv = "E2E_TLS_CLIENT_IMAGE"
	// ubi9-minimal ships curl >= 7.54, which is what --tlsv1.x and --tls-max
	// need. The digest is pinned so the suite does not float on :latest.
	execClientDefaultImage = "registry.access.redhat.com/ubi9/ubi-minimal@sha256:8eb2830d0936237fc13a1f2f7e45aecf90d69043380ad167fad0343632937f41"
	execPodReadyTimeout    = 3 * time.Minute
)

func execClientImage() string {
	if img := os.Getenv(execClientImageEnv); img != "" {
		return img
	}
	return execClientDefaultImage
}

// ensureExecClientPod creates the exec-client pod when it is missing and
// waits until it is Ready. Calling it again is fine; BeforeAll can run
// more than once when a suite is retried.
//
// If the pod exists but is not Running, we delete it and create a new one.
// RestartPolicy is Never, so a Succeeded, Failed, or Unknown pod stays that
// way. Waiting on Ready would just hit execPodReadyTimeout on every later call.
func ensureExecClientPod(ctx context.Context) error {
	pod, err := kubeClient.CoreV1().Pods(operatorNamespace).Get(ctx, execClientPodName, metav1.GetOptions{})
	switch {
	case err == nil:
		if pod.Status.Phase == corev1.PodRunning {
			return waitForExecClientPodReady(ctx)
		}
		if err := deleteExecClientPod(ctx); err != nil {
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
				Name:    execClientContainerName,
				Image:   execClientImage(),
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

// deleteExecClientPod deletes the pod created by ensureExecClientPod.
// A missing pod is not an error: AfterAll also runs when the pod was never created.
func deleteExecClientPod(ctx context.Context) error {
	err := kubeClient.CoreV1().Pods(operatorNamespace).Delete(ctx, execClientPodName, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// execInClientPod runs command in the exec-client pod through the exec
// API, the same way oc exec does, and returns stdout and stderr. If the
// command exits nonzero, err is a *utilexec.CodeExitError. curlExitCode
// reads that status back out.
//
// The pod is checked before every call. When it is already Ready that is
// only a Get. BeforeAll is not enough for the whole suite: on a small CI
// cluster (compact or FIPS, where masters are also workers) this pod has
// no controller, so node pressure can evict it. If we did not create it
// again, every later check would fail with "pod not found".
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

// curlExitCode returns curl's exit status from an execInClientPod error.
// It returns -1 when the failure is not curl's own exit, for example when
// we could not reach the exec-client pod at all.
func curlExitCode(err error) int {
	var exitErr utilexec.CodeExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitStatus()
	}
	return -1
}
