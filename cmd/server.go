package cmd

import (
	"github.com/spf13/cobra"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Manage the UE5 editor server daemon",
	Long: `The server daemon manages Unreal Editor instances, captures their logs,
and provides an API for starting, stopping, and querying editor state.

Use 'ue5 server start' to launch the daemon, or commands like 'ue5 server run'
will auto-start it when needed.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	rootCmd.AddCommand(serverCmd)
}
