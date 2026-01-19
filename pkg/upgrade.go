package pkg

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/log"
)

const (
	GitHubOwner = "Benbentwo"
	GitHubRepo  = "ue5"
)

// GitHubRelease represents the GitHub API response for a release
type GitHubRelease struct {
	TagName string         `json:"tag_name"`
	Name    string         `json:"name"`
	Body    string         `json:"body"`
	Assets  []ReleaseAsset `json:"assets"`
	HTMLURL string         `json:"html_url"`
}

// ReleaseAsset represents a downloadable asset from a release
type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// UpgradeInfo contains information about an available upgrade
type UpgradeInfo struct {
	CurrentVersion string
	LatestVersion  string
	DownloadURL    string
	AssetName      string
	ReleaseURL     string
	HasUpdate      bool
}

// CheckForUpgrade checks GitHub for the latest release and compares versions
func CheckForUpgrade(currentVersion string) (*UpgradeInfo, error) {
	release, err := fetchLatestRelease()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch latest release: %w", err)
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	currentClean := strings.TrimPrefix(currentVersion, "v")

	info := &UpgradeInfo{
		CurrentVersion: currentClean,
		LatestVersion:  latestVersion,
		ReleaseURL:     release.HTMLURL,
		HasUpdate:      compareVersions(currentClean, latestVersion) < 0,
	}

	// Find the appropriate asset for this platform
	assetPattern := getAssetPatternForPlatform()
	for _, asset := range release.Assets {
		if strings.HasSuffix(asset.Name, assetPattern) {
			info.DownloadURL = asset.BrowserDownloadURL
			info.AssetName = asset.Name
			break
		}
	}

	if info.HasUpdate && info.DownloadURL == "" {
		log.Warn("No matching binary found for your platform", "pattern", assetPattern)
	}

	return info, nil
}

// fetchLatestRelease fetches the latest release from GitHub API
func fetchLatestRelease() (*GitHubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", GitHubOwner, GitHubRepo)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "ue5-cli")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to decode release: %w", err)
	}

	return &release, nil
}

// getAssetPatternForPlatform returns a pattern to match asset names for the current platform
// GoReleaser uses format: ue5_<version>_<os>_<arch>.tar.gz
func getAssetPatternForPlatform() string {
	goos := runtime.GOOS
	arch := runtime.GOARCH

	switch goos {
	case "darwin":
		if arch == "arm64" {
			return "_darwin_arm64.tar.gz"
		}
		return "_darwin_amd64.tar.gz"
	case "linux":
		if arch == "arm64" {
			return "_linux_arm64.tar.gz"
		}
		return "_linux_amd64.tar.gz"
	case "windows":
		if arch == "arm64" {
			return "_windows_arm64.tar.gz"
		}
		return "_windows_amd64.tar.gz"
	default:
		return ""
	}
}

// compareVersions compares two semantic versions
// Returns -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2
func compareVersions(v1, v2 string) int {
	// Handle "dev" version - always considered older
	if v1 == "dev" {
		return -1
	}
	if v2 == "dev" {
		return 1
	}

	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(parts1) {
			_, _ = fmt.Sscanf(parts1[i], "%d", &n1)
		}
		if i < len(parts2) {
			_, _ = fmt.Sscanf(parts2[i], "%d", &n2)
		}

		if n1 < n2 {
			return -1
		}
		if n1 > n2 {
			return 1
		}
	}
	return 0
}

// DownloadAndInstall downloads the latest release and installs it
func DownloadAndInstall(info *UpgradeInfo) error {
	if info.DownloadURL == "" {
		return fmt.Errorf("no download URL available for your platform")
	}

	log.Info("Downloading", "url", info.DownloadURL)

	// Download to temp file
	tmpDir, err := os.MkdirTemp("", "ue5-upgrade-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	archivePath := filepath.Join(tmpDir, info.AssetName)
	if err := downloadFile(info.DownloadURL, archivePath); err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}

	log.Info("Download complete", "size", getFileSize(archivePath))

	// Extract the binary
	extractedBinary := filepath.Join(tmpDir, "ue5")
	if runtime.GOOS == "windows" {
		extractedBinary = filepath.Join(tmpDir, "ue5.exe")
	}

	if err := extractArchive(archivePath, tmpDir); err != nil {
		return fmt.Errorf("failed to extract: %w", err)
	}

	// Get current executable path
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get current executable path: %w", err)
	}
	currentExe, err = filepath.EvalSymlinks(currentExe)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	log.Info("Installing", "path", currentExe)

	// Replace the current binary
	if err := replaceBinary(extractedBinary, currentExe); err != nil {
		return fmt.Errorf("failed to install: %w", err)
	}

	return nil
}

// downloadFile downloads a file from URL to the specified path
func downloadFile(url, destPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, resp.Body)
	return err
}

// getFileSize returns a human-readable file size
func getFileSize(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "unknown"
	}
	size := info.Size()
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

// extractArchive extracts a tar.gz or zip archive
func extractArchive(archivePath, destDir string) error {
	if strings.HasSuffix(archivePath, ".zip") {
		return extractZip(archivePath, destDir)
	}
	return extractTarGz(archivePath, destDir)
}

// extractTarGz extracts a .tar.gz archive
func extractTarGz(archivePath, destDir string) error {
	cmd := exec.Command("tar", "-xzf", archivePath, "-C", destDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, string(output))
	}
	return nil
}

// extractZip extracts a .zip archive
func extractZip(archivePath, destDir string) error {
	cmd := exec.Command("unzip", "-o", archivePath, "-d", destDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, string(output))
	}
	return nil
}

// replaceBinary replaces the current binary with a new one
func replaceBinary(newBinary, currentBinary string) error {
	// Check if new binary exists
	if _, err := os.Stat(newBinary); os.IsNotExist(err) {
		return fmt.Errorf("new binary not found at %s", newBinary)
	}

	// Make the new binary executable
	if err := os.Chmod(newBinary, 0755); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}

	// On Unix, we can rename over the running binary
	// On Windows, we need to rename the old binary first
	if runtime.GOOS == "windows" {
		oldBinary := currentBinary + ".old"
		_ = os.Remove(oldBinary) // Remove any existing old binary
		if err := os.Rename(currentBinary, oldBinary); err != nil {
			return fmt.Errorf("failed to rename old binary: %w", err)
		}
	}

	// Copy new binary to current location
	if err := copyFile(newBinary, currentBinary); err != nil {
		return fmt.Errorf("failed to copy new binary: %w", err)
	}

	// Make sure the new binary is executable
	if err := os.Chmod(currentBinary, 0755); err != nil {
		return fmt.Errorf("failed to make installed binary executable: %w", err)
	}

	return nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = sourceFile.Close() }()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = destFile.Close() }()

	_, err = io.Copy(destFile, sourceFile)
	return err
}
