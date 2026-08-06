package tls

import (
	"context"
	"fmt"
	"os"
	"time"

	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
	libgoclient "github.com/openshift/library-go/pkg/config/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	sigsyaml "sigs.k8s.io/yaml"
)

// ResolveFromCluster builds a kube client the same way Controllercmd does
// (GetKubeConfigOrInClusterConfig) and fetches the operator's effective TLS
// settings from apiserver.config.openshift.io/cluster.
//
// Client construction and the APIServer Get are retried with exponential
// backoff for ~30s. rest.InClusterConfig can transiently fail to read projected
// SA token/CA files right after scheduling, and the API server can be briefly
// unreachable during a rollout. Failure after the budget is fatal: this
// operator must not silently serve TLS settings it could not read.
func ResolveFromCluster(ctx context.Context, kubeConfigFile, userAgent string) (ResolvedProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var resolved ResolvedProfile
	var lastErr error
	attempt := func(ctx context.Context) (bool, error) {
		restConfig, err := libgoclient.GetKubeConfigOrInClusterConfig(kubeConfigFile, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to build kubeconfig: %w", err)
		} else if configClient, cErr := configclient.NewForConfig(rest.AddUserAgent(restConfig, userAgent)); cErr != nil {
			lastErr = fmt.Errorf("failed to create config client: %w", cErr)
		} else {
			resolved, lastErr = FetchAndResolve(ctx, configClient.ConfigV1())
		}
		if lastErr != nil {
			klog.Warningf("failed to resolve cluster TLS security profile, will retry: %v", lastErr)
			return false, nil
		}
		return true, nil
	}

	// ExponentialBackoffWithContext stops sleeping when ctx's timeout fires.
	// attempt reports failures as (false, nil) so backoff keeps retrying;
	// lastErr carries the real failure to the return below.
	backoff := wait.Backoff{Duration: 2 * time.Second, Factor: 2, Steps: 4}
	if err := wait.ExponentialBackoffWithContext(ctx, backoff, attempt); err != nil {
		if lastErr != nil {
			return ResolvedProfile{}, lastErr
		}
		return ResolvedProfile{}, err
	}
	return resolved, nil
}

// WriteConfigFile generates a GenericOperatorConfig carrying resolved's TLS
// settings, writes it to a uniquely-named temp file (the "*" in the pattern
// keeps the .yaml extension), and returns its path for the caller to point
// --config at. Returns "" without creating a file when resolved isn't honored,
// since ServingInfo would not carry anything worth persisting.
func WriteConfigFile(resolved ResolvedProfile) (string, error) {
	if !resolved.Honor {
		return "", nil
	}

	config := &operatorv1alpha1.GenericOperatorConfig{
		TypeMeta: metav1.TypeMeta{APIVersion: operatorv1alpha1.GroupVersion.String(), Kind: "GenericOperatorConfig"},
	}
	ApplyToServingInfo(&config.ServingInfo, resolved)

	content, err := sigsyaml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal operator config: %w", err)
	}
	tmpFile, err := os.CreateTemp("", "sscsi-operator-config-*.yaml")
	if err != nil {
		return "", fmt.Errorf("failed to create temp config file: %w", err)
	}
	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("failed to write temp config file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp config file: %w", err)
	}
	return tmpFile.Name(), nil
}
