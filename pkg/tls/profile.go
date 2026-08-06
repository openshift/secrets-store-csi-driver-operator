package tls

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"
)

const (
	// APIServerName is the singleton APIServer resource name.
	APIServerName = "cluster"
)

// ResolvedProfile holds the cluster TLS settings relevant to this operator's
// Controllercmd metrics server, plus metadata needed by the SecurityProfileWatcher.
type ResolvedProfile struct {
	// Adherence is the raw tlsAdherence value from the APIServer (may be empty).
	Adherence configv1.TLSAdherencePolicy
	// Spec is the resolved TLS profile (Intermediate when the APIServer profile is unset).
	Spec configv1.TLSProfileSpec
	// Honor is true when ShouldHonorClusterTLSProfile(Adherence) is true.
	Honor bool
}

// FetchAndResolve loads apiserver.config.openshift.io/cluster and resolves the
// TLS profile the operator should use for its HTTPS metrics server.
//
// Behavior:
//   - NotFound: treat as empty APIServer and fall back to Intermediate for Spec
//   - Other fetch errors: returned to the caller (fail loud; do not serve unknown TLS)
//   - Honor=false (Legacy / empty adherence): Spec is still resolved for watcher seeding,
//     but ApplyToServingInfo will leave Controllercmd defaults alone
//   - Honor=true: ApplyToServingInfo writes Spec into ServingInfo
func FetchAndResolve(ctx context.Context, client configv1client.APIServersGetter) (ResolvedProfile, error) {
	apiServer, err := client.APIServers().Get(ctx, APIServerName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return ResolvedProfile{}, fmt.Errorf("failed to get apiserver.config.openshift.io/%s: %w", APIServerName, err)
		}
		klog.Warningf("apiserver.config.openshift.io/%s not found; falling back to Intermediate TLS profile", APIServerName)
		apiServer = &configv1.APIServer{}
	}

	return ResolveFromAPIServer(apiServer)
}

// ResolveFromAPIServer resolves TLS settings from an already-fetched APIServer object.
func ResolveFromAPIServer(apiServer *configv1.APIServer) (ResolvedProfile, error) {
	if apiServer == nil {
		apiServer = &configv1.APIServer{}
	}

	spec, err := GetTLSProfileSpec(apiServer.Spec.TLSSecurityProfile)
	if err != nil {
		return ResolvedProfile{}, err
	}

	adherence := apiServer.Spec.TLSAdherence
	honor := libgocrypto.ShouldHonorClusterTLSProfile(adherence)
	if adherence != configv1.TLSAdherencePolicyNoOpinion &&
		adherence != configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly &&
		adherence != configv1.TLSAdherencePolicyStrictAllComponents {
		klog.Warningf("apiserver.config.openshift.io/%s has unknown tlsAdherence %q; treating as StrictAllComponents", APIServerName, adherence)
	}

	return ResolvedProfile{
		Adherence: adherence,
		Spec:      spec,
		Honor:     honor,
	}, nil
}

// GetTLSProfileSpec returns the effective TLSProfileSpec for the given profile.
// Nil / empty profile types fall back to Intermediate. Custom with a nil Custom
// field returns an error.
func GetTLSProfileSpec(profile *configv1.TLSSecurityProfile) (configv1.TLSProfileSpec, error) {
	defaultProfile := *configv1.TLSProfiles[libgocrypto.DefaultTLSProfileType]
	if profile == nil || profile.Type == "" {
		return defaultProfile, nil
	}

	if profile.Type != configv1.TLSProfileCustomType {
		if tlsConfig, ok := configv1.TLSProfiles[profile.Type]; ok {
			return *tlsConfig, nil
		}
		klog.Warningf("unknown TLS profile type %q; falling back to Intermediate", profile.Type)
		return defaultProfile, nil
	}

	if profile.Custom == nil {
		return configv1.TLSProfileSpec{}, fmt.Errorf("custom TLS profile specified but Custom field is nil")
	}
	return profile.Custom.TLSProfileSpec, nil
}

// ApplyToServingInfo writes MinTLSVersion and CipherSuites into servingInfo when
// the resolved profile should be honored. Otherwise it leaves servingInfo unchanged
// so Controllercmd recommended defaults remain in effect (Legacy adherence).
func ApplyToServingInfo(servingInfo *configv1.HTTPServingInfo, resolved ResolvedProfile) {
	if servingInfo == nil || !resolved.Honor {
		if servingInfo != nil && !resolved.Honor {
			klog.Infof("TLS adherence policy is %q; using Controllercmd default TLS settings", resolved.Adherence)
		}
		return
	}

	servingInfo.MinTLSVersion = string(resolved.Spec.MinTLSVersion)
	servingInfo.CipherSuites = libgocrypto.OpenSSLToIANACipherSuites(resolved.Spec.Ciphers)
	if len(resolved.Spec.Ciphers) > 0 && len(servingInfo.CipherSuites) == 0 {
		// Every configured cipher was unsupported by Go's crypto/tls and
		// silently dropped by OpenSSLToIANACipherSuites (logged only at
		// klog V(4)). The server then falls back to Controllercmd default
		// ciphers, which may be broader than the cluster policy intends.
		klog.Warningf("all %d cipher(s) from the cluster TLS profile are unsupported by Go's crypto/tls and were "+
			"dropped; the metrics server will use Controllercmd default ciphers instead of %v",
			len(resolved.Spec.Ciphers), resolved.Spec.Ciphers)
	}
	klog.Infof("Applied cluster TLS profile to metrics serving config: minTLSVersion=%s, cipherSuites=%v",
		servingInfo.MinTLSVersion, servingInfo.CipherSuites)
}
