package bichme

import (
	"bytes"
	"cmp"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"
	"time"
)

type HistoryItem struct {
	Time    time.Time
	Servers []string
	Command string
}

var slash = string(os.PathSeparator)

func ListHistory(fsys fs.FS) ([]HistoryItem, error) {
	items := make(map[string]HistoryItem)
	fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Error("Walkdir failed", "error", err)
			return err
		}

		switch strings.Count(path, slash) {
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
			items[path] = HistoryItem{Time: t}
		case 2:
			if d.IsDir() {
				return nil
			}
			entry, ok := items[entryName(path)]
			if !ok {
				panic(path)
			}
			switch d.Name() {
			case "servers":
				c, err := fs.ReadFile(fsys, path)
				if err != nil {
					slog.Error("Failed to read servers", "path", path, "error", err)
				}
				servers := bytes.Split(c, newline)
				entry.Servers = make([]string, len(servers))
				for i, server := range servers {
					entry.Servers[i] = strings.SplitN(string(server), ":", 2)[0]
				}
			case "cmd":
				c, err := fs.ReadFile(fsys, path)
				if err != nil {
					slog.Error("Failed to read servers", "path", path, "error", err)
				}
				entry.Command = string(c)
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
	parts := strings.SplitN(path, slash, 2)
	return time.Parse(time.RFC3339, fmt.Sprintf("%sT%sZ", parts[0], parts[1]))
}

func entryName(path string) string {
	parts := strings.SplitN(path, slash, 3)
	return fmt.Sprintf("%s/%s", parts[0], parts[1])
}
