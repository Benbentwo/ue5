/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"github.com/Benbentwo/ue5/pkg"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start the Unreal Editor for the current project",
	Long:  `This command launches the Unreal Editor for your project, opening the .uproject file directly in the engine.`,
	Run:   RunRunCommand,
}

func RunRunCommand(cmd *cobra.Command, args []string) {
	if EnginePath == "" {
		log.Error("No engine path found")
		return
	}

	log.Info("Starting Unreal Editor", "project", ProjectName, "engine", UProject.EngineAssociation)
	err := pkg.RunEditor(EnginePath, ResolvedUProject)
	if err != nil {
		log.Error("Failed to start Unreal Editor", "error", err)
		return
	}
}

func init() {
	rootCmd.AddCommand(runCmd)
}
