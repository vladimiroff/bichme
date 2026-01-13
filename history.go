package bichme

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type HistoryItem struct {
	Path     string
	Time     time.Time
	Duration time.Duration
	Hosts    []string
	Files    []string
	Logs     []string
	Command  string
}

// Read implements io.Reader.
func (hi HistoryItem) Read(p []byte) (n int, err error) {
	var buf bytes.Buffer
	if _, err = hi.WriteTo(&buf); err != nil {
		return 0, err
	}

	n = copy(p, buf.Bytes())
	if n < buf.Len() {
		return n, io.ErrShortBuffer
	}

	return n, io.EOF
}

// WriteTo implements io.WriterTo.
func (hi HistoryItem) WriteTo(w io.Writer) (n int64, err error) {
	t, terr := fmt.Fprintf(w, "Start Time:\t%s\n", hi.Time)
	d, derr := fmt.Fprintf(w, "Duration:\t%s\n", hi.Duration)
	c, cerr := fmt.Fprintf(w, "Command:\t%s\n", hi.Command)
	f, ferr := fmt.Fprintf(w, "Files:\t\t%s\n\n", strings.Join(hi.Files, "\n\t\t"))
	h, herr := fmt.Fprintf(w, "Hosts:\t\t%s\n\n", strings.Join(hi.Hosts, "\n\t\t"))
	l, lerr := fmt.Fprintf(w, "Logs:\t\t%s\n\n", strings.Join(hi.Logs, "\n\t\t"))
	return int64(t + d + f + c + h + l), errors.Join(err, terr, derr, cerr, ferr, herr, lerr)
}

// Delete the underlying state directory.
func (hi HistoryItem) Delete() error { return os.RemoveAll(hi.Path) }

func ListHistory(root string) ([]HistoryItem, error) {
	fsys := os.DirFS(root)
	items := make(map[string]HistoryItem)
	fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Error("Walkdir failed", "error", err)
			return err
		}

		switch strings.Count(path, "/") {
		case 0:
			return nil
		case 1:
			if !d.IsDir() {
				return nil
			}

			t, err := entryTime(path)
			if err != nil {
				slog.Error("Bad history entry", "name", path, "error", err)
				return fs.SkipDir
			}
			items[path] = HistoryItem{Path: filepath.Join(root, path), Time: t}
		case 2:
			if d.IsDir() {
				return nil
			}
			entry, ok := items[entryName(path)]
			if !ok {
				panic(path)
			}
			switch d.Name() {
			case "start":
				f, err := fs.ReadFile(fsys, path)
				if err != nil {
					slog.Error("Failed to read start", "path", path, "error", err)
					return nil
				}
				t, err := time.Parse(time.RFC3339, string(f))
				if err != nil {
					slog.Error("Failed to parse start", "path", path, "content", string(f), "error", err)
				}
				entry.Time = t
			case "command":
				f, err := fs.ReadFile(fsys, path)
				if err != nil {
					slog.Error("Failed to read command", "path", path, "error", err)
					return nil
				}
				entry.Command = strings.TrimSpace(string(f))
			case "files":
				f, err := fs.ReadFile(fsys, path)
				if err != nil {
					slog.Error("Failed to read files", "path", path, "error", err)
					return nil
				}
				files := bytes.Split(f, newline)
				entry.Files = make([]string, len(files))
				for i, file := range files {
					entry.Files[i] = string(file)
				}
			case "hosts":
				f, err := fs.ReadFile(fsys, path)
				if err != nil {
					slog.Error("Failed to read hosts", "path", path, "error", err)
					return nil
				}
				hosts := bytes.Split(f, newline)
				entry.Hosts = make([]string, len(hosts))
				for i, host := range hosts {
					entry.Hosts[i] = strings.SplitN(string(host), ":", 2)[0]
				}
			case "duration":
				f, err := fs.ReadFile(fsys, path)
				if err != nil {
					slog.Error("Failed to read duration", "path", path, "error", err)
					return nil
				}
				d, err := time.ParseDuration(string(f))
				if err != nil {
					slog.Error("Failed to parse duration", "path", path, "content", string(f), "error", err)
				}
				entry.Duration = d.Round(time.Second)
			default:
				if strings.HasSuffix(d.Name(), ".log") {
					entry.Logs = append(entry.Logs, filepath.Join(root, path))
				}
			}
			items[entryName(path)] = entry

		default:
			return fs.SkipDir
		}

		return nil
	})

	sortFunc := func(a, b HistoryItem) int {
		return cmp.Compare(b.Time.UnixMicro(), a.Time.UnixMicro())
	}
	return slices.SortedFunc(maps.Values(items), sortFunc), nil
}

func entryTime(path string) (time.Time, error) {
	parts := strings.SplitN(path, "/", 2)
	timePart := strings.SplitN(parts[1], ".", 2)[0] // strip pid
	return time.Parse("2006-01-02/15-04-05", parts[0]+"/"+timePart)
}

func entryName(path string) string {
	parts := strings.SplitN(path, "/", 3)
	return fmt.Sprintf("%s/%s", parts[0], parts[1])
}
