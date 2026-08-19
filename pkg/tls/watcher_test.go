package tls

import (
	"context"
	"sync/atomic"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

func TestSecurityProfileWatcherHandle(t *testing.T) {
	intermediate := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]

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
	strictIntermediateAPI := &configv1.APIServer{
		Spec: configv1.APIServerSpec{
			TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
			TLSSecurityProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileIntermediateType,
			},
		},
	}
	unresolvableCustomAPI := &configv1.APIServer{
		Spec: configv1.APIServerSpec{
			TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
			TLSSecurityProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				// Custom field intentionally nil → GetTLSProfileSpec error
			},
		},
	}

	tests := []struct {
		name              string
		initialSpec       configv1.TLSProfileSpec
		initialAdherence  configv1.TLSAdherencePolicy
		handles           []*configv1.APIServer
		wantOnChangeCount int32
	}{
		{
			name:              "no change does not fire OnChange",
			initialSpec:       intermediate,
			initialAdherence:  configv1.TLSAdherencePolicyNoOpinion,
			handles:           []*configv1.APIServer{{}},
			wantOnChangeCount: 0,
		},
		{
			name:              "profile change fires OnChange",
			initialSpec:       intermediate,
			initialAdherence:  configv1.TLSAdherencePolicyStrictAllComponents,
			handles:           []*configv1.APIServer{modernAPI},
			wantOnChangeCount: 1,
		},
		{
			name:              "adherence change fires OnChange",
			initialSpec:       intermediate,
			initialAdherence:  configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			handles:           []*configv1.APIServer{strictIntermediateAPI},
			wantOnChangeCount: 1,
		},
		{
			name:              "unresolvable live config fires OnChange",
			initialSpec:       intermediate,
			initialAdherence:  configv1.TLSAdherencePolicyStrictAllComponents,
			handles:           []*configv1.APIServer{unresolvableCustomAPI},
			wantOnChangeCount: 1,
		},
		{
			name:             "OnChange fires at most once",
			initialSpec:      intermediate,
			initialAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
			handles: []*configv1.APIServer{
				modernAPI,
				oldAPI,
				unresolvableCustomAPI,
			},
			wantOnChangeCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fireCount atomic.Int32
			w := &SecurityProfileWatcher{
				InitialTLSProfileSpec:     tt.initialSpec,
				InitialTLSAdherencePolicy: tt.initialAdherence,
				OnChange:                  func() { fireCount.Add(1) },
			}
			for _, api := range tt.handles {
				w.handle(api)
			}
			if got := fireCount.Load(); got != tt.wantOnChangeCount {
				t.Fatalf("OnChange fired %d times, want %d", got, tt.wantOnChangeCount)
			}
		})
	}
}

func TestResolveFromClusterCanceledContext(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() context.Context
		wantErr bool
	}{
		{
			name: "canceled context with bad kubeconfig errors",
			setup: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveFromCluster(tt.setup(), "/nonexistent/kubeconfig", "test-agent")
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveFromCluster() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
