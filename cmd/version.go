package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	Run: func(cmd *cobra.Command, args []string) {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), cmd.Root().Version); err != nil {
			return
		}
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
