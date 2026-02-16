package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/Benbentwo/ue5/pkg"
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

	if !forceUpgrade {
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

	fmt.Println("\nUpgrading...")
	if err := pkg.DownloadAndInstall(info); err != nil {
		log.Error("Upgrade failed", "error", err)
		os.Exit(1)
	}

	fmt.Printf("\nSuccessfully upgraded to version %s!\n", info.LatestVersion)
}
