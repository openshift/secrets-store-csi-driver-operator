package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	opv1 "github.com/openshift/api/operator/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	pollInterval = 2 * time.Second
	pollTimeout  = 5 * time.Minute
	// apiCallTimeout bounds every individual API request this suite makes
	apiCallTimeout = 30 * time.Second
)

// withAPITimeout returns a context bounded by apiCallTimeout. Callers must
// defer the returned cancel func.
func withAPITimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), apiCallTimeout)
}

// setSecretsStoreConfig patches the ClusterCSIDriver's driverConfig to
// driverType SecretsStore with the given secretsStore config. Callers that
// only want to change one of secretRotation/tokenRequests should use
// setSecretRotation/setTokenRequests instead, which read-modify-write
// through the current live value of the field they're not changing --
// calling this directly with only one field set would blow away the
// other, since setDriverConfig replaces driverConfig wholesale (see its
// doc comment for why a partial/merge update isn't safe here either).
func setSecretsStoreConfig(secretsStore opv1.SecretsStoreCSIDriverConfigSpec) {
	setDriverConfig(opv1.CSIDriverConfigSpec{
		DriverType:   opv1.SecretsStoreDriverType,
		SecretsStore: secretsStore,
	})
}

// clearDriverConfig removes driverConfig entirely, restoring "omitted"
// (no opinion) behavior.
func clearDriverConfig() {
	setDriverConfig(opv1.CSIDriverConfigSpec{})
}

// setSecretsStoreField read-modify-writes a single field of the live
// SecretsStoreCSIDriverConfigSpec via mutate, preserving every other
// field (notably whatever tokenRequests/secretRotation configuration is
// currently live) exactly as-is. If the result is the zero value, this
// clears driverConfig entirely instead of sending
// {driverType: SecretsStore, secretsStore: {}}, since the CSIDriverConfigSpec
// union CEL rule requires secretsStore to be present precisely when
// driverType is "SecretsStore".
func setSecretsStoreField(mutate func(*opv1.SecretsStoreCSIDriverConfigSpec)) {
	ctx, cancel := withAPITimeout()
	defer cancel()
	driver, err := clusterCSIDriverClient.Get(ctx, driverName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to get ClusterCSIDriver %q", driverName)

	secretsStore := driver.Spec.DriverConfig.SecretsStore
	mutate(&secretsStore)
	if secretsStore == (opv1.SecretsStoreCSIDriverConfigSpec{}) {
		clearDriverConfig()
		return
	}
	setSecretsStoreConfig(secretsStore)
}

// setSecretRotation sets driverConfig.secretsStore.secretRotation to want
// while preserving whatever tokenRequests configuration is currently live.
//
// secret_rotation_test.go's specs use this (instead of calling
// setSecretsStoreConfig directly) so they behave correctly regardless of
// Ginkgo's (randomized) ordering relative to token_requests_test.go: once
// tokenRequests.type has been permanently transitioned to Managed,
// omitting tokenRequests from the desired driverConfig is rejected by the
// CEL immutability rule ("tokenRequests type cannot be changed from
// Managed"), since that would implicitly revert tokenRequests away from
// Managed too.
func setSecretRotation(want opv1.SecretsStoreSecretRotation) {
	setSecretsStoreField(func(s *opv1.SecretsStoreCSIDriverConfigSpec) {
		s.SecretRotation = want
	})
}

// clearSecretRotation resets secretRotation to its omitted (zero) value.
// See setSecretRotation.
func clearSecretRotation() {
	setSecretRotation(opv1.SecretsStoreSecretRotation{})
}

// setTokenRequests sets driverConfig.secretsStore.tokenRequests to want
// while preserving whatever secretRotation configuration is currently
// live. Symmetric to setSecretRotation, for the same reason: leaving
// secretRotation out of the desired driverConfig would clear it, which is
// wrong if secret_rotation_test.go's specs happen to run first.
func setTokenRequests(want opv1.SecretsStoreTokenRequests) {
	setSecretsStoreField(func(s *opv1.SecretsStoreCSIDriverConfigSpec) {
		s.TokenRequests = want
	})
}

// setDriverConfig sets spec.driverConfig to exactly want, retrying on
// conflict.
//
// This uses a JSON Patch (RFC 6902) "add" or "remove" op targeting the
// whole /spec/driverConfig value, rather than a JSON merge patch (RFC
// 7396). A merge patch recurses into nested objects and only touches keys
// explicitly present in the patch body, so e.g. setting
// secretsStore.secretRotation={type: None} via a merge patch would leave a
// pre-existing secretsStore.secretRotation.custom block untouched --
// producing an invalid {type: None, custom: {...}} combination that the
// API rejects. "add"/"remove" replace the entire value at the path
// atomically, which correctly clears sibling fields that aren't part of
// want.
func setDriverConfig(want opv1.CSIDriverConfigSpec) {
	By(fmt.Sprintf("setting ClusterCSIDriver %q driverConfig to %+v", driverName, want))

	attempt := 0
	Eventually(func() error {
		attempt++
		ctx, cancel := withAPITimeout()
		defer cancel()
		driver, err := clusterCSIDriverClient.Get(ctx, driverName, metav1.GetOptions{})
		if err != nil {
			GinkgoWriter.Printf("[setDriverConfig attempt %d] get failed: %v\n", attempt, err)
			return err
		}
		if driver.Spec.DriverConfig == want {
			return nil
		}

		patch, err := driverConfigJSONPatch(want)
		if err != nil {
			return err
		}
		ctx2, cancel2 := withAPITimeout()
		defer cancel2()
		_, err = clusterCSIDriverClient.Patch(ctx2, driverName, types.JSONPatchType, patch, metav1.PatchOptions{})
		if err != nil {
			GinkgoWriter.Printf("[setDriverConfig attempt %d] patch failed: %v\n", attempt, err)
		}
		return err
	}, pollTimeout, pollInterval).Should(Succeed(), "failed to update ClusterCSIDriver %q driverConfig", driverName)
}

// driverConfigJSONPatch builds a JSON Patch (RFC 6902) document that
// replaces spec.driverConfig with want wholesale: "remove" when want is
// the zero value (restoring the "omitted" state), otherwise "add" (which,
// per RFC 6902, both creates the member if absent and overwrites it if
// present -- unlike "replace", which requires the path to already exist).
func driverConfigJSONPatch(want opv1.CSIDriverConfigSpec) ([]byte, error) {
	if want == (opv1.CSIDriverConfigSpec{}) {
		return []byte(`[{"op":"remove","path":"/spec/driverConfig"}]`), nil
	}
	value, err := json.Marshal(want)
	if err != nil {
		return nil, err
	}
	return json.Marshal([]map[string]any{
		{"op": "add", "path": "/spec/driverConfig", "value": json.RawMessage(value)},
	})
}

// waitForRequiresRepublish polls the live CSIDriver object until
// spec.requiresRepublish equals want.
func waitForRequiresRepublish(want bool) {
	By(fmt.Sprintf("waiting for CSIDriver %q requiresRepublish to converge to %t", driverName, want))
	attempt := 0
	Eventually(func() (bool, error) {
		attempt++
		ctx, cancel := withAPITimeout()
		defer cancel()
		driver, err := kubeClient.StorageV1().CSIDrivers().Get(ctx, driverName, metav1.GetOptions{})
		if err != nil {
			GinkgoWriter.Printf("[waitForRequiresRepublish attempt %d] get failed: %v\n", attempt, err)
			return false, err
		}
		GinkgoWriter.Printf("[waitForRequiresRepublish attempt %d] observed=%v want=%t\n", attempt, ptrBoolStr(driver.Spec.RequiresRepublish), want)
		return driver.Spec.RequiresRepublish != nil && *driver.Spec.RequiresRepublish == want, nil
	}, pollTimeout, pollInterval).Should(BeTrue(), "CSIDriver %q requiresRepublish did not converge to %t", driverName, want)
}

// ptrBoolStr renders a *bool for log lines, without panicking on nil.
func ptrBoolStr(b *bool) string {
	if b == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%t", *b)
}

// waitForTokenRequests polls the live CSIDriver object until its
// tokenRequests exactly match want. A nil/empty want means "no
// tokenRequests".
func waitForTokenRequests(want []storagev1.TokenRequest) {
	wantSorted := sortedTokenRequests(want)
	By(fmt.Sprintf("waiting for CSIDriver %q tokenRequests to converge to %+v", driverName, wantSorted))
	attempt := 0
	Eventually(func() ([]storagev1.TokenRequest, error) {
		attempt++
		ctx, cancel := withAPITimeout()
		defer cancel()
		driver, err := kubeClient.StorageV1().CSIDrivers().Get(ctx, driverName, metav1.GetOptions{})
		if err != nil {
			GinkgoWriter.Printf("[waitForTokenRequests attempt %d] get failed: %v\n", attempt, err)
			return nil, err
		}
		got := sortedTokenRequests(driver.Spec.TokenRequests)
		GinkgoWriter.Printf("[waitForTokenRequests attempt %d] observed=%+v want=%+v\n", attempt, got, wantSorted)
		return got, nil
	}, pollTimeout, pollInterval).Should(Equal(wantSorted), "CSIDriver %q tokenRequests did not converge to %+v", driverName, want)
}

// sortedTokenRequests returns a copy of trs sorted by audience, for
// order-independent comparison. Like append([]T(nil), trs...) generally,
// this collapses a nil/empty input to nil, so "no tokenRequests" compares
// equal regardless of which side it came from.
func sortedTokenRequests(trs []storagev1.TokenRequest) []storagev1.TokenRequest {
	out := append([]storagev1.TokenRequest(nil), trs...)
	sort.Slice(out, func(i, j int) bool { return out[i].Audience < out[j].Audience })
	return out
}

func audiencesOf(trs []storagev1.TokenRequest) []string {
	audiences := make([]string, 0, len(trs))
	for _, tr := range trs {
		audiences = append(audiences, tr.Audience)
	}
	return audiences
}

// waitForDaemonSetArgs polls the node DaemonSet's csi-driver container
// until, for every prefix in wantArgs, an arg with that prefix and the
// corresponding value is present.
func waitForDaemonSetArgs(wantArgs map[string]string) {
	By(fmt.Sprintf("waiting for DaemonSet %s/%s csi-driver args to converge to %v", operatorNamespace, daemonSetName, wantArgs))
	attempt := 0
	Eventually(func() (map[string]string, error) {
		attempt++
		ctx, cancel := withAPITimeout()
		defer cancel()
		ds, err := kubeClient.AppsV1().DaemonSets(operatorNamespace).Get(ctx, daemonSetName, metav1.GetOptions{})
		if err != nil {
			GinkgoWriter.Printf("[waitForDaemonSetArgs attempt %d] get failed: %v\n", attempt, err)
			return nil, err
		}
		for _, c := range ds.Spec.Template.Spec.Containers {
			if c.Name != csiDriverContainer {
				continue
			}
			got := map[string]string{}
			for prefix := range wantArgs {
				got[prefix] = argValue(c.Args, prefix)
			}
			GinkgoWriter.Printf("[waitForDaemonSetArgs attempt %d] observed=%v want=%v generation=%d observedGeneration=%d updated=%d/%d available=%d\n",
				attempt, got, wantArgs, ds.Generation, ds.Status.ObservedGeneration, ds.Status.UpdatedNumberScheduled, ds.Status.DesiredNumberScheduled, ds.Status.NumberAvailable)
			return got, nil
		}
		return nil, fmt.Errorf("container %q not found in DaemonSet %s/%s", csiDriverContainer, operatorNamespace, daemonSetName)
	}, pollTimeout, pollInterval).Should(Equal(wantArgs), "DaemonSet %s/%s args did not converge", operatorNamespace, daemonSetName)
}

// argValue returns the value portion of the first arg starting with
// prefix, or "" if none matches.
func argValue(args []string, prefix string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	return ""
}
