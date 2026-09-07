package operator

import (
	"testing"

	opv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	corev1informers "k8s.io/client-go/informers/core/v1"
	fakekube "k8s.io/client-go/kubernetes/fake"
)

const testOperatorNamespace = "openshift-cluster-csi-drivers"

// newFakeSecretInformer returns a corev1informers.SecretInformer backed by a
// fake clientset, with its lister store pre-seeded with secrets (without
// starting the informer's watch loop — table-driven unit tests only need the
// lister's cached Get/List, not live watch events).
func newFakeSecretInformer(t *testing.T, secrets ...*corev1.Secret) corev1informers.SecretInformer {
	t.Helper()

	client := fakekube.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(client, 0)
	secretInformer := factory.Core().V1().Secrets()
	store := secretInformer.Informer().GetStore()
	for _, s := range secrets {
		if err := store.Add(s); err != nil {
			t.Fatalf("failed to seed fake secret informer store: %v", err)
		}
	}
	return secretInformer
}

func newTestDaemonSetForTLSHook() *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "secrets-store-csi-driver-node", Namespace: testOperatorNamespace},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: csiDriverContainerName,
							Args: []string{
								"--metrics-addr=:8095",
							},
						},
					},
				},
			},
		},
	}
}

func TestWithOperandMetricsTLSDaemonSetHook(t *testing.T) {
	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operandMetricsTLSSecretName,
			Namespace: testOperatorNamespace,
		},
		Data: map[string][]byte{
			"tls.crt": []byte("fake-cert"),
			"tls.key": []byte("fake-key"),
		},
	}

	t.Run("secret present: volume and volumeMount injected, status annotation set, no error", func(t *testing.T) {
		informer := newFakeSecretInformer(t, existingSecret)
		hook := withOperandMetricsTLSDaemonSetHook(informer, testOperatorNamespace)

		daemonSet := newTestDaemonSetForTLSHook()
		if err := hook(&opv1.OperatorSpec{}, daemonSet); err != nil {
			t.Fatalf("unexpected error from hook: %v", err)
		}

		podSpec := daemonSet.Spec.Template.Spec
		if len(podSpec.Volumes) != 1 {
			t.Fatalf("expected exactly 1 volume, got %d: %+v", len(podSpec.Volumes), podSpec.Volumes)
		}
		vol := podSpec.Volumes[0]
		if vol.Name != operandMetricsTLSVolumeName {
			t.Errorf("expected volume name %q, got %q", operandMetricsTLSVolumeName, vol.Name)
		}
		if vol.Secret == nil || vol.Secret.SecretName != operandMetricsTLSSecretName {
			t.Errorf("expected volume to source Secret %q, got %+v", operandMetricsTLSSecretName, vol.Secret)
		}

		container := podSpec.Containers[0]
		if len(container.VolumeMounts) != 1 {
			t.Fatalf("expected exactly 1 volumeMount, got %d: %+v", len(container.VolumeMounts), container.VolumeMounts)
		}
		mount := container.VolumeMounts[0]
		if mount.Name != operandMetricsTLSVolumeName {
			t.Errorf("expected volumeMount name %q, got %q", operandMetricsTLSVolumeName, mount.Name)
		}
		if mount.MountPath != operandMetricsTLSMountPath {
			t.Errorf("expected mountPath %q, got %q", operandMetricsTLSMountPath, mount.MountPath)
		}
		if !mount.ReadOnly {
			t.Error("expected volumeMount to be read-only")
		}

		// Container args must be untouched: the operand binary has no TLS
		// flag to set (T1_1 finding) — this hook must not invent one.
		if len(container.Args) != 1 || container.Args[0] != "--metrics-addr=:8095" {
			t.Errorf("expected container args to be unmodified, got %v", container.Args)
		}

		gotStatus := daemonSet.Annotations[operandMetricsTLSStatusAnnotation]
		if gotStatus != "cert-mounted-enforcement-pending-operand-support" {
			t.Errorf("expected status annotation %q, got %q", "cert-mounted-enforcement-pending-operand-support", gotStatus)
		}
	})

	t.Run("secret absent: hook returns error, fail-closed, no volume added", func(t *testing.T) {
		informer := newFakeSecretInformer(t) // no secrets seeded
		hook := withOperandMetricsTLSDaemonSetHook(informer, testOperatorNamespace)

		daemonSet := newTestDaemonSetForTLSHook()
		err := hook(&opv1.OperatorSpec{}, daemonSet)
		if err == nil {
			t.Fatal("expected an error when the TLS cert secret is absent, got nil")
		}

		if len(daemonSet.Spec.Template.Spec.Volumes) != 0 {
			t.Errorf("expected no volumes to be added when the hook fails closed, got %+v", daemonSet.Spec.Template.Spec.Volumes)
		}
		if len(daemonSet.Spec.Template.Spec.Containers[0].VolumeMounts) != 0 {
			t.Errorf("expected no volumeMounts to be added when the hook fails closed, got %+v", daemonSet.Spec.Template.Spec.Containers[0].VolumeMounts)
		}
	})

	t.Run("container not found: hook returns error", func(t *testing.T) {
		informer := newFakeSecretInformer(t, existingSecret)
		hook := withOperandMetricsTLSDaemonSetHook(informer, testOperatorNamespace)

		daemonSet := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "secrets-store-csi-driver-node", Namespace: testOperatorNamespace},
			Spec: appsv1.DaemonSetSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "some-other-container"}},
					},
				},
			},
		}
		if err := hook(&opv1.OperatorSpec{}, daemonSet); err == nil {
			t.Fatal("expected an error when the csi-driver container is not found, got nil")
		}
	})

	t.Run("idempotent: running the hook twice does not duplicate volume/volumeMount", func(t *testing.T) {
		informer := newFakeSecretInformer(t, existingSecret)
		hook := withOperandMetricsTLSDaemonSetHook(informer, testOperatorNamespace)

		daemonSet := newTestDaemonSetForTLSHook()
		if err := hook(&opv1.OperatorSpec{}, daemonSet); err != nil {
			t.Fatalf("unexpected error on first run: %v", err)
		}
		if err := hook(&opv1.OperatorSpec{}, daemonSet); err != nil {
			t.Fatalf("unexpected error on second run: %v", err)
		}

		if got := len(daemonSet.Spec.Template.Spec.Volumes); got != 1 {
			t.Errorf("expected exactly 1 volume after running the hook twice, got %d", got)
		}
		if got := len(daemonSet.Spec.Template.Spec.Containers[0].VolumeMounts); got != 1 {
			t.Errorf("expected exactly 1 volumeMount after running the hook twice, got %d", got)
		}
	})
}

func TestWithOperandPprofPostureHook(t *testing.T) {
	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operandMetricsTLSSecretName,
			Namespace: testOperatorNamespace,
		},
		Data: map[string][]byte{
			"tls.crt": []byte("fake-cert"),
			"tls.key": []byte("fake-key"),
		},
	}

	t.Run("default state: no enable-pprof arg -> disabled status, no error, no secret lookup needed", func(t *testing.T) {
		informer := newFakeSecretInformer(t) // no secrets seeded: must not be consulted in the default-disabled path
		hook := withOperandPprofPostureHook(informer, testOperatorNamespace)

		daemonSet := newTestDaemonSetForTLSHook()
		if err := hook(&opv1.OperatorSpec{}, daemonSet); err != nil {
			t.Fatalf("unexpected error from hook: %v", err)
		}

		gotStatus := daemonSet.Annotations[operandPprofStatusAnnotation]
		if gotStatus != "disabled-by-default-no-port-opened-no-cert-requested" {
			t.Errorf("expected status annotation %q, got %q", "disabled-by-default-no-port-opened-no-cert-requested", gotStatus)
		}

		// No volume/volumeMount should be added when pprof is disabled -- no
		// port is opened, no cert is requested (FR-009).
		if len(daemonSet.Spec.Template.Spec.Volumes) != 0 {
			t.Errorf("expected no volumes when pprof is disabled, got %+v", daemonSet.Spec.Template.Spec.Volumes)
		}
	})

	t.Run("enabled + secret present: enabled-no-tls status, no error", func(t *testing.T) {
		informer := newFakeSecretInformer(t, existingSecret)
		hook := withOperandPprofPostureHook(informer, testOperatorNamespace)

		daemonSet := newTestDaemonSetForTLSHook()
		daemonSet.Spec.Template.Spec.Containers[0].Args = append(daemonSet.Spec.Template.Spec.Containers[0].Args, "--enable-pprof=true")

		if err := hook(&opv1.OperatorSpec{}, daemonSet); err != nil {
			t.Fatalf("unexpected error from hook: %v", err)
		}

		gotStatus := daemonSet.Annotations[operandPprofStatusAnnotation]
		if gotStatus != "enabled-no-tls-support-in-operand-binary" {
			t.Errorf("expected status annotation %q, got %q", "enabled-no-tls-support-in-operand-binary", gotStatus)
		}
	})

	t.Run("enabled + secret absent: hook returns error, fail-closed", func(t *testing.T) {
		informer := newFakeSecretInformer(t) // no secrets seeded
		hook := withOperandPprofPostureHook(informer, testOperatorNamespace)

		daemonSet := newTestDaemonSetForTLSHook()
		daemonSet.Spec.Template.Spec.Containers[0].Args = append(daemonSet.Spec.Template.Spec.Containers[0].Args, "--enable-pprof=true")

		err := hook(&opv1.OperatorSpec{}, daemonSet)
		if err == nil {
			t.Fatal("expected an error when pprof is enabled but the shared serving-cert secret is absent, got nil")
		}
	})

	t.Run("container not found: hook returns error", func(t *testing.T) {
		informer := newFakeSecretInformer(t, existingSecret)
		hook := withOperandPprofPostureHook(informer, testOperatorNamespace)

		daemonSet := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "secrets-store-csi-driver-node", Namespace: testOperatorNamespace},
			Spec: appsv1.DaemonSetSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "some-other-container"}},
					},
				},
			},
		}
		if err := hook(&opv1.OperatorSpec{}, daemonSet); err == nil {
			t.Fatal("expected an error when the csi-driver container is not found, got nil")
		}
	})
}

func TestContainerArgEnabled(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		prefix   string
		expected bool
	}{
		{name: "exact match true", args: []string{"--enable-pprof=true"}, prefix: "--enable-pprof=", expected: true},
		{name: "exact match false", args: []string{"--enable-pprof=false"}, prefix: "--enable-pprof=", expected: false},
		{name: "arg absent", args: []string{"--metrics-addr=:8095"}, prefix: "--enable-pprof=", expected: false},
		{name: "no args", args: nil, prefix: "--enable-pprof=", expected: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			container := &corev1.Container{Args: tc.args}
			if got := containerArgEnabled(container, tc.prefix); got != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

func TestInjectSecretVolumeMount(t *testing.T) {
	t.Run("appends volume and volumeMount when absent", func(t *testing.T) {
		daemonSet := newTestDaemonSetForTLSHook()
		container := &daemonSet.Spec.Template.Spec.Containers[0]

		injectSecretVolumeMount(daemonSet, container, "vol", "my-secret", "/mnt/path")

		if len(daemonSet.Spec.Template.Spec.Volumes) != 1 {
			t.Fatalf("expected 1 volume, got %d", len(daemonSet.Spec.Template.Spec.Volumes))
		}
		if len(container.VolumeMounts) != 1 {
			t.Fatalf("expected 1 volumeMount, got %d", len(container.VolumeMounts))
		}
	})

	t.Run("updates existing volume/volumeMount in place rather than duplicating", func(t *testing.T) {
		daemonSet := newTestDaemonSetForTLSHook()
		daemonSet.Spec.Template.Spec.Volumes = []corev1.Volume{
			{Name: "vol", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "stale-secret"}}},
		}
		container := &daemonSet.Spec.Template.Spec.Containers[0]
		container.VolumeMounts = []corev1.VolumeMount{
			{Name: "vol", MountPath: "/old/path", ReadOnly: false},
		}

		injectSecretVolumeMount(daemonSet, container, "vol", "my-secret", "/mnt/path")

		if len(daemonSet.Spec.Template.Spec.Volumes) != 1 {
			t.Fatalf("expected volume to be updated in place, not duplicated; got %d volumes", len(daemonSet.Spec.Template.Spec.Volumes))
		}
		if daemonSet.Spec.Template.Spec.Volumes[0].Secret.SecretName != "my-secret" {
			t.Errorf("expected volume's secret name to be updated to %q, got %q", "my-secret", daemonSet.Spec.Template.Spec.Volumes[0].Secret.SecretName)
		}
		if len(container.VolumeMounts) != 1 {
			t.Fatalf("expected volumeMount to be updated in place, not duplicated; got %d mounts", len(container.VolumeMounts))
		}
		if container.VolumeMounts[0].MountPath != "/mnt/path" || !container.VolumeMounts[0].ReadOnly {
			t.Errorf("expected volumeMount to be updated to mountPath=/mnt/path, readOnly=true, got %+v", container.VolumeMounts[0])
		}
	})
}
