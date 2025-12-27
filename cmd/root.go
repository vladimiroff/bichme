package cmd

import (
	"log/slog"
	"os"
	osUser "os/user"
	"time"

	"github.com/spf13/cobra"
)

var (
	verbose     bool   // sets DEBUG as default log level when enabled
	historyPath string // defines where are executions logged.

	defaultPath = os.ExpandEnv("$HOME/.local/state/bichme/history/")
)

// Arguments that are used by both shell and exec
var (
	user    string
	port    int
	retries int
	history bool
	workers int

	connectTimeout time.Duration
	executeTimeout time.Duration
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "bichme",
	Short: "Utility for quick and dirty execution on multiple machines at once",
	// Long:  "", // TODO
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

// defaultUser to login as if -u|--user is not passed.
//
// TODO: should probably figure out a way to allow overriding that via
// ~/.ssh/config on a per-host basis.
func defaultUser() string {
	user, err := osUser.Current()
	if err != nil {
		slog.Error("Failed to get current user, using 'root' as default user", "error", err)
		return "root"
	}
	return user.Username
}

func init() {
	rootCmd.PersistentFlags().StringVar(&historyPath, "history-path", defaultPath, "where to store history")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enables debug output")
}
