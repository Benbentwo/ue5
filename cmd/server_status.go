package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Benbentwo/ue5/pkg/server"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

var statusJSON bool

var serverStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of managed editor instances",
	Run: func(cmd *cobra.Command, args []string) {
		client := server.NewClient()

		if !client.IsRunning() {
			if statusJSON {
				fmt.Println(`{"daemon_running":false,"instances":[]}`)
			} else {
				log.Info("Daemon is not running")
			}
			return
		}

		// Get daemon info
		pong, _ := client.Ping()

		// Get instances
		resp, err := client.Send(server.Request{
			ID:   "list-instances",
			Type: server.ReqListInstances,
		})
		if err != nil {
			log.Fatal("Failed to get status", "error", err)
		}

		if statusJSON {
			output := map[string]interface{}{
				"daemon_running": true,
				"instances":      resp.Instances,
			}
			if pong != nil {
				output["version"] = pong.Version
				output["uptime"] = pong.Uptime
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(output)
			return
		}

		// Human-readable output
		if pong != nil {
			log.Info("Daemon running", "version", pong.Version, "uptime", pong.Uptime)
		}

		if len(resp.Instances) == 0 {
			log.Info("No managed editor instances")
			return
		}

		for _, inst := range resp.Instances {
			log.Info("Instance",
				"project", inst.ProjectName,
				"state", inst.State,
				"pid", inst.PID,
				"engine", inst.EngineVersion,
				"started", inst.StartedAt.Format("15:04:05"),
			)
		}
	},
}

func init() {
	serverStatusCmd.Flags().BoolVar(&statusJSON, "json", false, "Output as JSON")
	serverCmd.AddCommand(serverStatusCmd)
}
