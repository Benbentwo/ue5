package cmd

import (
	"github.com/Benbentwo/ue5/pkg/server"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

var (
	killForce bool
	killAll   bool
)

var serverKillCmd = &cobra.Command{
	Use:   "kill",
	Short: "Stop a managed editor instance",
	Long: `Stops one or all managed editor instances. By default, sends SIGTERM
for a graceful shutdown. Use --force to send SIGKILL immediately.`,
	Run: func(cmd *cobra.Command, args []string) {
		client := server.NewClient()

		if !client.IsRunning() {
			log.Info("Daemon is not running")
			return
		}

		if killAll {
			// Get all instances and stop each one
			resp, err := client.Send(server.Request{
				ID:   "list-instances",
				Type: server.ReqListInstances,
			})
			if err != nil {
				log.Fatal("Failed to list instances", "error", err)
			}

			if len(resp.Instances) == 0 {
				log.Info("No running instances to stop")
				return
			}

			for _, inst := range resp.Instances {
				if inst.State == server.StateRunning || inst.State == server.StateStarting {
					stopResp, err := client.Send(server.Request{
						ID:   "stop-editor",
						Type: server.ReqStopEditor,
						StopEditor: &server.StopEditorRequest{
							ProjectPath: inst.ProjectPath,
							Force:       killForce,
						},
					})
					if err != nil {
						log.Error("Failed to stop editor", "project", inst.ProjectName, "error", err)
						continue
					}
					if stopResp.Success {
						log.Info("Stopped editor", "project", inst.ProjectName)
					} else {
						log.Error("Failed to stop editor", "project", inst.ProjectName, "error", stopResp.Error)
					}
				}
			}
			return
		}

		// Stop specific project - resolve from current directory
		UpdateProjectPath()

		resp, err := client.Send(server.Request{
			ID:   "stop-editor",
			Type: server.ReqStopEditor,
			StopEditor: &server.StopEditorRequest{
				ProjectPath: ResolvedUProject,
				Force:       killForce,
			},
		})
		if err != nil {
			log.Fatal("Failed to stop editor", "error", err)
		}

		if resp.Success {
			if resp.Instance != nil {
				log.Info("Editor stopped", "project", resp.Instance.ProjectName, "state", resp.Instance.State)
			}
		} else {
			log.Error("Failed to stop editor", "error", resp.Error)
		}
	},
}

func init() {
	serverKillCmd.Flags().BoolVarP(&killForce, "force", "f", false, "Force kill (SIGKILL) instead of graceful shutdown")
	serverKillCmd.Flags().BoolVar(&killAll, "all", false, "Stop all managed instances")
	serverCmd.AddCommand(serverKillCmd)
}
