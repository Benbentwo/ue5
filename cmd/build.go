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
	Short: "Build your Project",
	Long:  `This command builds your Unreal Engine project. It can build any target but defaults to the editor target.`,
	Run:   RunBuildCommand,
}

func RunBuildCommand(cmd *cobra.Command, args []string) {
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
