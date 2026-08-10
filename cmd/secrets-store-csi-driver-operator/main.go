package main

import (
	"context"
	"fmt"
	"os"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	"k8s.io/component-base/cli"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"

	"github.com/openshift/secrets-store-csi-driver-operator/pkg/operator"
	sscsitls "github.com/openshift/secrets-store-csi-driver-operator/pkg/tls"
	"github.com/openshift/secrets-store-csi-driver-operator/pkg/version"
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
	cmd.AddCommand(newStartCommand())
	return cmd
}

// newStartCommand builds the stock Controllercmd "start" command unchanged and
// adds a PersistentPreRunE that resolves the cluster TLS security profile and
// feeds it through --config so StartController's own config parsing picks it
// up. This avoids re-implementing StartController just to reach ServingInfo.
//
// The resolved profile is also threaded to RunOperator (for the live-change
// watcher) via a closure variable that PersistentPreRunE fills before Run —
// cobra runs those strictly in that order on the same goroutine.
func newStartCommand() *cobra.Command {
	var resolvedTLS sscsitls.ResolvedProfile

	startFunc := func(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
		return operator.RunOperator(ctx, controllerContext, resolvedTLS)
	}
	cmdcfg := controllercmd.NewControllerCommandConfig(componentName, version.Get(), startFunc, clock.RealClock{})

	cmd := cmdcfg.NewCommand()
	cmd.Use = "start"
	cmd.Short = "Start the Secrets Store CSI Driver Operator"

	existingPreRunE := cmd.PersistentPreRunE
	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if existingPreRunE != nil {
			if err := existingPreRunE(cmd, args); err != nil {
				return err
			}
		}
		kubeConfigFile, err := cmd.Flags().GetString("kubeconfig")
		if err != nil {
			return err
		}
		resolvedTLS, err = sscsitls.ResolveFromCluster(context.Background(), kubeConfigFile, componentName)
		if err != nil {
			return fmt.Errorf("failed to resolve cluster TLS security profile: %w", err)
		}
		return applyTLSProfileToConfigFlag(cmd, resolvedTLS)
	}

	return cmd
}

// applyTLSProfileToConfigFlag points --config at a generated config file
// carrying resolved's TLS settings. Errors if --config is already set rather
// than merging: the shipped CSV never passes --config.
func applyTLSProfileToConfigFlag(cmd *cobra.Command, resolved sscsitls.ResolvedProfile) error {
	if !resolved.Honor {
		klog.Infof("TLS adherence policy is %q; leaving --config as-is", resolved.Adherence)
		return nil
	}

	configFile, err := cmd.Flags().GetString("config")
	if err != nil {
		return err
	}
	if configFile != "" {
		return fmt.Errorf("cannot apply cluster TLS security profile: --config %q is already set and merging "+
			"into an existing config file is not yet supported", configFile)
	}

	tmpFile, err := sscsitls.WriteConfigFile(resolved)
	if err != nil {
		return err
	}
	return cmd.Flags().Set("config", tmpFile)
}
