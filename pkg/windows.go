//go:build windows
// +build windows

package pkg

import (
	"fmt"
	"github.com/charmbracelet/log"
	"golang.org/x/sys/windows/registry"
	"path/filepath"
	"strings"
)

const (
	EpicGamesLauncherRegKey     = `SOFTWARE\WOW6432Node\EpicGames\Epic Games Updater`
	EpicGamesLauncherPathSubkey = `EpicGamesLauncherExecutable`
	UnrealVersionSelectorPath   = `Engine\Binaries\Win64\UnrealVersionSelector.exe`
)

func UnrealEngineVersionSelectorExe() string {
	basepath, err := GetEpicGamesLauncherPath()
	if err != nil {
		log.Error(err)
	}

	return filepath.Join(basepath, UnrealVersionSelectorPath)
}

func GetEpicGamesLauncherPath() (string, error) {
	path, err := GetWindowsRegeditValue(EpicGamesLauncherRegKey, EpicGamesLauncherPathSubkey)
	if err != nil {
		log.Error("Error getting Epic Games Launcher path from registry", "error", err)
		return "", fmt.Errorf("failed to get Epic Games Launcher path: %w", err)
	}
	path = strings.TrimSuffix(path, `Portal/Binaries/Win64/EpicGamesLauncher.exe`) // TODO make this more robust
	log.Debug("Epic Games Launcher path", "path", path)
	// C:/Program Files (x86)/Epic Games/Launcher/Portal/Binaries/Win64/EpicGamesLauncher.exe
	// ->
	// C:/Program Files (x86)/Epic Games/Launcher/
	return path, nil
}

func GetWindowsRegeditValue(key, subkey string) (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, key, registry.QUERY_VALUE)
	if err != nil {
		log.Fatal(err)
	}
	defer k.Close()

	s, _, err := k.GetStringValue(subkey)
	if err != nil {
		log.Fatal(err)
	}
	return s, nil
}
