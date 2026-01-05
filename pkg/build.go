package pkg

import (
	"github.com/charmbracelet/log"
	"os/exec"
	"path/filepath"
)

var (
	WindowsBuildScript = []string{"Engine", "Build", "BatchFiles", "Build.bat"}
	UnixBuildScript    = []string{"Engine", "Build", "BatchFiles", "Mac", "Build.sh"}

	WindowsEditorBinary = []string{"Engine", "Binaries", "Win64", "UnrealEditor.exe"}
	MacEditorBinary     = []string{"Engine", "Binaries", "Mac", "UnrealEditor.app", "Contents", "MacOS", "UnrealEditor"}
	LinuxEditorBinary   = []string{"Engine", "Binaries", "Linux", "UnrealEditor"}
)

func RunBuildScript(EnginePath, Target string, Platform string, State string, ProjectPath string) error {
	// Build the script to run the Unreal Engine project

	osPath := OsStringSliceSwitcher(WindowsBuildScript, UnixBuildScript, UnixBuildScript)
	basePath := []string{EnginePath}
	pathElements := append(basePath, osPath...)
	buildScript := filepath.Join(pathElements...)
	log.Info("Running build script", "script", buildScript, "target", Target, "platform", Platform, "state", State, "projectPath", ProjectPath)

	cmd := exec.Command(buildScript, Target, Platform, State, ProjectPath)

	return RunCmd(cmd)
}

func RunEditor(EnginePath, ProjectPath string) error {
	osPath := OsStringSliceSwitcher(WindowsEditorBinary, MacEditorBinary, LinuxEditorBinary)
	basePath := []string{EnginePath}
	pathElements := append(basePath, osPath...)
	editorBinary := filepath.Join(pathElements...)

	log.Info("Launching editor", "binary", editorBinary, "project", ProjectPath)

	cmd := exec.Command(editorBinary, ProjectPath)
	cmd.Start()
	return nil
}
