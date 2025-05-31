/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"github.com/Benbentwo/ue5/pkg"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

// buildCmd represents the build command
var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: RunBuildCommand,
}

func RunBuildCommand(cmd *cobra.Command, args []string) {
	///Path/To/UE5/Engine/Build/BatchFiles/Mac/Build.sh ProjectNameEditor Mac Development "/Path/To/Project/ProjectName.uproject"
	if EnginePath == "" {
		return
	}

	err := pkg.RunBuildScript(EnginePath, ProjectName+"Editor", pkg.GetPlatform(), "Development", ResolvedUProject)
	if err != nil {
		return
	}
	log.Info("✅  Project built successfully", "project", ProjectName, "target", Target, "state", State)
}

func init() {
	rootCmd.AddCommand(buildCmd)

	buildCmd.PersistentFlags().StringVarP(&Target, "target", "t", ProjectName+"Editor", "Target to build, e.g. MyProjectEditor")
	buildCmd.PersistentFlags().StringVarP(&State, "state", "s", "Development", "State of the build, e.g. Development or Shipping")
}
