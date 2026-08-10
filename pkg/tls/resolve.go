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
// Client construction and the APIServer Get are retried for ~30s.
// rest.InClusterConfig can transiently fail to read projected SA token/CA
// files right after scheduling, and the API server can be briefly unreachable
// during a rollout. Failure after the budget is fatal: this operator must not
// silently serve TLS settings it could not read.
func ResolveFromCluster(ctx context.Context, kubeConfigFile, userAgent string) (ResolvedProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var resolved ResolvedProfile
	var lastErr error
	attempt := func(ctx context.Context) (bool, error) {
		failureClass := ""
		restConfig, err := libgoclient.GetKubeConfigOrInClusterConfig(kubeConfigFile, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to build kubeconfig: %w", err)
			failureClass = "kubeconfig build failed"
		} else if configClient, cErr := configclient.NewForConfig(rest.AddUserAgent(restConfig, userAgent)); cErr != nil {
			lastErr = fmt.Errorf("failed to create config client: %w", cErr)
			failureClass = "config client creation failed"
		} else {
			resolved, lastErr = FetchAndResolve(ctx, configClient.ConfigV1())
			if lastErr != nil {
				failureClass = "APIServer TLS profile fetch or resolve failed"
			}
		}
		if lastErr != nil {
			klog.Warningf("failed to resolve cluster TLS security profile, will retry: %s", failureClass)
			return false, nil
		}
		return true, nil
	}

	// PollUntilContextCancel retries until success or the 30s context deadline.
	// attempt reports failures as (false, nil) so polling continues;
	// lastErr carries the real failure to the return below.
	if err := wait.PollUntilContextCancel(ctx, 2*time.Second, true, attempt); err != nil {
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
	if err := ApplyToServingInfo(&config.ServingInfo, resolved); err != nil {
		return "", err
	}

	content, err := sigsyaml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal operator config: %w", err)
	}
	tmpFile, err := os.CreateTemp("", "sscsi-operator-config-*.yaml")
	if err != nil {
		return "", fmt.Errorf("failed to create temp config file: %w", err)
	}
	if _, err := tmpFile.Write(content); err != nil {
		name := tmpFile.Name()
		if closeErr := tmpFile.Close(); closeErr != nil {
			klog.Warningf("failed to close incomplete temp config file %q: %v", name, closeErr)
		}
		if removeErr := os.Remove(name); removeErr != nil && !os.IsNotExist(removeErr) {
			klog.Warningf("failed to remove incomplete temp config file %q: %v", name, removeErr)
		}
		return "", fmt.Errorf("failed to write temp config file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp config file: %w", err)
	}
	return tmpFile.Name(), nil
}
