package cmd

import (
	"context"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // required import to enable kube client-go auth plugins
)

type GlobalFlags struct {
	KubeContext    string
	KubeConfigPath string
}

func (g *GlobalFlags) AddToFlags(flags *pflag.FlagSet) {
	flags.StringVar(&g.KubeContext, "context", "", "Kubernetes context for the cluster to runDependencies the command in.")
}

func RootCommand(ctx context.Context) *cobra.Command {
	globalFlags := &GlobalFlags{}

	cmd := &cobra.Command{
		Use:   "mesh-helper [command]",
		Short: "Use the `mesh-helper` command line interface (CLI) tool to help administer mesh based environments.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
		},
		SilenceErrors: true,
	}

	// set global CLI flags
	globalFlags.AddToFlags(cmd.PersistentFlags())

	cmd.AddCommand(
		dependenciesCmd(),
		endpointsCmd(ctx, globalFlags),
	)

	return cmd
}
