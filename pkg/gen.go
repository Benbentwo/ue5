package pkg

import (
	"github.com/charmbracelet/log"
	"os/exec"
	"path"
)

func RunGenScript(EnginePath, ProjectPath string) error {
	genScript := path.Join(EnginePath, "Engine", "Build", "BatchFiles", "Mac", "GenerateProjectFiles.sh")
	log.Info("Running generate script", "script", genScript, "projectPath", ProjectPath)

	cmd := exec.Command(genScript, ProjectPath)

	return RunCmd(cmd)
}
