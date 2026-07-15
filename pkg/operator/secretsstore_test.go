package operator

import (
	"errors"
	"reflect"
	"testing"
	"time"

	opv1 "github.com/openshift/api/operator/v1"
	operatorv1listers "github.com/openshift/client-go/operator/listers/operator/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
)

// fakeClusterCSIDriverLister is a minimal fake for operatorv1listers.ClusterCSIDriverLister.
type fakeClusterCSIDriverLister struct {
	driver *opv1.ClusterCSIDriver
	err    error
}

func (f *fakeClusterCSIDriverLister) List(selector labels.Selector) ([]*opv1.ClusterCSIDriver, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.driver == nil {
		return nil, nil
	}
	return []*opv1.ClusterCSIDriver{f.driver}, nil
}

func (f *fakeClusterCSIDriverLister) Get(name string) (*opv1.ClusterCSIDriver, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.driver == nil {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "clustercsidrivers"}, name)
	}
	return f.driver, nil
}

// newFakeClusterCSIDriverLister returns a fakeClusterCSIDriverLister
func newFakeClusterCSIDriverLister(t *testing.T, driver *opv1.ClusterCSIDriver) operatorv1listers.ClusterCSIDriverLister {
	t.Helper()
	return &fakeClusterCSIDriverLister{driver: driver}
}

func TestGetClusterCSIDriverConfig(t *testing.T) {
	const driverName = "secrets-store.csi.k8s.io"
	audience := "sts.amazonaws.com"

	managedDriver := &opv1.ClusterCSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: driverName},
		Spec: opv1.ClusterCSIDriverSpec{
			DriverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					TokenRequests: opv1.SecretsStoreTokenRequests{
						Type: opv1.TokenRequestsManaged,
						Managed: opv1.ManagedTokenRequests{
							Audiences: &[]opv1.SecretsStoreTokenRequest{{Audience: &audience}},
						},
					},
				},
			},
		},
	}

	cases := []struct {
		name       string
		driver     *opv1.ClusterCSIDriver
		listerErr  error
		wantErr    bool
		wantConfig opv1.CSIDriverConfigSpec
	}{
		{
			name:       "ClusterCSIDriver not found returns zero value config",
			wantConfig: opv1.CSIDriverConfigSpec{},
		},
		{
			name:       "ClusterCSIDriver present returns its driverConfig",
			driver:     managedDriver,
			wantConfig: managedDriver.Spec.DriverConfig,
		},
		{
			name:      "lister error is propagated",
			listerErr: errors.New("boom"),
			wantErr:   true,
		},
		{
			name:       "NotFound error is treated as omitted config, not propagated",
			listerErr:  apierrors.NewNotFound(schema.GroupResource{Resource: "clustercsidrivers"}, driverName),
			wantConfig: opv1.CSIDriverConfigSpec{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lister := &fakeClusterCSIDriverLister{driver: tc.driver, err: tc.listerErr}
			config, err := getClusterCSIDriverConfig(lister, driverName)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if config != tc.wantConfig {
				t.Errorf("expected config %+v, got %+v", tc.wantConfig, config)
			}
		})
	}
}

func TestGetSecretRotationConfig(t *testing.T) {
	cases := []struct {
		name             string
		driverConfig     opv1.CSIDriverConfigSpec
		expectedEnabled  bool
		expectedInterval time.Duration
	}{
		{
			name:             "zero-value driverConfig (driverType unset) returns defaults",
			driverConfig:     opv1.CSIDriverConfigSpec{},
			expectedEnabled:  true,
			expectedInterval: 2 * time.Minute,
		},
		{
			name: "driverType not SecretsStore returns defaults",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.AWSDriverType,
			},
			expectedEnabled:  true,
			expectedInterval: 2 * time.Minute,
		},
		{
			name: "driverType SecretsStore with zero-value secretsStore returns defaults",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
			},
			expectedEnabled:  true,
			expectedInterval: 2 * time.Minute,
		},
		{
			name: "secretRotation zero value (type unset) returns defaults",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType:   opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{},
			},
			expectedEnabled:  true,
			expectedInterval: 2 * time.Minute,
		},
		{
			name: "type None disables rotation",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					SecretRotation: opv1.SecretsStoreSecretRotation{
						Type: opv1.SecretRotationNone,
					},
				},
			},
			expectedEnabled:  false,
			expectedInterval: 2 * time.Minute,
		},
		{
			name: "type Custom with explicit minimumRefreshAge uses that interval",
			driverConfig: opv1.CSIDriverConfigSpec{
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
			expectedEnabled:  true,
			expectedInterval: 5 * time.Minute,
		},
		{
			name: "type Custom with omitted minimumRefreshAge defaults to 120s",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					SecretRotation: opv1.SecretsStoreSecretRotation{
						Type: opv1.SecretRotationCustom,
					},
				},
			},
			expectedEnabled:  true,
			expectedInterval: 120 * time.Second,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enabled, interval := getSecretRotationConfig(tc.driverConfig)
			if enabled != tc.expectedEnabled {
				t.Errorf("expected enabled to be %v, got %v", tc.expectedEnabled, enabled)
			}
			if interval != tc.expectedInterval {
				t.Errorf("expected interval to be %v, got %v", tc.expectedInterval, interval)
			}
		})
	}
}

func TestGetRequiresRepublish(t *testing.T) {
	cases := []struct {
		name         string
		rotationType opv1.SecretRotationType
		expected     bool
	}{
		{
			name:         "omitted rotation mirrors enabled default (true)",
			rotationType: "",
			expected:     true,
		},
		{
			name:         "type Custom mirrors enabled (true)",
			rotationType: opv1.SecretRotationCustom,
			expected:     true,
		},
		{
			name:         "type None mirrors disabled (false)",
			rotationType: opv1.SecretRotationNone,
			expected:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			driverConfig := opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					SecretRotation: opv1.SecretsStoreSecretRotation{Type: tc.rotationType},
				},
			}
			got := getRequiresRepublish(driverConfig)
			if got == nil || *got != tc.expected {
				t.Errorf("expected requiresRepublish=%v, got %v", tc.expected, got)
			}
		})
	}
}

func TestGetEffectiveTokenRequests(t *testing.T) {
	awsAudience := "sts.amazonaws.com"
	azureAudience := "api://AzureADTokenExchange"
	existing := []storagev1.TokenRequest{{Audience: azureAudience}}

	cases := []struct {
		name         string
		driverConfig opv1.CSIDriverConfigSpec
		expected     []storagev1.TokenRequest
	}{
		{
			name: "driverType not SecretsStore preserves existing",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.AWSDriverType,
			},
			expected: existing,
		},
		{
			name: "driverType SecretsStore with zero-value secretsStore preserves existing",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
			},
			expected: existing,
		},
		{
			name: "tokenRequests zero value (omitted) preserves existing",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType:   opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{},
			},
			expected: existing,
		},
		{
			name: "type Unmanaged preserves existing",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					TokenRequests: opv1.SecretsStoreTokenRequests{
						Type: opv1.TokenRequestsUnmanaged,
					},
				},
			},
			expected: existing,
		},
		{
			name: "type Managed with nil audiences preserves existing",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					TokenRequests: opv1.SecretsStoreTokenRequests{
						Type: opv1.TokenRequestsManaged,
					},
				},
			},
			expected: existing,
		},
		{
			name: "type Managed with explicit empty audiences clears tokenRequests",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					TokenRequests: opv1.SecretsStoreTokenRequests{
						Type: opv1.TokenRequestsManaged,
						Managed: opv1.ManagedTokenRequests{
							Audiences: &[]opv1.SecretsStoreTokenRequest{},
						},
					},
				},
			},
			expected: []storagev1.TokenRequest{},
		},
		{
			name: "type Managed with audiences replaces existing entirely",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					TokenRequests: opv1.SecretsStoreTokenRequests{
						Type: opv1.TokenRequestsManaged,
						Managed: opv1.ManagedTokenRequests{
							Audiences: &[]opv1.SecretsStoreTokenRequest{
								{Audience: &awsAudience, ExpirationSeconds: 3600},
							},
						},
					},
				},
			},
			expected: []storagev1.TokenRequest{
				{Audience: awsAudience, ExpirationSeconds: ptr.To(int64(3600))},
			},
		},
		{
			name: "type Managed with multiple audiences propagates all",
			driverConfig: opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					TokenRequests: opv1.SecretsStoreTokenRequests{
						Type: opv1.TokenRequestsManaged,
						Managed: opv1.ManagedTokenRequests{
							Audiences: &[]opv1.SecretsStoreTokenRequest{
								{Audience: &awsAudience, ExpirationSeconds: 3600},
								{Audience: &azureAudience},
							},
						},
					},
				},
			},
			expected: []storagev1.TokenRequest{
				{Audience: awsAudience, ExpirationSeconds: ptr.To(int64(3600))},
				{Audience: azureAudience},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := getEffectiveTokenRequests(tc.driverConfig, existing)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("expected %+v, got %+v", tc.expected, got)
			}
		})
	}
}
