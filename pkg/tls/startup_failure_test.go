package tls

import (
	"errors"
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

func TestStartupBlockedOnUnresolvableTLSProfile(t *testing.T) {
	apiServer := &configv1.APIServer{
		Spec: configv1.APIServerSpec{
			TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
			TLSSecurityProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
			},
		},
	}

	_, err := ResolveFromAPIServer(apiServer)
	if err == nil {
		t.Fatal("expected error for unresolvable custom TLS profile")
	}
	if !strings.Contains(err.Error(), "custom TLS profile") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartupBlockedOnUnsupportedCipherSuite(t *testing.T) {
	resolved := ResolvedProfile{
		Adherence: configv1.TLSAdherencePolicyStrictAllComponents,
		Honor:     true,
		Spec: configv1.TLSProfileSpec{
			MinTLSVersion: configv1.VersionTLS12,
			Ciphers:       []string{"NOT-A-REAL-OPENSSL-CIPHER"},
		},
	}

	_, err := WriteConfigFile(resolved)
	if err == nil {
		t.Fatal("expected error when all cluster ciphers are unsupported by Go crypto/tls")
	}
	if !strings.Contains(err.Error(), "unsupported by Go") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartupBlockedErrorMessage(t *testing.T) {
	err := NewStartupBlockedError(errors.New("apiserver unreachable"))
	msg := err.Error()
	if !strings.Contains(msg, "cluster TLS security profile") {
		t.Fatalf("error message should mention cluster TLS profile, got: %q", msg)
	}
}
