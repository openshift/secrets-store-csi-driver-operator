package tls

import (
	"sync/atomic"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

func TestSecurityProfileWatcherHandle(t *testing.T) {
	intermediate := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	modern := *configv1.TLSProfiles[configv1.TLSProfileModernType]

	t.Run("no change does not fire OnChange", func(t *testing.T) {
		var called atomic.Bool
		w := &SecurityProfileWatcher{
			InitialTLSProfileSpec:     intermediate,
			InitialTLSAdherencePolicy: configv1.TLSAdherencePolicyNoOpinion,
			OnChange:                  func() { called.Store(true) },
		}
		w.handle(&configv1.APIServer{})
		if called.Load() {
			t.Fatal("OnChange should not be called when nothing changed")
		}
	})

	t.Run("profile change fires OnChange", func(t *testing.T) {
		var called atomic.Bool
		w := &SecurityProfileWatcher{
			InitialTLSProfileSpec:     intermediate,
			InitialTLSAdherencePolicy: configv1.TLSAdherencePolicyStrictAllComponents,
			OnChange:                  func() { called.Store(true) },
		}
		w.handle(&configv1.APIServer{
			Spec: configv1.APIServerSpec{
				TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
				TLSSecurityProfile: &configv1.TLSSecurityProfile{
					Type: configv1.TLSProfileModernType,
				},
			},
		})
		if !called.Load() {
			t.Fatal("OnChange should be called when TLS profile changes")
		}
		if !reflectDeepEqualProfile(w.InitialTLSProfileSpec, modern) {
			t.Fatalf("watcher should update seeded profile, got %#v", w.InitialTLSProfileSpec)
		}
	})

	t.Run("adherence change fires OnChange", func(t *testing.T) {
		var called atomic.Bool
		w := &SecurityProfileWatcher{
			InitialTLSProfileSpec:     intermediate,
			InitialTLSAdherencePolicy: configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			OnChange:                  func() { called.Store(true) },
		}
		w.handle(&configv1.APIServer{
			Spec: configv1.APIServerSpec{
				TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
				TLSSecurityProfile: &configv1.TLSSecurityProfile{
					Type: configv1.TLSProfileIntermediateType,
				},
			},
		})
		if !called.Load() {
			t.Fatal("OnChange should be called when tlsAdherence changes")
		}
		if w.InitialTLSAdherencePolicy != configv1.TLSAdherencePolicyStrictAllComponents {
			t.Fatalf("watcher should update seeded adherence, got %q", w.InitialTLSAdherencePolicy)
		}
	})
}

func reflectDeepEqualProfile(a, b configv1.TLSProfileSpec) bool {
	if a.MinTLSVersion != b.MinTLSVersion {
		return false
	}
	if len(a.Ciphers) != len(b.Ciphers) {
		return false
	}
	for i := range a.Ciphers {
		if a.Ciphers[i] != b.Ciphers[i] {
			return false
		}
	}
	return true
}
