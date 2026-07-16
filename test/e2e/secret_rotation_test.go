package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	opv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("secret rotation", func() {
	BeforeEach(clearSecretRotation)
	AfterEach(clearSecretRotation)

	It("uses the pre-feature defaults when driverConfig.secretsStore is omitted", func() {
		// clearSecretRotation in BeforeEach already omitted secretRotation;
		// nothing further to configure.
		waitForRequiresRepublish(true)
		waitForDaemonSetArgs(map[string]string{
			"--enable-secret-rotation=": "true",
			"--rotation-poll-interval=": "2m",
		})
	})

	It("uses the given interval when secretRotation.type is Custom with an explicit minimumRefreshAge", func() {
		setSecretRotation(opv1.SecretsStoreSecretRotation{
			Type: opv1.SecretRotationCustom,
			Custom: opv1.CustomSecretRotation{
				MinimumRefreshAge: 300, // 5 minutes
			},
		})

		waitForRequiresRepublish(true)
		waitForDaemonSetArgs(map[string]string{
			"--enable-secret-rotation=": "true",
			"--rotation-poll-interval=": "5m",
		})
	})

	It("rejects secretRotation.type Custom without a custom block", func() {
		ctx, cancel := withAPITimeout()
		defer cancel()
		driver, err := clusterCSIDriverClient.Get(ctx, driverName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		driver.Spec.DriverConfig.DriverType = opv1.SecretsStoreDriverType
		driver.Spec.DriverConfig.SecretsStore.SecretRotation = opv1.SecretsStoreSecretRotation{
			Type: opv1.SecretRotationCustom,
			// custom intentionally omitted.
		}
		_, err = clusterCSIDriverClient.Update(ctx, driver, metav1.UpdateOptions{})

		Expect(err).To(HaveOccurred(), "expected the API server to reject secretRotation.type Custom without a custom block")
		Expect(err.Error()).To(ContainSubstring("custom must be set when type is 'Custom'"))
	})

	It("disables rotation and sets requiresRepublish=false when secretRotation.type is None", func() {
		setSecretRotation(opv1.SecretsStoreSecretRotation{
			Type: opv1.SecretRotationNone,
		})

		waitForRequiresRepublish(false)
		waitForDaemonSetArgs(map[string]string{
			"--enable-secret-rotation=": "false",
		})
	})

	It("re-enables rotation when toggled from None back to Custom", func() {
		setSecretRotation(opv1.SecretsStoreSecretRotation{
			Type: opv1.SecretRotationNone,
		})
		waitForRequiresRepublish(false)
		waitForDaemonSetArgs(map[string]string{
			"--enable-secret-rotation=": "false",
		})

		setSecretRotation(opv1.SecretsStoreSecretRotation{
			Type: opv1.SecretRotationCustom,
			Custom: opv1.CustomSecretRotation{
				MinimumRefreshAge: 60,
			},
		})

		waitForRequiresRepublish(true)
		waitForDaemonSetArgs(map[string]string{
			"--enable-secret-rotation=": "true",
			"--rotation-poll-interval=": "1m",
		})
	})
})
