package tls

import (
	"context"
	"fmt"
	"slices"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/api/features"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
	"github.com/openshift/library-go/pkg/operator/configobserver/featuregates"
)

const (
	// clusterVersionName is the singleton ClusterVersion object.
	clusterVersionName = "version"
	// featureGateResourceName is the singleton FeatureGate object.
	featureGateResourceName = "cluster"
)

// pollTLSGroupPreferences reports whether TLSGroupPreferences is enabled.
//
// The HTTPS config is written once, before RunOperator starts its shared
// informer factory, so this polls clusterversions/version and
// featuregates/cluster with GETs. A timeout, cancellation, or failed read
// returns an error. A payload version that does not list the gate is
// disabled: the gate cannot be on, and Enabled panics when a name is in
// neither list. Later gate flips are handled by the long-lived accessor in
// RunOperator.
func pollTLSGroupPreferences(ctx context.Context, client configclient.Interface) (bool, error) {
	var current featuregates.FeatureGate
	var lastErr error
	attempt := func(ctx context.Context) (bool, error) {
		current, lastErr = readTLSGroupPreferences(ctx, client)
		if lastErr != nil {
			klog.Warningf("failed to read FeatureGate %s, will retry: %v", features.FeatureGateTLSGroupPreferences, lastErr)
			return false, nil
		}
		return true, nil
	}
	// NotFound and a payload version that is not in the FeatureGate status yet
	// are retried until ctx ends. Both can lag briefly while the cluster is coming up.
	if err := wait.PollUntilContextCancel(ctx, 2*time.Second, true, attempt); err != nil {
		if lastErr != nil {
			return false, lastErr
		}
		if ctx.Err() == context.DeadlineExceeded {
			return false, fmt.Errorf("timed out waiting for FeatureGate %s", features.FeatureGateTLSGroupPreferences)
		}
		return false, fmt.Errorf("feature gate read canceled before %s was read: %w", features.FeatureGateTLSGroupPreferences, context.Cause(ctx))
	}

	if !slices.Contains(current.KnownFeatures(), features.FeatureGateTLSGroupPreferences) {
		klog.Infof("FeatureGate %s is not listed for this payload version; leaving curve preferences unset", features.FeatureGateTLSGroupPreferences)
		return false, nil
	}
	enabled := current.Enabled(features.FeatureGateTLSGroupPreferences)
	klog.Infof("FeatureGate %s enabled=%t", features.FeatureGateTLSGroupPreferences, enabled)
	return enabled, nil
}

// readTLSGroupPreferences GETs the ClusterVersion and FeatureGate singletons
// and selects the enabled/disabled lists for this payload. The version choice
// matches library-go's FeatureGateAccess when the desired version is the OLM
// marker: status.desired.version, then status.history[0] when desired is empty.
func readTLSGroupPreferences(ctx context.Context, client configclient.Interface) (featuregates.FeatureGate, error) {
	clusterVersion, err := client.ConfigV1().ClusterVersions().Get(ctx, clusterVersionName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get clusterversion.config.openshift.io/%s: %w", clusterVersionName, err)
	}
	featureGate, err := client.ConfigV1().FeatureGates().Get(ctx, featureGateResourceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get featuregates.config.openshift.io/%s: %w", featureGateResourceName, err)
	}

	access, err := featuregates.NewHardcodedFeatureGateAccessFromFeatureGate(featureGate, payloadVersion(clusterVersion))
	if err != nil {
		return nil, err
	}
	return access.CurrentFeatureGates()
}

// payloadVersion is the version library-go selects when both FeatureGateAccess
// version arguments are the OLM marker: status.desired.version, then the
// newest history entry when desired is empty.
func payloadVersion(clusterVersion *configv1.ClusterVersion) string {
	version := clusterVersion.Status.Desired.Version
	if version == "" && len(clusterVersion.Status.History) > 0 {
		version = clusterVersion.Status.History[0].Version
	}
	return version
}

// TLSGroupPreferencesFlipped reports whether TLSGroupPreferences changed its
// enabled state. Absence from both lists is disabled, matching startup. The
// initial callback (Previous == nil) is ignored, as are changes to any other
// feature gate.
func TLSGroupPreferencesFlipped(change featuregates.FeatureChange) bool {
	if change.Previous == nil {
		return false
	}
	return tlsGroupPreferencesEnabled(*change.Previous) != tlsGroupPreferencesEnabled(change.New)
}

// tlsGroupPreferencesEnabled reports the same answer as FeatureGate.Enabled
// when the gate is listed. An unlisted gate is disabled. Enabled wins when a
// name is present in both lists, which is how library-go evaluates it.
func tlsGroupPreferencesEnabled(featuresList featuregates.Features) bool {
	return slices.Contains(featuresList.Enabled, features.FeatureGateTLSGroupPreferences)
}
