package operator

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	opv1 "github.com/openshift/api/operator/v1"
	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	"github.com/openshift/library-go/pkg/crypto"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const testServingInfoConfigMapYAML = `apiVersion: v1
kind: ConfigMap
metadata:
  name: secrets-store-csi-driver-operator-serving-info
  namespace: test-namespace
data:
  config.yaml: ""
`

func servingInfoBaseAssetFunc(name string) ([]byte, error) {
	if name != servingInfoConfigMapAssetName {
		return nil, errors.New("unexpected asset name in test: " + name)
	}
	return []byte(testServingInfoConfigMapYAML), nil
}

func clusterCSIDriverWithObservedConfig(t *testing.T, raw []byte) *opv1.ClusterCSIDriver {
	t.Helper()
	return &opv1.ClusterCSIDriver{
		ObjectMeta: metav1.ObjectMeta{Name: providerName},
		Spec: opv1.ClusterCSIDriverSpec{
			OperatorSpec: opv1.OperatorSpec{
				ObservedConfig: runtime.RawExtension{Raw: raw},
			},
		},
	}
}

func decodeGeneratedOperatorConfig(t *testing.T, data string) operatorv1alpha1.GenericOperatorConfig {
	t.Helper()
	cfg := operatorv1alpha1.GenericOperatorConfig{}
	if err := json.Unmarshal([]byte(data), &cfg); err != nil {
		t.Fatalf("failed to unmarshal generated config.yaml content: %v", err)
	}
	return cfg
}

func TestWithServingInfoConfigMapAsset(t *testing.T) {
	defaultMinVersion := crypto.TLSVersionToNameOrDie(crypto.DefaultTLSVersion())
	defaultCipherSuites := crypto.CipherSuitesToNamesOrDie(crypto.DefaultCiphers())

	observedConfigJSON := []byte(`{
		"targetcsiconfig": {
			"servingInfo": {
				"minTLSVersion": "VersionTLS13",
				"cipherSuites": ["TLS_AES_128_GCM_SHA256"]
			}
		}
	}`)

	cases := []struct {
		name string

		requestedAssetName string
		base               func(string) ([]byte, error)

		clusterCSIDriver    *opv1.ClusterCSIDriver
		clusterCSIDriverErr error

		wantErrContains  string
		wantPassthrough  string
		wantMinVersion   string
		wantCipherSuites []string
	}{
		{
			name:               "pass-through for other asset names",
			requestedAssetName: "csidriver.yaml",
			base: func(name string) ([]byte, error) {
				return []byte("unrelated content for " + name), nil
			},
			wantPassthrough: "unrelated content for csidriver.yaml",
		},
		{
			name:             "no ClusterCSIDriver yet: safe FIPS-approved defaults, not an error",
			wantMinVersion:   defaultMinVersion,
			wantCipherSuites: defaultCipherSuites,
		},
		{
			name:             "ClusterCSIDriver exists but has not observed anything yet: safe defaults",
			clusterCSIDriver: clusterCSIDriverWithObservedConfig(t, nil),
			wantMinVersion:   defaultMinVersion,
			wantCipherSuites: defaultCipherSuites,
		},
		{
			name:             "valid observed config is rendered into config.yaml",
			clusterCSIDriver: clusterCSIDriverWithObservedConfig(t, observedConfigJSON),
			wantMinVersion:   "VersionTLS13",
			wantCipherSuites: []string{"TLS_AES_128_GCM_SHA256"},
		},
		{
			name:                "ClusterCSIDriver lister error surfaces as Degraded-worthy error",
			clusterCSIDriverErr: errors.New("myerror"),
			wantErrContains:     "myerror",
		},
		{
			name:             "malformed (non-JSON) observed config surfaces as an error, not a silent default",
			clusterCSIDriver: clusterCSIDriverWithObservedConfig(t, []byte(`{not-valid-json`)),
			wantErrContains:  "not valid JSON",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := tc.base
			if base == nil {
				base = servingInfoBaseAssetFunc
			}
			requestedAssetName := tc.requestedAssetName
			if requestedAssetName == "" {
				requestedAssetName = servingInfoConfigMapAssetName
			}

			clusterCSIDriverLister := &fakeClusterCSIDriverLister{driver: tc.clusterCSIDriver, err: tc.clusterCSIDriverErr}

			wrapped := withServingInfoConfigMapAsset(base, clusterCSIDriverLister, providerName)
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

			if tc.wantPassthrough != "" {
				if string(got) != tc.wantPassthrough {
					t.Errorf("expected pass-through content %q, got %q", tc.wantPassthrough, string(got))
				}
				return
			}

			configMap := resourceread.ReadConfigMapV1OrDie(got)
			cfg := decodeGeneratedOperatorConfig(t, configMap.Data[servingInfoConfigMapDataKey])

			if cfg.ServingInfo.MinTLSVersion != tc.wantMinVersion {
				t.Errorf("expected minTLSVersion %q, got %q", tc.wantMinVersion, cfg.ServingInfo.MinTLSVersion)
			}
			if !stringSlicesEqual(cfg.ServingInfo.CipherSuites, tc.wantCipherSuites) {
				t.Errorf("expected cipherSuites %v, got %v", tc.wantCipherSuites, cfg.ServingInfo.CipherSuites)
			}
			// Namespace substitution and object identity from the base asset must survive rendering.
			if configMap.Namespace != "test-namespace" {
				t.Errorf("expected namespace %q to be preserved from base manifest, got %q", "test-namespace", configMap.Namespace)
			}
			if configMap.Name != "secrets-store-csi-driver-operator-serving-info" {
				t.Errorf("unexpected ConfigMap name %q", configMap.Name)
			}
		})
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
