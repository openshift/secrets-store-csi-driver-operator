package tls

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/api/features"
	"github.com/openshift/library-go/pkg/operator/configobserver/featuregates"
)

func TestPayloadVersion(t *testing.T) {
	tests := []struct {
		name           string
		clusterVersion *configv1.ClusterVersion
		want           string
	}{
		{
			name: "desired version is used",
			clusterVersion: &configv1.ClusterVersion{
				Status: configv1.ClusterVersionStatus{
					Desired: configv1.Release{Version: "4.21.0"},
					History: []configv1.UpdateHistory{{Version: "4.20.0"}},
				},
			},
			want: "4.21.0",
		},
		{
			name: "empty desired version falls back to the newest history entry",
			clusterVersion: &configv1.ClusterVersion{
				Status: configv1.ClusterVersionStatus{
					History: []configv1.UpdateHistory{{Version: "4.20.0"}, {Version: "4.19.0"}},
				},
			},
			want: "4.20.0",
		},
		{
			name:           "empty status stays empty",
			clusterVersion: &configv1.ClusterVersion{},
			want:           "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := payloadVersion(tt.clusterVersion); got != tt.want {
				t.Fatalf("payloadVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTLSGroupPreferencesFlipped(t *testing.T) {
	gate := features.FeatureGateTLSGroupPreferences
	other := configv1.FeatureGateName("SomeOtherGate")

	tests := []struct {
		name   string
		change featuregates.FeatureChange
		want   bool
	}{
		{
			name: "initial callback is ignored",
			change: featuregates.FeatureChange{
				New: featuregates.Features{Enabled: []configv1.FeatureGateName{gate}},
			},
		},
		{
			name: "disabled to enabled restarts",
			change: featuregates.FeatureChange{
				Previous: &featuregates.Features{Disabled: []configv1.FeatureGateName{gate}},
				New:      featuregates.Features{Enabled: []configv1.FeatureGateName{gate}},
			},
			want: true,
		},
		{
			name: "enabled to disabled restarts",
			change: featuregates.FeatureChange{
				Previous: &featuregates.Features{Enabled: []configv1.FeatureGateName{gate}},
				New:      featuregates.Features{Disabled: []configv1.FeatureGateName{gate}},
			},
			want: true,
		},
		{
			name: "staying enabled does not restart",
			change: featuregates.FeatureChange{
				Previous: &featuregates.Features{Enabled: []configv1.FeatureGateName{gate}},
				New:      featuregates.Features{Enabled: []configv1.FeatureGateName{gate, other}},
			},
		},
		{
			name: "another gate flipping does not restart",
			change: featuregates.FeatureChange{
				Previous: &featuregates.Features{
					Enabled:  []configv1.FeatureGateName{gate, other},
					Disabled: []configv1.FeatureGateName{},
				},
				New: featuregates.Features{
					Enabled:  []configv1.FeatureGateName{gate},
					Disabled: []configv1.FeatureGateName{other},
				},
			},
		},
		{
			name: "disabled to unlisted does not restart",
			change: featuregates.FeatureChange{
				Previous: &featuregates.Features{Disabled: []configv1.FeatureGateName{gate}},
				New:      featuregates.Features{},
			},
		},
		{
			name: "unlisted to enabled restarts",
			change: featuregates.FeatureChange{
				Previous: &featuregates.Features{},
				New:      featuregates.Features{Enabled: []configv1.FeatureGateName{gate}},
			},
			want: true,
		},
		{
			name: "enabled to unlisted restarts",
			change: featuregates.FeatureChange{
				Previous: &featuregates.Features{Enabled: []configv1.FeatureGateName{gate}},
				New:      featuregates.Features{},
			},
			want: true,
		},
		{
			name: "unlisted to disabled does not restart",
			change: featuregates.FeatureChange{
				Previous: &featuregates.Features{},
				New:      featuregates.Features{Disabled: []configv1.FeatureGateName{gate}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TLSGroupPreferencesFlipped(tt.change); got != tt.want {
				t.Fatalf("TLSGroupPreferencesFlipped() = %v, want %v", got, tt.want)
			}
		})
	}
}
