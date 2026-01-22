package bichme

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
)

// HostResult captures the final execution state for a single host.
type HostResult struct {
	Error    string        `json:"error,omitempty"`
	Tries    int           `json:"tries"`
	Duration time.Duration `json:"duration"`
}

var id = runID()

// Opts carries CLI arguments from ./cmd into Run(). Values are copied into
// each Job at creation time - jobs don't share this struct.
type Opts struct {
	User         string
	Port         int
	Retries      int
	Workers      int
	Files        []string
	ConnTimeout  time.Duration
	ExecTimeout  time.Duration
	History      bool
	HistoryPath  string
	UploadPath   string
	Insecure     bool
	DownloadPath string
	Tasks        Tasks
}

type jobResult struct {
	host string
	err  error
}

func writeMetaFile(path, name, content string) error {
	return os.WriteFile(filepath.Join(path, name), []byte(content), 0644)
}

func writeHostsJSON(path string, archive map[*Job]error) error {
	results := make(map[string]HostResult, len(archive))
	for job, err := range archive {
		result := HostResult{
			Tries:    job.tries,
			Duration: job.duration,
		}
		if err != nil {
			switch {
			case errors.Is(err, ErrConnection):
				result.Error = "connection"
			case errors.Is(err, ErrFileTransfer):
				result.Error = "transfer"
			case errors.Is(err, ErrExecution):
				result.Error = "execution"
			default:
				result.Error = "unknown"
			}
		}
		results[job.hostname()] = result
	}

	data, _ := json.MarshalIndent(results, "", "  ")
	return writeMetaFile(path, "hosts.json", string(data))
}

func Run(ctx context.Context, servers []string, cmd string, opts Opts) error {
	start := time.Now()
	auths := loadSSHAuth()
	hostKeyVerifier, err := loadHostKeyVerifier(opts.Insecure)
	if err != nil {
		return fmt.Errorf("load host key verification: %w", err)
	}

	jobCh := make(chan *Job)
	resCh := make(chan jobResult)
	var wg sync.WaitGroup
	wg.Add(opts.Workers)
	for range opts.Workers {
		go func() {
			defer wg.Done()

			for job := range jobCh {
				resCh <- jobResult{host: job.host, err: job.Start(ctx)}
			}
		}()
	}

	jobs := make(map[string]*Job, len(servers))
	archive := make(map[*Job]error, len(servers))

	if opts.History {
		path := filepath.Join(opts.HistoryPath, id)
		if err := os.MkdirAll(path, 0700); err != nil {
			slog.Error("failed to initialize history", "error", err)
			opts.History = false
		}
		if err := writeMetaFile(path, "command", cmd); err != nil {
			slog.Error("failed to write command", "error", err)
		}
		if err := writeMetaFile(path, "files", strings.Join(opts.Files, "\n")); err != nil {
			slog.Error("failed to write files", "error", err)
		}
		if err := writeMetaFile(path, "start", start.Format(time.RFC3339)); err != nil {
			slog.Error("failed to write files", "error", err)
		}
		defer func(start time.Time) {
			if err := writeMetaFile(path, "duration", time.Since(start).String()); err != nil {
				slog.Error("failed to write duration", "error", err)
			}
			if err := writeHostsJSON(path, archive); err != nil {
				slog.Error("failed to write hosts.json", "error", err)
			}
		}(start)
		opts.HistoryPath = path
	}
	for _, server := range servers {
		user := opts.User
		if strings.Contains(server, "@") {
			parts := strings.Split(server, "@")
			user = parts[0]
			server = parts[1]
		}

		hostKey := hostKeyVerifier(server)
		cfg := &ssh.ClientConfig{
			User:              user,
			Auth:              auths,
			HostKeyCallback:   hostKey.Callback,
			HostKeyAlgorithms: hostKey.Algorithms,
			Timeout:           opts.ConnTimeout,
			ClientVersion:     "SSH-2.0-bichme-" + Version(),
		}

		var path string
		if opts.Tasks.Has(UploadTask) {
			path = opts.UploadPath
		} else if opts.Tasks.Has(DownloadTask) {
			path = opts.DownloadPath
		}

		j := &Job{
			host:        server,
			cmd:         cmd,
			sshConfig:   cfg,
			tasks:       opts.Tasks,
			port:        opts.Port,
			execTimeout: opts.ExecTimeout,
			maxRetries:  opts.Retries,
			files:       opts.Files,
			path:        path,
			historyPath: opts.HistoryPath,
		}

		jobs[server] = j
		archive[j] = nil
		jobCh <- j
	}

	var once sync.Once
	finish := func() {
		close(jobCh)
		wg.Wait()
		close(resCh)
	}

	SIGUSR1 := make(chan os.Signal, 1)
	signal.Notify(SIGUSR1, syscall.SIGUSR1)

	for {
		select {
		case <-ctx.Done():
			go once.Do(finish)
		case <-SIGUSR1:
			WriteStats(os.Stderr, archive)
		case res, ok := <-resCh:
			if !ok {
				WriteStats(os.Stderr, archive)
				return nil
			}

			closing := ctx.Err() != nil
			job := jobs[res.host]

			slog.Debug("Job done", "host", res.host, "try", job.tries, "error", res.err)
			archive[job] = res.err
			if job.tasks.Done() {
				delete(jobs, res.host)
			} else if !closing {
				archive[job] = nil
				jobCh <- job
			}

			if len(jobs) == 0 {
				go once.Do(finish)
			}

		}
	}
}
