package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// historyCmd lists previous executions
var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "List executions and their result",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("history called")
	},
}

// historyInspectCmd provides full data for given execution.
var historyInspectCmd = &cobra.Command{
	Use:   "history",
	Short: "List executions and their result",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("history called")
	},
}

func init() {
	rootCmd.AddCommand(historyCmd)
	historyCmd.AddCommand(historyInspectCmd)
}
