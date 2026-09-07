package operator

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	"github.com/openshift/library-go/pkg/crypto"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"
)

// sc004Baseline captures the exact MinTLSVersion/CipherSuites values produced by today's
// pre-feature default TLS configuration path — configdefaults.SetRecommendedServingInfoDefaults
// (vendor/github.com/openshift/library-go/pkg/config/configdefaults/config_default.go:45-56),
// which is unconditionally invoked by ControllerBuilder.WithServer today because this operator's
// CSV never passes --config (repo-assessment.md §1.4). Captured by direct inspection of
// crypto.TLSVersionToNameOrDie(crypto.DefaultTLSVersion()) and
// crypto.CipherSuitesToNamesOrDie(crypto.DefaultCiphers()) on the pinned repo-assessment commit
// (58653d56), NOT re-derived from ResolveTLSServingProfile — an independent baseline is required
// for this to be a meaningful regression check (see SC-004 note below).
var sc004BaselineMinTLSVersion = crypto.TLSVersionToNameOrDie(crypto.DefaultTLSVersion())
var sc004BaselineCipherSuites = crypto.CipherSuitesToNamesOrDie(crypto.DefaultCiphers())

func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

func TestResolveTLSServingProfile(t *testing.T) {
	customProfile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{
			TLSProfileSpec: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS13,
				Ciphers:       []string{"ECDHE-ECDSA-AES128-GCM-SHA256"},
			},
		},
	}

	// Phase 2 (US-002) fidelity fixtures: a mixed TLS-1.2/TLS-1.3 cipher list in a
	// deliberately non-alphabetical order, proving ResolveTLSServingProfile preserves the
	// admin's exact ordering (not just the correct set) when mapping OpenSSL names to IANA
	// names — a distinct property from the single-cipher Custom case above. Also uses
	// VersionTLS12 (rather than the VersionTLS13 already exercised above) so the version
	// field is proven to round-trip independently of that other fixture.
	mixedOrderCustomProfile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{
			TLSProfileSpec: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
				Ciphers: []string{
					"ECDHE-RSA-AES256-GCM-SHA384",
					"TLS_AES_128_GCM_SHA256",
					"ECDHE-ECDSA-CHACHA20-POLY1305",
				},
			},
		},
	}

	// Phase 2 (US-002) fidelity fixture: an unsupported/unmapped OpenSSL cipher name mixed
	// in with valid ones, proving the unsupported entry is silently dropped (matching
	// crypto.OpenSSLToIANACipherSuites's own documented behavior) without affecting the
	// other, valid ciphers in the same custom list.
	unsupportedCipherCustomProfile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{
			TLSProfileSpec: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
				Ciphers: []string{
					"ECDHE-RSA-AES128-GCM-SHA256",
					"NOT-A-REAL-CIPHER",
					"TLS_AES_256_GCM_SHA384",
				},
			},
		},
	}

	cases := []struct {
		name                 string
		apiServer            *configv1.APIServer
		expectedMinTLS       string
		expectedCipherSuites []string
	}{
		{
			// Guard A fails immediately (nil *APIServer): profileType falls back to
			// crypto.DefaultTLSProfileType before Guard B is ever evaluated. Traces to FR-004.
			name:                 "nil APIServer falls back to the default (Intermediate) profile",
			apiServer:            nil,
			expectedMinTLS:       string(configv1.TLSProfiles[crypto.DefaultTLSProfileType].MinTLSVersion),
			expectedCipherSuites: crypto.OpenSSLToIANACipherSuites(configv1.TLSProfiles[crypto.DefaultTLSProfileType].Ciphers),
		},
		{
			// Guard A passes the apiServer!=nil check but fails the inner profile!=nil check:
			// same default-profile-type branch as the nil-APIServer case; Guard B never reached.
			// Traces to FR-004.
			name: "non-nil APIServer with nil TLSSecurityProfile falls back to the default profile",
			apiServer: &configv1.APIServer{
				Spec: configv1.APIServerSpec{TLSSecurityProfile: nil},
			},
			expectedMinTLS:       string(configv1.TLSProfiles[crypto.DefaultTLSProfileType].MinTLSVersion),
			expectedCipherSuites: crypto.OpenSSLToIANACipherSuites(configv1.TLSProfiles[crypto.DefaultTLSProfileType].Ciphers),
		},
		{
			// Guard A passes with a non-nil, non-Custom profile: Guard B's `profileType == Custom`
			// condition is false, so the configv1.TLSProfiles[profileType] branch is taken directly.
			name: "Old profile type resolves to the Old TLSProfiles entry",
			apiServer: &configv1.APIServer{
				Spec: configv1.APIServerSpec{
					TLSSecurityProfile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileOldType},
				},
			},
			expectedMinTLS:       string(configv1.TLSProfiles[configv1.TLSProfileOldType].MinTLSVersion),
			expectedCipherSuites: crypto.OpenSSLToIANACipherSuites(configv1.TLSProfiles[configv1.TLSProfileOldType].Ciphers),
		},
		{
			// Same guard path as Old, different profileType value — proves the branch is
			// type-driven, not hardcoded to a single preset.
			name: "Modern profile type resolves to the Modern TLSProfiles entry",
			apiServer: &configv1.APIServer{
				Spec: configv1.APIServerSpec{
					TLSSecurityProfile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType},
				},
			},
			expectedMinTLS:       string(configv1.TLSProfiles[configv1.TLSProfileModernType].MinTLSVersion),
			expectedCipherSuites: crypto.OpenSSLToIANACipherSuites(configv1.TLSProfiles[configv1.TLSProfileModernType].Ciphers),
		},
		{
			// Guard A passes with a non-nil profile; reaches Guard B, profileType==Custom is true
			// AND profile.Custom!=nil is true -> takes the profile.Custom.TLSProfileSpec branch,
			// proving the admin-supplied custom cipher list/version is honored, not silently
			// substituted with a preset. Traces to US-002/FR-002 (Phase 2 will extend this matrix).
			name: "Custom profile type with a populated Custom spec is honored exactly",
			apiServer: &configv1.APIServer{
				Spec: configv1.APIServerSpec{TLSSecurityProfile: customProfile},
			},
			expectedMinTLS:       string(configv1.VersionTLS13),
			expectedCipherSuites: crypto.OpenSSLToIANACipherSuites([]string{"ECDHE-ECDSA-AES128-GCM-SHA256"}),
		},
		{
			// Guard A passes; reaches Guard B, profileType==Custom is true but profile.Custom==nil
			// fails the inner nil-check -> falls through to the same configv1.TLSProfiles[default]
			// branch as the nil-profile case, proving the defensive fallback fires even when the
			// type claims Custom. Traces to FR-004 (must not crash/misbehave on malformed input).
			name: "Custom profile type with nil Custom spec falls back to the default profile",
			apiServer: &configv1.APIServer{
				Spec: configv1.APIServerSpec{
					TLSSecurityProfile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileCustomType, Custom: nil},
				},
			},
			expectedMinTLS:       string(configv1.TLSProfiles[crypto.DefaultTLSProfileType].MinTLSVersion),
			expectedCipherSuites: crypto.OpenSSLToIANACipherSuites(configv1.TLSProfiles[crypto.DefaultTLSProfileType].Ciphers),
		},
		{
			// Fidelity property (US-002/FR-002): a mixed TLS-1.2/TLS-1.3 cipher list, in a
			// deliberately non-alphabetical order, is mapped to IANA names in the *same
			// order* — proving the admin's custom preference order is honored, not
			// silently reordered or normalized.
			name:           "Custom profile type with a mixed-order, mixed-TLS-version cipher list preserves exact order",
			apiServer:      &configv1.APIServer{Spec: configv1.APIServerSpec{TLSSecurityProfile: mixedOrderCustomProfile}},
			expectedMinTLS: string(configv1.VersionTLS12),
			expectedCipherSuites: []string{
				"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
				"TLS_AES_128_GCM_SHA256",
				"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256",
			},
		},
		{
			// Fidelity property (US-002/FR-002): an unsupported/unmapped OpenSSL cipher
			// name is silently dropped from the result — matching
			// crypto.OpenSSLToIANACipherSuites's own documented drop-and-log behavior —
			// without affecting the other, valid ciphers in the same custom list.
			name:           "Custom profile type with an unsupported cipher name silently drops only that entry",
			apiServer:      &configv1.APIServer{Spec: configv1.APIServerSpec{TLSSecurityProfile: unsupportedCipherCustomProfile}},
			expectedMinTLS: string(configv1.VersionTLS12),
			expectedCipherSuites: []string{
				"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				"TLS_AES_256_GCM_SHA384",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			minTLS, ciphers := ResolveTLSServingProfile(tc.apiServer)
			if minTLS != tc.expectedMinTLS {
				t.Errorf("expected MinTLSVersion %q, got %q", tc.expectedMinTLS, minTLS)
			}
			if !reflect.DeepEqual(ciphers, tc.expectedCipherSuites) {
				t.Errorf("expected CipherSuites %v, got %v", tc.expectedCipherSuites, ciphers)
			}
		})
	}
}

// TestResolveTLSServingProfile_SC004Regression is the dedicated exact-value regression check
// required by plan.md §6 / tasks.md T1_1 for SC-004 ("client-observable accessibility ... is
// unchanged from pre-change behavior" for a cluster with no explicit custom TLS profile).
//
// IMPLEMENTATION-TIME FINDING (deviation from tasks.md's literal wording, recorded here and in
// implementation/task-reports/T1_1.md / deviation-observed.md): tasks.md's acceptance criteria,
// written before this code existed, asked for a field-by-field IDENTICAL comparison including
// CipherSuites order. Direct inspection during implementation shows this is not achievable
// byte-for-byte, for a legitimate, non-bug reason:
//   - Today's static default path (configdefaults.SetRecommendedServingInfoDefaults) sources its
//     cipher list from crypto.DefaultCiphers(), whose own comment says it is "Aligned with
//     intermediate profile of the 5.7 version of the Mozilla Server Side TLS guidelines" and lists
//     TLS 1.2 ciphers before TLS 1.3 ciphers.
//   - configv1.TLSProfiles (consumed by ResolveTLSServingProfile via the vendored
//     getSecurityProfileCiphers pattern) is explicitly documented as "based on version 5.8 of the
//     Mozilla Server Side TLS configuration guidelines" and lists TLS 1.3 ciphers before TLS 1.2.
//   - The two lists are the SAME 9 cipher suites (verified below) in a DIFFERENT ORDER. This is an
//     intentional consequence of FR-002 ("MUST NOT rely on the underlying language/runtime's
//     default TLS settings" — i.e. MUST switch to the officially-resolved TLSProfiles source), not
//     a bug in this implementation.
//
// SC-004's actual requirement is "client-observable accessibility... unchanged" — a TLS client
// that could connect before (offering any cipher from the 9-member set) can still connect after
// (the same 9-member set is accepted); reordering the server's *preference* among an identical
// accepted set does not change which clients are accepted. This test therefore asserts:
//  1. MinTLSVersion is byte-for-byte identical to the captured baseline (it is: both resolve to
//     "VersionTLS12").
//  2. CipherSuites is set-equal (order-independent) to the captured baseline — genuinely stricter
//     than "a valid TLS 1.2 profile" (a wrong/insecure cipher, or a missing/extra cipher, still
//     fails this test), while not being a false-positive failure over an intentional, correctly
//     -sourced reordering.
func TestResolveTLSServingProfile_SC004Regression(t *testing.T) {
	minTLS, ciphers := ResolveTLSServingProfile(nil)

	if minTLS != sc004BaselineMinTLSVersion {
		t.Fatalf("SC-004 regression: MinTLSVersion changed for the default/no-custom-profile case: baseline %q, got %q", sc004BaselineMinTLSVersion, minTLS)
	}

	got := sortedCopy(ciphers)
	want := sortedCopy(sc004BaselineCipherSuites)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SC-004 regression: CipherSuites set changed for the default/no-custom-profile case:\nbaseline (sorted): %v\ngot (sorted):      %v", want, got)
	}

	if len(sc004BaselineCipherSuites) != len(ciphers) {
		t.Fatalf("SC-004 regression: cipher suite count changed: baseline had %d, got %d", len(sc004BaselineCipherSuites), len(ciphers))
	}
}

func TestWriteTLSServingConfigFile(t *testing.T) {
	minTLS := "VersionTLS12"
	ciphers := []string{"TLS_AES_128_GCM_SHA256", "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256"}

	cases := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name: "writes a well-formed config file to an existing directory",
			path: filepath.Join(t.TempDir(), "tls-serving-config.yaml"),
		},
		{
			// Non-fatal-error contract (FR-004): callers must be able to detect and log a write
			// failure without this function panicking.
			name:    "returns a wrapped error for a non-existent parent directory",
			path:    filepath.Join(t.TempDir(), "missing-dir", "tls-serving-config.yaml"),
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := WriteTLSServingConfigFile(tc.path, minTLS, ciphers)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error writing to %q, got nil", tc.path)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error writing TLS serving config file: %v", err)
			}

			raw, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatalf("failed to read back written file: %v", err)
			}

			// Round-trip through the exact same decode path ControllerCommandConfig.Config()
			// uses (kyaml.ToJSON -> unstructured -> FromUnstructured into GenericOperatorConfig),
			// so this test proves compatibility with the real consumer, not just with this
			// package's own struct tags.
			jsonData, err := kyaml.ToJSON(raw)
			if err != nil {
				t.Fatalf("failed to convert written YAML to JSON: %v", err)
			}
			uncastObj, err := runtime.Decode(unstructured.UnstructuredJSONScheme, jsonData)
			if err != nil {
				t.Fatalf("failed to decode written config as unstructured: %v", err)
			}
			u, ok := uncastObj.(*unstructured.Unstructured)
			if !ok {
				t.Fatalf("decoded object is not *unstructured.Unstructured: %T", uncastObj)
			}
			u.SetGroupVersionKind(operatorv1alpha1.GroupVersion.WithKind("GenericOperatorConfig"))

			decoded := &operatorv1alpha1.GenericOperatorConfig{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, decoded); err != nil {
				t.Fatalf("failed to convert unstructured to GenericOperatorConfig: %v", err)
			}

			if decoded.ServingInfo.MinTLSVersion != minTLS {
				t.Errorf("expected MinTLSVersion %q, got %q", minTLS, decoded.ServingInfo.MinTLSVersion)
			}
			if !reflect.DeepEqual(decoded.ServingInfo.CipherSuites, ciphers) {
				t.Errorf("expected CipherSuites %v, got %v", ciphers, decoded.ServingInfo.CipherSuites)
			}
		})
	}
}
