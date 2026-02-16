package cmd

import (
	"github.com/Benbentwo/ue5/pkg/server"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
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

		if !resp.Success {
			log.Error("Failed to start editor", "error", resp.Error)
			return
		}

		if resp.Instance != nil {
			log.Info("Editor started",
				"project", resp.Instance.ProjectName,
				"pid", resp.Instance.PID,
				"log", resp.Instance.LogFile,
			)
		}
	},
}

func init() {
	serverCmd.AddCommand(serverRunCmd)
}
