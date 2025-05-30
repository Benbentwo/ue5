package pkg

import (
	"github.com/charmbracelet/log"
	"os"
	"os/exec"
	"path"
)

///Path/To/UE5/Engine/Build/BatchFiles/RunUAT.sh BuildCookRun \
//-project="/Path/To/Project/ProjectName.uproject" \
//-noP4 -platform=Mac -clientconfig=Development -serverconfig=Development \
//-cook -allmaps -build -stage -pak -archive \
//-archivedirectory="/Path/To/Output"

func RunPackageScript(EnginePath, Platform string, State string, ProjectPath string) error {
	// Build the script to run the Unreal Engine project
	buildScript := path.Join(EnginePath, "Engine", "Build", "BatchFiles", "RunUAT.sh")
	log.Info("Running Package script", "script", buildScript, "platform", Platform, "state", State, "projectPath", ProjectPath)
	archivedir := makeArchiveDirectory(ProjectPath, "dist")

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
	ProjectDir := path.Dir(ResolvedUProject)
	ResolvedArchiveDirPlatform := path.Join(ProjectDir, archiveDir, GetPlatform())
	ResolvedArchiveDir := path.Join(ProjectDir, archiveDir)
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
