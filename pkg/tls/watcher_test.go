package tls

import (
	"context"
	"sync/atomic"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

func TestSecurityProfileWatcherHandle(t *testing.T) {
	intermediate := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]

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
	})

	t.Run("unresolvable live config fires OnChange", func(t *testing.T) {
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
					Type: configv1.TLSProfileCustomType,
					// Custom field intentionally nil → GetTLSProfileSpec error
				},
			},
		})
		if !called.Load() {
			t.Fatal("OnChange should be called when live TLS config is unresolvable")
		}
	})

	t.Run("OnChange fires at most once", func(t *testing.T) {
		var fireCount atomic.Int32
		w := &SecurityProfileWatcher{
			InitialTLSProfileSpec:     intermediate,
			InitialTLSAdherencePolicy: configv1.TLSAdherencePolicyStrictAllComponents,
			OnChange:                  func() { fireCount.Add(1) },
		}
		modernAPI := &configv1.APIServer{
			Spec: configv1.APIServerSpec{
				TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
				TLSSecurityProfile: &configv1.TLSSecurityProfile{
					Type: configv1.TLSProfileModernType,
				},
			},
		}
		oldAPI := &configv1.APIServer{
			Spec: configv1.APIServerSpec{
				TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
				TLSSecurityProfile: &configv1.TLSSecurityProfile{
					Type: configv1.TLSProfileOldType,
				},
			},
		}
		w.handle(modernAPI)
		w.handle(oldAPI)
		w.handle(&configv1.APIServer{
			Spec: configv1.APIServerSpec{
				TLSAdherence:       configv1.TLSAdherencePolicyStrictAllComponents,
				TLSSecurityProfile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileCustomType},
			},
		})
		if got := fireCount.Load(); got != 1 {
			t.Fatalf("OnChange fired %d times, want exactly 1", got)
		}
	})
}

func TestResolveFromClusterCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ResolveFromCluster(ctx, "/nonexistent/kubeconfig", "test-agent")
	if err == nil {
		t.Fatal("expected error with canceled context / bad kubeconfig")
	}
}
