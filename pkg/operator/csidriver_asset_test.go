package operator

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	opv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
)

// fakeCSIDriverLister is a minimal fake for storagev1listers.CSIDriverLister.
type fakeCSIDriverLister struct {
	driver *storagev1.CSIDriver
	err    error
}

func (f *fakeCSIDriverLister) List(selector labels.Selector) ([]*storagev1.CSIDriver, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.driver == nil {
		return nil, nil
	}
	return []*storagev1.CSIDriver{f.driver}, nil
}

func (f *fakeCSIDriverLister) Get(name string) (*storagev1.CSIDriver, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.driver == nil {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "csidrivers"}, name)
	}
	return f.driver, nil
}

const testCSIDriverYAML = `apiVersion: storage.k8s.io/v1
kind: CSIDriver
metadata:
  name: secrets-store.csi.k8s.io
spec:
  podInfoOnMount: true
  attachRequired: false
  fsGroupPolicy: File
  volumeLifecycleModes:
  - Ephemeral
`

func baseAssetFunc(name string) ([]byte, error) {
	if name != csidriverAssetName {
		return nil, errors.New("unexpected asset name in test: " + name)
	}
	return []byte(testCSIDriverYAML), nil
}

func decodeTestCSIDriver(t *testing.T, b []byte) *storagev1.CSIDriver {
	t.Helper()
	return resourceread.ReadCSIDriverV1OrDie(b)
}

// secretsStoreDriverConfig builds a ClusterCSIDriver with driverType SecretsStore
// and the given secretsStore config
func secretsStoreDriverConfig(secretsStore opv1.SecretsStoreCSIDriverConfigSpec) *opv1.ClusterCSIDriver {
	return &opv1.ClusterCSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: providerName},
		Spec: opv1.ClusterCSIDriverSpec{
			DriverConfig: opv1.CSIDriverConfigSpec{
				DriverType:   opv1.SecretsStoreDriverType,
				SecretsStore: secretsStore,
			},
		},
	}
}

func TestWithSecretsStoreCSIDriverAsset(t *testing.T) {
	awsAudience := "sts.amazonaws.com"
	azureAudience := "api://AzureADTokenExchange"

	cases := []struct {
		name string

		// Dispatch overrides; unset means exercise the CSIDriver rendering
		// path via baseAssetFunc/csidriverAssetName (every case but the
		// pass-through one below).
		requestedAssetName string
		base               func(string) ([]byte, error)

		clusterCSIDriver    *opv1.ClusterCSIDriver
		clusterCSIDriverErr error

		existingTokenRequests []storagev1.TokenRequest
		existingDriverErr     error

		wantErrContains        string
		wantPassthroughContent string
		wantRequiresRepublish  bool
		wantTokenRequests      []storagev1.TokenRequest
	}{
		{
			name:               "pass-through for non-csidriver.yaml assets",
			requestedAssetName: "node_sa.yaml",
			base: func(name string) ([]byte, error) {
				return []byte("unrelated content for " + name), nil
			},
			wantPassthroughContent: "unrelated content for node_sa.yaml",
		},
		{
			name:                  "no ClusterCSIDriver yet: requiresRepublish defaults to true, no tokenRequests",
			wantRequiresRepublish: true,
		},
		{
			name:                  "omitted secretRotation defaults requiresRepublish to true",
			clusterCSIDriver:      secretsStoreDriverConfig(opv1.SecretsStoreCSIDriverConfigSpec{SecretRotation: opv1.SecretsStoreSecretRotation{Type: ""}}),
			wantRequiresRepublish: true,
		},
		{
			name:                  "secretRotation type Custom sets requiresRepublish to true",
			clusterCSIDriver:      secretsStoreDriverConfig(opv1.SecretsStoreCSIDriverConfigSpec{SecretRotation: opv1.SecretsStoreSecretRotation{Type: opv1.SecretRotationCustom}}),
			wantRequiresRepublish: true,
		},
		{
			name:                  "secretRotation type None sets requiresRepublish to false",
			clusterCSIDriver:      secretsStoreDriverConfig(opv1.SecretsStoreCSIDriverConfigSpec{SecretRotation: opv1.SecretsStoreSecretRotation{Type: opv1.SecretRotationNone}}),
			wantRequiresRepublish: false,
		},
		{
			name: "tokenRequests type Managed with audience+expiration propagates to CSIDriver",
			clusterCSIDriver: secretsStoreDriverConfig(opv1.SecretsStoreCSIDriverConfigSpec{
				TokenRequests: opv1.SecretsStoreTokenRequests{
					Type: opv1.TokenRequestsManaged,
					Managed: opv1.ManagedTokenRequests{
						Audiences: &[]opv1.SecretsStoreTokenRequest{{Audience: &awsAudience, ExpirationSeconds: 3600}},
					},
				},
			}),
			wantRequiresRepublish: true,
			wantTokenRequests:     []storagev1.TokenRequest{{Audience: awsAudience, ExpirationSeconds: ptr.To(int64(3600))}},
		},
		{
			name:                "ClusterCSIDriver lister error is wrapped",
			clusterCSIDriverErr: errors.New("myerror"),
			wantErrContains:     "myerror",
		},
		{
			name:                  "no driverConfig set (upgrade scenario) preserves existing tokenRequests",
			existingTokenRequests: []storagev1.TokenRequest{{Audience: azureAudience}},
			wantRequiresRepublish: true,
			wantTokenRequests:     []storagev1.TokenRequest{{Audience: azureAudience}},
		},
		{
			name: "tokenRequests type Unmanaged preserves existing tokenRequests",
			clusterCSIDriver: secretsStoreDriverConfig(opv1.SecretsStoreCSIDriverConfigSpec{
				TokenRequests: opv1.SecretsStoreTokenRequests{Type: opv1.TokenRequestsUnmanaged},
			}),
			existingTokenRequests: []storagev1.TokenRequest{{Audience: azureAudience}},
			wantRequiresRepublish: true,
			wantTokenRequests:     []storagev1.TokenRequest{{Audience: azureAudience}},
		},
		{
			name: "tokenRequests type Managed overrides existing (unmanaged) tokenRequests",
			clusterCSIDriver: secretsStoreDriverConfig(opv1.SecretsStoreCSIDriverConfigSpec{
				TokenRequests: opv1.SecretsStoreTokenRequests{
					Type: opv1.TokenRequestsManaged,
					Managed: opv1.ManagedTokenRequests{
						Audiences: &[]opv1.SecretsStoreTokenRequest{{Audience: &awsAudience}},
					},
				},
			}),
			existingTokenRequests: []storagev1.TokenRequest{{Audience: "old-unmanaged-audience"}},
			wantRequiresRepublish: true,
			wantTokenRequests:     []storagev1.TokenRequest{{Audience: awsAudience}},
		},
		{
			name: "tokenRequests type Managed with explicit empty audiences clears existing tokenRequests",
			clusterCSIDriver: secretsStoreDriverConfig(opv1.SecretsStoreCSIDriverConfigSpec{
				TokenRequests: opv1.SecretsStoreTokenRequests{
					Type: opv1.TokenRequestsManaged,
					Managed: opv1.ManagedTokenRequests{
						Audiences: &[]opv1.SecretsStoreTokenRequest{}, // explicit empty list
					},
				},
			}),
			existingTokenRequests: []storagev1.TokenRequest{{Audience: "pre-existing-audience"}},
			wantRequiresRepublish: true,
			wantTokenRequests:     nil, // cleared
		},
		{
			name:              "live CSIDriver lister error is wrapped",
			existingDriverErr: errors.New("live-error"),
			wantErrContains:   "live-error",
		},
		{
			name: "multiple audiences (AWS + Azure) are all propagated",
			clusterCSIDriver: secretsStoreDriverConfig(opv1.SecretsStoreCSIDriverConfigSpec{
				TokenRequests: opv1.SecretsStoreTokenRequests{
					Type: opv1.TokenRequestsManaged,
					Managed: opv1.ManagedTokenRequests{
						Audiences: &[]opv1.SecretsStoreTokenRequest{
							{Audience: &awsAudience, ExpirationSeconds: 3600},
							{Audience: &azureAudience},
						},
					},
				},
			}),
			wantRequiresRepublish: true,
			wantTokenRequests: []storagev1.TokenRequest{
				{Audience: awsAudience, ExpirationSeconds: ptr.To(int64(3600))},
				{Audience: azureAudience},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := tc.base
			if base == nil {
				base = baseAssetFunc
			}
			requestedAssetName := tc.requestedAssetName
			if requestedAssetName == "" {
				requestedAssetName = csidriverAssetName
			}

			clusterCSIDriverLister := &fakeClusterCSIDriverLister{driver: tc.clusterCSIDriver, err: tc.clusterCSIDriverErr}

			var existingDriver *storagev1.CSIDriver
			if tc.existingTokenRequests != nil {
				existingDriver = &storagev1.CSIDriver{Spec: storagev1.CSIDriverSpec{TokenRequests: tc.existingTokenRequests}}
			}
			csiDriverLister := &fakeCSIDriverLister{driver: existingDriver, err: tc.existingDriverErr}

			wrapped := withSecretsStoreCSIDriverAsset(base, clusterCSIDriverLister, csiDriverLister, providerName)
			got, err := wrapped(requestedAssetName)

			if tc.wantErrContains != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErrContains, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.wantPassthroughContent != "" {
				if string(got) != tc.wantPassthroughContent {
					t.Errorf("expected pass-through content %q, got %q", tc.wantPassthroughContent, string(got))
				}
				return
			}

			driver := decodeTestCSIDriver(t, got)
			if driver.Spec.RequiresRepublish == nil || *driver.Spec.RequiresRepublish != tc.wantRequiresRepublish {
				t.Errorf("expected requiresRepublish=%v, got %v", tc.wantRequiresRepublish, driver.Spec.RequiresRepublish)
			}
			// A JSON round-trip (marshal with omitempty, then decode) collapses
			// an explicit empty tokenRequests slice to nil, so cases that expect
			// tokenRequests to be cleared must set wantTokenRequests to nil, not
			// []storagev1.TokenRequest{} -- reflect.DeepEqual treats those as
			// unequal, per the "clears existing tokenRequests" case above.
			if !reflect.DeepEqual(driver.Spec.TokenRequests, tc.wantTokenRequests) {
				t.Errorf("expected tokenRequests %+v, got %+v", tc.wantTokenRequests, driver.Spec.TokenRequests)
			}
		})
	}
}
