package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// shellCmd run a single command on multiple machines.
var shellCmd = &cobra.Command{
	Use:   "shell <servers> <command>",
	Short: "Run a single command on multiple machines",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("shell %q %q\n", args[0], args[1])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(shellCmd)
	shellCmd.Flags().StringVarP(&user, "user", "u", defaultUser(), "user to login as")
	shellCmd.Flags().IntVarP(&port, "port", "p", 22, "SSH port to connect to")
	shellCmd.Flags().IntVar(&retries, "retries", 5, "how many retries to perform on failed executions")
	shellCmd.Flags().IntVarP(&workers, "workers", "w", 10, "how many workers to execute commands in parallel")
	shellCmd.Flags().DurationVar(&connectTimeout, "conn-timeout", 30*time.Second, "connection timeout")
	shellCmd.Flags().DurationVarP(&executeTimeout, "exec-timeout", "t", 1*time.Hour, "execution timeout")
	shellCmd.Flags().BoolVar(&history, "history", true, "write execution into history")
}
