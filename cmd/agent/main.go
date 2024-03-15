package main

import (
	"flag"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"os"
)

var log = logrus.New()

func main() {
	cmd := NewAgentCommand()
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func NewAgentCommand() (cmd *cobra.Command) {
	opts := NewOptions()
	cmd = &cobra.Command{
		Use:  "antrea-agent",
		Long: "The Antrea agent runs on each node.",
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
