package cmd

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"bichme"

	"github.com/spf13/cobra"
)

// shellCmd run a single command on multiple machines.
var shellCmd = &cobra.Command{
	Use:   "shell <servers> <command>",
	Short: "Run a single command on multiple machines",
	Args:  cobra.MinimumNArgs(2),
	PreRunE: func(_ *cobra.Command, _ []string) error {
		return errors.Join(
			minLen("user", user, 1),
			minInt("port", port, 1), maxInt("port", port, 65535),
			minInt("workers", workers, 1),
			minInt("retries", retries, 1),
		)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		hosts, err := readHosts(args[0])
		if err != nil {
			return fmt.Errorf("read servers: %w", err)
		}
		return bichme.Run(cmd.Context(), hosts, strings.Join(args[1:], " "), opts(bichme.ExecTask))
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
	shellCmd.Flags().BoolVarP(&insecure, "insecure", "i", false, "skip host key verification")
}
