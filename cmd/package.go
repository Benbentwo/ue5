package cmd

import (
	"github.com/Benbentwo/ue5/pkg"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

var (
	OutputDir string // Directory to output the package
)

// packageCmd represents the package command
var packageCmd = &cobra.Command{
	Use:   "package",
	Short: "Package your Unreal Engine project for shipping",
	Long:  `This command packages your Unreal Engine project for shipping. It builds, cooks, and stages the project, creating a distributable package.`,
	Run: func(cmd *cobra.Command, args []string) {
		err := pkg.RunPackageScript(EnginePath, pkg.GetPlatform(), State, ResolvedUProject, OutputDir)
		if err != nil {
			log.Error("Failed to run package script", "error", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(packageCmd)

	packageCmd.PersistentFlags().StringVarP(&State, "state", "s", "Development", "State of the build, e.g. Development or Shipping")
	packageCmd.PersistentFlags().StringVarP(&OutputDir, "output", "o", "dist", "Directory to output the package, defaults to 'dist' in the project directory")

}
