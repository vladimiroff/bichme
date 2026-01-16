package cmd

import (
	"errors"
	"fmt"
	"time"

	"bichme"

	"github.com/spf13/cobra"
)

// downloadCmd downloads files from multiple machines
var downloadCmd = &cobra.Command{
	Use:   "download <servers> <pattern>...",
	Short: "Download files matching patterns from multiple machines",
	Long: `Download files from multiple machines in parallel.

Patterns are glob expressions that are expanded on the remote side.
Downloaded files are stored in per-host subdirectories under --output.

Examples:
  bichme download servers.txt /var/log/*.log
  bichme download servers.txt '*.txt' ~/config.json -o ~/downloads
  bichme download servers.txt '/etc/nginx/*.conf' '/var/log/nginx/*'`,
	Args: cobra.MinimumNArgs(2),
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
		files = args[1:] // remote patterns to download
		return bichme.Run(cmd.Context(), hosts, "", opts(bichme.DownloadTask))
	},
}

func init() {
	rootCmd.AddCommand(downloadCmd)
	downloadCmd.Flags().StringVarP(&user, "user", "u", defaultUser(), "user to login as")
	downloadCmd.Flags().IntVarP(&port, "port", "p", 22, "SSH port to connect to")
	downloadCmd.Flags().IntVar(&retries, "retries", 5, "how many retries to perform on failed downloads")
	downloadCmd.Flags().IntVarP(&workers, "workers", "w", 10, "how many workers to download in parallel")
	downloadCmd.Flags().DurationVar(&connectTimeout, "conn-timeout", 30*time.Second, "connection timeout")
	downloadCmd.Flags().BoolVar(&history, "history", false, "write execution into history")
	downloadCmd.Flags().BoolVarP(&insecure, "insecure", "i", false, "skip host key verification")
	downloadCmd.Flags().StringVarP(&outputPath, "output", "o", defaultOutputPath, "local directory to download files to")
}
