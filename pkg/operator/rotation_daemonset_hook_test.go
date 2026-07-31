package operator

import (
	"reflect"
	"testing"
	"time"

	opv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// newTestDaemonSet returns a DaemonSet with a csi-driver container carrying
// the hardcoded rotation args from assets/node.yaml.
func newTestDaemonSet() *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "secrets-store-csi-driver-node", Namespace: "openshift-cluster-csi-drivers"},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: csiDriverContainerName,
							Args: []string{
								"--enable-secret-rotation=true",
								"--rotation-poll-interval=2m",
							},
						},
					},
				},
			},
		},
	}
}

func TestWithSecretRotationDaemonSetHook(t *testing.T) {
	cases := []struct {
		name         string
		driver       *opv1.ClusterCSIDriver
		expectedArgs []string
	}{
		{
			name:   "ClusterCSIDriver not found keeps defaults",
			driver: nil,
			expectedArgs: []string{
				"--enable-secret-rotation=true",
				"--rotation-poll-interval=2m",
			},
		},
		{
			name: "no driverConfig set keeps defaults",
			driver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
			},
			expectedArgs: []string{
				"--enable-secret-rotation=true",
				"--rotation-poll-interval=2m",
			},
		},
		{
			name: "type None disables rotation",
			driver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: opv1.SecretsStoreSecretRotation{
								Type: opv1.SecretRotationNone,
							},
						},
					},
				},
			},
			expectedArgs: []string{
				"--enable-secret-rotation=false",
				"--rotation-poll-interval=2m",
			},
		},
		{
			name: "type Custom with explicit interval sets that interval",
			driver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: opv1.SecretsStoreSecretRotation{
								Type: opv1.SecretRotationCustom,
								Custom: opv1.CustomSecretRotation{
									MinimumRefreshAge: 300,
								},
							},
						},
					},
				},
			},
			expectedArgs: []string{
				"--enable-secret-rotation=true",
				"--rotation-poll-interval=5m",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lister := newFakeClusterCSIDriverLister(t, tc.driver)
			hook := withSecretRotationDaemonSetHook(lister, providerName)

			daemonSet := newTestDaemonSet()
			if err := hook(&opv1.OperatorSpec{}, daemonSet); err != nil {
				t.Fatalf("unexpected error from hook: %v", err)
			}

			gotArgs := daemonSet.Spec.Template.Spec.Containers[0].Args
			if !reflect.DeepEqual(gotArgs, tc.expectedArgs) {
				t.Fatalf("expected args to be %v, got %v", tc.expectedArgs, gotArgs)
			}
		})
	}
}

func TestFormatRotationInterval(t *testing.T) {
	cases := []struct {
		name     string
		interval time.Duration
		expected string
	}{
		{
			name:     "whole minutes render as Nm (preserves pre-feature literal)",
			interval: 2 * time.Minute,
			expected: "2m",
		},
		{
			name:     "5 minutes renders as 5m",
			interval: 5 * time.Minute,
			expected: "5m",
		},
		{
			name:     "non-whole-minute duration falls back to time.Duration.String()",
			interval: 90 * time.Second,
			expected: "1m30s",
		},
		{
			name:     "sub-minute duration falls back to time.Duration.String()",
			interval: 45 * time.Second,
			expected: "45s",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatRotationInterval(tc.interval)
			if got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestSetArg(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		prefix   string
		value    string
		expected []string
	}{
		{
			name:     "replaces an existing arg matching the prefix",
			args:     []string{"--enable-secret-rotation=true", "--rotation-poll-interval=2m"},
			prefix:   "--rotation-poll-interval=",
			value:    "5m",
			expected: []string{"--enable-secret-rotation=true", "--rotation-poll-interval=5m"},
		},
		{
			name:     "appends the arg when no existing element matches the prefix",
			args:     []string{"--enable-secret-rotation=true"},
			prefix:   "--rotation-poll-interval=",
			value:    "2m",
			expected: []string{"--enable-secret-rotation=true", "--rotation-poll-interval=2m"},
		},
		{
			name:     "does not reorder or otherwise affect unrelated args",
			args:     []string{"--a=1", "--rotation-poll-interval=2m", "--b=2"},
			prefix:   "--rotation-poll-interval=",
			value:    "10s",
			expected: []string{"--a=1", "--rotation-poll-interval=10s", "--b=2"},
		},
		{
			name:     "appends into an empty args slice",
			args:     []string{},
			prefix:   "--enable-secret-rotation=",
			value:    "false",
			expected: []string{"--enable-secret-rotation=false"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := setArg(tc.args, tc.prefix, tc.value)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Fatalf("expected args to be %v, got %v", tc.expected, got)
			}
		})
	}
}
