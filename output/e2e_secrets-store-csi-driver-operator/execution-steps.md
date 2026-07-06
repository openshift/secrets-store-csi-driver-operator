# E2E Execution Steps: secrets-store-csi-driver-operator

## Prerequisites

```bash
which oc
oc version
oc whoami
oc get nodes
oc get clusterversion
# Verify operator is installed
oc get deployment -n openshift-cluster-csi-drivers
oc get clustercsidriver secrets-store.csi.k8s.io
```

## Environment Variables

```bash
export E2E_PROVIDER_NAMESPACE=openshift-cluster-csi-drivers
export PROVISIONER_NAME=secrets-store.csi.k8s.io
export E2E_DAEMONSET_NAME=secrets-store-csi-driver-node
export E2E_DRIVER_CONTAINER=csi-driver
```

## Step 1: Verify Baseline

```bash
oc get csidriver ${PROVISIONER_NAME} -o yaml
oc get daemonset ${E2E_DAEMONSET_NAME} -n ${E2E_PROVIDER_NAMESPACE}
oc get pods -n ${E2E_PROVIDER_NAMESPACE} -l app=${E2E_DAEMONSET_NAME}
```

## Step 2: Run Existing E2E Suite (baseline)

```bash
cd <repo-root>
make test-e2e
# or directly:
bash hack/e2e.sh
```

## Step 3: SC-001 — Disable Secret Rotation

```bash
oc apply -f - <<'EOF'
apiVersion: operator.openshift.io/v1
kind: ClusterCSIDriver
metadata:
  name: secrets-store.csi.k8s.io
spec:
  managementState: Managed
  driverConfig:
    driverType: SecretsStore
    secretsStore:
      secretRotation:
        type: None
EOF

oc rollout status daemonset/secrets-store-csi-driver-node \
  -n openshift-cluster-csi-drivers --timeout=120s

# Verify args on all pods
oc get pods -n openshift-cluster-csi-drivers -l app=secrets-store-csi-driver-node \
  -o 'jsonpath={range .items[*]}{.metadata.name}{": "}{.spec.containers[?(@.name=="csi-driver")].args}{"\n"}{end}'

# Verify CSIDriver.spec.requiresRepublish
oc get csidriver secrets-store.csi.k8s.io -o jsonpath='{.spec.requiresRepublish}'
```

Expected: every pod shows `--enable-secret-rotation=false`; `requiresRepublish` is `false` or absent.

## Step 4: SC-002 — Custom Rotation Interval (300s)

```bash
oc apply -f - <<'EOF'
apiVersion: operator.openshift.io/v1
kind: ClusterCSIDriver
metadata:
  name: secrets-store.csi.k8s.io
spec:
  managementState: Managed
  driverConfig:
    driverType: SecretsStore
    secretsStore:
      secretRotation:
        type: Custom
        custom:
          rotationPollIntervalSeconds: 300
EOF

oc rollout status daemonset/secrets-store-csi-driver-node \
  -n openshift-cluster-csi-drivers --timeout=120s

oc get pods -n openshift-cluster-csi-drivers -l app=secrets-store-csi-driver-node \
  -o 'jsonpath={range .items[*]}{.metadata.name}{": "}{.spec.containers[?(@.name=="csi-driver")].args}{"\n"}{end}'
```

Expected: every pod shows `--rotation-poll-interval=5m0s` and `--enable-secret-rotation=true`.

## Step 5: SC-003 — Managed WIF Audiences

```bash
oc apply -f - <<'EOF'
apiVersion: operator.openshift.io/v1
kind: ClusterCSIDriver
metadata:
  name: secrets-store.csi.k8s.io
spec:
  managementState: Managed
  driverConfig:
    driverType: SecretsStore
    secretsStore:
      tokenRequests:
        type: Managed
        managed:
          audiences:
            - audience: "sts.amazonaws.com"
            - audience: "api://AzureADTokenExchange"
EOF

sleep 5
oc get csidriver secrets-store.csi.k8s.io \
  -o 'jsonpath={.spec.tokenRequests}' | python3 -m json.tool
```

Expected: `spec.tokenRequests` contains both `sts.amazonaws.com` and `api://AzureADTokenExchange`.

## Step 6: SC-004 — Upgrade No-Op

```bash
oc patch clustercsidriver secrets-store.csi.k8s.io \
  --type=merge -p '{"spec":{"driverConfig":null}}'

DS_GEN_BEFORE=$(oc get daemonset secrets-store-csi-driver-node \
  -n openshift-cluster-csi-drivers \
  -o jsonpath='{.metadata.generation}')
echo "Generation before: ${DS_GEN_BEFORE}"

# Restart operator
oc delete pods -n openshift-cluster-csi-drivers \
  -l name=secrets-store-csi-driver-operator
sleep 15

DS_GEN_AFTER=$(oc get daemonset secrets-store-csi-driver-node \
  -n openshift-cluster-csi-drivers \
  -o jsonpath='{.metadata.generation}')
echo "Generation after:  ${DS_GEN_AFTER}"
[ "${DS_GEN_BEFORE}" = "${DS_GEN_AFTER}" ] && echo "PASS: no rollout" || echo "FAIL: unexpected rollout"
```

## Step 7: SC-005 — Unmanaged TokenRequests Preserved

```bash
oc apply -f - <<'EOF'
apiVersion: operator.openshift.io/v1
kind: ClusterCSIDriver
metadata:
  name: secrets-store.csi.k8s.io
spec:
  managementState: Managed
  driverConfig:
    driverType: SecretsStore
    secretsStore:
      tokenRequests:
        type: Unmanaged
EOF

oc patch csidriver secrets-store.csi.k8s.io --type=merge \
  -p '{"spec":{"tokenRequests":[{"audience":"manual-audience-test"}]}}'

oc annotate clustercsidriver secrets-store.csi.k8s.io \
  e2e-test/force-reconcile="$(date +%s)" --overwrite
sleep 10

oc get csidriver secrets-store.csi.k8s.io \
  -o jsonpath='{.spec.tokenRequests[0].audience}'
# Expected: "manual-audience-test"
```

## Step 8: SC-006 — API Immutability (manual / informational)

```bash
# First set Managed
oc apply -f - <<'EOF'
apiVersion: operator.openshift.io/v1
kind: ClusterCSIDriver
metadata:
  name: secrets-store.csi.k8s.io
spec:
  managementState: Managed
  driverConfig:
    driverType: SecretsStore
    secretsStore:
      tokenRequests:
        type: Managed
        managed:
          audiences:
            - audience: "sts.amazonaws.com"
EOF
sleep 5

# Attempt to revert to Unmanaged — expect 422
oc patch clustercsidriver secrets-store.csi.k8s.io --type=merge \
  -p '{"spec":{"driverConfig":{"secretsStore":{"tokenRequests":{"type":"Unmanaged"}}}}}'
# Expected: Error from server (Invalid): ... CEL validation
# If it succeeds: gap documented — follow-up against openshift/api
```

## Step 9: Restore and Cleanup

```bash
oc patch clustercsidriver secrets-store.csi.k8s.io \
  --type=merge -p '{"spec":{"driverConfig":null}}'
oc rollout status daemonset/secrets-store-csi-driver-node \
  -n openshift-cluster-csi-drivers --timeout=120s
```
