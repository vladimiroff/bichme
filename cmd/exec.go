package cmd

import (
	"errors"
	"fmt"
	"os"
	"path"
	"time"

	"vld.bg/bichme"

	"github.com/spf13/cobra"
)

// execCmd runs a given executable on multiple machines
var execCmd = &cobra.Command{
	Use:   "exec <servers> <file>",
	Short: "Execute given executable on multiple machines",
	Long: `Upload and execute a file on multiple machines in parallel.

The file is transferred via SFTP to each host and then executed. Additional
files can be uploaded alongside the main executable using the -f flag.

By default, uploaded files remain on the remote hosts after execution.
Use --cleanup to delete them after successful execution.

Examples:
  bichme exec servers.txt ./deploy.sh
  bichme exec servers.txt ./install.sh -f config.yaml -f data.json
  bichme exec servers.txt ./script.sh --cleanup
  bichme exec servers.txt ./backup.sh -t 4h -w 5`,
	Args: cobra.ExactArgs(2),
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

		info, err := os.Stat(args[1])
		if err != nil {
			return fmt.Errorf("read executable: %w", err)
		}
		if info.IsDir() {
			return fmt.Errorf("can not execute directory")
		}
		files = append([]string{args[1]}, files...)

		command := "./" + info.Name()
		if uploadPath != "" {
			command = path.Join(uploadPath, info.Name())
		}
		return bichme.Run(cmd.Context(), hosts, command, opts(bichme.ExecTask|bichme.UploadTask))
	},
}

func init() {
	rootCmd.AddCommand(execCmd)
	execCmd.Flags().StringVarP(&user, "user", "u", defaultUser(), "user to login as")
	execCmd.Flags().IntVarP(&port, "port", "p", 22, "SSH port to connect to")
	execCmd.Flags().IntVarP(&retries, "retries", "r", 5, "how many retries to perform on failed executions")
	execCmd.Flags().IntVarP(&workers, "workers", "w", 10, "how many workers to execute commands in parallel")
	execCmd.Flags().StringArrayVarP(&files, "files", "f", nil, "additional files to be uploaded before execution")
	execCmd.Flags().DurationVar(&connectTimeout, "conn-timeout", 30*time.Second, "connection timeout")
	execCmd.Flags().DurationVarP(&executeTimeout, "exec-timeout", "t", 1*time.Hour, "execution timeout")
	execCmd.Flags().BoolVar(&history, "history", true, "write execution into history")
	execCmd.Flags().BoolVarP(&insecure, "insecure", "i", false, "skip host key verification")
	execCmd.Flags().BoolVarP(&cleanup, "cleanup", "c", false, "delete uploaded files after successful execution")
}
