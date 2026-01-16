package cmd

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	osUser "os/user"
	"slices"
	"strings"
	"time"

	"bichme"

	"github.com/spf13/cobra"
)

var (
	verbose     bool   // sets DEBUG as default log level when enabled
	historyPath string // defines where are executions logged.
	uploadPath  string // defines where are files uploaded.
	outputPath  string // defines where files are downloaded to.

	defaultHistoryPath = os.ExpandEnv("$HOME/.local/state/bichme/history/")
	defaultUploadPath  = ""
	defaultOutputPath  = "."
)

// Arguments that are used by both shell and exec
var (
	user     string
	port     int
	retries  int
	history  bool
	workers  int
	files    []string
	insecure bool
	cleanup  bool

	connectTimeout time.Duration
	executeTimeout time.Duration
)

// opts populates cli args into bichme.Opts. It takes Tasks as an argument as
// the command is supposed to know what tasks is to perform. On top of the
// given t it toggles history which is resolved by --history and cleanup which
// is resolved by --cleanup.
func opts(t bichme.Tasks) bichme.Opts {
	if history {
		t.Set(bichme.KeepHistoryTask)
	}
	if cleanup {
		t.Set(bichme.CleanupTask)
	}

	return bichme.Opts{
		User:         user,
		Port:         port,
		Retries:      retries,
		Workers:      workers,
		Files:        files,
		ConnTimeout:  connectTimeout,
		ExecTimeout:  executeTimeout,
		History:      history,
		HistoryPath:  historyPath,
		UploadPath:   uploadPath,
		Insecure:     insecure,
		DownloadPath: outputPath,
		Tasks:        t,
	}
}

// readHosts reads filename and returns all the hosts from inside, sorted with
// removed duplicates. It ignores empty lines and treats # as comments. For
// each host with a port suffix, the given (or default --port) value is used.
func readHosts(filename string) ([]string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.Split(scanner.Text(), "#")[0])
		if len(line) == 0 {
			continue
		}

		if !strings.Contains(line, ":") {
			line += fmt.Sprintf(":%d", port)
		}
		lines = append(lines, line)
	}

	slices.Sort(lines)
	return slices.Compact(lines), nil
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:     "bichme",
	Short:   "Utility for quick and dirty execution on multiple machines at once",
	Version: bichme.Version(),
	Long: `bichme - parallel SSH command execution across multiple servers.

Connect to multiple hosts via SSH, execute commands or upload scripts,
and aggregate output with per-host prefixes. A lightweight alternative
to Ansible for ad-hoc operations.

Features:
  - Parallel SSH connections with configurable worker pool
  - File download via SFTP
  - File upload via SFTP before execution
  - Automatic retry on failures
  - Execution history with logs

Authentication is handled via ssh-agent and all the unencrypted SSH keys in '~/.ssh'.`,
	PersistentPreRun: func(_ *cobra.Command, _ []string) {
		if verbose {
			slog.SetLogLoggerLevel(slog.LevelDebug)
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute(ctx context.Context) {
	err := rootCmd.ExecuteContext(ctx)
	if err != nil {
		os.Exit(1)
	}
}

func die(format string, v ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", v...)
	os.Exit(1)
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
	rootCmd.PersistentFlags().StringVar(&historyPath, "history-path", defaultHistoryPath, "where to store history")
	rootCmd.PersistentFlags().StringVar(&uploadPath, "upload-path", defaultUploadPath, "where to upload files on remote machines")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enables debug output")
}
