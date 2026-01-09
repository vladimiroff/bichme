package bichme

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

var id = runID()

// Opts is a quick and dirty way to pass CLI args from ./cmd, without having a
// special "Runner" type or tossing around things in the global namespace.
//
// TODO: figure out a saner approach.
type Opts struct {
	User        string
	Port        int
	Retries     int
	Workers     int
	Files       []string
	ConnTimeout time.Duration
	ExecTimeout time.Duration
	History     bool
	HistoryPath string
	UploadPath  string
}

type jobResult struct {
	host string
	err  error
}

func writeMetaFile(path, name, content string) error {
	return os.WriteFile(filepath.Join(path, name), []byte(content), 0644)
}

func Run(ctx context.Context, servers []string, cmd string, opts Opts) error {
	var auths []ssh.AuthMethod
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		c, err := net.Dial("unix", sock)
		if err == nil {
			ag := agent.NewClient(c)
			auths = append(auths, ssh.PublicKeysCallback(ag.Signers))
		}
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
		defer func(start time.Time) {
			if err := writeMetaFile(path, "duration", time.Since(start).String()); err != nil {
				slog.Error("failed to write files", "error", err)
			}
		}(time.Now())
		opts.HistoryPath = path
	}

	jobs := make(map[string]*Job, len(servers))
	archive := make(map[*Job]error, len(servers))
	for _, server := range servers {
		cfg := &ssh.ClientConfig{
			User:            opts.User,
			Auth:            auths,
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         opts.ConnTimeout,
			ClientVersion:   "SSH-2.0-bichme" + Version(),
		}

		if strings.Contains(server, "@") {
			parts := strings.Split(server, "@")
			cfg.User = parts[0] // TODO: password inside?
			server = parts[1]
		}

		j := &Job{
			host:   server,
			cmd:    cmd,
			opts:   &opts,
			config: cfg,
			tasks:  ExecTask,
			// cleanup: true,
		}
		if len(opts.Files) > 0 {
			j.tasks.Set(UploadTask)
		}
		if opts.History {
			j.tasks.Set(KeepHistoryTask)
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
