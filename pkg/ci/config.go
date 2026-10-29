package ci

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Config is ~/.ue5/ci/config.json. Every field has a working default, so a
// box needs no config file at all to start taking jobs.
type Config struct {
	// Enabled turns the runner on. Off by default: a developer laptop running
	// the daemon for editor management must never start pulling builds.
	Enabled bool `json:"enabled"`
	// Box is the name shown on the dashboard and in commit statuses.
	Box string `json:"box"`
	// WorkRoot holds one checkout per repo: <WorkRoot>/<Game> and
	// <WorkRoot>/<PluginsDir>/<Name>. Point it at an existing checkout root to
	// reuse what a previous CI setup already downloaded.
	WorkRoot string `json:"work_root"`
	// PluginsDir is where pinned plugin repos go, relative to WorkRoot. The
	// default suits a .uproject with "AdditionalPluginDirectories": ["../Shared/Plugins"].
	PluginsDir string `json:"plugins_dir"`
	// ArtifactRoot receives each job's packaged output, <ArtifactRoot>/<Game>/<job>.
	ArtifactRoot string `json:"artifact_root"`
	// KeepArtifacts is how many packaged builds to keep per game.
	KeepArtifacts int `json:"keep_artifacts"`
	// DiskFloorGB holds the queue (rather than failing every job) while free
	// space on WorkRoot is below this.
	DiskFloorGB int `json:"disk_floor_gb"`
	// ReclaimCommand runs once when the disk is below the floor, before holding.
	ReclaimCommand []string `json:"reclaim_command,omitempty"`
	// PublicURL is how other machines reach this dashboard; it goes into the
	// commit status link. Default http://<hostname>.local:9516.
	PublicURL string `json:"public_url"`
	// Peers are other build boxes' dashboards, shown on the same page.
	Peers []string `json:"peers,omitempty"`
	// TimeoutMinutes caps one job per kind.
	TimeoutMinutes map[Kind]int `json:"timeout_minutes,omitempty"`
}

// Dir is ~/.ue5/ci.
func Dir(home string) string { return filepath.Join(home, "ci") }

// LoadConfig reads the config file and fills defaults.
func LoadConfig(home string) (Config, error) {
	var c Config
	data, err := os.ReadFile(filepath.Join(Dir(home), "config.json"))
	if err != nil && !os.IsNotExist(err) {
		return c, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &c); err != nil {
			return c, err
		}
	}
	c.fill()
	return c, nil
}

func (c *Config) fill() {
	host := localHostName()
	if c.Box == "" {
		c.Box = host
	}
	userHome, _ := os.UserHomeDir()
	if c.WorkRoot == "" {
		c.WorkRoot = filepath.Join(userHome, "ue5-ci", "work")
	}
	c.WorkRoot = expandHome(c.WorkRoot, userHome)
	if c.PluginsDir == "" {
		c.PluginsDir = filepath.Join("Shared", "Plugins")
	}
	if c.ArtifactRoot == "" {
		c.ArtifactRoot = filepath.Join(c.WorkRoot, "_artifacts")
	}
	c.ArtifactRoot = expandHome(c.ArtifactRoot, userHome)
	if c.KeepArtifacts <= 0 {
		c.KeepArtifacts = 3
	}
	if c.DiskFloorGB <= 0 {
		c.DiskFloorGB = 50
	}
	if c.PublicURL == "" {
		c.PublicURL = "http://" + host + ".local:9516"
	}
	c.PublicURL = strings.TrimRight(c.PublicURL, "/")
	if c.TimeoutMinutes == nil {
		c.TimeoutMinutes = map[Kind]int{}
	}
	for k, v := range map[Kind]int{KindCompile: 180, KindPackage: 480, KindSteam: 120} {
		if c.TimeoutMinutes[k] <= 0 {
			c.TimeoutMinutes[k] = v
		}
	}
}

// Timeout for one job of kind k.
func (c Config) Timeout(k Kind) time.Duration {
	return time.Duration(c.TimeoutMinutes[k]) * time.Minute
}

func expandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

// DispatchToken returns the box's dispatch token, creating it (0600) on first
// use. Only the user the daemon runs as can read it, and the Azure agent runs as
// that same user, so it can dispatch while another device on the LAN cannot.
func DispatchToken(home string) (string, error) {
	path := filepath.Join(Dir(home), "token")
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		return strings.TrimSpace(string(b)), nil
	}
	if err := os.MkdirAll(Dir(home), 0o700); err != nil {
		return "", err
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b)
	if err := os.WriteFile(path, []byte(tok+"\n"), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

// Secret looks a named secret up, first in the environment
// (UE5_CI_<NAME>, e.g. UE5_CI_AZURE_PAT), then in the macOS Keychain
// (service "ue5-ci", account <name>), then in ~/.ue5/ci/secrets/<name> (a
// file only its owner can read; the Windows fallback until Credential Manager
// support lands). Empty means not configured.
func Secret(home, name string) string {
	env := "UE5_CI_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	if v := strings.TrimSpace(os.Getenv(env)); v != "" {
		return v
	}
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("security", "find-generic-password", "-s", "ue5-ci", "-a", name, "-w").Output()
		if err == nil {
			if v := strings.TrimSpace(string(out)); v != "" {
				return v
			}
		}
	}
	if b, err := os.ReadFile(filepath.Join(Dir(home), "secrets", name)); err == nil {
		return strings.TrimSpace(string(b))
	}
	return ""
}

// localHostName is the name other machines on the LAN reach this one by
// (<name>.local via Bonjour/mDNS). On macOS that is LocalHostName, which can
// differ from os.Hostname (e.g. "Mac.localdomain" from DHCP).
func localHostName() string {
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("scutil", "--get", "LocalHostName").Output(); err == nil {
			if h := strings.TrimSpace(string(out)); h != "" {
				return h
			}
		}
	}
	host, _ := os.Hostname()
	for _, suffix := range []string{".local", ".localdomain", ".lan", ".home"} {
		host = strings.TrimSuffix(host, suffix)
	}
	return host
}
