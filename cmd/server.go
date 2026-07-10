package cmd

import (
	"github.com/spf13/cobra"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Manage the UE5 editor server daemon",
	Long: `The server daemon manages Unreal Editor instances, captures their logs,
and provides an API for starting, stopping, and querying editor state.

Use 'ue5 server start' to launch the daemon. Commands that need it (run,
rebuild, logs, status, build-info, agents) auto-start it when the socket is
dead, so a killed daemon heals on the next query. Only 'stop' and 'kill'
treat a dead daemon as final.`,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

func init() {
	rootCmd.AddCommand(serverCmd)
}
