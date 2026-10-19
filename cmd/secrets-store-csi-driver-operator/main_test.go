package main

import (
	"os"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	sscsitls "github.com/openshift/secrets-store-csi-driver-operator/pkg/tls"
	"github.com/spf13/cobra"
)

func TestApplyTLSProfileToConfigFlag(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "operator config file")

	t.Run("Legacy adherence leaves config unset", func(t *testing.T) {
		legacy := sscsitls.ResolvedProfile{
			Adherence: configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			Honor:     false,
		}
		if err := applyTLSProfileToConfigFlag(cmd, legacy); err != nil {
			t.Fatalf("applyTLSProfileToConfigFlag() error = %v", err)
		}
		config, err := cmd.Flags().GetString("config")
		if err != nil {
			t.Fatalf("GetString(config): %v", err)
		}
		if config != "" {
			t.Fatalf("config = %q, want empty under Legacy adherence", config)
		}
	})

	t.Run("Strict adherence sets config to temp file", func(t *testing.T) {
		if err := cmd.Flags().Set("config", ""); err != nil {
			t.Fatalf("reset config flag: %v", err)
		}
		strict := sscsitls.ResolvedProfile{
			Adherence: configv1.TLSAdherencePolicyStrictAllComponents,
			Honor:     true,
			Spec:      *configv1.TLSProfiles[configv1.TLSProfileIntermediateType],
		}
		if err := applyTLSProfileToConfigFlag(cmd, strict); err != nil {
			t.Fatalf("applyTLSProfileToConfigFlag() error = %v", err)
		}
		config, err := cmd.Flags().GetString("config")
		if err != nil {
			t.Fatalf("GetString(config): %v", err)
		}
		if config == "" {
			t.Fatal("config is empty, want path to generated operator config")
		}
		t.Cleanup(func() {
			if err := os.Remove(config); err != nil && !os.IsNotExist(err) {
				t.Errorf("failed to remove temp config %q: %v", config, err)
			}
		})
	})
}
