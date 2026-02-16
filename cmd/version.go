package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Benbentwo/ue5/pkg"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	Run: func(cmd *cobra.Command, args []string) {
		printVersionInfo(cmd.OutOrStdout(), Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

func printVersionInfo(out io.Writer, cliVersion string) {
	if _, err := fmt.Fprintf(out, "CLI: %s\n", cliVersion); err != nil {
		return
	}
	engineVersion := detectEngineVersion()
	if engineVersion == "" {
		_, _ = fmt.Fprintln(out, "Engine: (not detected)")
		return
	}
	_, _ = fmt.Fprintf(out, "Engine: %s\n", engineVersion)
}

func detectEngineVersion() string {
	uprojectPath, err := resolveUProjectForVersion()
	if err != nil || uprojectPath == "" {
		return ""
	}
	uproject, err := pkg.NewUprojectE(uprojectPath)
	if err != nil || uproject == nil {
		return ""
	}
	return strings.TrimSpace(uproject.EngineAssociation)
}

func resolveUProjectForVersion() (string, error) {
	dir := projectPathFlag
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}

	if strings.HasSuffix(dir, ".uproject") {
		return dir, nil
	}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", nil
	}

	matches, err := filepath.Glob(filepath.Join(dir, "*.uproject"))
	if err != nil || len(matches) == 0 {
		return "", nil
	}
	return matches[0], nil
}
