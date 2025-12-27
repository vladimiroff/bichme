package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// execCmd runs a given executable on multiple machines
var execCmd = &cobra.Command{
	Use:   "exec <servers> <file>",
	Short: "Execute given executable on multiple machines",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("exec %q %q\n", args[0], args[1])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(execCmd)
	execCmd.Flags().StringVarP(&user, "user", "u", defaultUser(), "user to login as")
	execCmd.Flags().IntVarP(&port, "port", "p", 22, "SSH port to connect to")
	execCmd.Flags().IntVar(&retries, "retries", 5, "how many retries to perform on failed executions")
	execCmd.Flags().IntVarP(&workers, "workers", "w", 10, "how many workers to execute commands in parallel")
	execCmd.Flags().DurationVar(&connectTimeout, "conn-timeout", 30*time.Second, "connection timeout")
	execCmd.Flags().DurationVarP(&executeTimeout, "exec-timeout", "t", 1*time.Hour, "execution timeout")
	execCmd.Flags().BoolVar(&history, "history", true, "write execution into history")
}
