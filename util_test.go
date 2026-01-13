package bichme

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pkg/sftp"
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

func discardLogs(t *testing.T) {
	t.Helper()
	orig := slog.Default()
	t.Cleanup(func() { slog.SetDefault(orig) })
	slog.SetDefault(slog.New(slog.DiscardHandler))
}

func makeHistoryEntry(t *testing.T, root, date, tm string) string {
	t.Helper()
	path := filepath.Join(root, date, tm)
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func readLines(r io.Reader) []string {
	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
}

func newTestOpts() *Opts {
	return &Opts{Port: 22, ExecTimeout: time.Second}
}

func dialAndSFTP(t *testing.T, j *Job) {
	t.Helper()
	if err := j.Dial(ctx); err != nil {
		t.Fatal(err)
	}
	var err error
	j.sftp, err = sftp.NewClient(j.ssh)
	if err != nil {
		t.Fatal(err)
	}
}

func cancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	return ctx
}
