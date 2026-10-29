package pkg

import (
	"github.com/charmbracelet/log"
	"os/exec"
	"path/filepath"

	"runtime"
)

const GenerateProjectFilesSubcommand = `/projectfiles`

var (
	WindowsGenScript = []string{"GenerateProjectFiles.bat"}
	MacGenScript     = []string{"Engine", "Build", "BatchFiles", "Mac", "GenerateProjectFiles.sh"}
)

func RunGenScript(EnginePath, ProjectPath string) error {
	var genScript string
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command(UnrealEngineVersionSelectorExe(), GenerateProjectFilesSubcommand, ProjectPath)
	} else {
		genScript = unixBasedProjectGeneration(EnginePath)
		cmd = exec.Command(genScript, ProjectPath)
	}

	log.Info("Running generate script", "projectPath", ProjectPath)

	return RunCmd(cmd)
}

func unixBasedProjectGeneration(EnginePath string) string {
	osSpecificScript := getOsGenScript()
	basePath := []string{EnginePath}
	pathElements := append(basePath, osSpecificScript...)
	genScript := filepath.Join(pathElements...)
	return genScript
}

func getOsGenScript() []string {
	switch runtime.GOOS {
	case "windows":
		return WindowsGenScript
	case "darwin":
		return MacGenScript
	case "linux":
		return MacGenScript
	default:
		return []string{}
	}
}
