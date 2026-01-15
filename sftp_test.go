package bichme

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkg/sftp"
)

func newInMemSFTP(t *testing.T, handlers sftp.Handlers) *sftp.Client {
	t.Helper()

	server, client := net.Pipe()
	srv := sftp.NewRequestServer(server, handlers)

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve() }()

	t.Cleanup(func() {
		srv.Close()
		server.Close()
		client.Close()

		if err := <-serveErr; err != nil &&
			!errors.Is(err, io.EOF) &&
			!errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("sftp server exited: %v", err)
		}
	})

	c, err := sftp.NewClientPipe(client, client)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

const testFileContent = "#!/bin/sh\necho ok"

func writeTestFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := filepath.Dir(name)
	if dir == "." {
		dir = t.TempDir()
	}
	path := filepath.Join(dir, filepath.Base(name))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

// setupRemoteFile creates a file on the remote SFTP server, creating parent directories as needed.
func setupRemoteFile(t *testing.T, client *sftp.Client, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if dir != "/" && dir != "." {
		if err := client.MkdirAll(dir); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	f, err := client.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	f.Write([]byte(content))
	f.Close()
}

func TestUpload(t *testing.T) {
	remoteDir := "/uploads"
	localFile := writeTestFile(t, "script.sh", testFileContent)
	largeFile := writeTestFile(t, "large.sh", strings.Repeat(testFileContent, 8192))

	t.Run("ok", func(t *testing.T) {
		client := newInMemSFTP(t, sftp.InMemHandler())

		if err := upload(ctx, client, remoteDir, localFile); err != nil {
			t.Fatalf("upload: %v", err)
		}

		remotePath := filepath.Join(remoteDir, filepath.Base(localFile))
		f, err := client.Open(remotePath)
		if err != nil {
			t.Fatalf("read remote: %v", err)
		}
		data, err := io.ReadAll(f)
		if err != nil {
			t.Fatalf("read remote content: %v", err)
		}
		if string(data) != testFileContent {
			t.Fatalf("content = %q, want %q", data, "data")
		}

		entries, err := client.ReadDir(remoteDir)
		if err != nil {
			t.Fatalf("readdir: %v", err)
		}
		for _, e := range entries {
			if strings.Contains(e.Name(), ".tmp") {
				t.Fatalf("temp file left behind: %s", e.Name())
			}
		}
	})

	err := errors.New("oops")
	cases := []struct {
		name    string
		handler func(*sftp.Handlers)
		file    string
	}{
		{
			name: "create",
			handler: func(h *sftp.Handlers) {
				h.FilePut = createFailingWriter{err: err}
			},
			file: localFile,
		},
		{
			name: "chmod",
			handler: func(h *sftp.Handlers) {
				h.FileCmd = chmodFailingCmd{FileCmder: h.FileCmd, err: err}
			},
			file: localFile,
		},
		{
			name: "copy",
			handler: func(h *sftp.Handlers) {
				h.FilePut = writeFailingWriter{FileWriter: h.FilePut, err: err}
			},
			file: largeFile, // to force buffer flush and propagate write errors
		},
		{
			name: "close",
			handler: func(h *sftp.Handlers) {
				h.FilePut = closeFailingWriter{FileWriter: h.FilePut, err: err}
			},
			file: localFile,
		},
		{
			name: "rename",
			handler: func(h *sftp.Handlers) {
				h.FileCmd = renameFailingCmd{FileCmder: h.FileCmd, err: err}
			},
			file: localFile,
		},
	}

	for _, tc := range cases {
		t.Run("bad_"+tc.name, func(t *testing.T) {
			handlers := sftp.InMemHandler()
			tc.handler(&handlers)
			client := newInMemSFTP(t, handlers)

			err := upload(ctx, client, remoteDir, tc.file)
			if err == nil {
				t.Fatal("expected err; got nil")
			}

			entries, err := client.ReadDir(remoteDir)
			if err != nil {
				t.Fatalf("readdir: %v", err)
			}
			for _, e := range entries {
				if strings.Contains(e.Name(), ".tmp") {
					t.Fatalf("temp file not cleaned up: %s", e.Name())
				}
				if e.Name() == filepath.Base(tc.file) {
					t.Fatalf("final file should not exist on")
				}
			}
		})
	}
}

func TestMakeExec(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		client := newInMemSFTP(t, sftp.InMemHandler())
		filename := "/script.sh"

		f, err := client.Create(filename)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		f.Close()

		if err := makeExec(ctx, client, filename); err != nil {
			t.Fatalf("makeExec: %v", err)
		}
	})

	t.Run("bad_chmod", func(t *testing.T) {
		handlers := sftp.InMemHandler()
		handlers.FileCmd = chmodFailingCmd{FileCmder: handlers.FileCmd, err: errors.New("chmod failed")}
		client := newInMemSFTP(t, handlers)

		filename := "/script.sh"
		f, err := client.Create(filename)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		f.Close()

		err = makeExec(ctx, client, filename)
		if err == nil || !strings.Contains(err.Error(), "chmod") {
			t.Fatalf("expected chmod error, got %v", err)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		client := newInMemSFTP(t, sftp.InMemHandler())
		ctx := cancelledCtx()
		err := makeExec(ctx, client, "/script.sh")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})
}

func TestSftpIsAlive(t *testing.T) {
	for _, alive := range []bool{true, false} {
		t.Run(fmt.Sprintf("%v", alive), func(t *testing.T) {
			client := newInMemSFTP(t, sftp.InMemHandler())
			if !alive {
				client.Close()
			}

			res := sftpIsAlive(client)
			if res != alive {
				t.Errorf("expected %v, got %v", alive, res)
			}
		})
	}
}

func TestDownload(t *testing.T) {
	localDir := t.TempDir()

	t.Run("ok", func(t *testing.T) {
		client := newInMemSFTP(t, sftp.InMemHandler())
		setupRemoteFile(t, client, "/remote/test.txt", testFileContent)

		downloadDir := filepath.Join(localDir, "ok")
		if err := download(ctx, client, downloadDir, "/remote/test.txt"); err != nil {
			t.Fatalf("download: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(downloadDir, "remote", "test.txt"))
		if err != nil {
			t.Fatalf("read local: %v", err)
		}
		if string(data) != testFileContent {
			t.Errorf("content = %q, want %q", data, testFileContent)
		}
	})

	t.Run("glob", func(t *testing.T) {
		client := newInMemSFTP(t, sftp.InMemHandler())
		for _, name := range []string{"app.log", "error.log", "debug.txt"} {
			setupRemoteFile(t, client, "/logs/"+name, name)
		}

		downloadDir := filepath.Join(localDir, "glob")
		if err := download(ctx, client, downloadDir, "/logs/*.log"); err != nil {
			t.Fatalf("download: %v", err)
		}

		// Full path preserved: <downloadDir>/logs/<file>
		for _, name := range []string{"app.log", "error.log"} {
			data, err := os.ReadFile(filepath.Join(downloadDir, "logs", name))
			if err != nil {
				t.Errorf("expected %s: %v", name, err)
				continue
			}
			if string(data) != name {
				t.Errorf("%s content = %q, want %q", name, data, name)
			}
		}

		// debug.txt should not exist
		if _, err := os.Stat(filepath.Join(downloadDir, "logs", "debug.txt")); err == nil {
			t.Error("debug.txt should not have been downloaded")
		}
	})

	t.Run("recursive_directory", func(t *testing.T) {
		client := newInMemSFTP(t, sftp.InMemHandler())
		for _, path := range []string{"/data/file.txt", "/data/subdir/inner.txt", "/data/subdir/nested/deep.txt"} {
			setupRemoteFile(t, client, path, path)
		}

		downloadDir := filepath.Join(localDir, "recursive")
		if err := download(ctx, client, downloadDir, "/data"); err != nil {
			t.Fatalf("download: %v", err)
		}

		for _, path := range []string{"/data/file.txt", "/data/subdir/inner.txt", "/data/subdir/nested/deep.txt"} {
			localPath := filepath.Join(downloadDir, path)
			data, err := os.ReadFile(localPath)
			if err != nil {
				t.Errorf("expected %s: %v", path, err)
				continue
			}
			if string(data) != path {
				t.Errorf("%s content = %q, want %q", path, data, path)
			}
		}

		if info, err := os.Stat(filepath.Join(downloadDir, "data", "subdir", "nested")); err != nil || !info.IsDir() {
			t.Error("nested directory should exist")
		}
	})

	t.Run("empty_directory", func(t *testing.T) {
		client := newInMemSFTP(t, sftp.InMemHandler())

		if err := client.MkdirAll("/empty/nested"); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		downloadDir := filepath.Join(localDir, "emptydir")
		if err := download(ctx, client, downloadDir, "/empty"); err != nil {
			t.Fatalf("download: %v", err)
		}

		if info, err := os.Stat(filepath.Join(downloadDir, "empty", "nested")); err != nil || !info.IsDir() {
			t.Error("empty nested directory should exist")
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		client := newInMemSFTP(t, sftp.InMemHandler())
		ctx := cancelledCtx()
		err := download(ctx, client, localDir, "/any")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("bad_glob", func(t *testing.T) {
		client := newInMemSFTP(t, sftp.InMemHandler())
		downloadDir := filepath.Join(localDir, "badglob")
		// Non-matching pattern should succeed with no files
		if err := download(ctx, client, downloadDir, "/nonexistent/*.log"); err != nil {
			t.Fatalf("download non-matching pattern: %v", err)
		}
	})

	t.Run("bad_downloadPath", func(t *testing.T) {
		client := newInMemSFTP(t, sftp.InMemHandler())
		setupRemoteFile(t, client, "/file.txt", "content")

		downloadDir := filepath.Join(localDir, "bad_downloadPath")
		if err := os.WriteFile(downloadDir, []byte("blocker"), 0644); err != nil {
			t.Fatalf("write blocker: %v", err)
		}

		err := download(ctx, client, downloadDir, "/file.txt")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "create dir") {
			t.Errorf("error = %v, want containing 'create dir'", err)
		}
	})
}

func TestDownloadFile(t *testing.T) {
	tt := []struct {
		name        string
		handler     func() sftp.Handlers
		setup       func(t *testing.T, client *sftp.Client, localDir string)
		remotePath  string
		wantContent string
		wantErr     string
	}{
		{
			name:    "ok",
			handler: sftp.InMemHandler,
			setup: func(t *testing.T, client *sftp.Client, localDir string) {
				setupRemoteFile(t, client, "/subdir/download.txt", testFileContent)
			},
			remotePath:  "/subdir/download.txt",
			wantContent: testFileContent,
		},
		{
			name:       "bad_remote_open",
			handler:    sftp.InMemHandler,
			setup:      func(t *testing.T, client *sftp.Client, localDir string) {},
			remotePath: "/nonexistent",
			wantErr:    "open",
		},
		{
			name:    "bad_mkdir",
			handler: sftp.InMemHandler,
			setup: func(t *testing.T, client *sftp.Client, localDir string) {
				setupRemoteFile(t, client, "/deep/file.txt", "")
				if err := os.WriteFile(filepath.Join(localDir, "deep"), []byte("blocker"), 0644); err != nil {
					t.Fatalf("write blocker: %v", err)
				}
			},
			remotePath: "/deep/file.txt",
			wantErr:    "create dir",
		},
		{
			name:    "skip_existing",
			handler: sftp.InMemHandler,
			setup: func(t *testing.T, client *sftp.Client, localDir string) {
				setupRemoteFile(t, client, "/file.txt", "remote content")
				if err := os.WriteFile(filepath.Join(localDir, "file.txt"), []byte("local content"), 0644); err != nil {
					t.Fatalf("write local: %v", err)
				}
			},
			remotePath:  "/file.txt",
			wantContent: "local content", // should keep local, not overwrite
		},
		{
			name: "bad_copy",
			handler: func() sftp.Handlers {
				h := sftp.InMemHandler()
				h.FileGet = readFailingReader{err: errors.New("read failed")}
				return h
			},
			setup: func(t *testing.T, client *sftp.Client, localDir string) {
				setupRemoteFile(t, client, "/file.txt", strings.Repeat("x", 65536)) // large enough to trigger read
			},
			remotePath: "/file.txt",
			wantErr:    "copy",
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			client := newInMemSFTP(t, tc.handler())
			localDir := t.TempDir()
			tc.setup(t, client, localDir)

			err := downloadFile(client, localDir, tc.remotePath)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %v, want containing %q", err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("downloadFile: %v", err)
			}

			localPath := filepath.Join(localDir, tc.remotePath)
			data, err := os.ReadFile(localPath)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if string(data) != tc.wantContent {
				t.Errorf("content = %q, want %q", data, tc.wantContent)
			}
		})
	}
}

// readFailingReader returns an error on read
type readFailingReader struct {
	err error
}

func (r readFailingReader) Fileread(req *sftp.Request) (io.ReaderAt, error) {
	return r, nil
}

func (r readFailingReader) ReadAt(p []byte, off int64) (int, error) {
	return 0, r.err
}

func TestDownloadSymlink(t *testing.T) {
	tt := []struct {
		name       string
		setup      func(t *testing.T, localDir string) *sftp.Client
		remotePath string
		wantTarget string
		wantErr    string
	}{
		{
			name: "ok",
			setup: func(t *testing.T, localDir string) *sftp.Client {
				client := newInMemSFTP(t, sftp.InMemHandler())
				setupRemoteFile(t, client, "/target.txt", "target content")
				if err := client.Symlink("/target.txt", "/link.txt"); err != nil {
					t.Fatalf("symlink: %v", err)
				}
				return client
			},
			remotePath: "/link.txt",
			wantTarget: "/target.txt",
		},
		{
			name: "nested_path",
			setup: func(t *testing.T, localDir string) *sftp.Client {
				client := newInMemSFTP(t, sftp.InMemHandler())
				if err := client.MkdirAll("/a/b"); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := client.Symlink("../target", "/a/b/link"); err != nil {
					t.Fatalf("symlink: %v", err)
				}
				return client
			},
			remotePath: "/a/b/link",
			wantTarget: "../target",
		},
		{
			name: "skip_existing",
			setup: func(t *testing.T, localDir string) *sftp.Client {
				client := newInMemSFTP(t, sftp.InMemHandler())
				if err := client.Symlink("/new_target", "/existing"); err != nil {
					t.Fatalf("symlink: %v", err)
				}
				if err := os.Symlink("/old_target", filepath.Join(localDir, "existing")); err != nil {
					t.Fatalf("local symlink: %v", err)
				}
				return client
			},
			remotePath: "/existing",
			wantTarget: "/old_target", // should keep old target, not overwrite
		},
		{
			name: "bad_readlink",
			setup: func(t *testing.T, localDir string) *sftp.Client {
				return newInMemSFTP(t, sftp.InMemHandler())
			},
			remotePath: "/nonexistent",
			wantErr:    "readlink",
		},
		{
			name: "bad_mkdir",
			setup: func(t *testing.T, localDir string) *sftp.Client {
				client := newInMemSFTP(t, sftp.InMemHandler())
				if err := client.MkdirAll("/deep"); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := client.Symlink("/target", "/deep/link"); err != nil {
					t.Fatalf("symlink: %v", err)
				}
				if err := os.WriteFile(filepath.Join(localDir, "deep"), []byte("file"), 0644); err != nil {
					t.Fatalf("write: %v", err)
				}
				return client
			},
			remotePath: "/deep/link",
			wantErr:    "create dir",
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			localDir := t.TempDir()
			client := tc.setup(t, localDir)

			err := downloadSymlink(client, localDir, tc.remotePath)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %v, want containing %q", err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("downloadSymlink: %v", err)
			}

			localPath := filepath.Join(localDir, tc.remotePath)
			target, err := os.Readlink(localPath)
			if err != nil {
				t.Fatalf("readlink local: %v", err)
			}
			if target != tc.wantTarget {
				t.Errorf("target = %q, want %q", target, tc.wantTarget)
			}
		})
	}
}

func TestDownloadDir(t *testing.T) {
	tt := []struct {
		name    string
		ctx     func() context.Context
		setup   func(t *testing.T, client *sftp.Client, localDir string)
		dir     string
		check   func(t *testing.T, localDir string)
		wantErr string
	}{
		{
			name: "ok",
			setup: func(t *testing.T, client *sftp.Client, localDir string) {
				for _, p := range []string{"/dir/a.txt", "/dir/sub/b.txt"} {
					setupRemoteFile(t, client, p, p)
				}
			},
			dir: "/dir",
			check: func(t *testing.T, localDir string) {
				for _, p := range []string{"/dir/a.txt", "/dir/sub/b.txt"} {
					data, err := os.ReadFile(filepath.Join(localDir, p))
					if err != nil {
						t.Errorf("read %s: %v", p, err)
						continue
					}
					if string(data) != p {
						t.Errorf("%s content = %q, want %q", p, data, p)
					}
				}
			},
		},
		{
			name: "with_symlink",
			setup: func(t *testing.T, client *sftp.Client, localDir string) {
				setupRemoteFile(t, client, "/dir/file.txt", "content")
				if err := client.Symlink("file.txt", "/dir/link"); err != nil {
					t.Fatalf("symlink: %v", err)
				}
			},
			dir: "/dir",
			check: func(t *testing.T, localDir string) {
				if _, err := os.Stat(filepath.Join(localDir, "dir", "file.txt")); err != nil {
					t.Errorf("file.txt should exist: %v", err)
				}
				target, err := os.Readlink(filepath.Join(localDir, "dir", "link"))
				if err != nil {
					t.Errorf("link should exist: %v", err)
				}
				if target != "file.txt" {
					t.Errorf("link target = %q, want %q", target, "file.txt")
				}
			},
		},
		{
			name: "bad_mkdir",
			setup: func(t *testing.T, client *sftp.Client, localDir string) {
				if err := client.MkdirAll("/dir/sub"); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(localDir, "dir"), []byte("blocker"), 0644); err != nil {
					t.Fatalf("write blocker: %v", err)
				}
			},
			dir:     "/dir",
			wantErr: "create dir",
		},
		{
			name: "bad_symlink_mkdir_in_dir",
			setup: func(t *testing.T, client *sftp.Client, localDir string) {
				if err := client.MkdirAll("/dir/sub"); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := client.Symlink("/target", "/dir/sub/link"); err != nil {
					t.Fatalf("symlink: %v", err)
				}
				if err := os.MkdirAll(filepath.Join(localDir, "dir"), 0755); err != nil {
					t.Fatalf("mkdir local: %v", err)
				}
				if err := os.WriteFile(filepath.Join(localDir, "dir", "sub"), []byte("blocker"), 0644); err != nil {
					t.Fatalf("write blocker: %v", err)
				}
			},
			dir:     "/dir",
			wantErr: "create dir",
		},
		{
			name: "bad_file_mkdir_in_dir",
			setup: func(t *testing.T, client *sftp.Client, localDir string) {
				setupRemoteFile(t, client, "/dir/sub/file.txt", "content")
				if err := os.MkdirAll(filepath.Join(localDir, "dir"), 0755); err != nil {
					t.Fatalf("mkdir local: %v", err)
				}
				if err := os.WriteFile(filepath.Join(localDir, "dir", "sub"), []byte("blocker"), 0644); err != nil {
					t.Fatalf("write blocker: %v", err)
				}
			},
			dir:     "/dir",
			wantErr: "create dir",
		},
		{
			name: "cancelled",
			ctx:  cancelledCtx,
			setup: func(t *testing.T, client *sftp.Client, localDir string) {
				setupRemoteFile(t, client, "/dir/file.txt", "")
			},
			dir:     "/dir",
			wantErr: "context canceled",
		},
		{
			name: "symlink_error_propagates",
			setup: func(t *testing.T, client *sftp.Client, localDir string) {
				if err := client.MkdirAll("/dir/nested"); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := client.Symlink("/target", "/dir/nested/link"); err != nil {
					t.Fatalf("symlink: %v", err)
				}
				if err := os.MkdirAll(filepath.Join(localDir, "dir"), 0755); err != nil {
					t.Fatalf("mkdir local: %v", err)
				}
				if err := os.WriteFile(filepath.Join(localDir, "dir", "nested"), []byte("blocker"), 0644); err != nil {
					t.Fatalf("write blocker: %v", err)
				}
			},
			dir:     "/dir",
			wantErr: "create dir",
		},
		{
			name: "file_error_propagates",
			setup: func(t *testing.T, client *sftp.Client, localDir string) {
				setupRemoteFile(t, client, "/dir/nested/file.txt", "content")
				if err := os.MkdirAll(filepath.Join(localDir, "dir"), 0755); err != nil {
					t.Fatalf("mkdir local: %v", err)
				}
				if err := os.WriteFile(filepath.Join(localDir, "dir", "nested"), []byte("blocker"), 0644); err != nil {
					t.Fatalf("write blocker: %v", err)
				}
			},
			dir:     "/dir",
			wantErr: "create dir",
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			client := newInMemSFTP(t, sftp.InMemHandler())
			localDir := t.TempDir()
			tc.setup(t, client, localDir)

			testCtx := ctx
			if tc.ctx != nil {
				testCtx = tc.ctx()
			}

			err := downloadDir(testCtx, client, localDir, tc.dir)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %v, want containing %q", err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("downloadDir: %v", err)
			}

			if tc.check != nil {
				tc.check(t, localDir)
			}
		})
	}
}

func TestDownloadPath(t *testing.T) {
	tt := []struct {
		name    string
		setup   func(t *testing.T, client *sftp.Client, localDir string)
		path    string
		check   func(t *testing.T, localDir string)
		wantErr string
	}{
		{
			name: "file",
			setup: func(t *testing.T, client *sftp.Client, localDir string) {
				setupRemoteFile(t, client, "/file.txt", "content")
			},
			path: "/file.txt",
			check: func(t *testing.T, localDir string) {
				data, err := os.ReadFile(filepath.Join(localDir, "file.txt"))
				if err != nil {
					t.Fatalf("read: %v", err)
				}
				if string(data) != "content" {
					t.Errorf("content = %q, want %q", data, "content")
				}
			},
		},
		{
			name: "directory",
			setup: func(t *testing.T, client *sftp.Client, localDir string) {
				setupRemoteFile(t, client, "/mydir/file.txt", "inside")
			},
			path: "/mydir",
			check: func(t *testing.T, localDir string) {
				data, err := os.ReadFile(filepath.Join(localDir, "mydir", "file.txt"))
				if err != nil {
					t.Fatalf("read: %v", err)
				}
				if string(data) != "inside" {
					t.Errorf("content = %q, want %q", data, "inside")
				}
			},
		},
		{
			name: "symlink",
			setup: func(t *testing.T, client *sftp.Client, localDir string) {
				if err := client.Symlink("/target", "/link"); err != nil {
					t.Fatalf("symlink: %v", err)
				}
			},
			path: "/link",
			check: func(t *testing.T, localDir string) {
				target, err := os.Readlink(filepath.Join(localDir, "link"))
				if err != nil {
					t.Fatalf("readlink: %v", err)
				}
				if target != "/target" {
					t.Errorf("target = %q, want %q", target, "/target")
				}
			},
		},
		{
			name:    "bad_lstat",
			setup:   func(t *testing.T, client *sftp.Client, localDir string) {},
			path:    "/nonexistent",
			wantErr: "lstat",
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			client := newInMemSFTP(t, sftp.InMemHandler())
			localDir := t.TempDir()
			tc.setup(t, client, localDir)

			err := downloadPath(ctx, client, localDir, tc.path)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %v, want containing %q", err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("downloadPath: %v", err)
			}

			if tc.check != nil {
				tc.check(t, localDir)
			}
		})
	}
}

type renameFailingCmd struct {
	sftp.FileCmder
	err error
}

func (c renameFailingCmd) Filecmd(r *sftp.Request) error {
	if r.Method == "Rename" || r.Method == "PosixRename" {
		return c.err
	}
	return c.FileCmder.Filecmd(r)
}

type chmodFailingCmd struct {
	sftp.FileCmder
	err error
}

func (c chmodFailingCmd) Filecmd(r *sftp.Request) error {
	if r.Method == "Setstat" {
		return c.err
	}
	return c.FileCmder.Filecmd(r)
}

type writeFailingWriter struct {
	sftp.FileWriter
	err error
}

func (w writeFailingWriter) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	real, err := w.FileWriter.Filewrite(r)
	if err != nil {
		return nil, err
	}
	return failingWriterAt{real: real, err: w.err}, nil
}

type failingWriterAt struct {
	real io.WriterAt
	err  error
}

func (w failingWriterAt) WriteAt(p []byte, off int64) (int, error) {
	return 0, w.err
}

func (w failingWriterAt) Close() error {
	if c, ok := w.real.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

type createFailingWriter struct {
	err error
}

func (w createFailingWriter) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	return nil, w.err
}

type closeFailingWriter struct {
	sftp.FileWriter
	err error
}

func (w closeFailingWriter) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	real, err := w.FileWriter.Filewrite(r)
	if err != nil {
		return nil, err
	}
	return closeFailingWriterAt{real: real, err: w.err}, nil
}

type closeFailingWriterAt struct {
	real io.WriterAt
	err  error
}

func (w closeFailingWriterAt) WriteAt(p []byte, off int64) (int, error) {
	return w.real.WriteAt(p, off)
}

func (w closeFailingWriterAt) Close() error {
	if c, ok := w.real.(io.Closer); ok {
		c.Close()
	}
	return w.err
}
