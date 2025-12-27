package bichme

import (
	"context"
	"fmt"
	"io"
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

type conn struct {
	host    string
	tries   int
	opts    *Opts
	config  *ssh.ClientConfig
	client  *ssh.Client
	connErr error
	execErr error
	output  io.ReadWriteCloser
}

type Opts struct {
	User        string
	Port        int
	Retries     int
	Workers     int
	ConnTimeout time.Duration
	ExecTimeout time.Duration
	History     bool
	HistoryPath string
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

	ch := make(chan *conn)
	var wg sync.WaitGroup
	wg.Add(opts.Workers)
	for range opts.Workers {
		go func(ch <-chan *conn) {
			defer wg.Done()

			for conn := range ch {
				// TODO: retries
				slog.Debug("Running", "host", conn.host)
				err := run(ctx, conn, cmd)
				slog.Debug("Done", "host", conn.host, "error", err)
			}
		}(ch)
	}

	if opts.History {
		t := time.Now()
		date := t.Format(time.DateOnly)
		time := t.Format(time.TimeOnly)
		opts.HistoryPath = filepath.Join(opts.HistoryPath, date, fmt.Sprintf("%s.%d", time, os.Getpid()))
		if err := os.MkdirAll(opts.HistoryPath, 0700); err != nil {
			slog.Error("failed to initialize history", "error", err)
			opts.History = false
		}
	}

	conns := make([]*conn, len(servers))
	for i, server := range servers {
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

		conns[i] = &conn{host: server, config: cfg, opts: &opts}
		ch <- conns[i]
	}

	close(ch)
	wg.Wait()
	return nil
}

func run(ctx context.Context, c *conn, cmd string) error {
	out := NewOutput(c.host)
	if c.opts.History {
		filename := filepath.Join(c.opts.HistoryPath, fmt.Sprintf("%s.%d.log", c.host, c.tries))
		f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			slog.Error("Failed to open output file", "host", c.host, "error", err)
		} else {
			out.SetFile(f)
		}
	}
	defer out.Close()

	c.tries++
	host := c.host
	if !strings.Contains(host, ":") {
		host += fmt.Sprintf(":%d", c.opts.Port)
	}
	client, err := dialContext(ctx, "tcp", host, c.config)
	if err != nil {
		c.connErr = fmt.Errorf("dial: %w", err)
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		c.connErr = fmt.Errorf("session: %w", err)
		return err
	}
	defer session.Close()

	session.Stderr = out
	session.Stdout = out
	errCh := make(chan error)
	go func() { errCh <- session.Run(cmd) }()
	select {
	case err := <-errCh:
		return err
	case <-time.After(c.opts.ExecTimeout):
		return os.ErrDeadlineExceeded
	case <-ctx.Done():
		return ctx.Err()
	}
}

type clientErr struct {
	client *ssh.Client
	err    error
}

func dialContext(ctx context.Context, n, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ch := make(chan clientErr)
	go func() {
		client, err := ssh.Dial(n, addr, cfg)
		ch <- clientErr{client, err}
	}()
	select {
	case res := <-ch:
		return res.client, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
