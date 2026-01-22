package cmd

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"vld.bg/bichme"

	"github.com/spf13/cobra"
)

// historyCmd lists previous executions
var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "List previous executions and their result",
	Long: `List previous executions and their results.

Displays a table of recorded executions with ID, start time, duration,
number of hosts, number of files, and the command that was run.

Use 'history show <id>' to view the full output of a specific execution.
Use 'history purge' to clean up old history entries.

The history location can be changed with --history-path.`,
	Run: func(cmd *cobra.Command, args []string) {
		items, err := bichme.ListHistory(historyPath)
		if err != nil {
			die("ERROR: %v\n", err)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 0, ' ', tabwriter.Debug|tabwriter.TabIndent)
		fmt.Fprintln(w, "ID\t Start Time \t Duration \t OK \t Fail \t Files \t Command ")
		fmt.Fprintln(w, "------\t---------------------\t----------\t----\t------\t-------\t--------------")
		for i, item := range items {
			succeeded, failed := item.Summary()
			fmt.Fprintf(w, " %d\t %s\t %s\t %d\t %d\t %d\t %s\n",
				i+1, item.Time.Format(time.DateTime), item.Duration,
				succeeded, failed, len(item.Files), item.Command)
		}
		w.Flush()
	},
}

// historyInspectCmd provides full data for given execution.
var historyInspectCmd = &cobra.Command{
	Use:   "show [id...]",
	Short: "Show all the details of specific execution",
	Long: `Show the full output and details of one or more previous executions.

The ID corresponds to the number shown in 'bichme history' output.
If no ID is provided, shows the most recent execution (ID 1).

Examples:
  bichme history show        # show most recent
  bichme history show 1      # show execution #1
  bichme history show 1 2 3  # show multiple executions`,
	PreRunE: func(_ *cobra.Command, args []string) error {
		for i, arg := range args {
			if _, err := strconv.Atoi(arg); err != nil {
				return fmt.Errorf("bad argument %d: %v", i, err)
			}
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		items, err := bichme.ListHistory(historyPath)
		if err != nil {
			die("ERROR: %v\n", err)
		}
		if len(args) == 0 {
			args = append(args, "1")
		}

		for _, arg := range args {
			n, _ := strconv.Atoi(arg)
			if n > len(items) {
				die("ERROR: failed to show execution %d out of %d", n, len(items))
			}
			fmt.Println("---------------------------------------------------")
			io.Copy(os.Stdout, items[n-1])
		}
	},
}

var (
	purgeOlderThan time.Duration
	purgeKeep      int
	purgeAll       bool

	errBadPurgeVars = errors.New("either --(keep|older-than) or --all should be passed")
)

// historyPurgeCmd purges previous executions.
var historyPurgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Purge previous executions",
	Long: `Delete old execution history entries.

Exactly one of --keep, --older-than, or --all must be specified.

Examples:
  bichme history purge --keep 10         # keep only the 10 most recent
  bichme history purge --older-than 24h  # delete entries older than 24 hours
  bichme history purge --older-than 7d   # delete entries older than 7 days
  bichme history purge --all             # delete all history`,
	PreRunE: func(_ *cobra.Command, args []string) error {
		if purgeOlderThan == 0 && purgeKeep == 0 && !purgeAll {
			return errBadPurgeVars
		}

		if purgeAll && (purgeOlderThan > 0 || purgeKeep > 0) {
			return errBadPurgeVars
		}
		return nil
	},
	Run: func(cmd *cobra.Command, _ []string) {
		items, err := bichme.ListHistory(historyPath)
		if err != nil {
			die("ERROR: %v\n", err)
		}

		now := time.Now().UTC()
		for i, item := range items {
			if purgeAll ||
				(purgeKeep > 0 && purgeKeep <= i) ||
				(purgeOlderThan > 0 && now.Sub(item.Time) > purgeOlderThan) {

				delErr := item.Delete()
				slog.Info("Deleted", "id", i+1, "from", item.Time, "error", delErr)
				err = errors.Join(err, delErr)
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(historyCmd)
	historyCmd.AddCommand(historyInspectCmd)
	historyCmd.AddCommand(historyPurgeCmd)

	historyPurgeCmd.Flags().IntVar(&purgeKeep, "keep", 0, "how many of the latest executions to keep")
	historyPurgeCmd.Flags().DurationVar(&purgeOlderThan, "older-than", 0, "older than how much time to purge")
	historyPurgeCmd.Flags().BoolVarP(&purgeAll, "all", "a", false, "whether to just delete all previous executions")
}
