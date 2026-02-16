package cmd

import (
	"github.com/Benbentwo/ue5/pkg/server"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

var serverForeground bool

var serverStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the server daemon",
	Long:  `Starts the UE5 server daemon. Use --foreground to run in the foreground for debugging.`,
	Run: func(cmd *cobra.Command, args []string) {
		if serverForeground {
			// Run daemon in foreground (blocking)
			d := server.NewDaemon(Version)
			if err := d.Run(); err != nil {
				log.Fatal("Daemon failed", "error", err)
			}
			return
		}

		// Check if already running
		client := server.NewClient()
		if client.IsRunning() {
			pong, _ := client.Ping()
			if pong != nil {
				log.Info("Daemon already running", "version", pong.Version, "uptime", pong.Uptime, "instances", pong.Instances)
			} else {
				log.Info("Daemon already running")
			}
			return
		}

		// Start daemon in background
		if err := server.EnsureDaemon(); err != nil {
			log.Fatal("Failed to start daemon", "error", err)
		}

		// Confirm it's running
		pong, err := client.Ping()
		if err != nil {
			log.Error("Daemon started but not responding", "error", err)
			return
		}
		log.Info("Daemon started", "version", pong.Version, "socket", server.SocketPath())
	},
}

func init() {
	serverStartCmd.Flags().BoolVarP(&serverForeground, "foreground", "f", false, "Run daemon in the foreground")
	serverCmd.AddCommand(serverStartCmd)
}
