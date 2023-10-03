# secrets-store-csi-driver-operator

An operator to deploy the [Secrets Store CSI Driver](https://github.com/openshift/secrets-store-csi-driver).

# Quick start

To build and run the operator locally:

```shell
# Create only the resources the operator needs to run via CLI
oc apply -f - <<EOF
apiVersion: operator.openshift.io/v1
kind: ClusterCSIDriver
metadata:
    name: secrets-store.csi.k8s.io
spec:
  logLevel: Normal
  managementState: Managed
  operatorLogLevel: Trace
EOF

# Build the operator
make

# Set the environment variables
export OPERATOR_NAME=secrets-store-csi-driver-operator
export DRIVER_IMAGE=registry.k8s.io/csi-secrets-store/driver:v1.3.3
export NODE_DRIVER_REGISTRAR_IMAGE=quay.io/openshift/origin-csi-node-driver-registrar:latest
export LIVENESS_PROBE_IMAGE=quay.io/openshift/origin-csi-livenessprobe:latest

# Run the operator via CLI
./secrets-store-csi-driver-operator start --kubeconfig $KUBECONFIG --namespace openshift-cluster-csi-drivers
```

# OLM

To build bundle and index images, use the `hack/create-bundle` script:

```shell
cd hack
./create-bundle registry.ci.openshift.org/ocp/4.15:secrets-store-csi-driver registry.ci.openshift.org/ocp/4.15:secrets-store-csi-driver-operator quay.io/<my_user>/secrets-store-bundle quay.io/<my_user>/secrets-store-index
```

At the end it will print a command that creates `Subscription` for the newly created index image.

# Using the must-gather image

The `must-gather` image for secrets-store-csi-driver-operator supplements the [openshift/must-gather](https://github.com/openshift/must-gather) image to gather Secrets Store related resources.

```shell
oc adm must-gather --image=quay.io/openshift/origin-secrets-store-csi-mustgather:latest
```

This command creates a must-gather containing:
- Logs and resources in the operator namespace (`openshift-cluster-csi-drivers`)
- `SecretProviderClass` and `SecretProviderClassPodStatus` objects
- `ClusterCSIDriver` and `CSIDriver` objects

To build the `must-gather` image locally:

```shell
REPO=quay.io/<user>/secrets-store-csi-mustgather:latest
docker build -t ${REPO} -f Dockerfile.mustgather .
```
