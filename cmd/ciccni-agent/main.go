package main

import (
	"flag"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/klog"
)

var log = logrus.New()

func main() {
	cmd := newAgentCommand()
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newAgentCommand() (cmd *cobra.Command) {
	opts := NewOptions()
	cmd = &cobra.Command{
		Use:  "ciccni-agent",
		Long: "The ciccni agent runs on each node.",
		Run: func(cmd *cobra.Command, args []string) {
			if err := opts.complete(args); err != nil {

			}

			if err := run(opts); err != nil {
				klog.Fatalf("Error running agent: %v", err)
			}
		},
	}

	flags := cmd.Flags()
	opts.addFlags(flags)
	// Install log flags
	flags.AddGoFlagSet(flag.CommandLine)

	return

}
