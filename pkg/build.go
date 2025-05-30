package pkg

import (
	"github.com/charmbracelet/log"
	"os/exec"
	"path"
)

func RunBuildScript(EnginePath, Target string, Platform string, State string, ProjectPath string) error {
	// Build the script to run the Unreal Engine project
	buildScript := path.Join(EnginePath, "Engine", "Build", "BatchFiles", "Mac", "Build.sh")
	log.Info("Running build script", "script", buildScript, "target", Target, "platform", Platform, "state", State, "projectPath", ProjectPath)

	cmd := exec.Command(buildScript, Target, Platform, State, ProjectPath)

	return RunCmd(cmd)
}
