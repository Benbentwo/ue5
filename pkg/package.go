package pkg

import (
	"github.com/charmbracelet/log"
	"os"
	"os/exec"
	"path/filepath"
)

var (
	WindowsUATScript = []string{"Engine", "Build", "BatchFiles", "RunUAT.bat"}
	UnixUATScript    = []string{"Engine", "Build", "BatchFiles", "RunUAT.sh"}
)

func RunPackageScript(EnginePath, Platform string, State string, ProjectPath string, OutDir string) error {
	// Build the script to run the Unreal Engine project
	osPath := OsStringSliceSwitcher(WindowsUATScript, UnixUATScript, UnixUATScript)
	basePath := []string{EnginePath}
	pathElements := append(basePath, osPath...)
	buildScript := filepath.Join(pathElements...)

	log.Info("Running Package script", "script", buildScript, "platform", Platform, "state", State, "projectPath", ProjectPath)
	archivedir := makeArchiveDirectory(ProjectPath, OutDir)

	cmd := exec.Command(buildScript,
		"BuildCookRun",
		"-project="+ProjectPath,
		"-noP4",
		"-platform="+Platform,
		"-clientconfig="+State,
		"-serverconfig="+State,
		"-cook",
		"-allmaps",
		"-build",
		"-stage",
		"-prereqs",
		"-pak",
		"-archive",
		"-archivedirectory="+archivedir,
	)

	return RunCmd(cmd)
}

func makeArchiveDirectory(ResolvedUProject string, archiveDir string) string {
	ProjectDir := filepath.Dir(ResolvedUProject)
	ResolvedArchiveDirPlatform := filepath.Join(ProjectDir, archiveDir, GetPlatform())
	ResolvedArchiveDir := filepath.Join(ProjectDir, archiveDir)
	info, err := os.Stat(ResolvedArchiveDirPlatform)
	if os.IsNotExist(err) {
		err := os.MkdirAll(ResolvedArchiveDirPlatform, os.ModePerm)
		if err != nil {
			return ResolvedArchiveDir
		}
	} else {
		// It already exists, check if it's a directory
		if info.IsDir() {
			return ResolvedArchiveDir
		} else {
			log.Fatal("Archive directory exists but is not a directory", "path", ResolvedArchiveDir)
			return ResolvedArchiveDir
		}
	}
	return ResolvedArchiveDir
}
