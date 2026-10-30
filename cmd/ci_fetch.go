package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Benbentwo/ue5/pkg/ci"
	"github.com/spf13/cobra"
)

var (
	ciFetchFrom      string
	ciFetchTokenFile string
	ciFetchGame      string
	ciFetchSHA       string
	ciFetchKind      string
	ciFetchPlatform  string
	ciFetchDest      string
	ciFetchTimeout   time.Duration
)

var ciFetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Download another box's build artifacts for one commit",
	Long: `Finds the newest succeeded job on another box's build service for a game,
commit, kind and platform, and extracts its artifacts into --dest. A recipe on one
box uses it to ship what another box built, e.g. a store upload that includes a
package built on a different OS.

Artifact downloads need the SERVING box's dispatch token, so pass a copy of it
with --from-token-file.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		tok, err := os.ReadFile(ciFetchTokenFile)
		if err != nil {
			return fmt.Errorf("read the remote box's token: %w", err)
		}
		if ciFetchDest == "" {
			return fmt.Errorf("--dest is required")
		}
		if err := os.MkdirAll(ciFetchDest, 0o755); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), ciFetchTimeout)
		defer cancel()
		j, files, bytes, err := ci.Fetch{
			URL: ciFetchFrom, Token: strings.TrimSpace(string(tok)),
			Game: ciFetchGame, SHA: ciFetchSHA, Kind: ci.Kind(ciFetchKind), Platform: ciFetchPlatform,
		}.Run(ctx, ciFetchDest)
		if err != nil {
			return err
		}
		fmt.Printf("fetched %s from %s: %d file(s), %.1f GB into %s\n", j.ID, ciFetchFrom, files, float64(bytes)/(1<<30), ciFetchDest)
		return nil
	},
}

func init() {
	f := ciFetchCmd.Flags()
	f.StringVar(&ciFetchFrom, "from", "", "the other box's build service, e.g. http://buildbox.local:9516")
	f.StringVar(&ciFetchTokenFile, "from-token-file", "", "a file holding the other box's dispatch token")
	f.StringVar(&ciFetchGame, "game", "", "game name")
	f.StringVar(&ciFetchSHA, "sha", "", "full commit SHA")
	f.StringVar(&ciFetchKind, "kind", string(ci.KindPackage), "job kind")
	f.StringVar(&ciFetchPlatform, "platform", "", "platform, e.g. Mac")
	f.StringVar(&ciFetchDest, "dest", "", "directory to extract into")
	f.DurationVar(&ciFetchTimeout, "timeout", 2*time.Hour, "give up after this long")
	for _, name := range []string{"from", "from-token-file", "game", "sha", "platform", "dest"} {
		_ = ciFetchCmd.MarkFlagRequired(name)
	}
	ciCmd.AddCommand(ciFetchCmd)
}
