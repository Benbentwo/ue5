package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Benbentwo/ue5/pkg/server"
	"github.com/spf13/cobra"
)

var (
	quitEditorPID  int
	quitEditorPort int
	quitEditorBoot time.Duration
)

var serverQuitEditorCmd = &cobra.Command{
	Use:   "quit-editor",
	Short: "Ask one editor process to quit by itself (no signal, no daemon)",
	Long: `Asks the editor with the given pid to quit by itself over its SadTire MCP
bridge: waits (bounded) for the bridge to be boot-ready, ends Play-In-Editor
if it is running, closes all asset editors, sends QUIT_EDITOR, and waits for
the process to exit.

This is the sequence 'ue5 server kill' runs inside the daemon, exposed for
tooling that must stop an editor the daemon does not track. It never sends a
signal: when it exits non-zero the editor is still running and the caller
owns the SIGTERM/SIGKILL escalation.

The bridge port is read from -SadTireMCPPort on the process's command line
unless --port is given. An editor with no port is never probed on a guess.

Prints one JSON object. Exit 0 = the process has exited; 1 = it has not.`,
	Run: func(cmd *cobra.Command, args []string) {
		exited, err := server.QuitEditorGracefully(quitEditorPID, quitEditorPort, quitEditorBoot)
		out := map[string]interface{}{"pid": quitEditorPID, "exited": exited}
		if err != nil {
			out["error"] = err.Error()
		}
		data, _ := json.Marshal(out)
		fmt.Println(string(data))
		if !exited {
			os.Exit(1)
		}
	},
}

func init() {
	serverQuitEditorCmd.Flags().IntVar(&quitEditorPID, "pid", 0, "PID of the editor process (required)")
	serverQuitEditorCmd.Flags().IntVar(&quitEditorPort, "port", 0, "SadTire MCP bridge port (default: read from the process command line)")
	serverQuitEditorCmd.Flags().DurationVar(&quitEditorBoot, "boot-wait", 0, "Bound on waiting for the bridge to answer (default: 3m for a young editor, 15s for one older than 10m)")
	_ = serverQuitEditorCmd.MarkFlagRequired("pid")
	serverCmd.AddCommand(serverQuitEditorCmd)
}
