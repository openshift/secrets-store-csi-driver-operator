package operator

import (
	"reflect"
	"testing"

	"github.com/openshift/library-go/pkg/crypto"
)

func TestGetObservedTLSServingConfig(t *testing.T) {
	defaultMinVersion := crypto.TLSVersionToNameOrDie(crypto.DefaultTLSVersion())
	defaultCipherSuites := crypto.CipherSuitesToNamesOrDie(crypto.DefaultCiphers())

	cases := []struct {
		name             string
		observedConfig   []byte
		wantMinVersion   string
		wantCipherSuites []string
	}{
		{
			name:             "nil observed config returns safe defaults",
			observedConfig:   nil,
			wantMinVersion:   defaultMinVersion,
			wantCipherSuites: defaultCipherSuites,
		},
		{
			name:             "empty observed config returns safe defaults",
			observedConfig:   []byte(""),
			wantMinVersion:   defaultMinVersion,
			wantCipherSuites: defaultCipherSuites,
		},
		{
			name:             "empty JSON object returns safe defaults",
			observedConfig:   []byte(`{}`),
			wantMinVersion:   defaultMinVersion,
			wantCipherSuites: defaultCipherSuites,
		},
		{
			name: "valid observed config with a stricter profile is honored",
			observedConfig: []byte(`{
				"targetcsiconfig": {
					"servingInfo": {
						"minTLSVersion": "VersionTLS13",
						"cipherSuites": ["TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384"]
					}
				}
			}`),
			wantMinVersion:   "VersionTLS13",
			wantCipherSuites: []string{"TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384"},
		},
		{
			name: "only minTLSVersion observed falls back to default cipher suites",
			observedConfig: []byte(`{
				"targetcsiconfig": {
					"servingInfo": {
						"minTLSVersion": "VersionTLS13"
					}
				}
			}`),
			wantMinVersion:   "VersionTLS13",
			wantCipherSuites: defaultCipherSuites,
		},
		{
			name: "only cipherSuites observed falls back to default minTLSVersion",
			observedConfig: []byte(`{
				"targetcsiconfig": {
					"servingInfo": {
						"cipherSuites": ["TLS_AES_128_GCM_SHA256"]
					}
				}
			}`),
			wantMinVersion:   defaultMinVersion,
			wantCipherSuites: []string{"TLS_AES_128_GCM_SHA256"},
		},
		{
			name: "empty string minTLSVersion is treated as not observed",
			observedConfig: []byte(`{
				"targetcsiconfig": {
					"servingInfo": {
						"minTLSVersion": "",
						"cipherSuites": ["TLS_AES_128_GCM_SHA256"]
					}
				}
			}`),
			wantMinVersion:   defaultMinVersion,
			wantCipherSuites: []string{"TLS_AES_128_GCM_SHA256"},
		},
		{
			name: "empty cipherSuites list is treated as not observed",
			observedConfig: []byte(`{
				"targetcsiconfig": {
					"servingInfo": {
						"minTLSVersion": "VersionTLS13",
						"cipherSuites": []
					}
				}
			}`),
			wantMinVersion:   "VersionTLS13",
			wantCipherSuites: defaultCipherSuites,
		},
		{
			name:             "malformed (non-JSON) observed config returns safe defaults",
			observedConfig:   []byte(`{not-valid-json`),
			wantMinVersion:   defaultMinVersion,
			wantCipherSuites: defaultCipherSuites,
		},
		{
			name: "malformed cipherSuites type returns safe defaults for that field only",
			observedConfig: []byte(`{
				"targetcsiconfig": {
					"servingInfo": {
						"minTLSVersion": "VersionTLS13",
						"cipherSuites": "not-an-array"
					}
				}
			}`),
			wantMinVersion:   "VersionTLS13",
			wantCipherSuites: defaultCipherSuites,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotMinVersion, gotCipherSuites := getObservedTLSServingConfig(tc.observedConfig)

			if gotMinVersion != tc.wantMinVersion {
				t.Errorf("getObservedTLSServingConfig() minTLSVersion = %q, want %q", gotMinVersion, tc.wantMinVersion)
			}
			if !reflect.DeepEqual(gotCipherSuites, tc.wantCipherSuites) {
				t.Errorf("getObservedTLSServingConfig() cipherSuites = %v, want %v", gotCipherSuites, tc.wantCipherSuites)
			}
			if len(gotMinVersion) == 0 {
				t.Errorf("getObservedTLSServingConfig() must never return an empty minTLSVersion")
			}
			if len(gotCipherSuites) == 0 {
				t.Errorf("getObservedTLSServingConfig() must never return an empty cipherSuites list")
			}
		})
	}
}
