package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"bichme"

	"github.com/spf13/cobra"
)

// historyCmd lists previous executions
var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "List executions and their result",
	RunE: func(cmd *cobra.Command, args []string) error {
		items, err := bichme.ListHistory(os.DirFS(historyPath))
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 0, ' ', tabwriter.Debug|tabwriter.TabIndent)
		fmt.Fprintln(w, "ID\t Time \t Servers \t Command ")
		fmt.Fprintln(w, "------\t---------------------\t---------\t--------------")
		for i, item := range items {
			fmt.Fprintf(w, " %d\t %s \t %d\t %s\n",
				i, item.Time.Format(time.DateTime), len(item.Servers), item.Command)
		}
		return w.Flush()
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
