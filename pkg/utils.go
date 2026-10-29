package pkg

import (
	"bufio"
	"io"
	"os/exec"
	"runtime"

	"github.com/charmbracelet/log"
)

func GetPlatform() string {
	// Convert the OS to a platform string
	platform := OsToPlatform(runtime.GOOS)
	if platform == "" {
		log.Error("Unsupported operating system", "os", runtime.GOOS)
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
		return "Win64"
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
		parsed := ParseLine(line)

		switch parsed.Level {
		case LogLevelError:
			log.WithPrefix("|").Error(line)
		case LogLevelWarning:
			log.WithPrefix("|").Warn(line)
		default:
			log.WithPrefix("|").Info(line)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Error("Error reading output", "label", label, "error", err)
	}
}

func OsStringSliceSwitcher(Windows []string, Mac []string, Linux []string) []string {
	// Switch based on the current operating system
	switch runtime.GOOS {
	case "windows":
		return Windows
	case "darwin":
		return Mac
	case "linux":
		return Linux // Default to Mac for Linux as well
	default:
		log.Error("Unsupported operating system", "os", runtime.GOOS)
		return nil
	}
}
