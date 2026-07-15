package operator

import (
	"encoding/json"
	"fmt"

	operatorv1listers "github.com/openshift/client-go/operator/listers/operator/v1"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	storagev1listers "k8s.io/client-go/listers/storage/v1"
	"k8s.io/klog/v2"
)

// csidriverAssetName is the asset file name for the CSIDriver manifest
const csidriverAssetName = "csidriver.yaml"

// withSecretsStoreCSIDriverAsset wraps a base AssetFunc so that, for
// csidriver.yaml specifically, the returned bytes reflect the resolved
// secretsStore rotation and tokenRequests configuration read from the live
// ClusterCSIDriver, instead of the fully-static base manifest.
func withSecretsStoreCSIDriverAsset(
	base resourceapply.AssetFunc,
	clusterCSIDriverLister operatorv1listers.ClusterCSIDriverLister,
	csiDriverLister storagev1listers.CSIDriverLister,
	clusterCSIDriverName string,
) resourceapply.AssetFunc {
	return func(name string) ([]byte, error) {
		manifest, err := base(name)
		if err != nil {
			return nil, err
		}
		if name != csidriverAssetName {
			return manifest, nil
		}

		return renderSecretsStoreCSIDriver(manifest, clusterCSIDriverLister, csiDriverLister, clusterCSIDriverName)
	}
}

// renderSecretsStoreCSIDriver decodes the static csidriver.yaml manifest and
// overwrites spec.requiresRepublish and spec.tokenRequests with the values
// resolved from the live ClusterCSIDriver (and, when tokenRequests are
// Unmanaged/omitted, from the live CSIDriver object),
// returning the mutated object re-marshaled to JSON
// for the StaticResourceController to apply.
func renderSecretsStoreCSIDriver(
	manifest []byte,
	clusterCSIDriverLister operatorv1listers.ClusterCSIDriverLister,
	csiDriverLister storagev1listers.CSIDriverLister,
	clusterCSIDriverName string,
) ([]byte, error) {
	driverConfig, err := getClusterCSIDriverConfig(clusterCSIDriverLister, clusterCSIDriverName)
	if err != nil {
		return nil, err
	}

	existingTokenRequests, err := getExistingTokenRequests(csiDriverLister, clusterCSIDriverName)
	if err != nil {
		return nil, err
	}

	csiDriver := resourceread.ReadCSIDriverV1OrDie(manifest)
	csiDriver.Spec.RequiresRepublish = getRequiresRepublish(driverConfig)
	csiDriver.Spec.TokenRequests = getEffectiveTokenRequests(driverConfig, existingTokenRequests)
	klog.V(4).Infof("resolved CSIDriver %q config: requiresRepublish=%t tokenRequestsCount=%d",
		csiDriver.Name, *csiDriver.Spec.RequiresRepublish, len(csiDriver.Spec.TokenRequests))

	return json.Marshal(csiDriver)
}

// getExistingTokenRequests returns the tokenRequests currently present on the
// live storage.k8s.io/v1 CSIDriver object named name, or nil if the object
// has not been created yet.
func getExistingTokenRequests(lister storagev1listers.CSIDriverLister, name string) ([]storagev1.TokenRequest, error) {
	existing, err := lister.Get(name)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get CSIDriver %q: %w", name, err)
	}
	return existing.Spec.TokenRequests, nil
}
