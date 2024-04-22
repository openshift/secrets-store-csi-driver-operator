#!/bin/bash

set -o xtrace
set -o nounset
set -o pipefail

# The operator, driver, and e2e-provider pods must already be deployed on the cluster
# before running this test script.
export KUBECONFIG=${KUBECONFIG:-$HOME/.kube/config}
export E2E_PROVIDER_NAMESPACE=${E2E_PROVIDER_NAMESPACE:-openshift-cluster-csi-drivers}
export E2E_PROVIDER_APP_LABEL=${E2E_PROVIDER_APP_LABEL:-csi-secrets-store-e2e-provider}
export E2E_PROVIDER_SELECTOR="app=${E2E_PROVIDER_APP_LABEL}"
export PROVISIONER_NAME="secrets-store.csi.k8s.io"

# The test namespace is created with a "random" postfix
POSTFIX_CHARS=$(echo $RANDOM | md5sum | head -c5)
export E2E_TEST_NAMESPACE=secrets-store-test-ns-${POSTFIX_CHARS}
export E2E_TEST_SERVICEACCOUNT_NAME=default
export E2E_TEST_SERVICEACCOUNT=system:serviceaccount:${E2E_TEST_NAMESPACE}:${E2E_TEST_SERVICEACCOUNT_NAME}
export E2E_TEST_PROVIDER=e2e-provider
export E2E_TEST_IMAGE=quay.io/openshifttest/busybox:multiarch
export E2E_TEST_POD_TIMEOUT=120 # seconds
export E2E_TEST_CONTAINER_NAME=test-container

# Check that CSI Driver and E2E Provider pods exist
test_prechecks() {
	echo "Running test prechecks"
	oc get csidriver ${PROVISIONER_NAME} || return 1
	oc wait pod -n ${E2E_PROVIDER_NAMESPACE} --selector=${E2E_PROVIDER_SELECTOR} --for=condition=Ready --timeout=30s || return 1
	echo "test_prechecks PASSED"
	return 0
}

test_setup() {
	echo "Creating test namespace"
	oc new-project ${E2E_TEST_NAMESPACE} || return 1

	# Allow creation of privileged pods for this test. The e2e-provider must be
	# privileged to bind to a unix domain socket on the host, and the test pod
	# must be privileged to read files created by the e2e-provider.
	oc adm policy add-scc-to-user privileged ${E2E_TEST_SERVICEACCOUNT} || return 1
	oc label ns ${E2E_TEST_NAMESPACE} security.openshift.io/scc.podSecurityLabelSync=false pod-security.kubernetes.io/enforce=privileged pod-security.kubernetes.io/audit=privileged pod-security.kubernetes.io/warn=privileged --overwrite || return 1

	echo "Creating SecretProviderClass"
	oc apply -f - <<EOF
apiVersion: secrets-store.csi.x-k8s.io/v1
kind: SecretProviderClass
metadata:
  name: ${E2E_TEST_PROVIDER}
  namespace: ${E2E_TEST_NAMESPACE}
spec:
  provider: ${E2E_TEST_PROVIDER}
  parameters:
    objects: |
      array:
        - |
          objectName: foo
          objectVersion: v1
        - |
          objectName: fookey
          objectVersion: v1
EOF
	return $?
}

test_teardown() {
	echo "Deleting test namespace"
	oc delete project ${E2E_TEST_NAMESPACE}
	return $?
}

test_pods_dump() {
	echo "Describing pods in namespace ${E2E_TEST_NAMESPACE}"
	oc describe pods -n ${E2E_TEST_NAMESPACE}
	oc get pods -n ${E2E_TEST_NAMESPACE} -o yaml
	return 0
}

test_pod_create() {
	local TEST_POD_NAME=$1
	echo "Creating test pod ${TEST_POD_NAME}"
	oc apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: ${TEST_POD_NAME}
  namespace: ${E2E_TEST_NAMESPACE}
  labels:
    name: ${TEST_POD_NAME}
spec:
  serviceAccountName: ${E2E_TEST_SERVICEACCOUNT_NAME}
  containers:
  - name: ${E2E_TEST_CONTAINER_NAME}
    image: ${E2E_TEST_IMAGE}
    command:
    - sh
    - -c
    - cat /mnt/test-vol/foo && sleep ${E2E_TEST_POD_TIMEOUT}
    securityContext:
      privileged: true
    volumeMounts:
    - mountPath: /mnt/test-vol
      name: test-vol
      readOnly: true
    terminationMessagePolicy: FallbackToLogsOnError
  volumes:
  - csi:
      driver: ${PROVISIONER_NAME}
      readOnly: true
      volumeAttributes:
        secretProviderClass: ${E2E_TEST_PROVIDER}
    name: test-vol
EOF
	return $?
}

test_pod_delete() {
	local TEST_POD_NAME=$1
	echo "Deleting test pod ${TEST_POD_NAME}"
	oc delete pod/${TEST_POD_NAME} -n ${E2E_TEST_NAMESPACE}
	return $?
}

test_pod_wait() {
	local TEST_POD_NAME=$1
	echo "Waiting for pod ${TEST_POD_NAME} to be ready"
	oc wait pod -n ${E2E_TEST_NAMESPACE} --selector=name=${TEST_POD_NAME} --for=condition=Ready --timeout=${E2E_TEST_POD_TIMEOUT}s
	return $?
}

test_pod_log_check() {
	local TEST_POD_NAME=$1
	echo "Checking logs of pod ${TEST_POD_NAME} for secret value"
	LOG_CONTENTS=$(oc logs pod/${TEST_POD_NAME} -n ${E2E_TEST_NAMESPACE} -c ${E2E_TEST_CONTAINER_NAME})
	EXPECTED_VALUE=secret
	if [ "${LOG_CONTENTS}" != "${EXPECTED_VALUE}" ]; then
		echo "Log contents do not match expected value: ${EXPECTED_VALUE}"
		return 1
	fi
	return 0
}

test_pod_with_secret() {
	local TEST_POD_NAME=test-pod-with-secret
	test_pod_create ${TEST_POD_NAME} || return 1
	test_pod_wait ${TEST_POD_NAME} || return 1
	test_pods_dump
	test_pod_log_check ${TEST_POD_NAME} || return 1
	test_pod_delete ${TEST_POD_NAME} || return 1
	echo "test_pod_with_secret PASSED"
	return 0
}

test_prechecks
if [ $? -ne 0 ]; then
	echo "test_prechecks FAILED"
	exit 1
fi

test_setup
if [ $? -ne 0 ]; then
	echo "test_setup FAILED"
	test_teardown
	exit 1
fi

test_pod_with_secret
if [ $? -ne 0 ]; then
	echo "test_pod_with_secret FAILED"
	test_pods_dump
	test_teardown
	exit 1
fi

test_teardown
if [ $? -ne 0 ]; then
	echo "test_teardown FAILED"
	exit 1
fi

echo "All tests PASSED"
exit 0
