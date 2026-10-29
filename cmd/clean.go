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
	Plugins          = "Plugins"
)

var cleanAll bool

// cleanCmd represents the clean command
var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Removes cache and intermediate files from the project",
	Long: `
Removes the following directories from the project:
	- DerivedDataCache
	- Intermediate
	- Binaries
	- Build
	- dist

With --all flag, also cleans Intermediate and Binaries directories
from all plugins in the Plugins directory.
`,
	Run: func(cmd *cobra.Command, args []string) {
		log.Info("Cleaning project directories", "project", ResolvedUProject)

		CleanDir(DerivedDataCache)
		CleanDir(Intermediate)
		CleanDir(Binaries)
		CleanDir(Build)
		CleanDir(Dist)

		if cleanAll {
			CleanPluginDirs()
		}

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

func CleanPluginDirs() {
	projectPath := filepath.Dir(ResolvedUProject)
	pluginsPath := filepath.Join(projectPath, Plugins)

	entries, err := os.ReadDir(pluginsPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Debug("No Plugins directory found, skipping plugin cleanup")
			return
		}
		log.Fatal("Failed to read Plugins directory", "error", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pluginPath := filepath.Join(pluginsPath, entry.Name())
		cleanPluginDir(pluginPath, Intermediate)
		cleanPluginDir(pluginPath, Binaries)
	}
}

func cleanPluginDir(pluginPath, dir string) {
	rmPath := filepath.Join(pluginPath, dir)
	if _, err := os.Stat(rmPath); os.IsNotExist(err) {
		return
	}
	log.Debug("Cleaning plugin directory", "path", rmPath)
	err := os.RemoveAll(rmPath)
	if err != nil {
		log.Fatal("Failed to clean plugin directory", "dir", rmPath, "error", err)
	}
}

func init() {
	rootCmd.AddCommand(cleanCmd)
	cleanCmd.Flags().BoolVarP(&cleanAll, "all", "a", false, "Also clean Intermediate and Binaries directories from plugins")
}
