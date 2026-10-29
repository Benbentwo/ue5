package cmd

import (
	"github.com/Benbentwo/ue5/pkg/server"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

var (
	rebuildMode  string
	rebuildLabel string
	rebuildAgent string
)

var serverRebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "Rebuild the project via the server daemon",
	Long: `Triggers a rebuild of the UE5 project through the daemon.

Modes:
  full       - Stop editor, run UBT build, restart editor
  hot_reload - Build in background while editor stays running

Each rebuild carries a label describing what changed. When multiple agents
request rebuilds concurrently, requests are coalesced into a single build.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := server.EnsureDaemon(); err != nil {
			log.Fatal("Failed to ensure daemon is running", "error", err)
		}

		UpdateProjectPath()
		UpdateEnginePath()

		client := server.NewClient()
		resp, err := client.Send(server.Request{
			ID:   "rebuild",
			Type: server.ReqRebuild,
			Rebuild: &server.RebuildRequest{
				ProjectPath:   ResolvedUProject,
				EnginePath:    EnginePath,
				Mode:          server.BuildMode(rebuildMode),
				Label:         rebuildLabel,
				AgentID:       rebuildAgent,
				Target:        Target,
				Configuration: State,
			},
		})
		if err != nil {
			log.Fatal("Failed to send rebuild request", "error", err)
		}

		if !resp.Success {
			log.Error("Rebuild failed", "error", resp.Error)
			return
		}

		if resp.Build != nil {
			log.Info("Rebuild initiated",
				"build_id", resp.Build.ID,
				"mode", resp.Build.Mode,
				"status", resp.Build.Status,
				"labels", resp.Build.Labels,
			)
		}
	},
}

func init() {
	serverRebuildCmd.Flags().StringVar(&rebuildMode, "mode", "full", "Build mode: full or hot_reload")
	serverRebuildCmd.Flags().StringVar(&rebuildLabel, "label", "", "Description of what changed (required)")
	serverRebuildCmd.Flags().StringVar(&rebuildAgent, "agent", "", "Agent ID requesting the rebuild")
	_ = serverRebuildCmd.MarkFlagRequired("label")
	serverCmd.AddCommand(serverRebuildCmd)
}
