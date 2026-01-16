package bichme

import (
	"context"
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

	if opts.History {
		path := filepath.Join(opts.HistoryPath, id)
		if err := os.MkdirAll(path, 0700); err != nil {
			slog.Error("failed to initialize history", "error", err)
			opts.History = false
		}
		if err := writeMetaFile(path, "command", cmd); err != nil {
			slog.Error("failed to write command", "error", err)
		}
		if err := writeMetaFile(path, "hosts", strings.Join(servers, "\n")); err != nil {
			slog.Error("failed to write hosts", "error", err)
		}
		if err := writeMetaFile(path, "files", strings.Join(opts.Files, "\n")); err != nil {
			slog.Error("failed to write files", "error", err)
		}
		if err := writeMetaFile(path, "start", start.Format(time.RFC3339)); err != nil {
			slog.Error("failed to write files", "error", err)
		}
		defer func(start time.Time) {
			if err := writeMetaFile(path, "duration", time.Since(start).String()); err != nil {
				slog.Error("failed to write files", "error", err)
			}
		}(start)
		opts.HistoryPath = path
	}

	jobs := make(map[string]*Job, len(servers))
	archive := make(map[*Job]error, len(servers))
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
			WriteStats(os.Stdout, archive)
		case res, ok := <-resCh:
			if !ok {
				WriteStats(os.Stdout, archive)
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
