package bichme

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
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
