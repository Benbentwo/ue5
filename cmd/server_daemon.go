package cmd

import (
	"errors"
	"os"

	"github.com/Benbentwo/ue5/pkg/server"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

var serverDaemonCmd = &cobra.Command{
	Use:    "daemon",
	Short:  "Run the server daemon (internal use)",
	Hidden: true,
	Run: func(cmd *cobra.Command, args []string) {
		d := server.NewDaemon(Version)
		if err := d.Run(); err != nil {
			if errors.Is(err, server.ErrStoppedBySignal) {
				log.Info("Daemon stopped by a signal; exiting non-zero so a supervisor restarts it")
				os.Exit(2)
			}
			log.Fatal("Daemon failed", "error", err)
		}
	},
}

func init() {
	serverCmd.AddCommand(serverDaemonCmd)
}
