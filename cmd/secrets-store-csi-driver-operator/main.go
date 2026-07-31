package main

import (
	"context"
	"fmt"
	"os"
	"time"

	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
	libgoclient "github.com/openshift/library-go/pkg/config/client"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/serviceability"
	"github.com/openshift/secrets-store-csi-driver-operator/pkg/operator"
	sscsitls "github.com/openshift/secrets-store-csi-driver-operator/pkg/tls"
	"github.com/openshift/secrets-store-csi-driver-operator/pkg/version"
	"github.com/spf13/cobra"
	"k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/rest"
	"k8s.io/component-base/cli"
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
)

const componentName = "secrets-store-csi-driver-operator"

func main() {
	command := NewOperatorCommand()
	code := cli.Run(command)
	os.Exit(code)
}

func NewOperatorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets-store-csi-driver-operator",
		Short: "OpenShift Secrets Store CSI Driver Operator",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
			os.Exit(1)
		},
	}

	cmdcfg := controllercmd.NewControllerCommandConfig(
		componentName,
		version.Get(),
		nil, // startFunc is set in the custom start path below
		clock.RealClock{},
	)

	startCmd := newCommandWithTLSCustomization(cmdcfg)
	startCmd.Use = "start"
	startCmd.Short = "Start the Secrets Store CSI Driver Operator"
	cmd.AddCommand(startCmd)

	return cmd
}

// newCommandWithTLSCustomization creates a Controllercmd-based start command that
// applies the cluster TLS security profile to the operator metrics ServingInfo
// before the HTTPS server starts. Controllercmd's stock StartController has no
// hook for this, so we mirror the CNO pattern: reuse NewCommandWithContext for
// flags, then replace Run with a custom start path.
func newCommandWithTLSCustomization(cmdcfg *controllercmd.ControllerCommandConfig) *cobra.Command {
	cmd := cmdcfg.NewCommandWithContext(context.Background())

	cmd.Run = func(cmd *cobra.Command, args []string) {
		logs.InitLogs()

		ctx := server.SetupSignalContext()

		defer logs.FlushLogs()
		defer serviceability.BehaviorOnPanic(os.Getenv("OPENSHIFT_ON_PANIC"), version.Get())()
		defer serviceability.Profile(os.Getenv("OPENSHIFT_PROFILE")).Stop()

		serviceability.StartProfiler()

		// basicFlags on cmdcfg is unexported; fail fast if a flag is missing/renamed.
		kubeConfigFile, err := cmd.Flags().GetString("kubeconfig")
		if err != nil {
			klog.Fatal(err)
		}
		namespace, err := cmd.Flags().GetString("namespace")
		if err != nil {
			klog.Fatal(err)
		}
		bindAddress, err := cmd.Flags().GetString("listen")
		if err != nil {
			klog.Fatal(err)
		}

		if err := startControllerWithTLSCustomization(ctx, cmdcfg, kubeConfigFile, namespace, bindAddress); err != nil {
			klog.Fatal(err)
		}
	}

	return cmd
}

func startControllerWithTLSCustomization(
	ctx context.Context,
	cmdcfg *controllercmd.ControllerCommandConfig,
	kubeConfigFile string,
	namespace string,
	bindAddress string,
) error {
	unstructuredConfig, config, configContent, err := cmdcfg.Config()
	if err != nil {
		return err
	}
	if config == nil {
		config = &operatorv1alpha1.GenericOperatorConfig{}
	}

	startingFileContent, observedFiles, err := cmdcfg.AddDefaultRotationToConfig(config, configContent)
	if err != nil {
		return err
	}

	if len(bindAddress) != 0 {
		config.ServingInfo.BindAddress = bindAddress
	}

	resolvedTLS, err := applyClusterTLSProfile(ctx, config, kubeConfigFile)
	if err != nil {
		return fmt.Errorf("failed to apply cluster TLS profile: %w", err)
	}

	controllerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	exitOnChangeReactorCh := make(chan struct{})
	go func() {
		select {
		case <-exitOnChangeReactorCh:
			klog.Infof("Observed file change, triggering graceful restart")
			cancel()
		case <-ctx.Done():
			cancel()
		}
	}()

	startFunc := func(ctx context.Context, controllerConfig *controllercmd.ControllerContext) error {
		return operator.RunOperator(ctx, controllerConfig, resolvedTLS, cancel)
	}

	config.LeaderElection.Disable = cmdcfg.DisableLeaderElection

	builder := controllercmd.NewController(componentName, startFunc, clock.RealClock{}).
		WithKubeConfigFile(kubeConfigFile, nil).
		WithComponentNamespace(namespace).
		WithLeaderElection(config.LeaderElection, namespace, componentName+"-lock").
		WithVersion(version.Get()).
		WithEventRecorderOptions(events.RecommendedClusterSingletonCorrelatorOptions()).
		WithRestartOnChange(exitOnChangeReactorCh, startingFileContent, observedFiles...).
		WithComponentOwnerReference(cmdcfg.ComponentOwnerReference)

	if !cmdcfg.DisableServing {
		builder = builder.WithServer(config.ServingInfo, config.Authentication, config.Authorization)
		if cmdcfg.EnableHTTP2 {
			builder = builder.WithHTTP2()
		}
		if cmdcfg.SkipInClusterAuthenticationLookup {
			builder = builder.WithSkipInClusterAuthenticationLookup()
		}
	}

	return builder.Run(controllerCtx, unstructuredConfig)
}

func applyClusterTLSProfile(
	ctx context.Context,
	config *operatorv1alpha1.GenericOperatorConfig,
	kubeConfigFile string,
) (sscsitls.ResolvedProfile, error) {
	restConfig, err := libgoclient.GetKubeConfigOrInClusterConfig(kubeConfigFile, nil)
	if err != nil {
		return sscsitls.ResolvedProfile{}, fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	configClient, err := configclient.NewForConfig(rest.AddUserAgent(restConfig, componentName))
	if err != nil {
		return sscsitls.ResolvedProfile{}, fmt.Errorf("failed to create config client: %w", err)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resolved, err := sscsitls.FetchAndResolve(fetchCtx, configClient.ConfigV1())
	if err != nil {
		return sscsitls.ResolvedProfile{}, err
	}

	sscsitls.ApplyToServingInfo(&config.ServingInfo, resolved)
	return resolved, nil
}
