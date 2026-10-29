package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Benbentwo/ue5/pkg/server"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

var buildInfoJSON bool

var serverBuildInfoCmd = &cobra.Command{
	Use:   "build-info",
	Short: "Show current build metadata and feature history",
	Run: func(cmd *cobra.Command, args []string) {
		// Auto-start so build info survives daemon death: state.json is
		// persistent, and a freshly started daemon serves it faithfully.
		if err := server.EnsureDaemon(); err != nil {
			log.Warn("Could not auto-start daemon", "error", err)
		}

		client := server.NewClient()

		if !client.IsRunning() {
			if buildInfoJSON {
				fmt.Println(`{"current_build":null,"accumulated_features":[],"total_builds":0,"recent_builds":[]}`)
			} else {
				log.Info("Daemon is not running")
			}
			return
		}

		resp, err := client.Send(server.Request{
			ID:   "get-build-info",
			Type: server.ReqGetBuildInfo,
		})
		if err != nil {
			log.Fatal("Failed to get build info", "error", err)
		}

		if buildInfoJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(resp.BuildInfo); err != nil {
				log.Error("Failed to encode build info", "error", err)
			}
			return
		}

		info := resp.BuildInfo
		if info == nil {
			log.Info("No build information available")
			return
		}

		if info.CurrentBuild != nil {
			log.Info("Current build",
				"id", info.CurrentBuild.ID,
				"status", info.CurrentBuild.Status,
				"mode", info.CurrentBuild.Mode,
				"labels", info.CurrentBuild.Labels,
				"started", info.CurrentBuild.StartedAt.Format("15:04:05"),
			)
		} else {
			log.Info("No builds recorded yet")
		}

		log.Info("Build stats", "total", info.TotalBuilds, "features", len(info.AccumulatedFeatures))

		if len(info.AccumulatedFeatures) > 0 {
			for i, f := range info.AccumulatedFeatures {
				fmt.Printf("  %d. %s\n", i+1, f)
			}
		}
	},
}

func init() {
	serverBuildInfoCmd.Flags().BoolVar(&buildInfoJSON, "json", false, "Output as JSON")
	serverCmd.AddCommand(serverBuildInfoCmd)
}
