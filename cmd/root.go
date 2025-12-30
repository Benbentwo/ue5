package cmd

import (
	"github.com/Benbentwo/ue5/pkg"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
	"strings"
)

var (
	Version          = "dev"
	projectPathFlag  string // From --project flag
	ResolvedUProject string // Resolved path to *.uproject file
	UProject         *pkg.Uproject
	ProjectName      string // Name of the project, derived from the .uproject filename
	EnginePath       string // Path to the Unreal Engine installation
	Debug            bool   // Debug flag to enable debug logging
	Target           string // Target to build, e.g. "MyProjectEditor"
	State            string // State of the build, e.g. "Development" or "Shipping"
)

var rootCmd = &cobra.Command{
	Use:   "ue5",
	Short: "UE5 CLI to help build and package Unreal Engine 5 projects",
	Long:  `UE5 CLI is a command line tool to help build and package Unreal Engine 5 projects.`,
	Version: Version,
	Run: func(cmd *cobra.Command, args []string) {
		err := cmd.Help()
		if err != nil {
			return
		}
	},
	PersistentPreRun: PreRun,
}

// Function for main.go to call to execute the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		log.Error("Error executing command", "error", err)
		os.Exit(1)
	}
}

func PreRun(cmd *cobra.Command, args []string) {
	if cmd.Name() == "help" || cmd.Name() == "version" || cmd.Name() == "ue5" {
		return
	}
	if Debug {
		log.SetLevel(log.DebugLevel)
	}
	UpdateProjectPath()
	UpdateEnginePath()
	log.Info("Running", "project", ProjectName, "Engine", UProject.EngineAssociation, "EnginePath", EnginePath)
}

// UpdateProjectPath Determines the project path based on the --project flag or current directory
// and updates the ResolvedUProject variable.
func UpdateProjectPath() {
	log.Debug("Checking for project directory")
	dir := projectPathFlag
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			log.Fatal("could not get current directory", "error", err)
		}
	}

	info, err := os.Stat(dir)
	// TODO add `.uplugin` support
	if strings.HasSuffix(dir, ".uproject") {
		// If the path ends with .uproject, treat it as a project file
		log.Debug("Project path is a .uproject file", "path", dir)
		ResolvedUProject = dir
	} else if err != nil || !info.IsDir() {
		// If the path is not a directory or does not exist, log an error
		// e.g. if someone passes myproject.txt or a non-existent directory
		log.Fatal("invalid project directory", "dir", dir, "info", info, "error", err)
	} else {
		// else (if the path is a directory)
		// Search for *.uproject
		matches, err := filepath.Glob(filepath.Join(dir, "*.uproject"))
		if err != nil || len(matches) == 0 {
			log.Print("Please run from project directory or specify a project with --project <path>")
			log.Fatal("no .uproject file found in the directory", "dir", dir)
		}
		ResolvedUProject = matches[0]

	}
	log.Debug("✅ Determined project", "uproject", ResolvedUProject)
	FileName := filepath.Base(ResolvedUProject)
	ProjectName = strings.TrimSuffix(FileName, filepath.Ext(FileName))

	UProject = pkg.NewUproject(ResolvedUProject)
	log.Debug("Loaded UProject", "Name", ProjectName, "EngineVersion", UProject.EngineAssociation)
}

func UpdateEnginePath() {
	if ResolvedUProject == "" {
		log.Error("No project path set. Please run 'ue5cli --project <path>' to set the project path.")
		return
	}

	EnginePath = pkg.GetEnginePath(UProject.EngineAssociation)
	log.Debug("Engine Path", "path", EnginePath)
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&projectPathFlag, "project", "p", "", "Path to the project directory (default: current directory)")
	rootCmd.PersistentFlags().BoolVarP(&Debug, "debug", "d", false, "Enable debug logging")
	rootCmd.SetVersionTemplate("{{.Version}}\n")
}
