package cmd

import (
	"errors"
	"fmt"
	"time"

	"vld.bg/bichme"

	"github.com/spf13/cobra"
)

// pingCmd tests SSH connectivity to multiple machines.
var pingCmd = &cobra.Command{
	Use:   "ping <servers>",
	Short: "Test SSH connectivity to multiple machines",
	Long: `Test SSH connectivity to multiple machines in parallel.

Attempts to establish an SSH connection to each host. Reports success
or failure for each host without executing any commands.

Examples:
  bichme ping servers.txt
  bichme ping servers.txt -w 50
  bichme ping servers.txt --conn-timeout 5s`,
	Args: cobra.ExactArgs(1),
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
		return bichme.Run(cmd.Context(), hosts, "", opts(bichme.PingTask))
	},
}

func init() {
	rootCmd.AddCommand(pingCmd)
	pingCmd.Flags().StringVarP(&user, "user", "u", defaultUser(), "user to login as")
	pingCmd.Flags().IntVarP(&port, "port", "p", 22, "SSH port to connect to")
	pingCmd.Flags().IntVarP(&retries, "retries", "r", 5, "how many retries to perform on failed connections")
	pingCmd.Flags().IntVarP(&workers, "workers", "w", 10, "how many workers to test connections in parallel")
	pingCmd.Flags().DurationVar(&connectTimeout, "conn-timeout", 30*time.Second, "connection timeout")
	pingCmd.Flags().BoolVarP(&insecure, "insecure", "i", false, "skip host key verification")
}
