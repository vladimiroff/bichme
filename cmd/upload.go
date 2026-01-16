package cmd

import (
	"errors"
	"fmt"
	"time"

	"bichme"

	"github.com/spf13/cobra"
)

// uploadCmd uploads files to multiple machines
var uploadCmd = &cobra.Command{
	Use:   "upload <servers> <pattern>...",
	Short: "Upload files matching patterns to multiple machines",
	Long: `Upload files to multiple machines in parallel.

Patterns are glob expressions that are expanded on the local side.

Examples:
  bichme upload servers.txt migrations/*.sql
  bichme upload servers.txt a.out -o ~/scripts
  bichme upload servers.txt '/etc/nginx/*.conf' /etc/systemd/system/nginx.service`,
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
		files = args[1:] // local patterns to upload
		return bichme.Run(cmd.Context(), hosts, "", opts(bichme.UploadTask))
	},
}

func init() {
	rootCmd.AddCommand(uploadCmd)
	uploadCmd.Flags().StringVarP(&user, "user", "u", defaultUser(), "user to login as")
	uploadCmd.Flags().IntVarP(&port, "port", "p", 22, "SSH port to connect to")
	uploadCmd.Flags().IntVar(&retries, "retries", 5, "how many retries to perform on failed uploads")
	uploadCmd.Flags().IntVarP(&workers, "workers", "w", 10, "how many workers to upload in parallel")
	uploadCmd.Flags().DurationVar(&connectTimeout, "conn-timeout", 30*time.Second, "connection timeout")
	uploadCmd.Flags().BoolVar(&history, "history", false, "write execution into history")
	uploadCmd.Flags().BoolVarP(&insecure, "insecure", "i", false, "skip host key verification")
	uploadCmd.Flags().StringVarP(&uploadPath, "output", "o", defaultUploadPath, "remote directory to upload files to")
}
