package tls

import (
	"reflect"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"
)

func TestGetTLSProfileSpec(t *testing.T) {
	intermediate := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	modern := *configv1.TLSProfiles[configv1.TLSProfileModernType]
	old := *configv1.TLSProfiles[configv1.TLSProfileOldType]

	customSpec := configv1.TLSProfileSpec{
		Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
		MinTLSVersion: configv1.VersionTLS12,
	}

	tests := []struct {
		name    string
		profile *configv1.TLSSecurityProfile
		want    configv1.TLSProfileSpec
		wantErr bool
	}{
		{
			name:    "nil profile returns Intermediate",
			profile: nil,
			want:    intermediate,
		},
		{
			name:    "empty type returns Intermediate",
			profile: &configv1.TLSSecurityProfile{},
			want:    intermediate,
		},
		{
			name:    "Intermediate",
			profile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType},
			want:    intermediate,
		},
		{
			name:    "Modern",
			profile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType},
			want:    modern,
		},
		{
			name:    "Old",
			profile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileOldType},
			want:    old,
		},
		{
			name: "Custom",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{
					TLSProfileSpec: customSpec,
				},
			},
			want: customSpec,
		},
		{
			name: "Custom with nil Custom field errors",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
			},
			wantErr: true,
		},
		{
			name:    "unknown type falls back to Intermediate",
			profile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileType("DoesNotExist")},
			want:    intermediate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetTLSProfileSpec(tt.profile)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetTLSProfileSpec() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("GetTLSProfileSpec() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestResolveFromAPIServer(t *testing.T) {
	intermediate := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	modern := *configv1.TLSProfiles[configv1.TLSProfileModernType]

	tests := []struct {
		name      string
		apiServer *configv1.APIServer
		wantHonor bool
		wantSpec  configv1.TLSProfileSpec
	}{
		{
			name:      "nil APIServer defaults to Intermediate and does not honor",
			apiServer: nil,
			wantHonor: false,
			wantSpec:  intermediate,
		},
		{
			name:      "empty adherence does not honor",
			apiServer: &configv1.APIServer{},
			wantHonor: false,
			wantSpec:  intermediate,
		},
		{
			name: "Legacy adherence does not honor",
			apiServer: &configv1.APIServer{
				Spec: configv1.APIServerSpec{
					TLSAdherence: configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileModernType,
					},
				},
			},
			wantHonor: false,
			wantSpec:  modern,
		},
		{
			name: "Strict adherence honors Modern",
			apiServer: &configv1.APIServer{
				Spec: configv1.APIServerSpec{
					TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileModernType,
					},
				},
			},
			wantHonor: true,
			wantSpec:  modern,
		},
		{
			name: "unknown adherence honors (fail-secure)",
			apiServer: &configv1.APIServer{
				Spec: configv1.APIServerSpec{
					TLSAdherence: configv1.TLSAdherencePolicy("FutureMode"),
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileIntermediateType,
					},
				},
			},
			wantHonor: true,
			wantSpec:  intermediate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveFromAPIServer(tt.apiServer)
			if err != nil {
				t.Fatalf("ResolveFromAPIServer() unexpected error: %v", err)
			}
			if got.Honor != tt.wantHonor {
				t.Fatalf("Honor = %v, want %v", got.Honor, tt.wantHonor)
			}
			if !reflect.DeepEqual(got.Spec, tt.wantSpec) {
				t.Fatalf("Spec = %#v, want %#v", got.Spec, tt.wantSpec)
			}
		})
	}
}

func TestApplyToServingInfo(t *testing.T) {
	modern := *configv1.TLSProfiles[configv1.TLSProfileModernType]
	modernIANA := libgocrypto.OpenSSLToIANACipherSuites(modern.Ciphers)

	tests := []struct {
		name        string
		nilServing  bool
		resolved    ResolvedProfile
		wantErr     bool
		wantMinTLS  string
		wantCiphers []string
	}{
		{
			name: "does not mutate when Honor is false",
			resolved: ResolvedProfile{
				Adherence: configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
				Spec:      modern,
				Honor:     false,
			},
		},
		{
			name: "applies IANA ciphers when Honor is true",
			resolved: ResolvedProfile{
				Adherence: configv1.TLSAdherencePolicyStrictAllComponents,
				Spec:      modern,
				Honor:     true,
			},
			wantMinTLS:  string(modern.MinTLSVersion),
			wantCiphers: modernIANA,
		},
		{
			name:       "nil servingInfo is a no-op",
			nilServing: true,
			resolved:   ResolvedProfile{Honor: true, Spec: modern},
		},
		{
			name: "unsupported ciphers fail when honored",
			resolved: ResolvedProfile{
				Adherence: configv1.TLSAdherencePolicyStrictAllComponents,
				Honor:     true,
				Spec: configv1.TLSProfileSpec{
					MinTLSVersion: configv1.VersionTLS12,
					Ciphers:       []string{"NOT-A-REAL-OPENSSL-CIPHER"},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var serving *configv1.HTTPServingInfo
			if !tt.nilServing {
				serving = &configv1.HTTPServingInfo{}
			}

			err := ApplyToServingInfo(serving, tt.resolved)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ApplyToServingInfo() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.nilServing {
				return
			}
			if tt.wantErr {
				if serving.MinTLSVersion != "" || len(serving.CipherSuites) != 0 {
					t.Fatalf("expected ServingInfo unchanged on error, got %#v", serving)
				}
				return
			}
			if serving.MinTLSVersion != tt.wantMinTLS {
				t.Fatalf("MinTLSVersion = %q, want %q", serving.MinTLSVersion, tt.wantMinTLS)
			}
			if !reflect.DeepEqual(serving.CipherSuites, tt.wantCiphers) {
				t.Fatalf("CipherSuites = %#v, want %#v", serving.CipherSuites, tt.wantCiphers)
			}
		})
	}
}
