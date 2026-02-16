package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Benbentwo/ue5/pkg/server"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

var agentsJSON bool

var serverAgentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "List registered AI agents",
	Run: func(cmd *cobra.Command, args []string) {
		client := server.NewClient()

		if !client.IsRunning() {
			if agentsJSON {
				fmt.Println(`{"agents":[]}`)
			} else {
				log.Info("Daemon is not running")
			}
			return
		}

		resp, err := client.Send(server.Request{
			ID:   "list-agents",
			Type: server.ReqListAgents,
		})
		if err != nil {
			log.Fatal("Failed to list agents", "error", err)
		}

		if agentsJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(map[string]interface{}{
				"agents": resp.Agents,
			}); err != nil {
				log.Error("Failed to encode agents", "error", err)
			}
			return
		}

		if len(resp.Agents) == 0 {
			log.Info("No registered agents")
			return
		}

		for _, agent := range resp.Agents {
			log.Info("Agent",
				"id", agent.ID,
				"name", agent.Name,
				"description", agent.Description,
				"registered", agent.RegisteredAt.Format("15:04:05"),
				"last_seen", agent.LastSeenAt.Format("15:04:05"),
			)
		}
	},
}

func init() {
	serverAgentsCmd.Flags().BoolVar(&agentsJSON, "json", false, "Output as JSON")
	serverCmd.AddCommand(serverAgentsCmd)
}
