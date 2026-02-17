package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Benbentwo/ue5/pkg/server"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

var (
	serverRunWait         bool
	serverRunTimeout      time.Duration
	serverRunPollInterval time.Duration
	serverRunJSON         bool
)

var serverRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Start an editor instance via the server daemon",
	Long: `Starts the Unreal Editor for the current project, managed by the server daemon.
The daemon is auto-started if not already running. Logs are captured and queryable
via 'ue5 server logs'.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Ensure daemon is running
		if err := server.EnsureDaemon(); err != nil {
			log.Fatal("Failed to ensure daemon is running", "error", err)
		}

		// Resolve project and engine (reuse existing root.go logic)
		UpdateProjectPath()
		UpdateEnginePath()

		client := server.NewClient()
		resp, err := client.Send(server.Request{
			ID:   "start-editor",
			Type: server.ReqStartEditor,
			StartEditor: &server.StartEditorRequest{
				ProjectPath: ResolvedUProject,
				EnginePath:  EnginePath,
			},
		})
		if err != nil {
			log.Fatal("Failed to send start request", "error", err)
		}

		alreadyRunning := false
		if !resp.Success {
			alreadyRunning = strings.Contains(strings.ToLower(resp.Error), "already running")
			if !alreadyRunning {
				log.Error("Failed to start editor", "error", resp.Error)
				return
			}
		}

		instance := resp.Instance
		if instance == nil {
			instance, err = getProjectInstance(client, ResolvedUProject)
			if err != nil {
				log.Error("Failed to query instance after start", "error", err)
				return
			}
		}

		if serverRunWait {
			instance, err = waitForProjectState(client, ResolvedUProject, server.StateRunning, serverRunTimeout, serverRunPollInterval)
			if err != nil {
				log.Error("Editor did not reach running state", "error", err)
				if instance != nil {
					log.Info("Current instance state", "state", instance.State, "pid", instance.PID)
				}
				return
			}
		}

		if instance == nil {
			log.Error("No instance found for project after start request")
			return
		}

		if serverRunJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(instance); err != nil {
				log.Error("Failed to encode run output", "error", err)
			}
			return
		}

		if alreadyRunning {
			log.Info("Editor already running",
				"project", instance.ProjectName,
				"state", instance.State,
				"pid", instance.PID,
				"log", instance.LogFile,
			)
			return
		}

		log.Info("Editor launch acknowledged",
			"project", instance.ProjectName,
			"state", instance.State,
			"pid", instance.PID,
			"log", instance.LogFile,
		)
		if instance.State == server.StateStarting && !serverRunWait {
			log.Info("Instance is still starting. Use --wait or poll 'ue5 server status --json' for readiness.")
		}
	},
}

func getProjectInstance(client *server.Client, projectPath string) (*server.InstanceInfo, error) {
	resp, err := client.Send(server.Request{
		ID:   "list-instances",
		Type: server.ReqListInstances,
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("status request failed: %s", resp.Error)
	}

	for i := range resp.Instances {
		if resp.Instances[i].ProjectPath == projectPath {
			inst := resp.Instances[i]
			return &inst, nil
		}
	}
	return nil, nil
}

func waitForProjectState(
	client *server.Client,
	projectPath string,
	desired server.InstanceState,
	timeout time.Duration,
	pollInterval time.Duration,
) (*server.InstanceInfo, error) {
	if timeout <= 0 {
		return nil, fmt.Errorf("timeout must be greater than zero")
	}
	if pollInterval <= 0 {
		return nil, fmt.Errorf("poll interval must be greater than zero")
	}

	deadline := time.Now().Add(timeout)
	for {
		instance, err := getProjectInstance(client, projectPath)
		if err != nil {
			return nil, err
		}

		if instance != nil {
			if instance.State == desired {
				return instance, nil
			}
			if instance.State == server.StateStopped || instance.State == server.StateCrashed {
				return instance, fmt.Errorf("instance entered terminal state %q while waiting for %q", instance.State, desired)
			}
		}

		if time.Now().After(deadline) {
			if instance == nil {
				return nil, fmt.Errorf("timed out waiting for instance to appear")
			}
			return instance, fmt.Errorf("timed out waiting for %q (current state: %q)", desired, instance.State)
		}

		time.Sleep(pollInterval)
	}
}

func init() {
	serverRunCmd.Flags().BoolVar(&serverRunWait, "wait", false, "Wait until the instance reaches running state")
	serverRunCmd.Flags().DurationVar(&serverRunTimeout, "timeout", 90*time.Second, "Maximum wait duration for --wait")
	serverRunCmd.Flags().DurationVar(&serverRunPollInterval, "poll-interval", 1*time.Second, "Polling interval for --wait state checks")
	serverRunCmd.Flags().BoolVar(&serverRunJSON, "json", false, "Output instance info as JSON")
	serverCmd.AddCommand(serverRunCmd)
}
