package bichme

import (
	"context"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
		opts.HistoryPath = filepath.Join(opts.HistoryPath, id)
		if err := os.MkdirAll(opts.HistoryPath, 0700); err != nil {
			slog.Error("failed to initialize history", "error", err)
			opts.History = false
		}
	}

	jobs := make(map[string]*Job, len(servers))
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

		jobs[server] = &Job{
			host:   server,
			cmd:    cmd,
			opts:   &opts,
			config: cfg,
			tasks:  ExecTask,
			// cleanup: true,
		}
		if len(opts.Files) > 0 {
			jobs[server].tasks.Set(UploadTask)
		}
		jobCh <- jobs[server]
	}

	var once sync.Once
	for res := range resCh {
		ctxIsDone := ctx.Err() != nil

		slog.Debug("Job done", "host", res.host, "try", jobs[res.host].tries, "error", res.err)
		if jobs[res.host].tasks.Done() {
			delete(jobs, res.host)
		} else if !ctxIsDone {
			jobCh <- jobs[res.host]
		}

		if len(jobs) == 0 || ctxIsDone {
			go once.Do(func() {
				close(jobCh)
				wg.Wait()
				close(resCh)
			})
		}
	}
	return nil
}
