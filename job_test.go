package bichme

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestJobClose(t *testing.T) {
	sshDialHandlerMock(t, hardcodedOutputHandler("", 0))

	tt := []struct {
		name string
		job  func() *Job
	}{
		{"zero", func() *Job { return &Job{} }},
		{"connected", func() *Job {
			j := &Job{host: "h", port: 22, execTimeout: time.Second, out: NewOutput("h")}
			if err := j.Dial(ctx); err != nil {
				t.Fatal(err)
			}
			return j
		}},
		{"disconnected", func() *Job {
			j := &Job{host: "h", port: 22, execTimeout: time.Second, out: NewOutput("h")}
			if err := j.Dial(ctx); err != nil {
				t.Fatal(err)
			}
			j.Close()
			return j
		}},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.job().Close(); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestJobStart(t *testing.T) {
	discardStdout(t)

	t.Run("no_tasks", func(t *testing.T) {
		j := &Job{tasks: 0}
		if err := j.Start(ctx); err != nil {
			t.Error(err)
		}
		if j.tries != 0 {
			t.Errorf("tries = %d, want 0", j.tries)
		}
	})

	t.Run("exec", func(t *testing.T) {
		sshDialHandlerMock(t, hardcodedOutputHandler("ok", 0))
		j := &Job{host: "h", cmd: "true", tasks: ExecTask, port: 22, execTimeout: time.Second}
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

	t.Run("with_history", func(t *testing.T) {
		dir := t.TempDir()
		sshDialHandlerMock(t, hardcodedOutputHandler("", 0))
		j := &Job{
			host:        "h",
			tasks:       ExecTask | KeepHistoryTask,
			port:        22,
			execTimeout: time.Second,
			historyPath: dir,
		}
		defer j.Close()

		if err := j.Start(ctx); err != nil {
			t.Error(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "h_1.log")); err != nil {
			t.Errorf("history file: %v", err)
		}
	})

	t.Run("upload_and_exec", func(t *testing.T) {
		localFile := writeTestFile(t, "run.sh", testFileContent)
		remoteRoot := t.TempDir()
		sshDialHandlerMock(t, compositeHandler(
			sftpSubsystemHandler(remoteRoot),
			execRequestHandler("done", 0),
		))

		j := &Job{
			host:        "h",
			tasks:       UploadTask | ExecTask,
			port:        22,
			execTimeout: time.Second,
			files:       []string{localFile},
			uploadPath:  "work",
		}
		defer j.Close()

		if err := j.Start(ctx); err != nil {
			t.Error(err)
		}
		if !j.tasks.Done() {
			t.Error("tasks not done")
		}
		if _, err := os.Stat(filepath.Join(remoteRoot, "work", "run.sh")); err != nil {
			t.Errorf("file not uploaded: %v", err)
		}
	})

	errCases := []struct {
		name  string
		ctx   context.Context
		setup func(t *testing.T) *Job
		err   error
	}{
		{
			name:  "cancelled",
			ctx:   cancelledCtx(),
			setup: func(t *testing.T) *Job { return &Job{tasks: ExecTask, port: 22, execTimeout: time.Second} },
			err:   context.Canceled,
		},
		{
			name: "dial_refused",
			setup: func(t *testing.T) *Job {
				sshDialMock(t, func(_, _ string, _ *ssh.ClientConfig) (*ssh.Client, error) {
					return nil, errors.New("refused")
				})
				return &Job{host: "h", tasks: ExecTask, port: 22, execTimeout: time.Second}
			},
			err: ErrConnection,
		},
		{
			name: "exec_fail",
			setup: func(t *testing.T) *Job {
				sshDialHandlerMock(t, hardcodedOutputHandler("", 1))
				return &Job{host: "h", tasks: ExecTask, port: 22, execTimeout: time.Second, maxRetries: 1}
			},
			err: ErrExection,
		},
		{
			name: "timeout",
			setup: func(t *testing.T) *Job {
				sshDialHandlerMock(t, sleepHardcodedOutputHandler(100*time.Millisecond, "", 0))
				return &Job{host: "h", tasks: ExecTask, port: 22, execTimeout: 50 * time.Millisecond}
			},
			err: os.ErrDeadlineExceeded,
		},
		{
			name: "sftp_fail",
			setup: func(t *testing.T) *Job {
				localFile := writeTestFile(t, "s.sh", testFileContent)
				sshDialHandlerMock(t, compositeHandler(rejectSFTPHandler(), execRequestHandler("", 0)))
				return &Job{
					host:        "h",
					tasks:       UploadTask | ExecTask,
					port:        22,
					execTimeout: time.Second,
					files:       []string{localFile},
					uploadPath:  "w",
				}
			},
			err: ErrFileTransfer,
		},
		{
			name: "upload_fail",
			setup: func(t *testing.T) *Job {
				localFile := writeTestFile(t, "s.sh", testFileContent)
				remoteRoot := t.TempDir()
				os.Chmod(remoteRoot, 0555)
				t.Cleanup(func() { os.Chmod(remoteRoot, 0755) })
				sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot), execRequestHandler("", 0)))
				return &Job{
					host:        "h",
					tasks:       UploadTask | ExecTask,
					port:        22,
					execTimeout: time.Second,
					files:       []string{localFile},
					uploadPath:  "w",
				}
			},
			err: ErrFileTransfer,
		},
	}

	for _, tc := range errCases {
		t.Run("bad_"+tc.name, func(t *testing.T) {
			j := tc.setup(t)
			defer j.Close()

			testCtx := ctx
			if tc.ctx != nil {
				testCtx = tc.ctx
			}

			err := j.Start(testCtx)
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, tc.err) {
				t.Errorf("got %v, want %v", err, tc.err)
			}
		})
	}
}

func TestJobDial(t *testing.T) {
	tt := []struct {
		name string
		host string
		dial func(string, string, *ssh.ClientConfig) (*ssh.Client, error)
		ctx  context.Context
		err  error
	}{
		{
			name: "ok",
			host: "h",
		},
		{
			name: "with_port",
			host: "h:2222",
		},
		{
			name: "cancelled",
			host: "h",
			ctx:  cancelledCtx(),
			err:  context.Canceled,
		},
		{
			name: "refused",
			host: "h",
			dial: func(_, _ string, _ *ssh.ClientConfig) (*ssh.Client, error) {
				return nil, errors.New("refused")
			},
			err: errors.New("refused"),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			if tc.dial != nil {
				sshDialMock(t, tc.dial)
			} else {
				sshDialHandlerMock(t, hardcodedOutputHandler("", 0))
			}

			j := &Job{host: tc.host, port: 22, execTimeout: time.Second, out: NewOutput("h")}
			defer j.Close()

			testCtx := ctx
			if tc.ctx != nil {
				testCtx = tc.ctx
			}

			err := j.Dial(testCtx)
			if tc.err != nil {
				if err == nil {
					t.Fatal("expected error")
				}
				if tc.err != nil && !errors.Is(err, tc.err) && err.Error() != tc.err.Error() {
					t.Errorf("got %v, want %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if j.ssh == nil {
				t.Error("ssh client not set")
			}
		})
	}
}

func TestJobExec(t *testing.T) {
	tt := []struct {
		name   string
		status uint32
		ctx    context.Context
		err    bool
	}{
		{"ok", 0, nil, false},
		{"nonzero_exit", 1, nil, true},
		{"cancelled", 0, cancelledCtx(), true},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			sshDialHandlerMock(t, hardcodedOutputHandler("", tc.status))
			j := &Job{host: "h", port: 22, execTimeout: time.Second, out: NewOutput("h")}
			defer j.Close()

			if err := j.Dial(context.Background()); err != nil {
				t.Fatal(err)
			}

			testCtx := ctx
			if tc.ctx != nil {
				testCtx = tc.ctx
			}

			err := j.Exec(testCtx)
			if tc.err && err == nil {
				t.Error("expected error")
			}
			if !tc.err && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestJobUpload(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		localFile := writeTestFile(t, "script.sh", testFileContent)
		remoteRoot := t.TempDir()
		sshDialHandlerMock(t, compositeHandler(
			sftpSubsystemHandler(remoteRoot),
			execRequestHandler("ok", 0),
		))

		j := &Job{
			host:       "h",
			tasks:      UploadTask,
			port:       22,
			files:      []string{localFile},
			uploadPath: "uploads",
			out:        NewOutput("h"),
		}
		defer j.Close()
		dialAndSFTP(t, j)

		if err := j.Upload(ctx); err != nil {
			t.Fatal(err)
		}

		content, err := os.ReadFile(filepath.Join(remoteRoot, "uploads", "script.sh"))
		if err != nil {
			t.Fatalf("uploaded file not found: %v", err)
		}
		if string(content) != testFileContent {
			t.Errorf("content = %q, want %q", content, testFileContent)
		}
	})

	t.Run("multiple_files", func(t *testing.T) {
		localDir := t.TempDir()
		files := []string{
			writeTestFile(t, localDir+"/a.sh", "a"),
			writeTestFile(t, localDir+"/b.txt", "b"),
		}
		remoteRoot := t.TempDir()
		sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))

		j := &Job{
			host:       "h",
			tasks:      UploadTask,
			port:       22,
			files:      files,
			uploadPath: "up",
			out:        NewOutput("h"),
		}
		defer j.Close()
		dialAndSFTP(t, j)

		if err := j.Upload(ctx); err != nil {
			t.Fatal(err)
		}

		for i, f := range files {
			content, err := os.ReadFile(filepath.Join(remoteRoot, "up", filepath.Base(f)))
			if err != nil {
				t.Errorf("file %s not uploaded: %v", filepath.Base(f), err)
				continue
			}
			want := string([]byte{'a' + byte(i)})
			if string(content) != want {
				t.Errorf("file %s = %q, want %q", filepath.Base(f), content, want)
			}
		}
	})

	t.Run("preserves_cmd", func(t *testing.T) {
		localFile := writeTestFile(t, "script.sh", testFileContent)
		remoteRoot := t.TempDir()
		sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))

		j := &Job{
			host:       "h",
			cmd:        "cat data.txt",
			tasks:      UploadTask,
			port:       22,
			files:      []string{localFile},
			uploadPath: "uploads",
			out:        NewOutput("h"),
		}
		defer j.Close()
		dialAndSFTP(t, j)

		if err := j.Upload(ctx); err != nil {
			t.Fatal(err)
		}
		if j.cmd != "cat data.txt" {
			t.Errorf("cmd modified: %q", j.cmd)
		}
	})

	errCases := []struct {
		name  string
		ctx   context.Context
		setup func(t *testing.T, j *Job)
	}{
		{"cancelled", cancelledCtx(), nil},
		{"missing_file", nil, func(t *testing.T, j *Job) {
			remoteRoot := t.TempDir()
			sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))
			j.files = []string{"/nonexistent"}
			dialAndSFTP(t, j)
		}},
		{"mkdir_fail", nil, func(t *testing.T, j *Job) {
			remoteRoot := t.TempDir()
			os.Chmod(remoteRoot, 0555)
			t.Cleanup(func() { os.Chmod(remoteRoot, 0755) })
			sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))
			j.files = []string{writeTestFile(t, "s.sh", testFileContent)}
			dialAndSFTP(t, j)
		}},
	}

	for _, tc := range errCases {
		t.Run("bad_"+tc.name, func(t *testing.T) {
			j := &Job{
				host:       "h",
				tasks:      UploadTask,
				port:       22,
				uploadPath: "up",
				out:        NewOutput("h"),
			}
			defer j.Close()

			if tc.setup != nil {
				tc.setup(t, j)
			}

			testCtx := ctx
			if tc.ctx != nil {
				testCtx = tc.ctx
			}

			if err := j.Upload(testCtx); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestJobStartRetries(t *testing.T) {
	discardStdout(t)
	sshDialHandlerMock(t, hardcodedOutputHandler("", 1))

	tt := []struct {
		name     string
		tries    int
		retries  int
		wantDone bool
	}{
		{"available", 0, 2, false},
		{"exhausted", 1, 1, true},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			j := &Job{
				host:        "h",
				tasks:       ExecTask,
				tries:       tc.tries,
				port:        22,
				execTimeout: time.Second,
				maxRetries:  tc.retries,
			}
			defer j.Close()

			j.Start(ctx)
			if j.tasks.Done() != tc.wantDone {
				t.Errorf("tasks.Done() = %v, want %v", j.tasks.Done(), tc.wantDone)
			}
		})
	}
}
