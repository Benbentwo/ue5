package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Benbentwo/ue5/pkg"
	"github.com/Benbentwo/ue5/pkg/server"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

var (
	forceUpgrade bool
	checkOnly    bool
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade ue5 CLI to the latest version",
	Long: `Check for and install the latest version of the ue5 CLI.

This command will:
1. Check GitHub for the latest release
2. Compare with your current version
3. Download and install the new version (with your confirmation)
4. Restart the server daemon on the new binary if one was running
   (managed editor instances are stopped with it)

If a build is in progress, the upgrade refuses to run unless --force is
given, since restarting the daemon aborts the build.

Use --check to only check for updates without installing.
Use --force to skip the confirmation prompt.`,
	Run: runUpgrade,
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
	upgradeCmd.Flags().BoolVarP(&forceUpgrade, "force", "f", false, "Skip confirmation prompt")
	upgradeCmd.Flags().BoolVarP(&checkOnly, "check", "c", false, "Only check for updates, don't install")
}

func runUpgrade(cmd *cobra.Command, args []string) {
	fmt.Printf("Current version: %s\n", Version)
	fmt.Println("Checking for updates...")

	info, err := pkg.CheckForUpgrade(Version)
	if err != nil {
		log.Error("Failed to check for updates", "error", err)
		os.Exit(1)
	}

	if !info.HasUpdate {
		fmt.Printf("You are already running the latest version (%s)\n", info.CurrentVersion)
		return
	}

	fmt.Printf("\nNew version available: %s -> %s\n", info.CurrentVersion, info.LatestVersion)
	fmt.Printf("Release URL: %s\n", info.ReleaseURL)

	if info.DownloadURL == "" {
		log.Error("No download available for your platform")
		fmt.Println("Please download manually from the release page.")
		os.Exit(1)
	}

	if checkOnly {
		fmt.Println("\nRun 'ue5 upgrade' to install the update.")
		return
	}

	client := server.NewClient()
	daemonWasRunning := client.IsRunning()

	if !forceUpgrade {
		if daemonWasRunning {
			fmt.Println("\nThe server daemon is running. It will be restarted on the new")
			fmt.Println("binary after install (managed editor instances are stopped with it).")
		}
		fmt.Print("\nDo you want to upgrade? [y/N]: ")
		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			log.Error("Failed to read response", "error", err)
			os.Exit(1)
		}
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Upgrade cancelled.")
			return
		}
	}

	// Restarting the daemon aborts any in-flight build; refuse unless forced.
	if daemonWasRunning && !forceUpgrade {
		if resp, err := client.Send(server.Request{ID: "upgrade-build-check", Type: server.ReqGetBuildInfo}); err == nil &&
			resp.Success && resp.BuildInfo != nil && resp.BuildInfo.CurrentBuild != nil &&
			resp.BuildInfo.CurrentBuild.Status == server.BuildStatusBuilding {
			log.Error("A build is in progress; upgrading now would restart the daemon and abort it",
				"build_id", resp.BuildInfo.CurrentBuild.ID,
				"labels", resp.BuildInfo.CurrentBuild.Labels)
			fmt.Println("Wait for the build to finish, or re-run with --force to upgrade anyway.")
			os.Exit(1)
		}
	}

	fmt.Println("\nUpgrading...")
	installedPath, err := pkg.DownloadAndInstall(info)
	if err != nil {
		log.Error("Upgrade failed", "error", err)
		os.Exit(1)
	}

	fmt.Printf("\nSuccessfully upgraded to version %s!\n", info.LatestVersion)

	// The install swaps the binary via rename, so an already-running daemon
	// keeps executing the old (now-unlinked) version safely — but it never
	// picks up the new one, and before this restart existed an upgrade left
	// the daemon dead with nothing to revive it (2026-07-09 v0.3.25 outage).
	// Restart it deliberately: stop old, start new from the installed path.
	if daemonWasRunning {
		fmt.Println("Restarting daemon on the new binary (managed editors will be stopped)...")
		if err := server.StopDaemonAndWait(60 * time.Second); err != nil {
			log.Warn("Old daemon did not stop; it keeps serving the previous version", "error", err)
			fmt.Println("Run 'ue5 server stop' then 'ue5 server start' to move it to the new version.")
			return
		}
		if err := server.EnsureDaemonAt(installedPath); err != nil {
			log.Error("Daemon did not restart after the upgrade", "error", err)
			fmt.Println("\n*** DAEMON IS STOPPED — run 'ue5 server start' to restore builds and MCP. ***")
			os.Exit(1)
		}
		if pong, err := server.NewClient().Ping(); err == nil {
			log.Info("Daemon restarted", "version", pong.Version)
		} else {
			log.Info("Daemon restarted")
		}
	}
}
