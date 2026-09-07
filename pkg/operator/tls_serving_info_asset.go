package operator

import (
	"encoding/json"
	"fmt"

	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	operatorv1listers "github.com/openshift/client-go/operator/listers/operator/v1"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// servingInfoConfigMapAssetName is the asset file name for the operator's own
// --config-file-backed serving-info ConfigMap (SSCSI-264). See
// assets/serving-info-configmap.yaml and
// config/manifests/stable/secrets-store-csi-driver-operator-serving-info-configmap.yaml
// (the latter is the install-time bootstrap copy of the same object, applied before this
// operator's own reconciliation ever runs -- see that file's header comment).
const servingInfoConfigMapAssetName = "serving-info-configmap.yaml"

// servingInfoConfigMapDataKey is the ConfigMap key mounted at
// /var/run/configmaps/serving-info/config.yaml (see the CSV Deployment's volumeMounts) and
// passed to the operator's own process via --config.
const servingInfoConfigMapDataKey = "config.yaml"

// withServingInfoConfigMapAsset wraps a base AssetFunc so that, for
// serving-info-configmap.yaml specifically, the returned bytes carry the operator's own HTTPS
// serving-info policy (MinTLSVersion/CipherSuites) resolved from the live
// ClusterCSIDriver.spec.observedConfig, instead of the static (empty) base manifest.
//
// This makes WithConditionalStaticResourcesController's own periodic resync the single,
// ongoing delivery mechanism for this value -- exactly mirroring how
// withSecretsStoreCSIDriverAsset already re-renders csidriver.yaml from live cluster state in
// this same package. No second, independently-scheduled sync controller is introduced, so
// there is exactly one code path that ever writes this ConfigMap's data once the operator is
// running (see implementation/task-reports/T1_2.md and T1_3.md for the full hand-off/decision
// trail).
func withServingInfoConfigMapAsset(
	base resourceapply.AssetFunc,
	clusterCSIDriverLister operatorv1listers.ClusterCSIDriverLister,
	clusterCSIDriverName string,
) resourceapply.AssetFunc {
	return func(name string) ([]byte, error) {
		manifest, err := base(name)
		if err != nil {
			return nil, err
		}
		if name != servingInfoConfigMapAssetName {
			return manifest, nil
		}
		return renderServingInfoConfigMap(manifest, clusterCSIDriverLister, clusterCSIDriverName)
	}
}

// renderServingInfoConfigMap decodes the static serving-info-configmap.yaml manifest and
// overwrites its config.yaml data key with a GenericOperatorConfig reflecting the TLS serving
// policy resolved from the live ClusterCSIDriver's spec.observedConfig (via
// getObservedTLSServingConfig), returning the mutated object re-marshaled to JSON for the
// StaticResourceController to apply.
//
// A non-nil error here surfaces as this controller's <name>Degraded condition (library-go's
// staticresourcecontroller.StaticResourceController sets it automatically whenever an
// AssetFunc/apply call for one of its manifests fails) and leaves the ConfigMap's last
// successfully-applied content untouched for this sync cycle -- i.e. the operator continues
// serving under its last-known-good policy (specs.md Edge Cases), satisfying FR-006, without
// this function needing to implement status reporting itself.
//
// A ClusterCSIDriver that does not exist yet is treated the same as this package's existing
// getClusterCSIDriverConfig convention (not degraded; assume no observation yet) -- but a
// genuine API read failure, or an observedConfig payload present but too malformed to parse as
// JSON at all, is treated as a real error and does surface Degraded=True, per FR-006's
// "cannot be read or applied" wording. Missing/partial individual sub-fields within an
// otherwise well-formed observedConfig are NOT treated as degraded -- that is
// getObservedTLSServingConfig's normal, tested, safe-default path (T1_1), not an error
// condition.
func renderServingInfoConfigMap(
	manifest []byte,
	clusterCSIDriverLister operatorv1listers.ClusterCSIDriverLister,
	clusterCSIDriverName string,
) ([]byte, error) {
	observedConfigRaw, malformed, err := getObservedConfigRaw(clusterCSIDriverLister, clusterCSIDriverName)
	if err != nil {
		return nil, fmt.Errorf("failed to read observed config for ClusterCSIDriver %q: %w", clusterCSIDriverName, err)
	}
	if malformed {
		return nil, fmt.Errorf("observed config for ClusterCSIDriver %q is not valid JSON", clusterCSIDriverName)
	}

	minTLSVersion, cipherSuites := getObservedTLSServingConfig(observedConfigRaw)

	operatorConfig := operatorv1alpha1.GenericOperatorConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: operatorv1alpha1.GroupVersion.String(),
			Kind:       "GenericOperatorConfig",
		},
	}
	operatorConfig.ServingInfo.MinTLSVersion = minTLSVersion
	operatorConfig.ServingInfo.CipherSuites = cipherSuites

	configContent, err := json.Marshal(operatorConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal generated operator serving-info config: %w", err)
	}

	configMap := resourceread.ReadConfigMapV1OrDie(manifest)
	if configMap.Data == nil {
		configMap.Data = map[string]string{}
	}
	configMap.Data[servingInfoConfigMapDataKey] = string(configContent)

	klog.V(4).Infof("Resolved operator TLS serving policy: minTLSVersion=%s cipherSuiteCount=%d", minTLSVersion, len(cipherSuites))

	return json.Marshal(configMap)
}

// getObservedConfigRaw returns the raw bytes of ClusterCSIDriver.spec.observedConfig for
// name. Despite living under .spec (per opv1.OperatorSpec's own doc comment: "it exists in
// spec because it is an input to the level for the operator"), this field is the config
// observer's *output* channel, not admin-facing desired state -- it is written exclusively by
// CSIConfigObserverController (via the standard library-go configobserver.NewConfigObserver
// pattern wired in starter.go's WithCSIConfigObserverController) and only ever read here.
//
// A ClusterCSIDriver that does not exist yet returns (nil, false, nil) -- not an error,
// mirroring getClusterCSIDriverConfig's existing NotFound-tolerant convention in this package.
// A payload present but not valid JSON is reported via the malformed return value rather than
// silently swallowed, so the caller can decide to surface it as degraded.
func getObservedConfigRaw(clusterCSIDriverLister operatorv1listers.ClusterCSIDriverLister, name string) (raw []byte, malformed bool, err error) {
	driver, err := clusterCSIDriverLister.Get(name)
	if apierrors.IsNotFound(err) {
		klog.V(4).Infof("ClusterCSIDriver %q not found, assuming no observed TLS serving config yet", name)
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("failed to get ClusterCSIDriver %q: %w", name, err)
	}

	observedConfigRaw := driver.Spec.ObservedConfig.Raw
	if len(observedConfigRaw) == 0 {
		return nil, false, nil
	}
	if !json.Valid(observedConfigRaw) {
		return observedConfigRaw, true, nil
	}
	return observedConfigRaw, false, nil
}
