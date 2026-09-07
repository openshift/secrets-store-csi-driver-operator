package main

import (
	"context"
	"os"

	"k8s.io/utils/clock"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/component-base/cli"
	"k8s.io/klog/v2"

	configclient "github.com/openshift/client-go/config/clientset/versioned"
	libgoclient "github.com/openshift/library-go/pkg/config/client"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"

	"github.com/openshift/secrets-store-csi-driver-operator/pkg/operator"
	"github.com/openshift/secrets-store-csi-driver-operator/pkg/version"
)

func main() {
	bootstrapTLSServingConfig()

	command := NewOperatorCommand()
	code := cli.Run(command)
	os.Exit(code)
}

// bootstrapTLSServingConfig performs a one-time, best-effort resolve-and-write of this
// operator's TLS serving config file, before cli.Run(command) starts the "start" subcommand's
// ControllerCommandConfig (which reads the file back via --config, per the CSV args). Without
// this step, the very first process start would use library-go's own hardcoded default profile
// until the first APIServer informer event fires post-startup (see
// pkg/operator/tls_serving_config.go RegisterTLSServingConfigObserver for the steady-state path).
//
// This step is intentionally best-effort (FR-004): any failure (no in-cluster config available,
// APIServer/cluster not found, transient API error) is logged with a distinguishable
// klog.Warningf and otherwise ignored. It must never klog.Fatal, panic, or block startup —
// ControllerCommandConfig.StartController already falls back to its own static default profile
// when no (or no valid) --config file is present.
func bootstrapTLSServingConfig() {
	kubeConfig, err := libgoclient.GetKubeConfigOrInClusterConfig("", nil)
	if err != nil {
		klog.Warningf("TLS serving config bootstrap: failed to load a kubeconfig, starting with the default TLS serving profile: %v", err)
		return
	}

	configClient, err := configclient.NewForConfig(kubeConfig)
	if err != nil {
		klog.Warningf("TLS serving config bootstrap: failed to create a config client, starting with the default TLS serving profile: %v", err)
		return
	}

	apiServer, err := configClient.ConfigV1().APIServers().Get(context.Background(), "cluster", metav1.GetOptions{})
	if err != nil {
		klog.Warningf("TLS serving config bootstrap: failed to get APIServer/cluster, starting with the default TLS serving profile: %v", err)
		return
	}

	minTLSVersion, cipherSuites := operator.ResolveTLSServingProfile(apiServer)
	if err := operator.WriteTLSServingConfigFile(operator.TLSServingConfigFilePath, minTLSVersion, cipherSuites); err != nil {
		klog.Warningf("TLS serving config bootstrap: failed to write TLS serving config file %q, starting with the default TLS serving profile: %v", operator.TLSServingConfigFilePath, err)
		return
	}

	klog.V(2).Infof("TLS serving config bootstrap: wrote resolved TLS serving config (minTLSVersion=%s) to %q", minTLSVersion, operator.TLSServingConfigFilePath)
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

	ctrlCmd := controllercmd.NewControllerCommandConfig(
		"secrets-store-csi-driver-operator",
		version.Get(),
		operator.RunOperator,
		clock.RealClock{},
	).NewCommand()
	ctrlCmd.Use = "start"
	ctrlCmd.Short = "Start the Secrets Store CSI Driver Operator"

	cmd.AddCommand(ctrlCmd)

	return cmd
}
