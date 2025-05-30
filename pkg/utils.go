package pkg

import (
	"bufio"
	"github.com/charmbracelet/log"
	"io"
	"os/exec"
	"runtime"
	"strings"
)

func GetPlatform() string {
	// Get the current operating system
	os := runtime.GOOS
	// Convert the OS to a platform string
	platform := OsToPlatform(os)
	if platform == "" {
		log.Error("Unsupported operating system", "os", os)
		return ""
	}
	return platform
}

func OsToPlatform(os string) string {
	switch os {
	case "darwin":
		return "Mac"
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	default:
		return ""
	}
}

func RunCmd(cmd *exec.Cmd) error {
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return err
	}

	// Stream stdout
	go streamOutput(stdoutPipe, "stdout")
	// Stream stderr
	go streamOutput(stderrPipe, "stderr")

	// Wait for command to finish
	return cmd.Wait()
}

func streamOutput(r io.Reader, label string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if label == "stderr" {
			log.WithPrefix("|").Warn(line)
		} else if strings.Contains(line, "A conflicting instance of UnrealBuildTool is already running") {
			log.WithPrefix("|").Error(line)
		} else if strings.Contains(strings.ToLower(line), "error:") {
			log.WithPrefix("|").Error(line)
		} else {
			log.WithPrefix("|").Info(line)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Error("Error reading output", "label", label, "error", err)
	}
}
