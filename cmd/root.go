package cmd

import (
	"bufio"
	"context"
	"log/slog"
	"os"
	osUser "os/user"
	"strings"
	"time"

	"bichme"

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
	files   []string

	connectTimeout time.Duration
	executeTimeout time.Duration
)

// opts populates cli args into bichme.Opts.
func opts() bichme.Opts {
	return bichme.Opts{
		User:        user,
		Port:        port,
		Retries:     retries,
		Workers:     workers,
		Files:       files,
		ConnTimeout: connectTimeout,
		ExecTimeout: executeTimeout,
		History:     history,
		HistoryPath: historyPath,
	}
}

// readLines reads filename and returns non-empty lines.
func readLines(filename string) ([]string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:     "bichme",
	Short:   "Utility for quick and dirty execution on multiple machines at once",
	Version: bichme.Version(),
	PersistentPreRun: func(_ *cobra.Command, _ []string) {
		if verbose {
			slog.SetLogLoggerLevel(slog.LevelDebug)
		}
	},
	// Long:  "", // TODO
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute(ctx context.Context) {
	err := rootCmd.ExecuteContext(ctx)
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
