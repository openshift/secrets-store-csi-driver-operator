//go:build e2e
// +build e2e

// Package e2e contains cluster end-to-end tests for the Secrets Store CSI Driver Operator.
//
// TLS profile coverage lives in tls_profile_test.go and mirrors the scenario matrix
// exercised live for SSCSI-264 (and the intent of openshift/cert-manager-operator#449),
// adapted to Controllercmd HTTPS metrics on :8443 rather than operand CLI arg injection.
//
// Run:
//
//	make test-e2e-tls
//
// Prerequisites: kubeconfig with cluster-admin, operator installed in
// openshift-cluster-csi-drivers, and FeatureGate TLSAdherence (or equivalent) so
// apiserver.spec.tlsAdherence is served. The suite skips when tlsAdherence is
// unsupported.
package e2e
