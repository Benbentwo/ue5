package cmd

import (
	"github.com/Benbentwo/ue5/pkg"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

// packageCmd represents the package command
var packageCmd = &cobra.Command{
	Use:   "package",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		err := pkg.RunPackageScript(EnginePath, pkg.GetPlatform(), State, ResolvedUProject)
		if err != nil {
			log.Error("Failed to run package script", "error", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(packageCmd)

	packageCmd.PersistentFlags().StringVarP(&State, "state", "s", "Development", "State of the build, e.g. Development or Shipping")

}
