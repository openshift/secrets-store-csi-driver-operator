package e2e

import (
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	opv1 "github.com/openshift/api/operator/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// patchLiveCSIDriverTokenRequests directly sets spec.tokenRequests on the
// live storage.k8s.io/v1 CSIDriver object, bypassing ClusterCSIDriver, to
// simulate a pre-existing manual WIF configuration. It registers a
// DeferCleanup that restores whatever tokenRequests were present
// beforehand, so these preservation specs -- which assert the operator
// never touches this field -- don't leak manually-patched audiences into
// later runs.
func patchLiveCSIDriverTokenRequests(audiences []string) {
	ctx, cancel := withAPITimeout()
	defer cancel()
	driver, err := kubeClient.StorageV1().CSIDrivers().Get(ctx, driverName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to read CSIDriver %q before patching tokenRequests", driverName)
	original := driver.Spec.TokenRequests
	DeferCleanup(setLiveCSIDriverTokenRequests, original)

	trs := make([]storagev1.TokenRequest, 0, len(audiences))
	for _, a := range audiences {
		trs = append(trs, storagev1.TokenRequest{Audience: a})
	}
	setLiveCSIDriverTokenRequests(trs)
}

// setLiveCSIDriverTokenRequests sets spec.tokenRequests on the live
// storage.k8s.io/v1 CSIDriver object to trs, retrying on conflict.
func setLiveCSIDriverTokenRequests(trs []storagev1.TokenRequest) {
	Eventually(func() error {
		ctx, cancel := withAPITimeout()
		defer cancel()
		driver, err := kubeClient.StorageV1().CSIDrivers().Get(ctx, driverName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		driver.Spec.TokenRequests = trs
		_, err = kubeClient.StorageV1().CSIDrivers().Update(ctx, driver, metav1.UpdateOptions{})
		return err
	}, pollTimeout, pollInterval).Should(Succeed(), "failed to set CSIDriver %q tokenRequests to %+v", driverName, trs)
}

// liveTokenRequestAudiences returns the audiences currently on the live
// storage.k8s.io/v1 CSIDriver object.
func liveTokenRequestAudiences() ([]string, error) {
	ctx, cancel := withAPITimeout()
	defer cancel()
	driver, err := kubeClient.StorageV1().CSIDrivers().Get(ctx, driverName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return audiencesOf(driver.Spec.TokenRequests), nil
}

// Ordered guarantees the two Contexts below run in declaration order.
// This matters because tokenRequests.type: Managed is a one-way, CEL-enforced
// transition, so the Unmanaged/omitted-preservation Context must run before
// the Managed-transition Context -- otherwise it would already be permanently
// Managed by the time it runs.
var _ = Describe("tokenRequests", Ordered, func() {
	Context("preservation (Unmanaged / omitted)", func() {
		BeforeEach(clearDriverConfig)

		It("preserves existing tokenRequests when driverConfig.secretsStore is omitted (upgrade scenario)", func() {
			manualAudience := "manual-pre-existing-wif-fixture"
			patchLiveCSIDriverTokenRequests([]string{manualAudience})

			// driverConfig stays omitted for the whole check; the operator
			// must never overwrite the manually-patched tokenRequests.
			Consistently(liveTokenRequestAudiences, 30*time.Second, pollInterval).
				Should(Equal([]string{manualAudience}), "operator must preserve manually-patched tokenRequests when driverConfig is omitted")
		})

		It("preserves existing tokenRequests when tokenRequests.type is Unmanaged", func() {
			manualAudience := "manual-unmanaged-wif-fixture"
			patchLiveCSIDriverTokenRequests([]string{manualAudience})

			setTokenRequests(opv1.SecretsStoreTokenRequests{
				Type: opv1.TokenRequestsUnmanaged,
			})

			Consistently(liveTokenRequestAudiences, 30*time.Second, pollInterval).
				Should(Equal([]string{manualAudience}), "operator must preserve manually-patched tokenRequests when tokenRequests.type is Unmanaged")
		})
	})

	Context("transitioning to Managed (irreversible)", func() {
		BeforeEach(func() {
			if os.Getenv("RUN_IRREVERSIBLE_E2E") != "true" {
				Skip("set RUN_IRREVERSIBLE_E2E=true to run specs that permanently transition this cluster's ClusterCSIDriver tokenRequests.type to Managed -- this cannot be reverted afterward")
			}
		})

		It("sets tokenRequests from a single managed audience", func() {
			audience := "e2e-managed-single-audience"
			setTokenRequests(opv1.SecretsStoreTokenRequests{
				Type: opv1.TokenRequestsManaged,
				Managed: opv1.ManagedTokenRequests{
					Audiences: &[]opv1.SecretsStoreTokenRequest{
						{Audience: ptr.To(audience), ExpirationSeconds: 3600},
					},
				},
			})

			waitForTokenRequests([]storagev1.TokenRequest{
				{Audience: audience, ExpirationSeconds: ptr.To(int64(3600))},
			})
		})

		It("sets tokenRequests from multiple managed audiences (multi-cloud WIF)", func() {
			audience1 := "e2e-managed-audience-aws"
			audience2 := "e2e-managed-audience-azure"
			setTokenRequests(opv1.SecretsStoreTokenRequests{
				Type: opv1.TokenRequestsManaged,
				Managed: opv1.ManagedTokenRequests{
					Audiences: &[]opv1.SecretsStoreTokenRequest{
						{Audience: ptr.To(audience1), ExpirationSeconds: 7200},
						{Audience: ptr.To(audience2)},
					},
				},
			})

			waitForTokenRequests([]storagev1.TokenRequest{
				{Audience: audience1, ExpirationSeconds: ptr.To(int64(7200))},
				{Audience: audience2},
			})
		})

		It("clears tokenRequests when managed.audiences is an explicit empty list", func() {
			setTokenRequests(opv1.SecretsStoreTokenRequests{
				Type: opv1.TokenRequestsManaged,
				Managed: opv1.ManagedTokenRequests{
					Audiences: &[]opv1.SecretsStoreTokenRequest{},
				},
			})

			waitForTokenRequests(nil)
		})

		It("rejects an attempt to revert tokenRequests.type back to Unmanaged", func() {
			ctx, cancel := withAPITimeout()
			defer cancel()
			driver, err := clusterCSIDriverClient.Get(ctx, driverName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to read ClusterCSIDriver %q before attempting the reverting update", driverName)

			driver.Spec.DriverConfig = opv1.CSIDriverConfigSpec{
				DriverType: opv1.SecretsStoreDriverType,
				SecretsStore: opv1.SecretsStoreCSIDriverConfigSpec{
					TokenRequests: opv1.SecretsStoreTokenRequests{
						Type: opv1.TokenRequestsUnmanaged,
					},
				},
			}
			_, err = clusterCSIDriverClient.Update(ctx, driver, metav1.UpdateOptions{})

			Expect(err).To(HaveOccurred(), "expected the API server to reject reverting tokenRequests.type from Managed to Unmanaged")
			Expect(err.Error()).To(ContainSubstring("tokenRequests type cannot be changed from Managed"))
		})
	})
})
