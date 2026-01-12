package bichme

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

var ctx = context.Background()

var devNull *os.File

func init() {
	var err error
	devNull, err = os.Open(os.DevNull)
	if err != nil {
		panic(err)
	}
}

func discardStdout(t *testing.T) {
	init := os.Stdout
	t.Cleanup(func() { os.Stdout = init })
	os.Stdout = devNull
}

func TestJobClose(t *testing.T) {
	sshDialHandlerMock(t, hardcodedOutputHandler("", 0))

	tt := []struct {
		name string
		job  func() *Job
	}{
		{name: "zero", job: func() *Job { return &Job{} }},
		{name: "connected", job: func() *Job {
			j := &Job{host: "h", opts: &Opts{Port: 22}, out: NewOutput("h")}
			if err := j.Dial(ctx); err != nil {
				t.Fatal(err)
			}
			return j
		}},
		{name: "disconnected", job: func() *Job {
			j := &Job{host: "h", opts: &Opts{Port: 22}, out: NewOutput("h")}
			if err := j.Dial(ctx); err != nil {
				t.Fatal(err)
			}
			j.Close()
			return j
		}},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			job := tc.job()
			if err := job.Close(); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestJobStart(t *testing.T) {
	discardStdout(t)

	t.Run("done", func(t *testing.T) {
		j := &Job{tasks: 0}
		if err := j.Start(ctx); err != nil {
			t.Error(err)
		}
		if j.tries != 0 {
			t.Errorf("tries = %d, want 0", j.tries)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		j := &Job{tasks: ExecTask, opts: &Opts{Port: 22}}
		if err := j.Start(ctx); err == nil {
			t.Error("want error")
		}
	})

	t.Run("exec", func(t *testing.T) {
		sshDialHandlerMock(t, hardcodedOutputHandler("ok", 0))
		j := &Job{
			host:  "h",
			cmd:   "true",
			tasks: ExecTask,
			opts:  &Opts{Port: 22, ExecTimeout: time.Second},
		}
		defer j.Close()

		if err := j.Start(ctx); err != nil {
			t.Error(err)
		}
		if j.tries != 1 {
			t.Errorf("tries = %d, want 1", j.tries)
		}
		if !j.tasks.Done() {
			t.Error("tasks not done")
		}
	})

	t.Run("failure", func(t *testing.T) {
		sshDialHandlerMock(t, hardcodedOutputHandler("", 1))
		j := &Job{
			host:  "h",
			tasks: ExecTask,
			opts:  &Opts{Port: 22, ExecTimeout: time.Second, Retries: 1},
		}
		defer j.Close()

		err := j.Start(ctx)
		if !errors.Is(err, ErrExection) {
			t.Errorf("got %v, want ErrExection", err)
		}
		if j.tasks.Done() {
			t.Error("tasks should not be done with retries left")
		}
	})

	t.Run("exhausted", func(t *testing.T) {
		sshDialHandlerMock(t, hardcodedOutputHandler("", 1))
		j := &Job{
			host:  "h",
			tasks: ExecTask,
			tries: 1,
			opts:  &Opts{Port: 22, ExecTimeout: time.Second, Retries: 1},
		}
		defer j.Close()

		j.Start(ctx)
		if !j.tasks.Done() {
			t.Error("tasks should be done after retries exhausted")
		}
	})

	t.Run("history", func(t *testing.T) {
		dir := t.TempDir()
		sshDialHandlerMock(t, hardcodedOutputHandler("", 0))
		j := &Job{
			host:  "h",
			tasks: ExecTask | KeepHistoryTask,
			opts:  &Opts{Port: 22, ExecTimeout: time.Second, HistoryPath: dir},
		}
		defer j.Close()

		if err := j.Start(ctx); err != nil {
			t.Error(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "h_1.log")); err != nil {
			t.Errorf("history file: %v", err)
		}
	})

	t.Run("dialfail", func(t *testing.T) {
		sshDialMock(t, func(_, _ string, _ *ssh.ClientConfig) (*ssh.Client, error) {
			return nil, errors.New("refused")
		})
		j := &Job{host: "h", tasks: ExecTask, opts: &Opts{Port: 22}}

		err := j.Start(ctx)
		if !errors.Is(err, ErrConnection) {
			t.Errorf("got %v, want ErrConnection", err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		sshDialHandlerMock(t, sleepHardcodedOutputHandler(100*time.Millisecond, "", 0))
		j := &Job{host: "h", tasks: ExecTask, opts: &Opts{Port: 22, ExecTimeout: 50 * time.Millisecond}}

		defer j.Close()
		err := j.Start(ctx)
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Errorf("got %v, want DeadlineExceeded", err)
		}
	})
}

func TestJobDial(t *testing.T) {
	tt := []struct {
		name    string
		host    string
		wantErr bool
	}{
		{"ok", "h", false},
		{"port", "h:2222", false},
		{"error", "h", true},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			if tc.wantErr {
				sshDialMock(t, func(_, _ string, _ *ssh.ClientConfig) (*ssh.Client, error) {
					return nil, errors.New("oops")
				})
			} else {
				sshDialHandlerMock(t, hardcodedOutputHandler("", 0))
			}

			j := &Job{host: tc.host, opts: &Opts{Port: 22}, out: NewOutput("h")}
			defer j.Close()

			err := j.Dial(ctx)
			if tc.wantErr {
				if err == nil {
					t.Errorf("wanted error; got %v", err)
				}
				return
			}

			if err != nil {
				t.Error(err)
			}
			if j.ssh == nil {
				t.Error("ssh not set")
			}
		})
	}

	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		cancel()

		j := &Job{host: "h", opts: &Opts{Port: 22}}
		err := j.Dial(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("got %v, want Canceled", err)
		}
	})
}

func TestJobExec(t *testing.T) {
	tt := []struct {
		name   string
		status uint32
		err    bool
	}{
		{"ok", 0, true},
		{"fail", 1, false},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			sshDialHandlerMock(t, hardcodedOutputHandler("", tc.status))
			j := &Job{
				host: "h",
				opts: &Opts{Port: 22, ExecTimeout: time.Second},
				out:  NewOutput("h"),
			}
			defer j.Close()

			if err := j.Dial(ctx); err != nil {
				t.Fatal(err)
			}

			err := j.Exec(ctx)
			if tc.err && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tc.err && err == nil {
				t.Error("expected error")
			}
		})
	}

	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		cancel()

		sshDialHandlerMock(t, hardcodedOutputHandler("", 0))
		j := &Job{
			host: "h",
			opts: &Opts{Port: 22, ExecTimeout: time.Second},
			out:  NewOutput("h"),
		}
		defer j.Close()

		if err := j.Dial(context.Background()); err != nil {
			t.Fatal(err)
		}

		err := j.Exec(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("got %v, want Canceled", err)
		}
	})
}

func TestJobUpload(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		localFile := writeTestFile(t, "script.sh", testFileContent)
		remoteRoot := t.TempDir()
		handler := compositeHandler(
			sftpSubsystemHandler(remoteRoot),
			execRequestHandler("ok", 0),
		)
		sshDialHandlerMock(t, handler)

		j := &Job{
			host:  "h",
			tasks: UploadTask,
			opts:  &Opts{Port: 22, Files: []string{localFile}, UploadPath: "uploads"},
			out:   NewOutput("h"),
		}
		defer j.Close()

		if err := j.Dial(ctx); err != nil {
			t.Fatal(err)
		}

		var err error
		j.sftp, err = sftp.NewClient(j.ssh)
		if err != nil {
			t.Fatal(err)
		}

		if err := j.Upload(ctx); err != nil {
			t.Fatal(err)
		}

		uploaded := filepath.Join(remoteRoot, "uploads", "script.sh")
		content, err := os.ReadFile(uploaded)
		if err != nil {
			t.Errorf("uploaded file not found: %v", err)
		}
		if string(content) != testFileContent {
			t.Errorf("content mismatch: got %q", content)
		}

		if j.cmd != "" {
			t.Errorf("cmd = %q, want %q", j.cmd, "")
		}
	})

	t.Run("with_cmd", func(t *testing.T) {
		localFile := writeTestFile(t, "script.sh", testFileContent)
		remoteRoot := t.TempDir()
		sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))

		j := &Job{
			host:  "h",
			cmd:   "cat uploads/data.txt", // pre-set command
			tasks: UploadTask,
			opts:  &Opts{Port: 22, Files: []string{localFile}, UploadPath: "uploads"},
			out:   NewOutput("h"),
		}
		defer j.Close()

		if err := j.Dial(ctx); err != nil {
			t.Fatal(err)
		}

		var err error
		j.sftp, err = sftp.NewClient(j.ssh)
		if err != nil {
			t.Fatal(err)
		}

		if err := j.Upload(ctx); err != nil {
			t.Fatal(err)
		}

		// cmd should remain unchanged when pre-set
		if j.cmd != "cat uploads/data.txt" {
			t.Errorf("cmd was modified: %q", j.cmd)
		}
	})

	t.Run("multiple_files", func(t *testing.T) {
		localDir := t.TempDir()
		files := []string{
			writeTestFile(t, localDir+"/a.sh", "content:a.sh"),
			writeTestFile(t, localDir+"/b.txt", "content:b.txt"),
		}
		remoteRoot := t.TempDir()
		sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))

		j := &Job{
			host:  "h",
			tasks: UploadTask,
			opts:  &Opts{Port: 22, Files: files, UploadPath: "up"},
			out:   NewOutput("h"),
		}
		defer j.Close()

		if err := j.Dial(ctx); err != nil {
			t.Fatal(err)
		}

		var err error
		j.sftp, err = sftp.NewClient(j.ssh)
		if err != nil {
			t.Fatal(err)
		}

		if err := j.Upload(ctx); err != nil {
			t.Fatal(err)
		}

		for _, f := range files {
			uploaded := filepath.Join(remoteRoot, "up", filepath.Base(f))
			content, err := os.ReadFile(uploaded)
			if err != nil {
				t.Errorf("file %s not uploaded: %v", filepath.Base(f), err)
				continue
			}
			want := "content:" + filepath.Base(f)
			if string(content) != want {
				t.Errorf("file %s content = %q, want %q", filepath.Base(f), content, want)
			}
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		cancel()

		j := &Job{
			host: "h",
			opts: &Opts{Port: 22, Files: []string{"/nonexistent"}},
			out:  NewOutput("h"),
		}

		err := j.Upload(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("got %v, want Canceled", err)
		}
	})

	t.Run("missing_local_file", func(t *testing.T) {
		remoteRoot := t.TempDir()
		sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))

		j := &Job{
			host:  "h",
			tasks: UploadTask,
			opts:  &Opts{Port: 22, Files: []string{"/nonexistent/file.sh"}, UploadPath: "up"},
			out:   NewOutput("h"),
		}
		defer j.Close()

		if err := j.Dial(ctx); err != nil {
			t.Fatal(err)
		}

		var err error
		j.sftp, err = sftp.NewClient(j.ssh)
		if err != nil {
			t.Fatal(err)
		}

		err = j.Upload(ctx)
		if err == nil {
			t.Error("expected error for missing local file")
		}
	})

	t.Run("remote_mkdir_fail", func(t *testing.T) {
		localFile := writeTestFile(t, "scirpt.sh", testFileContent)
		remoteRoot := t.TempDir()
		if err := os.Chmod(remoteRoot, 0555); err != nil { // forbid mkdir
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(remoteRoot, 0755) })

		sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))

		j := &Job{
			host:  "h",
			tasks: UploadTask,
			opts:  &Opts{Port: 22, Files: []string{localFile}, UploadPath: "uploads"},
			out:   NewOutput("h"),
		}
		defer j.Close()

		if err := j.Dial(ctx); err != nil {
			t.Fatal(err)
		}

		var err error
		j.sftp, err = sftp.NewClient(j.ssh)
		if err != nil {
			t.Fatal(err)
		}

		err = j.Upload(ctx)
		if err == nil {
			t.Error("expected error for remote mkdir failure")
		}
	})
}

func TestJobStartWithUpload(t *testing.T) {
	discardStdout(t)
	t.Run("ok", func(t *testing.T) {
		localFile := writeTestFile(t, "run.sh", testFileContent)
		remoteRoot := t.TempDir()
		handler := compositeHandler(
			sftpSubsystemHandler(remoteRoot),
			execRequestHandler("done", 0),
		)
		sshDialHandlerMock(t, handler)

		j := &Job{
			host:  "h",
			tasks: UploadTask | ExecTask,
			opts:  &Opts{Port: 22, ExecTimeout: time.Second, Files: []string{localFile}, UploadPath: "work"},
		}
		defer j.Close()

		if err := j.Start(ctx); err != nil {
			t.Error(err)
		}

		if !j.tasks.Done() {
			t.Error("tasks not done")
		}

		uploaded := filepath.Join(remoteRoot, "work", "run.sh")
		if _, err := os.Stat(uploaded); err != nil {
			t.Errorf("file not uploaded: %v", err)
		}
	})

	t.Run("sftp_client_error", func(t *testing.T) {
		localFile := writeTestFile(t, "scirpt.sh", testFileContent)
		handler := compositeHandler(
			rejectSFTPHandler(),
			execRequestHandler("", 0),
		)
		sshDialHandlerMock(t, handler)

		j := &Job{
			host:  "h",
			tasks: UploadTask | ExecTask,
			opts:  &Opts{Port: 22, ExecTimeout: time.Second, Files: []string{localFile}, UploadPath: "work"},
		}
		defer j.Close()

		err := j.Start(ctx)
		if !errors.Is(err, ErrFileTransfer) {
			t.Errorf("got %v, want ErrFileTransfer", err)
		}
	})

	t.Run("upload_error", func(t *testing.T) {
		localFile := writeTestFile(t, "scirpt.sh", testFileContent)
		remoteRoot := t.TempDir()
		if err := os.Chmod(remoteRoot, 0555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(remoteRoot, 0755) })

		handler := compositeHandler(
			sftpSubsystemHandler(remoteRoot),
			execRequestHandler("", 0),
		)
		sshDialHandlerMock(t, handler)

		j := &Job{
			host:  "h",
			tasks: UploadTask | ExecTask,
			opts:  &Opts{Port: 22, ExecTimeout: time.Second, Files: []string{localFile}, UploadPath: "work"},
		}
		defer j.Close()

		err := j.Start(ctx)
		if !errors.Is(err, ErrFileTransfer) {
			t.Errorf("got %v, want ErrFileTransfer", err)
		}
	})
}
