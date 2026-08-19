package e2e

import (
	. "github.com/onsi/ginkgo/v2"
)

const (
	operatorDeploymentName = "secrets-store-csi-driver-operator"
	operatorAppLabel       = "app=secrets-store-csi-driver-operator"
	// apiServerRBACForbiddenLog is the reflector error emitted when the
	// operator SA lacks list/watch on apiservers.config.openshift.io.
	apiServerRBACForbiddenLog = "apiservers.config.openshift.io is forbidden"
)

var _ = Describe("operator RBAC", func() {
	It("does not log forbidden errors when watching apiservers.config.openshift.io", func() {
		waitForOperatorLogsWithoutSubstring(apiServerRBACForbiddenLog)
	})
})
