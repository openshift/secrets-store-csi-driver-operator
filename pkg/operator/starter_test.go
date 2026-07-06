package operator

import (
	"encoding/json"
	"strings"
	"testing"

	opv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
)

type FakeOperator struct {
	metav1.ObjectMeta
	Spec   opv1.OperatorSpec
	Status opv1.OperatorStatus
}

func TestGetOperatorSyncState(t *testing.T) {
	deletionTimestamp := metav1.Now()

	cases := []struct {
		name          string
		operator      *FakeOperator
		expectedState opv1.ManagementState
	}{
		{
			name: "should return managed when the operator state is managed",
			operator: &FakeOperator{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec:       opv1.OperatorSpec{ManagementState: opv1.Managed},
			},

			expectedState: opv1.Managed,
		},
		{
			name: "should return unmanaged when the operator state is unmanaged",
			operator: &FakeOperator{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec:       opv1.OperatorSpec{ManagementState: opv1.Unmanaged},
			},
			expectedState: opv1.Unmanaged,
		},
		{
			name: "should return removed when the operator state is removed",
			operator: &FakeOperator{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec:       opv1.OperatorSpec{ManagementState: opv1.Removed},
			},
			expectedState: opv1.Removed,
		},
		{
			name: "should return removed when the deletion timestamp is set",
			operator: &FakeOperator{
				ObjectMeta: metav1.ObjectMeta{
					Name:              providerName,
					DeletionTimestamp: &deletionTimestamp,
				},
				Spec: opv1.OperatorSpec{ManagementState: opv1.Managed},
			},
			expectedState: opv1.Removed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			operatorClient := v1helpers.NewFakeOperatorClientWithObjectMeta(&tc.operator.ObjectMeta, &tc.operator.Spec, &tc.operator.Status, nil)
			state := getOperatorSyncState(operatorClient)
			if state != tc.expectedState {
				t.Fatalf("expected sync state to be %v, got %v", tc.expectedState, state)
			}
		})
	}
}

// clusterCSIDriverGVR is the GroupResource used for test GenericLister construction.
var clusterCSIDriverGVR = schema.GroupResource{
	Group:    "operator.openshift.io",
	Resource: "clustercsidrivers",
}

// makeTestLister builds a cache.GenericLister backed by a real indexer.
// If clusterCSIDriver is nil the lister returns a NotFound error for any Get call,
// which exercises the static-baseline upgrade no-op path in generateCSIDriverBytes.
func makeTestLister(t *testing.T, clusterCSIDriver *opv1.ClusterCSIDriver) cache.GenericLister {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	if clusterCSIDriver != nil {
		objMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(clusterCSIDriver)
		if err != nil {
			t.Fatalf("ToUnstructured: %v", err)
		}
		if err := indexer.Add(&unstructured.Unstructured{Object: objMap}); err != nil {
			t.Fatalf("indexer.Add: %v", err)
		}
	}
	return cache.NewGenericLister(indexer, clusterCSIDriverGVR)
}

// strPtr returns a pointer to the given string literal.
func strPtr(s string) *string { return &s }

func TestCSIDriverAssetFunc(t *testing.T) {
	cases := []struct {
		name                 string
		clusterCSIDriver     *opv1.ClusterCSIDriver
		wantRequiresRepublish *bool
		wantTokenRequestsLen  int  // -1 means "expect nil"
	}{
		{
			name: "SecretsStore/TokenManaged/2audiences",
			clusterCSIDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							TokenRequests: opv1.SecretsStoreTokenRequests{
								Type: opv1.TokenRequestsManaged,
								Managed: opv1.ManagedTokenRequests{
									Audiences: &[]opv1.SecretsStoreTokenRequest{
										{Audience: strPtr("audience1")},
										{Audience: strPtr("audience2")},
									},
								},
							},
						},
					},
				},
			},
			wantTokenRequestsLen: 2,
		},
		{
			name: "SecretsStore/TokenManaged/emptyAudiences",
			clusterCSIDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							TokenRequests: opv1.SecretsStoreTokenRequests{
								Type:    opv1.TokenRequestsManaged,
								Managed: opv1.ManagedTokenRequests{Audiences: &[]opv1.SecretsStoreTokenRequest{}},
							},
						},
					},
				},
			},
			wantTokenRequestsLen: 0, // empty slice: omitempty makes it nil after round-trip
		},
		{
			// FR-005: Unmanaged → tokenRequests omitted from bytes so spec-hash is stable
			// and any live tokenRequests are preserved by ApplyCSIDriver.
			name: "SecretsStore/TokenUnmanaged/tokenRequestsAbsent",
			clusterCSIDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							TokenRequests: opv1.SecretsStoreTokenRequests{
								Type: opv1.TokenRequestsUnmanaged,
							},
						},
					},
				},
			},
			wantTokenRequestsLen: -1,
		},
		{
			// FR-001/FR-002: Custom rotation → requiresRepublish=true
			name: "SecretsStore/RotationCustom/requiresRepublishTrue",
			clusterCSIDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: opv1.SecretsStoreSecretRotation{
								Type:   opv1.SecretRotationCustom,
								Custom: opv1.CustomSecretRotation{RotationPollIntervalSeconds: 300},
							},
						},
					},
				},
			},
			wantRequiresRepublish: func() *bool { v := true; return &v }(),
		},
		{
			// SecretRotationNone → requiresRepublish stays nil (matches static baseline)
			name: "SecretsStore/RotationNone/requiresRepublishNil",
			clusterCSIDriver: &opv1.ClusterCSIDriver{
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
			wantRequiresRepublish: nil,
			wantTokenRequestsLen:  -1,
		},
		{
			// FR-003: driverType ≠ SecretsStore → no mutation (upgrade no-op)
			name: "driverTypeNotSecretsStore/noMutation",
			clusterCSIDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.AWSDriverType,
					},
				},
			},
			wantRequiresRepublish: nil,
			wantTokenRequestsLen:  -1,
		},
		{
			// Upgrade no-op: ClusterCSIDriver absent → static baseline returned unchanged
			name:             "clusterCSIDriverNotFound/staticBaseline",
			clusterCSIDriver: nil,
			wantTokenRequestsLen: -1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lister := makeTestLister(t, tc.clusterCSIDriver)
			assetFn := csiDriverAssetFunc(lister, "test-namespace")

			data, err := assetFn("csidriver.yaml")
			if err != nil {
				t.Fatalf("csiDriverAssetFunc(csidriver.yaml) returned error: %v", err)
			}

			var got storagev1.CSIDriver
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal CSIDriver: %v", err)
			}

			// Baseline annotation must survive in all cases (FR-008).
			if got.Annotations["csi.openshift.io/managed"] != "true" {
				t.Errorf("annotation csi.openshift.io/managed: want %q, got %q",
					"true", got.Annotations["csi.openshift.io/managed"])
			}

			// requiresRepublish check
			if tc.wantRequiresRepublish == nil {
				if got.Spec.RequiresRepublish != nil {
					t.Errorf("RequiresRepublish: want nil, got %v", *got.Spec.RequiresRepublish)
				}
			} else {
				if got.Spec.RequiresRepublish == nil {
					t.Errorf("RequiresRepublish: want %v, got nil", *tc.wantRequiresRepublish)
				} else if *got.Spec.RequiresRepublish != *tc.wantRequiresRepublish {
					t.Errorf("RequiresRepublish: want %v, got %v",
						*tc.wantRequiresRepublish, *got.Spec.RequiresRepublish)
				}
			}

			// tokenRequests check
			switch {
			case tc.wantTokenRequestsLen == -1:
				if got.Spec.TokenRequests != nil {
					t.Errorf("TokenRequests: want nil, got %v", got.Spec.TokenRequests)
				}
			case tc.wantTokenRequestsLen > 0:
				if len(got.Spec.TokenRequests) != tc.wantTokenRequestsLen {
					t.Errorf("TokenRequests length: want %d, got %d",
						tc.wantTokenRequestsLen, len(got.Spec.TokenRequests))
				}
			default: // wantTokenRequestsLen == 0
				if len(got.Spec.TokenRequests) != 0 {
					t.Errorf("TokenRequests length: want 0, got %d", len(got.Spec.TokenRequests))
				}
			}
		})
	}
}

// hasExactArg returns true if args contains the exact string want.
func hasExactArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// hasArgPrefix returns true if any arg in args starts with prefix.
func hasArgPrefix(args []string, prefix string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}

// makeDaemonSet returns a minimal DaemonSet with a single container named csiDriverContainerName.
func makeDaemonSet(args []string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: csiDriverContainerName, Args: args},
					},
				},
			},
		},
	}
}

func TestWithSecretRotationHook(t *testing.T) {
	// baseArgs represents the stable args unrelated to rotation.
	baseArgs := []string{"--endpoint=unix:///csi/csi.sock", "--nodeid=test-node"}

	cases := []struct {
		name             string
		clusterCSIDriver *opv1.ClusterCSIDriver
		preArgs          []string // rotation args pre-loaded into the container (idempotency tests)
		wantExactArgs    []string // each must appear verbatim in result args
		wantAbsent       []string // no result arg may have any of these as a prefix
		wantErrNonNil    bool
	}{
		{
			// FR-001: None → --enable-secret-rotation=false; no poll interval
			name: "SecretRotationNone/disableRotation",
			clusterCSIDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: opv1.SecretsStoreSecretRotation{Type: opv1.SecretRotationNone},
						},
					},
				},
			},
			wantExactArgs: []string{enableSecretRotationArg + "=false"},
			wantAbsent:    []string{rotationPollIntervalArg},
		},
		{
			// FR-002: Custom 300s → 5m0s
			name: "SecretRotationCustom/300s/5m0s",
			clusterCSIDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: opv1.SecretsStoreSecretRotation{
								Type:   opv1.SecretRotationCustom,
								Custom: opv1.CustomSecretRotation{RotationPollIntervalSeconds: 300},
							},
						},
					},
				},
			},
			wantExactArgs: []string{
				enableSecretRotationArg + "=true",
				rotationPollIntervalArg + "=5m0s",
			},
		},
		{
			// FR-002: Custom 120s → 2m0s
			name: "SecretRotationCustom/120s/2m0s",
			clusterCSIDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: opv1.SecretsStoreSecretRotation{
								Type:   opv1.SecretRotationCustom,
								Custom: opv1.CustomSecretRotation{RotationPollIntervalSeconds: 120},
							},
						},
					},
				},
			},
			wantExactArgs: []string{
				enableSecretRotationArg + "=true",
				rotationPollIntervalArg + "=2m0s",
			},
		},
		{
			// FR-002: Custom 1s → 1s
			name: "SecretRotationCustom/1s/1s",
			clusterCSIDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: opv1.SecretsStoreSecretRotation{
								Type:   opv1.SecretRotationCustom,
								Custom: opv1.CustomSecretRotation{RotationPollIntervalSeconds: 1},
							},
						},
					},
				},
			},
			wantExactArgs: []string{
				enableSecretRotationArg + "=true",
				rotationPollIntervalArg + "=1s",
			},
		},
		{
			// FR-003: Custom with interval=0 (omitted) → default 2m
			name: "SecretRotationCustom/0s/default2m",
			clusterCSIDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: opv1.SecretsStoreSecretRotation{
								Type: opv1.SecretRotationCustom,
								// RotationPollIntervalSeconds omitted (zero)
							},
						},
					},
				},
			},
			wantExactArgs: []string{
				enableSecretRotationArg + "=true",
				rotationPollIntervalArg + "=" + defaultRotationPollInterval,
			},
		},
		{
			// FR-003: driverType ≠ SecretsStore → no mutation (upgrade no-op)
			name: "driverTypeNotSecretsStore/noMutation",
			clusterCSIDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{DriverType: opv1.AWSDriverType},
				},
			},
			wantAbsent: []string{enableSecretRotationArg, rotationPollIntervalArg},
		},
		{
			// FR-003: ClusterCSIDriver absent → static baseline preserved
			name:             "clusterCSIDriverNotFound/noMutation",
			clusterCSIDriver: nil,
			wantAbsent:       []string{enableSecretRotationArg, rotationPollIntervalArg},
		},
		{
			// FR-007: pre-existing rotation args are stripped before new values applied
			name: "idempotency/preExistingArgsReplaced",
			preArgs: []string{
				enableSecretRotationArg + "=true",
				rotationPollIntervalArg + "=5m0s",
			},
			clusterCSIDriver: &opv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: providerName},
				Spec: opv1.ClusterCSIDriverSpec{
					DriverConfig: opv1.CSIDriverConfigSpec{
						DriverType: opv1.SecretsStoreDriverType,
						SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
							SecretRotation: opv1.SecretsStoreSecretRotation{Type: opv1.SecretRotationNone},
						},
					},
				},
			},
			wantExactArgs: []string{enableSecretRotationArg + "=false"},
			wantAbsent:    []string{rotationPollIntervalArg},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lister := makeTestLister(t, tc.clusterCSIDriver)
			hook := withSecretRotationHook(lister)

			initialArgs := append(append([]string{}, baseArgs...), tc.preArgs...)
			ds := makeDaemonSet(initialArgs)

			err := hook(nil, ds)
			if tc.wantErrNonNil {
				if err == nil {
					t.Fatalf("hook: expected non-nil error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("hook: unexpected error: %v", err)
			}

			containerArgs := ds.Spec.Template.Spec.Containers[0].Args

			// Verify base args are not corrupted.
			for _, base := range baseArgs {
				if !hasExactArg(containerArgs, base) {
					t.Errorf("base arg %q lost after hook", base)
				}
			}

			for _, want := range tc.wantExactArgs {
				if !hasExactArg(containerArgs, want) {
					t.Errorf("want arg %q not found in %v", want, containerArgs)
				}
			}
			for _, prefix := range tc.wantAbsent {
				if hasArgPrefix(containerArgs, prefix) {
					t.Errorf("arg with prefix %q must be absent, found in %v", prefix, containerArgs)
				}
			}
		})
	}
}
