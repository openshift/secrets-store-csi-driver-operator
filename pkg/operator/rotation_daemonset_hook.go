package operator

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	opv1 "github.com/openshift/api/operator/v1"
	operatorv1listers "github.com/openshift/client-go/operator/listers/operator/v1"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const (
	// csiDriverContainerName is the name of the driver container
	csiDriverContainerName = "csi-driver"
	// enableRotationArgPrefix is the csi-driver container's flag prefix that toggles secret rotation on/off.
	enableRotationArgPrefix = "--enable-secret-rotation="
	// rotationPollIntervalArgPrefix is the csi-driver container's flag prefix for the rotation poll interval.
	rotationPollIntervalArgPrefix = "--rotation-poll-interval="
)

// withSecretRotationDaemonSetHook returns a DaemonSetHookFunc that sets the
// csi-driver container's enableRotationArgPrefix and
// rotationPollIntervalArgPrefix args from the ClusterCSIDriver.
func withSecretRotationDaemonSetHook(clusterCSIDriverLister operatorv1listers.ClusterCSIDriverLister, driverName string) csidrivernodeservicecontroller.DaemonSetHookFunc {
	return func(_ *opv1.OperatorSpec, daemonSet *appsv1.DaemonSet) error {
		driverConfig, err := getClusterCSIDriverConfig(clusterCSIDriverLister, driverName)
		if err != nil {
			return err
		}
		enabled, interval := getSecretRotationConfig(driverConfig)
		klog.V(4).Infof("resolved secret rotation config for DaemonSet %s/%s: enabled=%t pollInterval=%s", daemonSet.Namespace, daemonSet.Name, enabled, formatRotationInterval(interval))

		container, err := findContainer(daemonSet, csiDriverContainerName)
		if err != nil {
			return err
		}

		container.Args = setArg(container.Args, enableRotationArgPrefix, strconv.FormatBool(enabled))
		container.Args = setArg(container.Args, rotationPollIntervalArgPrefix, formatRotationInterval(interval))

		return nil
	}
}

// findContainer returns a pointer to the named container within the
// DaemonSet's pod template so callers can mutate it in place, or an error if
// not found.
func findContainer(daemonSet *appsv1.DaemonSet, name string) (*corev1.Container, error) {
	containers := daemonSet.Spec.Template.Spec.Containers
	for i := range containers {
		if containers[i].Name == name {
			return &containers[i], nil
		}
	}
	return nil, fmt.Errorf("container %q not found in DaemonSet %s/%s", name, daemonSet.Namespace, daemonSet.Name)
}

// formatRotationInterval renders d as a --rotation-poll-interval value.
// time.Duration.String() always includes a trailing zero-valued unit for
// whole-minute/hour durations (e.g. "2m0s" for exactly two minutes), which
// does not match the literal "2m" historically hardcoded in assets/node.yaml
// and, left unfixed, would cause a needless DaemonSet diff/rollout for every
// cluster that has not configured driverConfig.secretsStore. Whole-minute
// durations are rendered as "Nm"; anything else falls back to the standard
// time.Duration.String() (e.g. "1m30s"), which is a valid Go duration string
// but simply doesn't have a historical literal to preserve.
func formatRotationInterval(d time.Duration) string {
	if d > 0 && d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int64(d/time.Minute))
	}
	return d.String()
}

// setArg finds the element of args whose value starts with prefix and
// replaces it with prefix+value, in place. If no element matches, prefix+value
// is appended. All other elements are left unchanged and in their original
// order.
func setArg(args []string, prefix, value string) []string {
	newArg := prefix + value
	for i, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			args[i] = newArg
			return args
		}
	}
	return append(args, newArg)
}
