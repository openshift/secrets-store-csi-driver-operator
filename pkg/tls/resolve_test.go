package tls

import (
	"os"
	"strings"
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
	customGroups := configv1.TLSProfileSpec{
		Ciphers:       intermediate.Ciphers,
		MinTLSVersion: intermediate.MinTLSVersion,
		Groups:        []configv1.TLSGroup{configv1.TLSGroupSecP256r1, "NOT-A-GROUP"},
	}
	unknownGroups := configv1.TLSProfileSpec{
		Ciphers:       intermediate.Ciphers,
		MinTLSVersion: intermediate.MinTLSVersion,
		Groups:        []configv1.TLSGroup{"NOT-A-GROUP"},
	}

	tests := []struct {
		name             string
		resolved         ResolvedProfile
		wantErr          bool
		wantEmptyPath    bool
		wantMinTLS       string
		wantCipherSuites bool
		wantCurves       []int32
	}{
		{
			name:          "not honoring writes no file",
			resolved:      ResolvedProfile{Honor: false, Spec: intermediate, UseGroups: true},
			wantEmptyPath: true,
		},
		{
			name:             "gate off writes min version and ciphers without curvePreferences",
			resolved:         ResolvedProfile{Honor: true, Spec: intermediate},
			wantMinTLS:       string(intermediate.MinTLSVersion),
			wantCipherSuites: true,
		},
		{
			name: "gate on writes curvePreferences from the profile groups",
			resolved: ResolvedProfile{
				Honor:     true,
				Spec:      customGroups,
				UseGroups: true,
			},
			wantMinTLS:       string(customGroups.MinTLSVersion),
			wantCipherSuites: true,
			wantCurves:       expectedCurvePreferences(customGroups),
		},
		{
			name: "fully filtered groups omit the curvePreferences key",
			resolved: ResolvedProfile{
				Honor:     true,
				Spec:      unknownGroups,
				UseGroups: true,
			},
			wantMinTLS:       string(unknownGroups.MinTLSVersion),
			wantCipherSuites: true,
			wantCurves:       expectedCurvePreferences(unknownGroups),
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
			if !equalCurvePreferences(config.ServingInfo.CurvePreferences, tt.wantCurves) {
				t.Errorf("CurvePreferences = %#v, want %#v", config.ServingInfo.CurvePreferences, tt.wantCurves)
			}
			hasCurveKey := strings.Contains(string(content), "curvePreferences")
			if len(tt.wantCurves) == 0 && hasCurveKey {
				t.Errorf("config file contains curvePreferences, want the key omitted:\n%s", content)
			}
			if len(tt.wantCurves) > 0 && !hasCurveKey {
				t.Errorf("config file missing curvePreferences:\n%s", content)
			}
		})
	}
}

func equalCurvePreferences(got, want []int32) bool {
	if len(got) == 0 && len(want) == 0 {
		return true
	}
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
