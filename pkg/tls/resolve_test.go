package tls

import (
	"os"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	sigsyaml "sigs.k8s.io/yaml"
)

func TestWriteConfigFile(t *testing.T) {
	intermediate := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]

	t.Run("not honoring writes no file", func(t *testing.T) {
		path, err := WriteConfigFile(ResolvedProfile{Honor: false, Spec: intermediate})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if path != "" {
			t.Errorf("path = %q, want empty", path)
			_ = os.Remove(path)
		}
	})

	t.Run("honoring writes a config file carrying the resolved TLS settings", func(t *testing.T) {
		resolved := ResolvedProfile{Honor: true, Spec: intermediate}
		path, err := WriteConfigFile(resolved)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer os.Remove(path)

		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %q: %v", path, err)
		}
		var config operatorv1alpha1.GenericOperatorConfig
		if err := sigsyaml.Unmarshal(content, &config); err != nil {
			t.Fatalf("failed to unmarshal written config: %v", err)
		}
		if config.ServingInfo.MinTLSVersion != string(resolved.Spec.MinTLSVersion) {
			t.Errorf("MinTLSVersion = %q, want %q", config.ServingInfo.MinTLSVersion, resolved.Spec.MinTLSVersion)
		}
		if len(config.ServingInfo.CipherSuites) == 0 {
			t.Errorf("expected non-empty CipherSuites")
		}
	})
}
