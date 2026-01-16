package bichme

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
			path:        "work",
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
			err: ErrExecution,
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
					path:        "w",
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
					path:        "w",
				}
			},
			err: ErrFileTransfer,
		},
		{
			name: "download_sftp_fail",
			setup: func(t *testing.T) *Job {
				sshDialHandlerMock(t, compositeHandler(rejectSFTPHandler()))
				return &Job{
					host:  "h",
					tasks: DownloadTask,
					port:  22,
					files: []string{"/any"},
					path:  t.TempDir(),
				}
			},
			err: ErrFileTransfer,
		},
		{
			name: "download_fail",
			setup: func(t *testing.T) *Job {
				remoteRoot := t.TempDir()
				localRoot := t.TempDir()
				if err := os.WriteFile(filepath.Join(remoteRoot, "file.txt"), []byte("data"), 0644); err != nil {
					t.Fatal(err)
				}
				sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))
				if err := os.WriteFile(filepath.Join(localRoot, "h"), []byte("blocker"), 0644); err != nil {
					t.Fatal(err)
				}
				return &Job{
					host:  "h",
					tasks: DownloadTask,
					port:  22,
					files: []string{"file.txt"},
					path:  localRoot,
				}
			},
			err: ErrFileTransfer,
		},
		{
			name: "cleanup_sftp_fail",
			setup: func(t *testing.T) *Job {
				localFile := writeTestFile(t, "s.sh", testFileContent)
				sshDialHandlerMock(t, compositeHandler(rejectSFTPHandler(), execRequestHandler("", 0)))
				return &Job{
					host:        "h",
					tasks:       ExecTask | CleanupTask, // no UploadTask, so sftp not opened
					port:        22,
					execTimeout: time.Second,
					files:       []string{localFile},
					path:        "w",
				}
			},
			err: ErrFileTransfer,
		},
		{
			name: "cleanup_fail",
			setup: func(t *testing.T) *Job {
				localFile := writeTestFile(t, "s.sh", testFileContent)
				remoteRoot := t.TempDir()
				sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot), execRequestHandler("", 0)))
				return &Job{
					host:        "h",
					tasks:       ExecTask | CleanupTask,
					port:        22,
					execTimeout: time.Second,
					maxRetries:  1,
					files:       []string{localFile},
					path:        "w", // file doesn't exist, cleanup will fail
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
			host:  "h",
			tasks: UploadTask,
			port:  22,
			files: []string{localFile},
			path:  "uploads",
			out:   NewOutput("h"),
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
			host:  "h",
			tasks: UploadTask,
			port:  22,
			files: files,
			path:  "up",
			out:   NewOutput("h"),
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
			host:  "h",
			cmd:   "cat data.txt",
			tasks: UploadTask,
			port:  22,
			files: []string{localFile},
			path:  "uploads",
			out:   NewOutput("h"),
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

	t.Run("preserves_permissions", func(t *testing.T) {
		localDir := t.TempDir()
		firstFile := filepath.Join(localDir, "main.sh")
		if err := os.WriteFile(firstFile, []byte(testFileContent), 0640); err != nil {
			t.Fatal(err)
		}
		secondFile := filepath.Join(localDir, "data.txt")
		if err := os.WriteFile(secondFile, []byte("data"), 0640); err != nil {
			t.Fatal(err)
		}

		remoteRoot := t.TempDir()
		sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))

		j := &Job{
			host:  "h",
			tasks: UploadTask,
			port:  22,
			files: []string{firstFile, secondFile},
			path:  "uploads",
			out:   NewOutput("h"),
		}
		defer j.Close()
		dialAndSFTP(t, j)

		if err := j.Upload(ctx); err != nil {
			t.Fatal(err)
		}

		firstRemote := filepath.Join(remoteRoot, "uploads", "main.sh")
		info, err := os.Stat(firstRemote)
		if err != nil {
			t.Fatalf("stat first: %v", err)
		}
		if info.Mode().Perm() != 0640 {
			t.Errorf("first file mode = %o, want %o", info.Mode().Perm(), 0640)
		}

		secondRemote := filepath.Join(remoteRoot, "uploads", "data.txt")
		info, err = os.Stat(secondRemote)
		if err != nil {
			t.Fatalf("stat second: %v", err)
		}
		if info.Mode().Perm() != 0640 {
			t.Errorf("second file mode = %o, want %o", info.Mode().Perm(), 0640)
		}
	})

	t.Run("makes_first_file_executable_when_exec_task_set", func(t *testing.T) {
		localDir := t.TempDir()
		firstFile := filepath.Join(localDir, "main.sh")
		if err := os.WriteFile(firstFile, []byte(testFileContent), 0640); err != nil {
			t.Fatal(err)
		}
		secondFile := filepath.Join(localDir, "data.txt")
		if err := os.WriteFile(secondFile, []byte("data"), 0640); err != nil {
			t.Fatal(err)
		}

		remoteRoot := t.TempDir()
		sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))

		j := &Job{
			host:  "h",
			tasks: UploadTask | ExecTask,
			port:  22,
			files: []string{firstFile, secondFile},
			path:  "uploads",
			out:   NewOutput("h"),
		}
		defer j.Close()
		dialAndSFTP(t, j)

		if err := j.Upload(ctx); err != nil {
			t.Fatal(err)
		}

		firstRemote := filepath.Join(remoteRoot, "uploads", "main.sh")
		info, err := os.Stat(firstRemote)
		if err != nil {
			t.Fatalf("stat first: %v", err)
		}
		if info.Mode().Perm() != 0640|0111 {
			t.Errorf("first file mode = %o, want %o (original | +x)", info.Mode().Perm(), 0751)
		}

		secondRemote := filepath.Join(remoteRoot, "uploads", "data.txt")
		info, err = os.Stat(secondRemote)
		if err != nil {
			t.Fatalf("stat second: %v", err)
		}
		if info.Mode().Perm() != 0640 {
			t.Errorf("second file mode = %o, want %o", info.Mode().Perm(), 0640)
		}
	})

	errCases := []struct {
		name  string
		ctx   context.Context
		setup func(t *testing.T, j *Job)
	}{
		{"cancelled", cancelledCtx(), func(t *testing.T, j *Job) {
			remoteRoot := t.TempDir()
			sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))
			j.files = []string{writeTestFile(t, "s.sh", testFileContent)}
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
				host:  "h",
				tasks: UploadTask,
				port:  22,
				path:  "up",
				out:   NewOutput("h"),
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

func TestJobDownload(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		remoteRoot := t.TempDir()
		localRoot := t.TempDir()

		remoteFile := filepath.Join(remoteRoot, "data.txt")
		if err := os.WriteFile(remoteFile, []byte(testFileContent), 0644); err != nil {
			t.Fatal(err)
		}

		sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))

		j := &Job{
			host:  "h",
			tasks: DownloadTask,
			port:  22,
			files: []string{"data.txt"}, // relative to remoteRoot (sftp server cwd)
			path:  localRoot,
			out:   NewOutput("h"),
		}
		defer j.Close()
		dialAndSFTP(t, j)

		if err := j.Download(ctx); err != nil {
			t.Fatal(err)
		}

		content, err := os.ReadFile(filepath.Join(localRoot, "h", "data.txt"))
		if err != nil {
			t.Fatalf("downloaded file not found: %v", err)
		}
		if string(content) != testFileContent {
			t.Errorf("content = %q, want %q", content, testFileContent)
		}
	})

	t.Run("multiple_files_with_glob", func(t *testing.T) {
		remoteRoot := t.TempDir()
		localRoot := t.TempDir()

		logsDir := filepath.Join(remoteRoot, "logs")
		if err := os.MkdirAll(logsDir, 0755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"app.log", "error.log", "config.txt"} {
			if err := os.WriteFile(filepath.Join(logsDir, name), []byte(name), 0644); err != nil {
				t.Fatal(err)
			}
		}

		sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))

		j := &Job{
			host:  "server1",
			tasks: DownloadTask,
			port:  22,
			files: []string{"logs/*.log"}, // relative to remoteRoot
			path:  localRoot,
			out:   NewOutput("server1"),
		}
		defer j.Close()
		dialAndSFTP(t, j)

		if err := j.Download(ctx); err != nil {
			t.Fatal(err)
		}

		for _, name := range []string{"app.log", "error.log"} {
			content, err := os.ReadFile(filepath.Join(localRoot, "server1", "logs", name))
			if err != nil {
				t.Errorf("expected %s: %v", name, err)
				continue
			}
			if string(content) != name {
				t.Errorf("%s content = %q, want %q", name, content, name)
			}
		}

		// config.txt should not exist
		if _, err := os.Stat(filepath.Join(localRoot, "server1", "logs", "config.txt")); err == nil {
			t.Error("config.txt should not have been downloaded")
		}
	})

	t.Run("preserves_file_permissions", func(t *testing.T) {
		remoteRoot := t.TempDir()
		localRoot := t.TempDir()

		remoteFile := filepath.Join(remoteRoot, "exec.sh")
		if err := os.WriteFile(remoteFile, []byte(testFileContent), 0750); err != nil {
			t.Fatal(err)
		}

		sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))

		j := &Job{
			host:  "h",
			tasks: DownloadTask,
			port:  22,
			files: []string{"exec.sh"},
			path:  localRoot,
			out:   NewOutput("h"),
		}
		defer j.Close()
		dialAndSFTP(t, j)

		if err := j.Download(ctx); err != nil {
			t.Fatal(err)
		}

		localPath := filepath.Join(localRoot, "h", "exec.sh")
		info, err := os.Stat(localPath)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm() != 0750 {
			t.Errorf("mode = %o, want %o", info.Mode().Perm(), 0750)
		}
	})

	t.Run("preserves_dir_permissions", func(t *testing.T) {
		remoteRoot := t.TempDir()
		localRoot := t.TempDir()

		restrictedDir := filepath.Join(remoteRoot, "restricted")
		if err := os.MkdirAll(restrictedDir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(restrictedDir, "secret.txt"), []byte("secret"), 0600); err != nil {
			t.Fatal(err)
		}

		sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))

		j := &Job{
			host:  "h",
			tasks: DownloadTask,
			port:  22,
			files: []string{"restricted"},
			path:  localRoot,
			out:   NewOutput("h"),
		}
		defer j.Close()
		dialAndSFTP(t, j)

		if err := j.Download(ctx); err != nil {
			t.Fatal(err)
		}

		dirPath := filepath.Join(localRoot, "h", "restricted")
		info, err := os.Stat(dirPath)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm() != 0700 {
			t.Errorf("dir mode = %o, want %o", info.Mode().Perm(), 0700)
		}

		filePath := filepath.Join(localRoot, "h", "restricted", "secret.txt")
		fileInfo, err := os.Stat(filePath)
		if err != nil {
			t.Fatalf("stat file: %v", err)
		}
		if fileInfo.Mode().Perm() != 0600 {
			t.Errorf("file mode = %o, want %o", fileInfo.Mode().Perm(), 0600)
		}
	})

	t.Run("no_matches_logs_error", func(t *testing.T) {
		remoteRoot := t.TempDir()
		localRoot := t.TempDir()

		sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))

		var buf bytes.Buffer
		out := NewOutput("h")
		out.SetStdout(&buf)

		j := &Job{
			host:  "h",
			tasks: DownloadTask,
			port:  22,
			files: []string{"nonexistent"},
			path:  localRoot,
			out:   out,
		}
		defer j.Close()
		dialAndSFTP(t, j)

		if err := j.Download(ctx); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(buf.String(), "no such file") {
			t.Errorf("expected 'no such file' in output, got: %q", buf.String())
		}
	})
}

func TestJobCleanup(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		localFile := writeTestFile(t, "script.sh", testFileContent)
		remoteRoot := t.TempDir()
		sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))

		j := &Job{
			host:  "h",
			tasks: CleanupTask,
			port:  22,
			files: []string{localFile},
			path:  "uploads",
			out:   NewOutput("h"),
		}
		defer j.Close()
		dialAndSFTP(t, j)

		// First upload the file
		if err := j.Upload(ctx); err != nil {
			t.Fatal(err)
		}

		// Verify file exists
		remotePath := filepath.Join(remoteRoot, "uploads", "script.sh")
		if _, err := os.Stat(remotePath); err != nil {
			t.Fatalf("file should exist before cleanup: %v", err)
		}

		// Now cleanup
		if err := j.Cleanup(ctx); err != nil {
			t.Fatal(err)
		}

		// Verify file is removed
		if _, err := os.Stat(remotePath); !os.IsNotExist(err) {
			t.Error("file should be removed after cleanup")
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
			host:  "h",
			tasks: CleanupTask,
			port:  22,
			files: files,
			path:  "up",
			out:   NewOutput("h"),
		}
		defer j.Close()
		dialAndSFTP(t, j)

		// Upload files first
		if err := j.Upload(ctx); err != nil {
			t.Fatal(err)
		}

		// Cleanup
		if err := j.Cleanup(ctx); err != nil {
			t.Fatal(err)
		}

		// Verify all files are removed
		for _, f := range files {
			remotePath := filepath.Join(remoteRoot, "up", filepath.Base(f))
			if _, err := os.Stat(remotePath); !os.IsNotExist(err) {
				t.Errorf("file %s should be removed", filepath.Base(f))
			}
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		j := &Job{
			host:  "h",
			tasks: CleanupTask,
			port:  22,
			files: []string{"/any"},
			path:  "up",
			out:   NewOutput("h"),
		}
		defer j.Close()

		err := j.Cleanup(cancelledCtx())
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("bad_remove", func(t *testing.T) {
		remoteRoot := t.TempDir()
		sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))

		j := &Job{
			host:  "h",
			tasks: CleanupTask,
			port:  22,
			files: []string{"/nonexistent.sh"},
			path:  "up",
			out:   NewOutput("h"),
		}
		defer j.Close()
		dialAndSFTP(t, j)

		err := j.Cleanup(ctx)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestJobStartWithCleanup(t *testing.T) {
	discardStdout(t)

	t.Run("upload_exec_cleanup", func(t *testing.T) {
		localFile := writeTestFile(t, "run.sh", testFileContent)
		remoteRoot := t.TempDir()
		sshDialHandlerMock(t, compositeHandler(
			sftpSubsystemHandler(remoteRoot),
			execRequestHandler("done", 0),
		))

		j := &Job{
			host:        "h",
			tasks:       UploadTask | ExecTask | CleanupTask,
			port:        22,
			execTimeout: time.Second,
			files:       []string{localFile},
			path:        "work",
		}
		defer j.Close()

		if err := j.Start(ctx); err != nil {
			t.Error(err)
		}
		if !j.tasks.Done() {
			t.Error("tasks not done")
		}

		// Verify file was cleaned up
		remotePath := filepath.Join(remoteRoot, "work", "run.sh")
		if _, err := os.Stat(remotePath); !os.IsNotExist(err) {
			t.Error("file should be removed after cleanup")
		}
	})

	t.Run("cleanup_skipped_on_exec_failure", func(t *testing.T) {
		localFile := writeTestFile(t, "run.sh", testFileContent)
		remoteRoot := t.TempDir()
		sshDialHandlerMock(t, compositeHandler(
			sftpSubsystemHandler(remoteRoot),
			execRequestHandler("failed", 1), // non-zero exit
		))

		j := &Job{
			host:        "h",
			tasks:       UploadTask | ExecTask | CleanupTask,
			port:        22,
			execTimeout: time.Second,
			maxRetries:  1,
			files:       []string{localFile},
			path:        "work",
		}
		defer j.Close()

		err := j.Start(ctx)
		if err == nil {
			t.Error("expected error from failed exec")
		}

		// Verify file was NOT cleaned up (cleanup should be skipped on error)
		remotePath := filepath.Join(remoteRoot, "work", "run.sh")
		if _, err := os.Stat(remotePath); err != nil {
			t.Errorf("file should still exist after failed exec: %v", err)
		}
	})

	t.Run("cleanup_without_upload", func(t *testing.T) {
		// Test that cleanup works even when sftp wasn't opened for upload
		localFile := writeTestFile(t, "run.sh", testFileContent)
		remoteRoot := t.TempDir()

		// Pre-create the file on "remote" to simulate it already existing
		uploadDir := filepath.Join(remoteRoot, "work")
		if err := os.MkdirAll(uploadDir, 0755); err != nil {
			t.Fatal(err)
		}
		remoteFile := filepath.Join(uploadDir, "run.sh")
		if err := os.WriteFile(remoteFile, []byte(testFileContent), 0644); err != nil {
			t.Fatal(err)
		}

		sshDialHandlerMock(t, compositeHandler(
			sftpSubsystemHandler(remoteRoot),
			execRequestHandler("done", 0),
		))

		j := &Job{
			host:        "h",
			tasks:       ExecTask | CleanupTask, // No UploadTask
			port:        22,
			execTimeout: time.Second,
			files:       []string{localFile},
			path:        "work",
		}
		defer j.Close()

		if err := j.Start(ctx); err != nil {
			t.Error(err)
		}

		// Verify file was cleaned up
		if _, err := os.Stat(remoteFile); !os.IsNotExist(err) {
			t.Error("file should be removed after cleanup")
		}
	})
}

func TestJobStartWithDownload(t *testing.T) {
	discardStdout(t)

	t.Run("download_only", func(t *testing.T) {
		remoteRoot := t.TempDir()
		localRoot := t.TempDir()

		if err := os.WriteFile(filepath.Join(remoteRoot, "file.txt"), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		sshDialHandlerMock(t, compositeHandler(sftpSubsystemHandler(remoteRoot)))

		j := &Job{
			host:  "h",
			tasks: DownloadTask,
			port:  22,
			files: []string{"file.txt"},
			path:  localRoot,
		}
		defer j.Close()

		if err := j.Start(ctx); err != nil {
			t.Error(err)
		}
		if !j.tasks.Done() {
			t.Error("tasks not done")
		}
		if _, err := os.Stat(filepath.Join(localRoot, "h", "file.txt")); err != nil {
			t.Errorf("file not downloaded: %v", err)
		}
	})

	t.Run("exec_then_download", func(t *testing.T) {
		remoteRoot := t.TempDir()
		localRoot := t.TempDir()

		if err := os.WriteFile(filepath.Join(remoteRoot, "output.txt"), []byte("result"), 0644); err != nil {
			t.Fatal(err)
		}

		sshDialHandlerMock(t, compositeHandler(
			sftpSubsystemHandler(remoteRoot),
			execRequestHandler("done", 0),
		))

		j := &Job{
			host:        "h",
			cmd:         "echo done",
			tasks:       ExecTask | DownloadTask,
			port:        22,
			execTimeout: time.Second,
			files:       []string{"output.txt"},
			path:        localRoot,
		}
		defer j.Close()

		if err := j.Start(ctx); err != nil {
			t.Error(err)
		}
		if !j.tasks.Done() {
			t.Error("tasks not done")
		}
		if _, err := os.Stat(filepath.Join(localRoot, "h", "output.txt")); err != nil {
			t.Errorf("file not downloaded: %v", err)
		}
	})
}
