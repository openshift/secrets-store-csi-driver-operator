package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
	"k8s.io/utils/ptr"
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
	// execClientImageEnv optionally overrides execClientDefaultImage, in
	// case that image isn't pullable in a given environment.
	execClientImageEnv = "E2E_TLS_CLIENT_IMAGE"
	// execClientDefaultImage needs a curl new enough to support
	// --tlsv1.x/--tls-max (curl >=7.54); ubi9-minimal's is.
	execClientDefaultImage = "registry.access.redhat.com/ubi9/ubi-minimal:latest"
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
func ensureExecClientPod(ctx context.Context) error {
	_, err := kubeClient.CoreV1().Pods(operatorNamespace).Get(ctx, execClientPodName, metav1.GetOptions{})
	if err == nil {
		return waitForExecClientPodReady(ctx)
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to check for existing exec-client pod %s/%s: %w", operatorNamespace, execClientPodName, err)
	}

	pod := &corev1.Pod{
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
				Command: []string{"sleep", "3600"},
			}},
		},
	}
	if _, err := kubeClient.CoreV1().Pods(operatorNamespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
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
func execInClientPod(ctx context.Context, command []string) (stdout, stderr string, err error) {
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

// metricsReaderServiceAccountName/ClusterRoleName back a dedicated identity
// used only to mint bearer tokens for scrapeOperatorMetrics (see
// mintMetricsReaderToken and metricsBearerToken in
// operator_insecure_tls_test.go).
//
// This exists because restConfig's own credentials can't be turned back
// into a bearer token: the e2e process authenticates however $KUBECONFIG
// says to (a CI-provisioned cluster typically hands out a client-certificate
// admin kubeconfig with no token at all, while a developer's local
// kubeconfig from `oc login` usually does have one) -- there is no generic
// way to recover a bearer token from an arbitrary rest.Config. Minting our
// own token via the TokenRequest API sidesteps that entirely and works the
// same way regardless of how the ambient kubeconfig authenticates.
//
// The operator's own metrics listener (library-go controllercmd's stock
// serving setup, see ToServerConfig) authorizes requests via
// DelegatingAuthorizationOptions, which performs a SubjectAccessReview with
// NonResourceAttributes{Path: "/metrics", Verb: "get"} -- RBAC only
// supports nonResourceURLs on ClusterRoles (not namespaced Roles), hence
// the ClusterRole/ClusterRoleBinding below rather than a Role/RoleBinding.
const (
	metricsReaderServiceAccountName = "sscsi-e2e-metrics-reader"
	metricsReaderClusterRoleName    = "sscsi-e2e-metrics-reader"
	// metricsReaderTokenExpirationSeconds is comfortably above
	// wireRetryTimeout: metricsBearerToken mints a fresh token on every
	// call anyway, so this just bounds how long a single minted token
	// stays valid if a caller held onto it.
	metricsReaderTokenExpirationSeconds = int64(10 * 60)
)

// ensureMetricsReaderRBAC creates (or reuses) the ServiceAccount and
// ClusterRole/ClusterRoleBinding that authorize metricsBearerToken's minted
// tokens to GET /metrics. Safe to call multiple times.
func ensureMetricsReaderRBAC(ctx context.Context) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      metricsReaderServiceAccountName,
			Namespace: operatorNamespace,
		},
	}
	if _, err := kubeClient.CoreV1().ServiceAccounts(operatorNamespace).Create(ctx, sa, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create metrics-reader ServiceAccount %s/%s: %w", operatorNamespace, metricsReaderServiceAccountName, err)
	}

	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: metricsReaderClusterRoleName},
		Rules: []rbacv1.PolicyRule{{
			NonResourceURLs: []string{"/metrics"},
			Verbs:           []string{"get"},
		}},
	}
	if _, err := kubeClient.RbacV1().ClusterRoles().Create(ctx, role, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create metrics-reader ClusterRole %s: %w", metricsReaderClusterRoleName, err)
	}

	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: metricsReaderClusterRoleName},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     metricsReaderClusterRoleName,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      metricsReaderServiceAccountName,
			Namespace: operatorNamespace,
		}},
	}
	if _, err := kubeClient.RbacV1().ClusterRoleBindings().Create(ctx, binding, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create metrics-reader ClusterRoleBinding %s: %w", metricsReaderClusterRoleName, err)
	}
	return nil
}

// deleteMetricsReaderRBAC tears down the objects created by
// ensureMetricsReaderRBAC. Safe to call even if they were never created.
func deleteMetricsReaderRBAC(ctx context.Context) {
	_ = kubeClient.RbacV1().ClusterRoleBindings().Delete(ctx, metricsReaderClusterRoleName, metav1.DeleteOptions{})
	_ = kubeClient.RbacV1().ClusterRoles().Delete(ctx, metricsReaderClusterRoleName, metav1.DeleteOptions{})
	_ = kubeClient.CoreV1().ServiceAccounts(operatorNamespace).Delete(ctx, metricsReaderServiceAccountName, metav1.DeleteOptions{})
}

// mintMetricsReaderToken requests a fresh, short-lived bearer token for the
// metrics-reader ServiceAccount via the TokenRequest API (the same
// mechanism behind `oc create token`/`kubectl create token`). Requires
// ensureMetricsReaderRBAC to have been called first.
func mintMetricsReaderToken(ctx context.Context) (string, error) {
	tr, err := kubeClient.CoreV1().ServiceAccounts(operatorNamespace).CreateToken(ctx, metricsReaderServiceAccountName, &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: ptr.To(metricsReaderTokenExpirationSeconds),
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create token for metrics-reader ServiceAccount %s/%s: %w", operatorNamespace, metricsReaderServiceAccountName, err)
	}
	if tr.Status.Token == "" {
		return "", fmt.Errorf("empty token returned for metrics-reader ServiceAccount %s/%s", operatorNamespace, metricsReaderServiceAccountName)
	}
	return tr.Status.Token, nil
}
