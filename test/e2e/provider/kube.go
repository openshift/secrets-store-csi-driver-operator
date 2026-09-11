package provider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/gomega"

	"github.com/openshift/secrets-store-csi-driver-operator/test/e2e/common"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/ptr"
)

const (
	secretProviderClassAPIVersion = "secrets-store.csi.x-k8s.io/v1"
	linuxNodeOS                   = "linux"
)

var secretProviderClassGVR = schema.GroupVersionResource{
	Group:    "secrets-store.csi.x-k8s.io",
	Version:  "v1",
	Resource: "secretproviderclasses",
}

// --- OpenShift cluster config ---

// OIDCIssuer returns the cluster service account issuer URL. Required for
// workload identity federation on Azure, AWS, and GCP.
func (e *Env) OIDCIssuer() (string, error) {
	ctx, cancel := e.WithAPITimeout()
	defer cancel()

	auth, err := e.OpenShiftConfig.Authentications().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("unable to get Authentication cluster: %w", err)
	}
	if auth.Spec.ServiceAccountIssuer == "" {
		return "", fmt.Errorf("cluster Authentication has no serviceAccountIssuer")
	}
	return auth.Spec.ServiceAccountIssuer, nil
}

// --- Namespaces and RBAC ---

// CreatePrivilegedNamespace creates a privileged namespace and grants the
// default ServiceAccount the privileged SCC.
func (e *Env) CreatePrivilegedNamespace(name string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"security.openshift.io/scc.podSecurityLabelSync": "false",
				"pod-security.kubernetes.io/enforce":             "privileged",
				"pod-security.kubernetes.io/audit":               "privileged",
				"pod-security.kubernetes.io/warn":                "privileged",
			},
		},
	}
	if _, err := e.Kube.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("unable to create namespace %q: %w", name, err)
	}
	return e.GrantPrivilegedSCC(name, "default")
}

// DeleteNamespace deletes a namespace, best-effort.
func (e *Env) DeleteNamespace(name string) error {
	return e.Kube.CoreV1().Namespaces().Delete(context.Background(), name, metav1.DeleteOptions{})
}

// GrantPrivilegedSCC grants the named ServiceAccount use of the privileged SCC.
func (e *Env) GrantPrivilegedSCC(namespace, serviceAccount string) error {
	ctx, cancel := e.WithAPITimeout()
	defer cancel()

	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("sscsi-e2e-privileged-%s", serviceAccount),
			Namespace: namespace,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     openShiftPrivilegedSCCClusterRole,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      serviceAccount,
			Namespace: namespace,
		}},
	}
	_, err := e.Kube.RbacV1().RoleBindings(namespace).Create(ctx, binding, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

// --- Manifest apply/delete (dynamic client) ---

// ApplyManifest upserts every object in a multi-document YAML manifest.
func (e *Env) ApplyManifest(manifest, defaultNamespace string) error {
	return e.forEachManifestObject(manifest, func(obj *unstructured.Unstructured) error {
		return e.upsertObject(obj, defaultNamespace)
	})
}

// DeleteManifest deletes every object in manifest, ignoring NotFound.
func (e *Env) DeleteManifest(manifest, defaultNamespace string) error {
	return e.forEachManifestObject(manifest, func(obj *unstructured.Unstructured) error {
		return e.deleteObject(obj, defaultNamespace, true)
	})
}

// ApplyManifestFromURL fetches and applies a remote multi-document manifest.
func (e *Env) ApplyManifestFromURL(url, defaultNamespace string) error {
	manifest, err := fetchManifest(url, common.APICallTimeout)
	if err != nil {
		return err
	}
	return e.ApplyManifest(manifest, defaultNamespace)
}

// DeleteManifestFromURL fetches and deletes objects from a remote manifest.
func (e *Env) DeleteManifestFromURL(url, defaultNamespace string) error {
	manifest, err := fetchManifest(url, common.APICallTimeout)
	if err != nil {
		return err
	}
	return e.DeleteManifest(manifest, defaultNamespace)
}

func fetchManifest(url string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("unable to fetch manifest from %q: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching manifest from %q: unexpected status %s", url, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("unable to read manifest from %q: %w", url, err)
	}
	return string(body), nil
}

func (e *Env) forEachManifestObject(manifest string, fn func(*unstructured.Unstructured) error) error {
	decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(obj); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("unable to decode manifest object: %w", err)
		}
		if len(obj.Object) == 0 {
			continue
		}
		if err := fn(obj); err != nil {
			return err
		}
	}
}

func (e *Env) upsertObject(obj *unstructured.Unstructured, defaultNamespace string) error {
	dr, err := e.resourceInterface(obj, defaultNamespace)
	if err != nil {
		return err
	}

	ctx, cancel := e.WithAPITimeout()
	defer cancel()

	existing, err := dr.Get(ctx, obj.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = dr.Create(ctx, obj, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	obj.SetResourceVersion(existing.GetResourceVersion())
	_, err = dr.Update(ctx, obj, metav1.UpdateOptions{})
	return err
}

func (e *Env) deleteObject(obj *unstructured.Unstructured, defaultNamespace string, ignoreNotFound bool) error {
	dr, err := e.resourceInterface(obj, defaultNamespace)
	if err != nil {
		return err
	}

	ctx, cancel := e.WithAPITimeout()
	defer cancel()

	err = dr.Delete(ctx, obj.GetName(), metav1.DeleteOptions{})
	if ignoreNotFound && apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (e *Env) resourceInterface(obj *unstructured.Unstructured, defaultNamespace string) (dynamic.ResourceInterface, error) {
	gvk := obj.GroupVersionKind()
	if gvk.Empty() {
		return nil, fmt.Errorf("manifest object %q has empty GroupVersionKind", obj.GetName())
	}

	mapping, err := e.RESTMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve REST mapping for %s: %w", gvk, err)
	}

	ns := obj.GetNamespace()
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if ns == "" {
			ns = defaultNamespace
			if ns == "" {
				return nil, fmt.Errorf("namespaced object %s/%s is missing a namespace", gvk.Kind, obj.GetName())
			}
			obj.SetNamespace(ns)
		}
		return e.Dynamic.Resource(mapping.Resource).Namespace(ns), nil
	}
	return e.Dynamic.Resource(mapping.Resource), nil
}

// --- SecretProviderClass ---

// SyncSecretObject builds a secretObjects entry for Kubernetes secret sync tests.
func SyncSecretObject(secretName, objectAlias, key, labelKey, labelValue string) map[string]interface{} {
	if objectAlias == "" {
		objectAlias = "secretalias"
	}
	if key == "" {
		key = "username"
	}
	obj := map[string]interface{}{
		"secretName": secretName,
		"type":       "Opaque",
		"data": []interface{}{
			map[string]interface{}{
				"objectName": objectAlias,
				"key":        key,
			},
		},
	}
	if labelKey != "" && labelValue != "" {
		obj["labels"] = map[string]interface{}{labelKey: labelValue}
	}
	return obj
}

// NewSecretProviderClass builds an unstructured SecretProviderClass.
func NewSecretProviderClass(namespace, name, provider string, parameters map[string]interface{}, secretObjects ...map[string]interface{}) *unstructured.Unstructured {
	spec := map[string]interface{}{
		"provider":   provider,
		"parameters": parameters,
	}
	if len(secretObjects) > 0 {
		objs := make([]interface{}, len(secretObjects))
		for i, o := range secretObjects {
			objs[i] = o
		}
		spec["secretObjects"] = objs
	}
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": secretProviderClassAPIVersion,
		"kind":       "SecretProviderClass",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
		},
		"spec": spec,
	}}
}

// CreateSecretProviderClass upserts spc in the cluster.
func (e *Env) CreateSecretProviderClass(spc *unstructured.Unstructured) error {
	return e.upsertObject(spc, spc.GetNamespace())
}

// SecretProviderClassProvider returns spec.provider for the named SPC.
func (e *Env) SecretProviderClassProvider(namespace, name string) (string, error) {
	ctx, cancel := e.WithAPITimeout()
	defer cancel()

	obj, err := e.Dynamic.Resource(secretProviderClassGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	provider, found, err := unstructured.NestedString(obj.Object, "spec", "provider")
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("SecretProviderClass %s/%s has no spec.provider", namespace, name)
	}
	return provider, nil
}

// --- Workloads: pods, deployments, exec ---

// CreatePod creates pod in the cluster.
func (e *Env) CreatePod(pod *corev1.Pod) error {
	ctx, cancel := e.WithAPITimeout()
	defer cancel()
	_, err := e.Kube.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	return err
}

// CreateDeployment creates deployment in the cluster.
func (e *Env) CreateDeployment(deployment *appsv1.Deployment) error {
	ctx, cancel := e.WithAPITimeout()
	defer cancel()
	_, err := e.Kube.AppsV1().Deployments(deployment.Namespace).Create(ctx, deployment, metav1.CreateOptions{})
	return err
}

// DeletePod deletes podName in namespace.
func (e *Env) DeletePod(namespace, podName string) error {
	return e.Kube.CoreV1().Pods(namespace).Delete(context.Background(), podName, metav1.DeleteOptions{})
}

// DeleteDeployment deletes a deployment by name in namespace.
func (e *Env) DeleteDeployment(namespace, name string) error {
	return e.Kube.AppsV1().Deployments(namespace).Delete(context.Background(), name, metav1.DeleteOptions{})
}

// ExecInPod runs command in the named container and returns trimmed stdout.
func (e *Env) ExecInPod(namespace, podName, container string, command []string) (string, error) {
	req := e.Kube.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(e.RestConfig, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("unable to build exec request for pod %s/%s: %w", namespace, podName, err)
	}

	var stdout, stderr bytes.Buffer
	err = executor.Stream(remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return "", fmt.Errorf("exec in pod %s/%s failed: %w: %s", namespace, podName, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// PodContainerName returns the first container name for podName.
func (e *Env) PodContainerName(namespace, podName string) (string, error) {
	ctx, cancel := e.WithAPITimeout()
	defer cancel()

	pod, err := e.Kube.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if len(pod.Spec.Containers) == 0 {
		return "", fmt.Errorf("pod %s/%s has no containers", namespace, podName)
	}
	return pod.Spec.Containers[0].Name, nil
}

// ReadMountedFile execs into the pod and cats mountPath.
func (e *Env) ReadMountedFile(namespace, podName, mountPath string) (string, error) {
	container, err := e.PodContainerName(namespace, podName)
	if err != nil {
		return "", err
	}
	return e.ExecInPod(namespace, podName, container, []string{"cat", mountPath})
}

// PodEnvValue returns the value of envName from the pod's environment.
func (e *Env) PodEnvValue(namespace, podName, envName string) (string, error) {
	container, err := e.PodContainerName(namespace, podName)
	if err != nil {
		return "", err
	}
	return e.ExecInPod(namespace, podName, container, []string{"printenv", envName})
}

// PodNameByLabel returns the first pod name matching app=<label>.
func (e *Env) PodNameByLabel(namespace, label string) (string, error) {
	pods, err := e.Kube.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "app=" + label})
	if err != nil {
		return "", err
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods with label app=%s in namespace %s", label, namespace)
	}
	return pods.Items[0].Name, nil
}

// CSIVolume builds a CSI inline volume referencing secretProviderClass.
func (e *Env) CSIVolume(name, spcName string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			CSI: &corev1.CSIVolumeSource{
				Driver:           common.DriverName,
				ReadOnly:         ptr.To(true),
				VolumeAttributes: map[string]string{"secretProviderClass": spcName},
			},
		},
	}
}

// BusyboxContainer returns a long-running test container using TestImage.
func (e *Env) BusyboxContainer(name string, mounts []corev1.VolumeMount, env ...corev1.EnvVar) corev1.Container {
	return corev1.Container{
		Name:            name,
		Image:           common.TestImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/bin/sleep", "3600"},
		VolumeMounts:    mounts,
		Env:             env,
	}
}

// InlineVolumePodSpec returns a pod spec mounting spcName at /mnt/secrets-store.
func (e *Env) InlineVolumePodSpec(spcName string, privileged bool) corev1.PodSpec {
	container := corev1.Container{
		Name:    "test-container",
		Image:   common.TestImage,
		Command: []string{"sh", "-c", "sleep 3600"},
		VolumeMounts: []corev1.VolumeMount{{
			Name:      "secrets-store-inline",
			MountPath: "/mnt/secrets-store",
			ReadOnly:  true,
		}},
	}
	if privileged {
		container.SecurityContext = &corev1.SecurityContext{Privileged: ptr.To(true)}
	}
	return corev1.PodSpec{
		ServiceAccountName: "default",
		Containers:         []corev1.Container{container},
		Volumes:            []corev1.Volume{e.CSIVolume("secrets-store-inline", spcName)},
	}
}

// BatsInlinePodSpec returns the pod spec from upstream pod-secrets-store-inline-volume-crd.yaml.
func (e *Env) BatsInlinePodSpec(spcName string) corev1.PodSpec {
	return corev1.PodSpec{
		TerminationGracePeriodSeconds: ptr.To(int64(0)),
		NodeSelector:                  map[string]string{"kubernetes.io/os": linuxNodeOS},
		Containers: []corev1.Container{
			e.BusyboxContainer("busybox", []corev1.VolumeMount{{
				Name:      "secrets-store-inline",
				MountPath: "/mnt/secrets-store",
				ReadOnly:  true,
			}}),
		},
		Volumes: []corev1.Volume{e.CSIVolume("secrets-store-inline", spcName)},
	}
}

// --- Synced Kubernetes Secrets ---

// GetSecretKey returns the decoded value of key in the named Secret.
func (e *Env) GetSecretKey(namespace, secretName, key string) (string, error) {
	secret, err := e.Kube.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	value, ok := secret.Data[key]
	if !ok {
		return "", apierrors.NewNotFound(corev1.Resource("secret"), secretName+"/"+key)
	}
	return string(value), nil
}

// GetSecretLabel returns the value of label on the named Secret.
func (e *Env) GetSecretLabel(namespace, secretName, label string) (string, error) {
	secret, err := e.Kube.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return secret.Labels[label], nil
}

// SecretOwnerReferenceCount returns len(secret.metadata.ownerReferences).
func (e *Env) SecretOwnerReferenceCount(namespace, secretName string) (int, error) {
	secret, err := e.Kube.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		return 0, err
	}
	return len(secret.OwnerReferences), nil
}

// WaitForSecretOwnerCount polls until secretName has exactly want ownerReferences.
func (e *Env) WaitForSecretOwnerCount(namespace, secretName string, want int) {
	Eventually(func() (int, error) {
		return e.SecretOwnerReferenceCount(namespace, secretName)
	}, common.PollTimeout, common.PollInterval).Should(Equal(want), "secret %s/%s ownerReferences did not converge to %d", namespace, secretName, want)
}

// WaitForSecretDeleted polls until secretName in namespace is gone.
func (e *Env) WaitForSecretDeleted(namespace, secretName string) {
	Eventually(func() error {
		_, err := e.Kube.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
		return err
	}, common.PollTimeout, common.PollInterval).Should(Satisfy(apierrors.IsNotFound), "secret %s/%s was not deleted", namespace, secretName)
}

// --- Wait helpers ---

// IsPodReady reports whether pod's Ready condition is True.
func IsPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

// WaitPodReady polls until the named pod's Ready condition is True.
func (e *Env) WaitPodReady(namespace, podName string) {
	Eventually(func() (bool, error) {
		pod, err := e.Kube.CoreV1().Pods(namespace).Get(context.Background(), podName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return IsPodReady(pod), nil
	}, common.PollTimeout, common.PollInterval).Should(BeTrue(), "pod %s/%s did not become Ready", namespace, podName)
}

// WaitForLabeledPodsReady waits until all pods with app=<label> are Ready.
func (e *Env) WaitForLabeledPodsReady(namespace, label string, timeout time.Duration) {
	Eventually(func() (bool, error) {
		pods, err := e.Kube.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "app=" + label})
		if err != nil {
			return false, err
		}
		if len(pods.Items) == 0 {
			return false, nil
		}
		for _, pod := range pods.Items {
			if !IsPodReady(&pod) {
				return false, nil
			}
		}
		return true, nil
	}, timeout, common.PollInterval).Should(BeTrue(), "pods with label app=%s in %s did not become Ready", label, namespace)
}

// WaitForPodDeleted polls until podName is gone from namespace.
func (e *Env) WaitForPodDeleted(namespace, podName string) {
	Eventually(func() error {
		_, err := e.Kube.CoreV1().Pods(namespace).Get(context.Background(), podName, metav1.GetOptions{})
		return err
	}, common.PollTimeout, common.PollInterval).Should(Satisfy(apierrors.IsNotFound), "pod %s/%s was not deleted", namespace, podName)
}

// WaitForPodMountFailure polls until pod events contain a FailedMount whose
// message matches wantSubstring.
func (e *Env) WaitForPodMountFailure(namespace, podName, wantSubstring string) {
	Eventually(func() (bool, error) {
		events, err := e.Kube.CoreV1().Events(namespace).List(context.Background(), metav1.ListOptions{
			FieldSelector: fmt.Sprintf("involvedObject.name=%s", podName),
		})
		if err != nil {
			return false, err
		}
		for _, event := range events.Items {
			if event.Reason == "FailedMount" && strings.Contains(event.Message, wantSubstring) {
				return true, nil
			}
		}
		return false, nil
	}, common.PollTimeout, common.PollInterval).Should(BeTrue(), "pod %s/%s did not emit FailedMount containing %q", namespace, podName, wantSubstring)
}

// WaitProviderReady polls until all pods with app=<label> in namespace are Ready.
func (e *Env) WaitProviderReady(namespace, appLabel string) {
	Eventually(func() (bool, error) {
		pods, err := e.Kube.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: "app=" + appLabel,
		})
		if err != nil {
			return false, err
		}
		if len(pods.Items) == 0 {
			return false, nil
		}
		for _, pod := range pods.Items {
			if !IsPodReady(&pod) {
				return false, nil
			}
		}
		return true, nil
	}, common.PollTimeout, common.PollInterval).Should(BeTrue(), "provider pods with label app=%s in namespace %q did not become Ready", appLabel, namespace)
}

// WaitForDaemonSetRollout polls until the node DaemonSet has rolled out.
func (e *Env) WaitForDaemonSetRollout() {
	Eventually(func() (bool, error) {
		ds, err := e.Kube.AppsV1().DaemonSets(common.OperatorNamespace).Get(context.Background(), common.DaemonSetName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return ds.Status.DesiredNumberScheduled > 0 &&
			ds.Status.UpdatedNumberScheduled == ds.Status.DesiredNumberScheduled &&
			ds.Status.NumberAvailable == ds.Status.DesiredNumberScheduled, nil
	}, common.PollTimeout, common.PollInterval).Should(BeTrue(), "DaemonSet %s/%s did not finish rolling out", common.OperatorNamespace, common.DaemonSetName)
}

// DaemonSetArgValue returns the value portion of the csi-driver container's
// arg with the given prefix.
func (e *Env) DaemonSetArgValue(prefix string) (string, error) {
	ds, err := e.Kube.AppsV1().DaemonSets(common.OperatorNamespace).Get(context.Background(), common.DaemonSetName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	for _, c := range ds.Spec.Template.Spec.Containers {
		if c.Name != common.CSIDriverContainer {
			continue
		}
		for _, arg := range c.Args {
			if strings.HasPrefix(arg, prefix) {
				return strings.TrimPrefix(arg, prefix), nil
			}
		}
		return "", fmt.Errorf("arg with prefix %q not found on container %q", prefix, common.CSIDriverContainer)
	}
	return "", fmt.Errorf("container %q not found in DaemonSet %s/%s", common.CSIDriverContainer, common.OperatorNamespace, common.DaemonSetName)
}

// WaitForTokenRequestAudiences polls the live CSIDriver until tokenRequests
// include wantAudience.
func (e *Env) WaitForTokenRequestAudiences(wantAudience string) {
	Eventually(func() (bool, error) {
		driver, err := e.Kube.StorageV1().CSIDrivers().Get(context.Background(), common.DriverName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, tr := range driver.Spec.TokenRequests {
			if tr.Audience == wantAudience {
				return true, nil
			}
		}
		return false, nil
	}, common.PollTimeout, common.PollInterval).Should(BeTrue(), "CSIDriver %q tokenRequests did not converge to include audience %q", common.DriverName, wantAudience)
}
