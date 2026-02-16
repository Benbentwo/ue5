package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Benbentwo/ue5/pkg/server"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

var (
	logsLines    int
	logsFollow   bool
	logsLevel    string
	logsCategory string
	logsPattern  string
	logsSince    string
	logsJSON     bool
)

var serverLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Query or stream editor logs",
	Long: `View captured logs from a managed editor instance. Supports filtering
by log level, UE log category, regex pattern, and time range.

Use --follow to stream new log lines in real-time.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Ensure daemon is running
		if err := server.EnsureDaemon(); err != nil {
			log.Fatal("Failed to ensure daemon is running", "error", err)
		}

		// Resolve project
		UpdateProjectPath()

		client := server.NewClient()

		if logsFollow {
			// Streaming mode
			ch, cancel, err := client.Stream(server.Request{
				ID:   "stream-logs",
				Type: server.ReqStreamLogs,
				StreamLogs: &server.StreamLogsRequest{
					ProjectPath: ResolvedUProject,
					Level:       logsLevel,
					Category:    logsCategory,
					Pattern:     logsPattern,
				},
			})
			if err != nil {
				log.Fatal("Failed to start log stream", "error", err)
			}
			defer cancel()

			for resp := range ch {
				if !resp.Success {
					log.Error("Stream error", "error", resp.Error)
					return
				}
				if resp.LogLine != nil {
					if logsJSON {
						data, _ := json.Marshal(resp.LogLine)
						fmt.Println(string(data))
					} else {
						fmt.Printf("[%s] %s | %s\n",
							resp.LogLine.Timestamp.Format("15:04:05"),
							resp.LogLine.Stream,
							resp.LogLine.Raw,
						)
					}
				}
			}
			return
		}

		// Query mode
		resp, err := client.Send(server.Request{
			ID:   "get-logs",
			Type: server.ReqGetLogs,
			GetLogs: &server.GetLogsRequest{
				ProjectPath: ResolvedUProject,
				Lines:       logsLines,
				Level:       logsLevel,
				Category:    logsCategory,
				Pattern:     logsPattern,
				Since:       logsSince,
			},
		})
		if err != nil {
			log.Fatal("Failed to query logs", "error", err)
		}

		if !resp.Success {
			log.Error("Log query failed", "error", resp.Error)
			return
		}

		if resp.Logs == nil {
			log.Info("No logs returned")
			return
		}

		if logsJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(resp.Logs)
			return
		}

		// Human-readable output
		log.Info("Log query results",
			"total", resp.Logs.TotalLines,
			"filtered", resp.Logs.FilteredLines,
			"showing", len(resp.Logs.Lines),
		)

		for _, line := range resp.Logs.Lines {
			prefix := ""
			switch line.Level {
			case "error":
				prefix = "ERR"
			case "warning":
				prefix = "WRN"
			default:
				prefix = "INF"
			}
			fmt.Printf("[%s] %s %s\n", line.Timestamp.Format("15:04:05"), prefix, line.Raw)
		}
	},
}

func init() {
	serverLogsCmd.Flags().IntVarP(&logsLines, "lines", "n", 100, "Number of recent lines to show")
	serverLogsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Stream new log lines (like tail -f)")
	serverLogsCmd.Flags().StringVarP(&logsLevel, "level", "l", "", "Filter by level: error, warning, info")
	serverLogsCmd.Flags().StringVarP(&logsCategory, "category", "c", "", "Filter by UE log category (e.g., LogCompile)")
	serverLogsCmd.Flags().StringVar(&logsPattern, "pattern", "", "Filter by regex pattern")
	serverLogsCmd.Flags().StringVar(&logsSince, "since", "", "Only lines after this time (duration like '5m' or RFC3339)")
	serverLogsCmd.Flags().BoolVar(&logsJSON, "json", false, "Output as JSON")
	serverCmd.AddCommand(serverLogsCmd)
}
