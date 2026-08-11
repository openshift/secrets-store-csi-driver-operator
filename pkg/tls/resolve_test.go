package tls

import (
	"os"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	sigsyaml "sigs.k8s.io/yaml"
)

func cleanupTempFile(t *testing.T, path string) {
	t.Helper()
	t.Cleanup(func() {
		if err := os.Remove(path); err != nil {
			t.Errorf("failed to remove temp file %q: %v", path, err)
		}
	})
}

func TestWriteConfigFile(t *testing.T) {
	intermediate := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]

	tests := []struct {
		name            string
		resolved        ResolvedProfile
		wantErr         bool
		wantEmptyPath   bool
		wantMinTLS      string
		wantCipherSuites bool
	}{
		{
			name:          "not honoring writes no file",
			resolved:      ResolvedProfile{Honor: false, Spec: intermediate},
			wantEmptyPath: true,
		},
		{
			name:             "honoring writes a config file carrying the resolved TLS settings",
			resolved:         ResolvedProfile{Honor: true, Spec: intermediate},
			wantMinTLS:       string(intermediate.MinTLSVersion),
			wantCipherSuites: true,
		},
		{
			name: "honoring with unsupported ciphers fails without writing a file",
			resolved: ResolvedProfile{
				Honor: true,
				Spec: configv1.TLSProfileSpec{
					MinTLSVersion: configv1.VersionTLS12,
					Ciphers:       []string{"NOT-A-REAL-OPENSSL-CIPHER"},
				},
			},
			wantErr:       true,
			wantEmptyPath: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := WriteConfigFile(tt.resolved)
			if (err != nil) != tt.wantErr {
				t.Fatalf("WriteConfigFile() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantEmptyPath {
				if path != "" {
					t.Errorf("path = %q, want empty", path)
					cleanupTempFile(t, path)
				}
				return
			}
			if path == "" {
				t.Fatal("path is empty, want written config file")
			}
			cleanupTempFile(t, path)

			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read %q: %v", path, err)
			}
			var config operatorv1alpha1.GenericOperatorConfig
			if err := sigsyaml.Unmarshal(content, &config); err != nil {
				t.Fatalf("failed to unmarshal written config: %v", err)
			}
			if config.ServingInfo.MinTLSVersion != tt.wantMinTLS {
				t.Errorf("MinTLSVersion = %q, want %q", config.ServingInfo.MinTLSVersion, tt.wantMinTLS)
			}
			if tt.wantCipherSuites && len(config.ServingInfo.CipherSuites) == 0 {
				t.Errorf("expected non-empty CipherSuites")
			}
		})
	}
}
