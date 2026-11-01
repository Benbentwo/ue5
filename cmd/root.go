package cmd

import (
	"fmt"
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
	Use:     "ue5",
	Short:   "UE5 CLI to help build and package Unreal Engine 5 projects",
	Long:    `UE5 CLI is a command line tool to help build and package Unreal Engine 5 projects.`,
	Version: "",
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
	versionRequested, err := cmd.Flags().GetBool("version")
	if err == nil && versionRequested {
		printVersionInfo(cmd.OutOrStdout(), Version)
		os.Exit(0)
	}
	if cmd.Name() == "help" || cmd.Name() == "version" || cmd.Name() == "upgrade" || cmd.Name() == "ue5" {
		return
	}
	// Skip PreRun for server subcommands — they handle project resolution independently
	if cmd.Name() == "server" || (cmd.Parent() != nil && cmd.Parent().Name() == "server") ||
		cmd.Name() == "ci" || (cmd.Parent() != nil && cmd.Parent().Name() == "ci") {
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

	resolved, err := resolveProjectFile(dir)
	if err != nil {
		log.Print("Please run from project directory or specify a project with --project <path>")
		log.Fatal("could not resolve project", "dir", dir, "error", err)
	}
	ResolvedUProject = resolved
	log.Debug("✅ Determined project", "uproject", ResolvedUProject)
	FileName := filepath.Base(ResolvedUProject)
	ProjectName = strings.TrimSuffix(FileName, filepath.Ext(FileName))

	UProject = pkg.NewUproject(ResolvedUProject)
	log.Debug("Loaded UProject", "Name", ProjectName, "EngineVersion", UProject.EngineAssociation)
}

// resolveProjectFile turns a --project value (a .uproject file or a directory
// holding one) into an absolute .uproject path. The path must be absolute
// because server commands send it to the daemon, which runs from a different
// working directory and would otherwise resolve "." against its own folder.
func resolveProjectFile(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("invalid project path %q: %w", dir, err)
	}
	// TODO add `.uplugin` support
	if strings.HasSuffix(abs, ".uproject") {
		return abs, nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("invalid project directory %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory or .uproject file: %s", abs)
	}
	matches, err := filepath.Glob(filepath.Join(abs, "*.uproject"))
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no .uproject file found in %s", abs)
	}
	return matches[0], nil
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
	rootCmd.PersistentFlags().Bool("version", false, "Print version and engine information")
	rootCmd.SetVersionTemplate("{{.Version}}\n")
}
