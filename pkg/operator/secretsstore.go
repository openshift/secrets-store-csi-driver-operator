package operator

import (
	"fmt"
	"time"

	opv1 "github.com/openshift/api/operator/v1"
	operatorv1listers "github.com/openshift/client-go/operator/listers/operator/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

// defaultRotationEnabled and defaultRotationInterval match the values hardcoded
// in assets/node.yaml prior to https://github.com/openshift/enhancements/pull/2012 feature
// and are returned whenever the administrator has expressed no opinion,
// so existing clusters see no change in behavior on upgrade.
const (
	defaultRotationEnabled  = true
	defaultRotationInterval = 2 * time.Minute
)

// getClusterCSIDriverConfig returns the driverConfig of the ClusterCSIDriver named name.
func getClusterCSIDriverConfig(clusterCSIDriverLister operatorv1listers.ClusterCSIDriverLister, name string) (opv1.CSIDriverConfigSpec, error) {
	driver, err := clusterCSIDriverLister.Get(name)
	if apierrors.IsNotFound(err) {
		klog.V(4).Infof("ClusterCSIDriver %q not found, assuming no secretsStore configuration", name)
		return opv1.CSIDriverConfigSpec{}, nil
	}
	if err != nil {
		return opv1.CSIDriverConfigSpec{}, fmt.Errorf("failed to get ClusterCSIDriver %q: %w", name, err)
	}

	return driver.Spec.DriverConfig, nil
}

// getSecretRotationConfig computes the effective secret-rotation enable flag
// and poll interval for the Secrets Store CSI driver from a
// ClusterCSIDriver's driverConfig.
//
//   - driverType != SecretsStore (including the unset/empty value): defaults
//   - driverType == SecretsStore but secretsStore is the zero value: defaults
//   - secretRotation is the zero value (type == ""): defaults
//   - secretRotation.type == None: rotation disabled
//   - secretRotation.type == Custom, custom.minimumRefreshAge > 0: that interval
//   - secretRotation.type == Custom, custom.minimumRefreshAge == 0 (omitted): default interval
func getSecretRotationConfig(driverConfig opv1.CSIDriverConfigSpec) (enabled bool, interval time.Duration) {
	if driverConfig.DriverType != opv1.SecretsStoreDriverType {
		return defaultRotationEnabled, defaultRotationInterval
	}

	rotation := driverConfig.SecretsStore.SecretRotation
	switch rotation.Type {
	case opv1.SecretRotationNone:
		return false, defaultRotationInterval
	case opv1.SecretRotationCustom:
		if rotation.Custom.MinimumRefreshAge > 0 {
			return true, time.Duration(rotation.Custom.MinimumRefreshAge) * time.Second
		}
		return true, defaultRotationInterval
	default:
		// Zero value (type == "") or any unrecognized future value: no
		// opinion expressed, keep the defaults.
		return defaultRotationEnabled, defaultRotationInterval
	}
}

// getRequiresRepublish returns the desired CSIDriver.spec.requiresRepublish
// value. It mirrors secretRotation's effective enable state:
// true when rotation is enabled, false otherwise.
func getRequiresRepublish(driverConfig opv1.CSIDriverConfigSpec) *bool {
	enabled, _ := getSecretRotationConfig(driverConfig)
	return ptr.To(enabled)
}

// getEffectiveTokenRequests computes the desired CSIDriver.spec.tokenRequests
// from driverConfig, given the tokenRequests currently set on the live  CSIDriver object.
//
//   - driverType != SecretsStore (including the unset/empty value): preserve existing
//   - driverType == SecretsStore, secretsStore is the zero value: preserve existing
//   - tokenRequests is the zero value (type == "", i.e. omitted): preserve existing
//   - type == Unmanaged: preserve existing (the managed field is not used)
//   - type == Managed, managed.audiences == nil: preserve existing.
//   - type == Managed, managed.audiences is a non-nil pointer to a slice: return
//     exactly that slice mapped to []storagev1.TokenRequest -- INCLUDING the
//     empty-slice case, which explicitly clears all tokenRequests.
func getEffectiveTokenRequests(driverConfig opv1.CSIDriverConfigSpec, existing []storagev1.TokenRequest) []storagev1.TokenRequest {
	if driverConfig.DriverType != opv1.SecretsStoreDriverType {
		return existing
	}

	tokenRequests := driverConfig.SecretsStore.TokenRequests
	switch tokenRequests.Type {
	case opv1.TokenRequestsManaged:
		if tokenRequests.Managed.Audiences == nil {
			return existing
		}
		audiences := *tokenRequests.Managed.Audiences
		result := make([]storagev1.TokenRequest, 0, len(audiences))
		for _, audience := range audiences {
			tr := storagev1.TokenRequest{Audience: ptr.Deref(audience.Audience, "")}
			if audience.ExpirationSeconds != 0 {
				tr.ExpirationSeconds = ptr.To(int64(audience.ExpirationSeconds))
			}
			result = append(result, tr)
		}
		return result
	case opv1.TokenRequestsUnmanaged:
		return existing
	default:
		// Zero value (type == ""): omitted, preserve existing.
		return existing
	}
}
