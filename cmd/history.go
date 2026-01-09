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

	"bichme"

	"github.com/spf13/cobra"
)

// historyCmd lists previous executions
var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "List previous executions and their result",
	Run: func(cmd *cobra.Command, args []string) {
		items, err := bichme.ListHistory(historyPath)
		if err != nil {
			die("ERROR: %v\n", err)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 0, ' ', tabwriter.Debug|tabwriter.TabIndent)
		fmt.Fprintln(w, "ID\t Start Time \t Duration \t Hosts \t Files \t Command ")
		fmt.Fprintln(w, "------\t---------------------\t----------\t-------\t-------\t--------------")
		for i, item := range items {
			fmt.Fprintf(w, " %d\t %s\t %s\t %d\t %d\t %s\n",
				i+1, item.Time.Format(time.DateTime), item.Duration,
				len(item.Hosts), len(item.Files), item.Command)
		}
		w.Flush()
	},
}

// historyInspectCmd provides full data for given execution.
var historyInspectCmd = &cobra.Command{
	Use:   "show",
	Short: "Show all the details of specific execution",
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

				slog.Info("Deleting", "id", i+1, "from", item.Time, "error", err)
				err = errors.Join(err, item.Delete())
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
