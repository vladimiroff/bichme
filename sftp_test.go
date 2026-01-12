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

		ctx, cancel := context.WithCancel(ctx)
		cancel()

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
