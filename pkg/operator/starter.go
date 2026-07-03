package operator

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	kubeclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"

	opv1 "github.com/openshift/api/operator/v1"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	applyoperatorv1 "github.com/openshift/client-go/operator/applyconfigurations/operator/v1"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/csi/csicontrollerset"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller"
	goc "github.com/openshift/library-go/pkg/operator/genericoperatorclient"
	"github.com/openshift/library-go/pkg/operator/management"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"github.com/openshift/secrets-store-csi-driver-operator/assets"
)

const (
	operatorName       = "secrets-store-csi-driver-operator"
	operandName        = "secrets-store-csi-driver"
	trustedCAConfigMap = "secrets-store-csi-driver-trusted-ca-bundle"
	providerName       = "secrets-store.csi.k8s.io"
	namespaceKey       = "${NAMESPACE}"
	resync             = 20 * time.Minute

	// csiDriverContainerName is the name of the CSI driver container in the node DaemonSet.
	csiDriverContainerName = "csi-driver"
	// enableSecretRotationArg is the CSI driver flag that controls automatic secret rotation.
	enableSecretRotationArg = "--enable-secret-rotation"
	// rotationPollIntervalArg is the CSI driver flag that sets the minimum rotation poll interval.
	rotationPollIntervalArg = "--rotation-poll-interval"
	// defaultRotationPollInterval is the poll interval applied when SecretRotationCustom is set
	// without an explicit rotationPollIntervalSeconds value (FR-003).
	defaultRotationPollInterval = "2m"
)

func RunOperator(ctx context.Context, controllerConfig *controllercmd.ControllerContext) error {
	operatorNamespace := controllerConfig.OperatorNamespace

	// Create core clientset and informers
	kubeClient := kubeclient.NewForConfigOrDie(rest.AddUserAgent(controllerConfig.KubeConfig, operatorName))
	kubeInformersForNamespaces := v1helpers.NewKubeInformersForNamespaces(kubeClient, operatorNamespace, "")
	configMapInformer := kubeInformersForNamespaces.InformersFor(operatorNamespace).Core().V1().ConfigMaps()

	// Create config clientset and informer. This is used to get the cluster ID
	configClient := configclient.NewForConfigOrDie(rest.AddUserAgent(controllerConfig.KubeConfig, operatorName))
	configInformers := configinformers.NewSharedInformerFactory(configClient, resync)

	// Create GenericOperatorclient. This is used by the library-go controllers created down below
	gvr := opv1.SchemeGroupVersion.WithResource("clustercsidrivers")
	gvk := opv1.SchemeGroupVersion.WithKind("ClusterCSIDriver")
	operatorClient, dynamicInformers, err := goc.NewClusterScopedOperatorClientWithConfigName(
		clock.RealClock{},
		controllerConfig.KubeConfig,
		gvr,
		gvk,
		providerName,
		extractOperatorSpec,
		extractOperatorStatus,
	)
	if err != nil {
		return err
	}

	dynamicClient, err := dynamic.NewForConfig(controllerConfig.KubeConfig)
	if err != nil {
		return err
	}

	csiControllerSet := csicontrollerset.NewCSIControllerSet(
		operatorClient,
		controllerConfig.EventRecorder,
	).WithLogLevelController().WithManagementStateController(
		operandName,
		true, // Set this operator as removable
	).WithConditionalStaticResourcesController(
		"SecretsStoreConditionalStaticResourcesController",
		kubeClient,
		dynamicClient,
		kubeInformersForNamespaces,
		replaceNamespaceFunc(operatorNamespace),
		[]string{
			"node_sa.yaml",
			"csidriver.yaml",
			"cabundle_cm.yaml",
			"rbac/privileged_role.yaml",
			"rbac/node_privileged_binding.yaml",
			"rbac/secretproviderclasses_role.yaml",
			"rbac/secretproviderclasses_binding.yaml",
			"network-policy/allow-ingress-to-metrics-operand.yaml",
		},
		func() bool {
			return getOperatorSyncState(operatorClient) == opv1.Managed
		},
		func() bool {
			return getOperatorSyncState(operatorClient) == opv1.Removed
		},
	).WithCSIConfigObserverController(
		"SecretsStoreDriverCSIConfigObserverController",
		configInformers,
	).WithCSIDriverNodeService(
		"SecretsStoreDriverNodeServiceController",
		replaceNamespaceFunc(operatorNamespace),
		"node.yaml",
		kubeClient,
		kubeInformersForNamespaces.InformersFor(operatorNamespace),
		nil,
		csidrivernodeservicecontroller.WithCABundleDaemonSetHook(
			operatorNamespace,
			trustedCAConfigMap,
			configMapInformer,
		),
	)

	klog.Info("Starting the informers")
	go kubeInformersForNamespaces.Start(ctx.Done())
	go dynamicInformers.Start(ctx.Done())
	go configInformers.Start(ctx.Done())

	klog.Info("Starting controllerset")
	go csiControllerSet.Run(ctx, 1)

	<-ctx.Done()

	return nil
}

func replaceNamespaceFunc(namespace string) resourceapply.AssetFunc {
	return func(name string) ([]byte, error) {
		content, err := assets.ReadFile(name)
		if err != nil {
			panic(err)
		}
		return bytes.ReplaceAll(content, []byte(namespaceKey), []byte(namespace)), nil
	}
}

// withSecretRotationHook returns a DaemonSetHookFunc that mutates the csi-driver container
// args in the node DaemonSet to configure secret rotation based on the ClusterCSIDriver
// secretsStore.secretRotation spec.
//
// Behaviour per secretRotation.type:
//
//	SecretRotationNone:   --enable-secret-rotation=false (disables kubelet republish)
//	SecretRotationCustom: --enable-secret-rotation=true  (enables republish)
//	                      --rotation-poll-interval=<duration> (custom interval, or "2m" default)
//	unset / empty:        no mutation — upgrade no-op (FR-003)
//
// Note: the DaemonSetHookFunc first parameter (*opv1.OperatorSpec) does not carry
// DriverConfig.SecretsStore, so this hook reads the full ClusterCSIDriver via the
// dynamic informer lister passed at construction time.
func withSecretRotationHook(clusterCSIDriverLister cache.GenericLister) csidrivernodeservicecontroller.DaemonSetHookFunc {
	return func(_ *opv1.OperatorSpec, daemonSet *appsv1.DaemonSet) error {
		obj, err := clusterCSIDriverLister.Get(providerName)
		if apierrors.IsNotFound(err) {
			// ClusterCSIDriver absent — no mutation (upgrade no-op).
			return nil
		}
		if err != nil {
			return fmt.Errorf("withSecretRotationHook: failed to get ClusterCSIDriver %q: %w", providerName, err)
		}

		unstr, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return fmt.Errorf("withSecretRotationHook: unexpected object type %T", obj)
		}

		clusterCSIDriver := &opv1.ClusterCSIDriver{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstr.Object, clusterCSIDriver); err != nil {
			return fmt.Errorf("withSecretRotationHook: failed to convert ClusterCSIDriver: %w", err)
		}

		if clusterCSIDriver.Spec.DriverConfig.DriverType != opv1.SecretsStoreDriverType {
			// Not a SecretsStore driver — no mutation.
			return nil
		}

		secretRotation := clusterCSIDriver.Spec.DriverConfig.SecretsStore.SecretRotation
		for i := range daemonSet.Spec.Template.Spec.Containers {
			if daemonSet.Spec.Template.Spec.Containers[i].Name == csiDriverContainerName {
				applySecretRotationArgs(&daemonSet.Spec.Template.Spec.Containers[i], secretRotation)
				break
			}
		}
		return nil
	}
}

// applySecretRotationArgs mutates container args to reflect the desired secret rotation
// configuration. Existing rotation args are removed before applying the new values to
// ensure idempotent reconciliation.
func applySecretRotationArgs(container *corev1.Container, rotation opv1.SecretsStoreSecretRotation) {
	container.Args = removeRotationArgs(container.Args)

	switch rotation.Type {
	case opv1.SecretRotationNone:
		container.Args = append(container.Args, enableSecretRotationArg+"=false")
	case opv1.SecretRotationCustom:
		container.Args = append(container.Args, enableSecretRotationArg+"=true")
		if rotation.Custom.RotationPollIntervalSeconds > 0 {
			d := time.Duration(rotation.Custom.RotationPollIntervalSeconds) * time.Second
			container.Args = append(container.Args, fmt.Sprintf("%s=%s", rotationPollIntervalArg, d.String()))
		} else {
			container.Args = append(container.Args, rotationPollIntervalArg+"="+defaultRotationPollInterval)
		}
	default:
		// Empty / unset type: no mutation — baseline node.yaml values are preserved (FR-003).
	}
}

// removeRotationArgs strips any existing --enable-secret-rotation and
// --rotation-poll-interval args from the container arg list.
func removeRotationArgs(args []string) []string {
	out := args[:0]
	for _, arg := range args {
		if !strings.HasPrefix(arg, enableSecretRotationArg) && !strings.HasPrefix(arg, rotationPollIntervalArg) {
			out = append(out, arg)
		}
	}
	return out
}

// getOperatorSyncState returns the management state of the operator to determine
// how to sync conditional resources. It returns one of the following states:
//
//	Managed: resources should be synced
//	Unmanaged: resources should NOT be synced
//	Removed: resources should be deleted
//
// Errors fetching the operator state will log an error and return Unmanaged
// to avoid syncing resources when the actual state is unknown.
func getOperatorSyncState(operatorClient v1helpers.OperatorClientWithFinalizers) opv1.ManagementState {
	opSpec, _, _, err := operatorClient.GetOperatorState()
	if err != nil {
		klog.Errorf("Failed to get operator state: %v", err)
		return opv1.Unmanaged
	}
	// return the state from the operator if it's not managed
	if opSpec.ManagementState != opv1.Managed {
		return opSpec.ManagementState
	}
	meta, err := operatorClient.GetObjectMeta()
	if err != nil {
		klog.Errorf("Failed to get operator object meta: %v", err)
		return opv1.Unmanaged
	}
	// deletion timestamp is treated the same as the state being removed
	if management.IsOperatorRemovable() && meta.DeletionTimestamp != nil {
		klog.Infof("Operator deletion timestamp is set, removing conditional resources")
		return opv1.Removed
	}
	return opv1.Managed
}

func extractOperatorSpec(obj *unstructured.Unstructured, fieldManager string) (*applyoperatorv1.OperatorSpecApplyConfiguration, error) {
	castObj := &opv1.ClusterCSIDriver{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, castObj); err != nil {
		return nil, fmt.Errorf("unable to convert to ClusterCSIDriver: %w", err)
	}
	ret, err := applyoperatorv1.ExtractClusterCSIDriver(castObj, fieldManager)
	if err != nil {
		return nil, fmt.Errorf("unable to extract fields for %q: %w", fieldManager, err)
	}
	if ret.Spec == nil {
		return nil, nil
	}
	return &ret.Spec.OperatorSpecApplyConfiguration, nil
}
func extractOperatorStatus(obj *unstructured.Unstructured, fieldManager string) (*applyoperatorv1.OperatorStatusApplyConfiguration, error) {
	castObj := &opv1.ClusterCSIDriver{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, castObj); err != nil {
		return nil, fmt.Errorf("unable to convert to ClusterCSIDriver: %w", err)
	}
	ret, err := applyoperatorv1.ExtractClusterCSIDriverStatus(castObj, fieldManager)
	if err != nil {
		return nil, fmt.Errorf("unable to extract fields for %q: %w", fieldManager, err)
	}

	if ret.Status == nil {
		return nil, nil
	}
	return &ret.Status.OperatorStatusApplyConfiguration, nil
}
