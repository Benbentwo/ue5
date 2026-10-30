package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Benbentwo/ue5/pkg/server"
	"github.com/spf13/cobra"
)

var serverAutostartCmd = &cobra.Command{
	Use:   "autostart [install|uninstall|status]",
	Short: "Start the daemon automatically at login",
	Long: `Registers the daemon to start when you log in: a LaunchAgent on macOS (also
restarted if it crashes), a per-user Run entry on Windows (no administrator
rights needed). Use it on a build box so
the build service survives a reboot without anyone starting it by hand.`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"install", "uninstall", "status"},
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "install":
			bin, err := os.Executable()
			if err != nil {
				return err
			}
			if real, err := filepath.EvalSymlinks(bin); err == nil {
				bin = real
			}
			what, err := server.InstallAutostart(bin)
			if err != nil {
				return err
			}
			fmt.Println("installed:", what)
		case "uninstall":
			if err := server.UninstallAutostart(); err != nil {
				return err
			}
			fmt.Println("uninstalled (a running daemon keeps running)")
		case "status":
			fmt.Println(server.AutostartStatus())
		default:
			return fmt.Errorf("unknown action %q: use install, uninstall or status", args[0])
		}
		return nil
	},
}

func init() {
	serverCmd.AddCommand(serverAutostartCmd)
}
