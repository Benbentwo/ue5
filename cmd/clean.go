/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
)

const (
	DerivedDataCache = "DerivedDataCache"
	Intermediate     = "Intermediate"
	Binaries         = "Binaries"
	Build            = "Build"
	Dist             = "dist"
)

// cleanCmd represents the clean command
var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Removes cache and intermediate files from the project",
	Long: `
Removes the following directories from the project:
	- DerivedDataCache
	- Intermediate
	- Binaries
	- dist
`,
	Run: func(cmd *cobra.Command, args []string) {
		log.Info("Cleaning project directories", "project", ResolvedUProject)

		CleanDir(DerivedDataCache)
		CleanDir(Intermediate)
		CleanDir(Binaries)
		CleanDir(Build)
		CleanDir(Dist)

		log.Info("✅  Project cleaned successfully", "project", ProjectName)
	},
}

func CleanDir(dir string) {
	projectPath := filepath.Dir(ResolvedUProject)
	rmPath := filepath.Join(projectPath, dir)
	err := os.RemoveAll(rmPath)
	if err != nil {
		log.Fatal("Failed to clean directory", "dir", rmPath, "error", err)
	}
}

func init() {
	rootCmd.AddCommand(cleanCmd)
}
