package pkg

import (
	"encoding/json"
	"github.com/charmbracelet/log"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	MacOsManifestsPath     = "~/Library/Application Support/Epic/EpicGamesLauncher/Data/Manifests/"
	WindowsOsManifestsPath = "C:\\ProgramData\\Epic\\EpicGamesLauncher\\Data\\Manifests"
)

func GetEnginePath(version string) string {
	manifest, err := FindManifestForEngineVersion(version)
	if err != nil {
		log.Error("Error finding manifest for engine version", "version", version, "error", err)
		return ""
	}
	if manifest == nil {
		log.Error("No manifest found for engine version", "version", version)
		return ""
	}
	return manifest.InstallLocation
}

func GetOSManifestsPath() string {
	log.Debug("Determining OS manifests path", "os", runtime.GOOS)
	switch runtime.GOOS {
	case "darwin":
		return MacOsManifestsPath
	case "windows":
		return WindowsOsManifestsPath
	case "linux":
		return MacOsManifestsPath
	default:
		return ""
	}

}
func GetOSManifestsPathSafe() string {
	path, err := expandPath(GetOSManifestsPath())
	if err != nil {
		log.Error("Error expanding OS manifests path", "error", err)
		return ""
	}
	return path
}
func expandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		// Replace only the leading ~ (~/ or ~)
		if path == "~" {
			return home, nil
		} else if strings.HasPrefix(path, "~/") {
			return filepath.Join(home, path[2:]), nil
		}
	}
	return path, nil
}

func FindManifestForEngineVersion(version string) (*Manifest, error) {
	log.Debug("Finding manifest for engine version", "version", version)
	manifestsPath := GetOSManifestsPathSafe()

	var CorrectManifest *Manifest
	log.Debug("Walking through manifests directory", "path", manifestsPath)
	err := filepath.WalkDir(manifestsPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err // skip files with read errors
		}

		if d.IsDir() {
			return nil // skip directories
		}

		manifest, err := NewManifestE(path)
		if err != nil {
			log.Debug("Skipping file due to error reading manifest", "path", path, "error", err)
			return nil
		}
		if manifest != nil && manifest.DisplayName == "Unreal Engine" {
			foundVersion := strings.TrimPrefix(manifest.AppName, "UE_")
			log.Debug("Found manifest for engine version", "path", path, "foundVersion", foundVersion)
			if foundVersion == version {
				log.Debug("Manifest matches requested version", "version", version)
				CorrectManifest = manifest
				return filepath.SkipDir // Stop walking the directory
			} else {
				log.Debug("Manifest does not match requested version", "version", version, "foundVersion", foundVersion)
			}
		}
		return nil // Continue walking the directory
	})
	if err != nil {
		log.Error("Error walking through manifests directory", "path", manifestsPath, "error", err)
		return nil, err // Return error if walking the directory fails
	}

	return CorrectManifest, nil
}

func NewManifestE(path string) (*Manifest, error) {
	log.Debug("Reading manifest file", "path", path)
	var manifest Manifest
	bytes, err := os.ReadFile(path) // Read the file content
	if err != nil {
		return nil, err // Return error if file reading fails
	}
	err = json.Unmarshal(bytes, &manifest)
	if err != nil {
		return nil, err
	} // Unmarshal JSON data into Uproject struct

	return &manifest, nil // Return the populated Uproject struct
}

func NewManifest(path string) *Manifest {
	uProject, err := NewManifestE(path)
	if err != nil {
		log.Fatal("Failed to render Manifest", "err", err) // Panic if there is an error reading or parsing the file
	}
	return uProject // Return the populated Uproject struct
}

type Manifest struct {
	FormatVersion               int           `json:"FormatVersion"`
	BIsIncompleteInstall        bool          `json:"bIsIncompleteInstall"`
	LaunchCommand               string        `json:"LaunchCommand"`
	LaunchExecutable            string        `json:"LaunchExecutable"`
	ManifestLocation            string        `json:"ManifestLocation"`
	ManifestHash                string        `json:"ManifestHash"`
	BIsApplication              bool          `json:"bIsApplication"`
	BIsExecutable               bool          `json:"bIsExecutable"`
	BIsManaged                  bool          `json:"bIsManaged"`
	BNeedsValidation            bool          `json:"bNeedsValidation"`
	BRequiresAuth               bool          `json:"bRequiresAuth"`
	BAllowMultipleInstances     bool          `json:"bAllowMultipleInstances"`
	BCanRunOffline              bool          `json:"bCanRunOffline"`
	BAllowUriCmdArgs            bool          `json:"bAllowUriCmdArgs"`
	BLaunchElevated             bool          `json:"bLaunchElevated"`
	BaseURLs                    []string      `json:"BaseURLs"`
	BuildLabel                  string        `json:"BuildLabel"`
	AppCategories               []string      `json:"AppCategories"`
	ChunkDbs                    []interface{} `json:"ChunkDbs"`
	CompatibleApps              []interface{} `json:"CompatibleApps"`
	DisplayName                 string        `json:"DisplayName"`
	InstallationGuid            string        `json:"InstallationGuid"`
	InstallLocation             string        `json:"InstallLocation"`
	InstallSessionId            string        `json:"InstallSessionId"`
	InstallTags                 []string      `json:"InstallTags"`
	InstallComponents           []string      `json:"InstallComponents"`
	HostInstallationGuid        string        `json:"HostInstallationGuid"`
	PrereqIds                   []interface{} `json:"PrereqIds"`
	PrereqSHA1Hash              string        `json:"PrereqSHA1Hash"`
	LastPrereqSucceededSHA1Hash string        `json:"LastPrereqSucceededSHA1Hash"`
	StagingLocation             string        `json:"StagingLocation"`
	TechnicalType               string        `json:"TechnicalType"`
	VaultThumbnailUrl           string        `json:"VaultThumbnailUrl"`
	VaultTitleText              string        `json:"VaultTitleText"`
	InstallSize                 int64         `json:"InstallSize"`
	MainWindowProcessName       string        `json:"MainWindowProcessName"`
	ProcessNames                []interface{} `json:"ProcessNames"`
	BackgroundProcessNames      []interface{} `json:"BackgroundProcessNames"`
	IgnoredProcessNames         []interface{} `json:"IgnoredProcessNames"`
	DlcProcessNames             []interface{} `json:"DlcProcessNames"`
	MandatoryAppFolderName      string        `json:"MandatoryAppFolderName"`
	OwnershipToken              string        `json:"OwnershipToken"`
	SidecarConfigRevision       int           `json:"SidecarConfigRevision"`
	CatalogNamespace            string        `json:"CatalogNamespace"`
	CatalogItemId               string        `json:"CatalogItemId"`
	AppName                     string        `json:"AppName"`
	AppVersionString            string        `json:"AppVersionString"`
	MainGameCatalogNamespace    string        `json:"MainGameCatalogNamespace"`
	MainGameCatalogItemId       string        `json:"MainGameCatalogItemId"`
	MainGameAppName             string        `json:"MainGameAppName"`
	AllowedUriEnvVars           []interface{} `json:"AllowedUriEnvVars"`
}
