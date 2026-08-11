package e2e

import (
	"context"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

type apiserverTLSConfig struct {
	tlsProfile *configv1.TLSSecurityProfile
	adherence  configv1.TLSAdherencePolicy
}

func getClusterAPIServerTLSConfig(ctx context.Context) (*apiserverTLSConfig, error) {
	apiServer, err := configClient.APIServers().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	cfg := &apiserverTLSConfig{
		adherence: apiServer.Spec.TLSAdherence,
	}
	if apiServer.Spec.TLSSecurityProfile != nil {
		cfg.tlsProfile = apiServer.Spec.TLSSecurityProfile.DeepCopy()
	}
	return cfg, nil
}

func updateClusterAPIServerTLSConfig(ctx context.Context, profile *configv1.TLSSecurityProfile, adherence configv1.TLSAdherencePolicy) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		apiServer, err := configClient.APIServers().Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			return err
		}
		updated := apiServer.DeepCopy()
		if profile != nil {
			updated.Spec.TLSSecurityProfile = profile.DeepCopy()
		} else {
			updated.Spec.TLSSecurityProfile = nil
		}
		updated.Spec.TLSAdherence = adherence
		_, err = configClient.APIServers().Update(ctx, updated, metav1.UpdateOptions{})
		return err
	})
}

// restoreClusterAPIServerTLSConfig reverts apiserver TLS settings captured before a test mutation.
// tlsAdherence cannot be removed once set, so an originally unset value is restored to Legacy.
func restoreClusterAPIServerTLSConfig(ctx context.Context, original *apiserverTLSConfig) error {
	if original == nil {
		return nil
	}
	adherence := original.adherence
	if adherence == configv1.TLSAdherencePolicyNoOpinion {
		adherence = configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly
	}
	return updateClusterAPIServerTLSConfig(ctx, original.tlsProfile, adherence)
}

func isTLSAdherenceUnsupported(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Field not served / validation / RBAC / managed-cluster admission denials.
	if strings.Contains(msg, "tlsadherence") || strings.Contains(msg, "unknown field") {
		return true
	}
	if strings.Contains(msg, "prevented from accessing red hat managed resources") {
		return true
	}
	if apierrors.IsInvalid(err) || apierrors.IsForbidden(err) || apierrors.IsNotFound(err) {
		return strings.Contains(msg, "tls") || strings.Contains(msg, "managed")
	}
	return false
}

func patchAPIServerAuditProfile(ctx context.Context, profileType configv1.AuditProfileType) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		apiServer, err := configClient.APIServers().Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			return err
		}
		updated := apiServer.DeepCopy()
		updated.Spec.Audit.Profile = profileType
		_, err = configClient.APIServers().Update(ctx, updated, metav1.UpdateOptions{})
		return err
	})
}
