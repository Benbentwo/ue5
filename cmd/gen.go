package cmd

import (
	"github.com/Benbentwo/ue5/pkg"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

// genCmd represents the gen command
var genCmd = &cobra.Command{
	Use:   "gen",
	Short: "Generate project files for your Unreal Engine Project",
	Long:  `Equivalent to running Unreal Engine's GenerateProjectFiles command. Which is the same as right clicking on the .uproject file and selecting "Generate Visual Studio project files" or "Generate Xcode project files" depending on your platform.`,
	Run: func(cmd *cobra.Command, args []string) {
		err := pkg.RunGenScript(EnginePath, ResolvedUProject)
		if err != nil {
			log.Error("Failed to run gen script", "error", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(genCmd)
}
