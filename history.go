package bichme

import (
	"bytes"
	"cmp"
	"encoding/json"
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
	Hosts    map[string]HostResult
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

// Summary returns counts of succeeded and failed hosts.
func (hi HistoryItem) Summary() (succeeded, failed int) {
	for _, result := range hi.Hosts {
		if result.Error == "" {
			succeeded++
		} else {
			failed++
		}
	}
	return succeeded, failed
}

// statusString returns a human-readable status for a host result.
func statusString(r HostResult) string {
	switch r.Error {
	case "":
		return "OK"
	case "connection":
		return "Connection Failed"
	case "transfer":
		return "Transfer Failed"
	case "execution":
		return "Execution Failed"
	default:
		return "Failed"
	}
}

// WriteTo implements io.WriterTo.
func (hi HistoryItem) WriteTo(w io.Writer) (n int64, err error) {
	t, terr := fmt.Fprintf(w, "Start Time:\t%s\n", hi.Time)
	d, derr := fmt.Fprintf(w, "Duration:\t%s\n", hi.Duration)
	c, cerr := fmt.Fprintf(w, "Command:\t%s\n", hi.Command)
	f, ferr := fmt.Fprintf(w, "Files:\t\t%s\n\n", strings.Join(hi.Files, "\n\t\t"))

	hosts := slices.Sorted(maps.Keys(hi.Hosts))
	okLines := make([]string, 0, len(hosts))
	errLines := make([]string, 0, len(hosts))
	for _, host := range hosts {
		r := hi.Hosts[host]
		if r.Error == "" {
			okLines = append(okLines, fmt.Sprintf("%s:\t%d tries in %s",
				host, r.Tries, r.Duration.Round(time.Second)))
		} else {
			errLines = append(errLines, fmt.Sprintf("%s:\t%s in %s",
				host, statusString(r), r.Duration.Round(time.Second)))
		}
	}

	s, serr := fmt.Fprintf(w, "Succeeded (%d):\n\t\t%s\n\n", len(okLines), strings.Join(okLines, "\n\t\t"))
	e, eerr := fmt.Fprintf(w, "Failed (%d):\n\t\t%s\n\n", len(errLines), strings.Join(errLines, "\n\t\t"))
	l, lerr := fmt.Fprintf(w, "Logs:\t\t%s\n\n", strings.Join(hi.Logs, "\n\t\t"))
	return int64(t + d + f + c + s + e + l), errors.Join(err, terr, derr, cerr, ferr, serr, eerr, lerr)
}

// Delete the underlying state directory.
func (hi HistoryItem) Delete() error { return os.RemoveAll(hi.Path) }

func ListHistory(root string) ([]HistoryItem, error) {
	fsys := os.DirFS(root)
	items := make(map[string]HistoryItem)
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
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
				return fs.SkipDir
			}
			entry, ok := items[entryName(path)]
			if !ok {
				slog.Error("Unexpected file in history", "path", path)
				return nil
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
			case "hosts.json":
				f, err := fs.ReadFile(fsys, path)
				if err != nil {
					slog.Error("Failed to read hosts.json", "path", path, "error", err)
					return nil
				}
				var hosts map[string]HostResult
				if err := json.Unmarshal(f, &hosts); err != nil {
					slog.Error("Failed to parse hosts.json", "path", path, "error", err)
					return nil
				}
				entry.Hosts = hosts
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
	if err != nil {
		err = fmt.Errorf("walking history directory: %w", err)
	}
	sortFunc := func(a, b HistoryItem) int {
		return cmp.Compare(b.Time.UnixMicro(), a.Time.UnixMicro())
	}
	return slices.SortedFunc(maps.Values(items), sortFunc), err
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
