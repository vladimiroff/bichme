package bichme

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

var (
	ErrConnection   = errors.New("connection failed")
	ErrFileTransfer = errors.New("file transfer failed")
	ErrExection     = errors.New("exection failed")
)

// Job represents a single task to be executed on a single host. A job holds
// its information while going through retries until completion or exhaustion.
type Job struct {
	host        string
	port        int
	cmd         string
	tries       int
	sshConfig   *ssh.ClientConfig
	execTimeout time.Duration
	maxRetries  int
	files       []string
	uploadPath  string
	historyPath string

	// handles
	ssh  *ssh.Client
	sftp *sftp.Client
	out  *Output

	// what the job should do
	tasks Tasks
}

func (j Job) hostname() string { return strings.SplitN(j.host, ":", 2)[0] }

// Close implements io.Closer.
func (j *Job) Close() error {
	var err error
	if j.sftp != nil {
		errors.Join(err, j.sftp.Close())
	}
	if j.ssh != nil {
		errors.Join(err, j.ssh.Close())
	}
	if j.ssh != nil {
		errors.Join(err, j.out.Close())
	}
	return err
}

// Start a job to do its remaining tasks.
func (j *Job) Start(ctx context.Context) error {
	if j.tasks.Done() {
		return nil
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	j.tries++
	j.out = NewOutput(j.hostname())

	var err error
	defer func() {
		if err == nil || j.tries > j.maxRetries {
			j.tasks = 0
		}
		// TODO: recognize err type and fill j.(conn|file|exec|)Err
		if err != nil {
			fmt.Fprintf(j.out, "\nERROR: %v\n", err)
		}
	}()

	if j.tasks.Has(KeepHistoryTask) {
		filename := filepath.Join(j.historyPath, fmt.Sprintf("%s_%d.log", j.hostname(), j.tries))
		f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			slog.Error("Failed to open output file", "host", j.host, "error", err)
		} else {
			j.out.SetFile(f)
		}
	}

	if j.ssh == nil || !sshIsAlive(j.ssh) {
		if err = j.Dial(ctx); err != nil {
			return fmt.Errorf("%w: %w", ErrConnection, err)
		}
	}
	if j.tasks.Has(UploadTask) {
		if j.sftp == nil || !sftpIsAlive(j.sftp) {
			j.sftp, err = sftp.NewClient(j.ssh)
			if err != nil {
				return fmt.Errorf("%w: open sftp session: %w", ErrFileTransfer, err)
			}
		}
		if err := j.Upload(ctx); err != nil {
			return fmt.Errorf("%w: %w", ErrFileTransfer, err)
		}
		j.tasks.Unset(UploadTask)
	}
	if j.tasks.Has(ExecTask) {
		if err = j.Exec(ctx); err != nil {
			err = fmt.Errorf("%w: %w", ErrExection, err)
		}
	}

	return err
}

// Upload files and make sure the first one will be executable.
func (j *Job) Upload(ctx context.Context) error {
	if err := upload(ctx, j.sftp, j.uploadPath, j.files...); err != nil {
		return fmt.Errorf("upload: %w", err)
	}

	if len(j.files) > 0 {
		filename := filepath.Join(j.uploadPath, filepath.Base(j.files[0]))
		if err := makeExec(ctx, j.sftp, filename); err != nil {
			return fmt.Errorf("make exec: %w", err)
		}
	}

	return nil
}

// just to be able to override it in tests
var sshDial = ssh.Dial

// Dial connects to the remote host.
func (j *Job) Dial(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	addr := j.host
	if !strings.Contains(addr, ":") { // TODO: move this while parsing
		addr += fmt.Sprintf(":%d", j.port)
	}

	ch := make(chan error)
	go func() {
		client, err := sshDial("tcp", addr, j.sshConfig)
		j.ssh = client
		ch <- err
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-ch:
		return err
	}
}

// Exec executes the job's command, but teeing output to the history and stdout.
func (j *Job) Exec(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	session, err := j.ssh.NewSession()
	if err != nil {
		return fmt.Errorf("open session: %w", err)
	}
	defer session.Close()
	session.Stderr = j.out
	session.Stdout = j.out

	errCh := make(chan error)
	go func() { errCh <- session.Run(j.cmd + "\n") }()
	select {
	case <-time.After(j.execTimeout):
		return os.ErrDeadlineExceeded
	case err = <-errCh:
		return err
	}
}
