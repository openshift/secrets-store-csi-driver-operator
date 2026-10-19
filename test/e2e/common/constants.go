// Package common holds constants shared by operator and cloud-provider e2e suites.
package common

import "time"

const (
	// DriverName is both the ClusterCSIDriver singleton's name and the
	// storage.k8s.io/v1 CSIDriver object's name.
	DriverName = "secrets-store.csi.k8s.io"
	// OperatorNamespace hosts the operator and its node DaemonSet.
	OperatorNamespace = "openshift-cluster-csi-drivers"
	// DaemonSetName is the driver's node DaemonSet.
	DaemonSetName = "secrets-store-csi-driver-node"
	// CSIDriverContainer is the driver container within the DaemonSet.
	CSIDriverContainer = "csi-driver"
	// TestImage is the multiarch busybox image used for e2e workload pods.
	TestImage = "quay.io/openshifttest/busybox:multiarch"

	PollInterval   = 2 * time.Second
	PollTimeout    = 5 * time.Minute
	APICallTimeout = 30 * time.Second
)
