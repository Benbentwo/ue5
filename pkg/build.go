package pkg

import (
	"fmt"
	"io"
	"os/exec"
	"path/filepath"

	"github.com/charmbracelet/log"
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

// RunBuildScriptToWriter runs the build script and pipes all output to the given writer
// instead of the charmbracelet logger. Used by the daemon's build orchestrator.
func RunBuildScriptToWriter(enginePath, target, platform, state, projectPath string, output io.Writer) error {
	osPath := OsStringSliceSwitcher(WindowsBuildScript, UnixBuildScript, UnixBuildScript)
	basePath := []string{enginePath}
	pathElements := append(basePath, osPath...)
	buildScript := filepath.Join(pathElements...)

	cmd := exec.Command(buildScript, target, platform, state, projectPath)
	cmd.Stdout = output
	cmd.Stderr = output
	return cmd.Run()
}

// RunBuildScriptPiped prepares a build command with piped stdout/stderr.
// The caller is responsible for calling cmd.Start(), reading from the pipes,
// and calling cmd.Wait(). extraArgs are passed through Build.sh/Build.bat
// verbatim to UnrealBuildTool.
func RunBuildScriptPiped(enginePath, target, platform, state, projectPath string, extraArgs ...string) (cmd *exec.Cmd, stdout io.ReadCloser, stderr io.ReadCloser, err error) {
	osPath := OsStringSliceSwitcher(WindowsBuildScript, UnixBuildScript, UnixBuildScript)
	basePath := []string{enginePath}
	pathElements := append(basePath, osPath...)
	buildScript := filepath.Join(pathElements...)

	args := append([]string{target, platform, state, projectPath}, extraArgs...)
	cmd = exec.Command(buildScript, args...)

	stdout, err = cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err = cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("stderr pipe: %w", err)
	}

	return cmd, stdout, stderr, nil
}

// EditorBinaryPath returns the full path to the Unreal Editor binary for the given engine installation.
func EditorBinaryPath(enginePath string) string {
	osPath := OsStringSliceSwitcher(WindowsEditorBinary, MacEditorBinary, LinuxEditorBinary)
	basePath := []string{enginePath}
	pathElements := append(basePath, osPath...)
	return filepath.Join(pathElements...)
}

func RunEditor(EnginePath, ProjectPath string) error {
	editorBinary := EditorBinaryPath(EnginePath)

	log.Info("Launching editor", "binary", editorBinary, "project", ProjectPath)

	cmd := exec.Command(editorBinary, ProjectPath)
	return cmd.Start()
}
