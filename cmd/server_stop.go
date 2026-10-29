package cmd

import (
	"github.com/Benbentwo/ue5/pkg/server"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

var serverStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the server daemon",
	Long:  `Stops the UE5 server daemon and all managed editor instances.`,
	Run: func(cmd *cobra.Command, args []string) {
		client := server.NewClient()

		if !client.IsRunning() {
			log.Info("Daemon is not running")
			return
		}

		resp, err := client.Send(server.Request{
			ID:   "shutdown",
			Type: server.ReqShutdown,
		})
		if err != nil {
			log.Error("Failed to send shutdown request", "error", err)
			return
		}

		if resp.Success {
			log.Info("Daemon shutdown initiated")
		} else {
			log.Error("Shutdown failed", "error", resp.Error)
		}
	},
}

func init() {
	serverCmd.AddCommand(serverStopCmd)
}
