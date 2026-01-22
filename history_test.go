package bichme

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestHistoryItemWriteTo(t *testing.T) {
	tt := []struct {
		name string
		item HistoryItem
		want []string
	}{
		{"empty", HistoryItem{}, []string{"Start Time:", "Duration:", "Command:", "Files:", "Succeeded", "Failed", "Logs:"}},
		{"full", HistoryItem{
			Path:     "/some/path",
			Time:     time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			Duration: 5 * time.Minute,
			Hosts: map[string]HostResult{
				"host1": {Tries: 1, Duration: 10 * time.Second},
				"host2": {Error: "connection", Tries: 3, Duration: 30 * time.Second},
			},
			Files:   []string{"file1.sh", "file2.sh"},
			Logs:    []string{"/path/to/log1.log", "/path/to/log2.log"},
			Command: "uptime",
		}, []string{
			"Start Time:\t2025-01-15 10:30:00 +0000 UTC",
			"Duration:\t5m0s",
			"Command:\tuptime",
			"Files:",
			"file1.sh", "file2.sh", "log1.log", "log2.log",
			"Succeeded (1):",
			"host1:\t1 tries in 10s",
			"Failed (1):",
			"host2:\tConnection Failed in 30s",
		}},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			n, err := tc.item.WriteTo(&buf)
			if err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if n != int64(buf.Len()) {
				t.Errorf("n = %d, buf.Len() = %d", n, buf.Len())
			}
			for _, want := range tc.want {
				if !strings.Contains(buf.String(), want) {
					t.Errorf("missing %q in output", want)
					t.Logf("output:\n%s", buf.String())
				}
			}
		})
	}
}

func TestHistoryItemRead(t *testing.T) {
	tt := []struct {
		name    string
		bufSize int
		err     error
	}{
		{"full_buffer", 4096, io.EOF},
		{"short_buffer", 10, io.ErrShortBuffer},
	}

	item := HistoryItem{
		Time:    time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		Command: "some-long-command-that-requires-space",
		Hosts: map[string]HostResult{
			"host1": {Tries: 1},
			"host2": {Tries: 1},
			"host3": {Tries: 1},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			buf := make([]byte, tc.bufSize)
			n, err := item.Read(buf)
			if err != tc.err {
				t.Errorf("err = %v, want %v", err, tc.err)
			}
			if n == 0 {
				t.Error("expected bytes read")
			}
		})
	}
}

func TestHistoryItemDelete(t *testing.T) {
	t.Run("exists", func(t *testing.T) {
		path := makeHistoryEntry(t, t.TempDir(), "2025-01-15", "10-30-00")
		if err := (HistoryItem{Path: path}).Delete(); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("directory should be deleted")
		}
	})

	t.Run("not_exists", func(t *testing.T) {
		if err := (HistoryItem{Path: "/nonexistent"}).Delete(); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	})
}

func TestListHistory(t *testing.T) {
	discardLogs(t)

	t.Run("empty", func(t *testing.T) {
		items, _ := ListHistory(t.TempDir())
		if len(items) != 0 {
			t.Errorf("got %d items, want 0", len(items))
		}
	})

	t.Run("missing", func(t *testing.T) {
		ListHistory("/nonexistent/path")
	})

	t.Run("full_entry", func(t *testing.T) {
		dir := t.TempDir()
		entry := makeHistoryEntry(t, dir, "2025-01-15", "10-30-00")
		writeTestFile(t, filepath.Join(entry, "start"), "2025-01-15T10:30:00Z")
		writeTestFile(t, filepath.Join(entry, "command"), "uptime")
		writeTestFile(t, filepath.Join(entry, "duration"), "5m0s")
		writeTestFile(t, filepath.Join(entry, "hosts.json"), `{
			"host1": {"tries": 1},
			"host2": {"error": "connection", "tries": 3}
		}`)
		writeTestFile(t, filepath.Join(entry, "files"), "script.sh\ndata.txt")
		writeTestFile(t, filepath.Join(entry, "host1.log"), "output")
		writeTestFile(t, filepath.Join(entry, "host2.log"), "output")

		items, _ := ListHistory(dir)
		if len(items) != 1 {
			t.Fatalf("got %d items, want 1", len(items))
		}

		item := items[0]
		if item.Command != "uptime" {
			t.Errorf("command = %q, want uptime", item.Command)
		}
		if item.Duration != 5*time.Minute {
			t.Errorf("duration = %v, want 5m0s", item.Duration)
		}
		if len(item.Hosts) != 2 {
			t.Errorf("hosts count = %d, want 2", len(item.Hosts))
		}
		if r, ok := item.Hosts["host1"]; !ok || r.Error != "" {
			t.Errorf("host1 result = %v", r)
		}
		if r, ok := item.Hosts["host2"]; !ok || r.Error != "connection" || r.Tries != 3 {
			t.Errorf("host2 result = %v", r)
		}
		if !slices.Equal(item.Files, []string{"script.sh", "data.txt"}) {
			t.Errorf("files = %v", item.Files)
		}
		if len(item.Logs) != 2 {
			t.Errorf("logs count = %d, want 2", len(item.Logs))
		}
		if item.Path != entry {
			t.Errorf("path = %q, want %q", item.Path, entry)
		}
	})

	t.Run("sort_desc", func(t *testing.T) {
		dir := t.TempDir()
		entries := []struct{ date, tm, start, cmd string }{
			{"2025-01-10", "08-00-00", "2025-01-10T08:00:00Z", "first"},
			{"2025-01-15", "10-30-00", "2025-01-15T10:30:00Z", "third"},
			{"2025-01-12", "12-00-00", "2025-01-12T12:00:00Z", "second"},
		}
		for _, e := range entries {
			entry := makeHistoryEntry(t, dir, e.date, e.tm)
			writeTestFile(t, filepath.Join(entry, "start"), e.start)
			writeTestFile(t, filepath.Join(entry, "command"), e.cmd)
		}

		items, _ := ListHistory(dir)
		want := []string{"third", "second", "first"}
		for i, item := range items {
			if item.Command != want[i] {
				t.Errorf("items[%d].Command = %q, want %q", i, item.Command, want[i])
			}
		}
	})

	t.Run("invalid", func(t *testing.T) {
		dir := t.TempDir()
		makeHistoryEntry(t, dir, "2025-01-15", "10-30-00")
		makeHistoryEntry(t, dir, "invalid-date", "time")

		items, _ := ListHistory(dir)
		if len(items) != 1 {
			t.Errorf("got %d items, want 1", len(items))
		}
	})

	t.Run("nested_file", func(t *testing.T) {
		dir := t.TempDir()
		makeHistoryEntry(t, dir, "2025-01-15", "10-30-00")
		writeTestFile(t, filepath.Join(dir, "2025-01-15", "random.txt"), "ignored")

		items, _ := ListHistory(dir)
		if len(items) != 1 {
			t.Errorf("got %d items, want 1", len(items))
		}
	})

	t.Run("nested_dir", func(t *testing.T) {
		dir := t.TempDir()
		entry := makeHistoryEntry(t, dir, "2025-01-15", "10-30-00")
		os.MkdirAll(filepath.Join(entry, "nested"), 0755)

		items, _ := ListHistory(dir)
		if len(items) != 1 {
			t.Errorf("got %d items, want 1", len(items))
		}
	})

	t.Run("deep_nesting_skipped", func(t *testing.T) {
		dir := t.TempDir()
		entry := makeHistoryEntry(t, dir, "2025-01-15", "10-30-00")
		deep := filepath.Join(entry, "deep", "nested")
		os.MkdirAll(deep, 0755)
		writeTestFile(t, filepath.Join(deep, "ignored.log"), "data")

		items, _ := ListHistory(dir)
		for _, log := range items[0].Logs {
			if strings.Contains(log, "ignored.log") {
				t.Error("deep log should not be included")
			}
		}
	})

	t.Run("random_file", func(t *testing.T) {
		dir := t.TempDir()
		entry := makeHistoryEntry(t, dir, "2025-01-15", "10-30-00")
		writeTestFile(t, filepath.Join(entry, "random.txt"), "ignored")

		items, _ := ListHistory(dir)
		if len(items[0].Logs) != 0 {
			t.Errorf("logs = %v, want empty", items[0].Logs)
		}
	})

	t.Run("invalid_start", func(t *testing.T) {
		dir := t.TempDir()
		entry := makeHistoryEntry(t, dir, "2025-01-15", "10-30-00")
		writeTestFile(t, filepath.Join(entry, "start"), "invalid")

		items, _ := ListHistory(dir)
		if len(items) != 1 {
			t.Fatalf("got %d items, want 1", len(items))
		}
	})

	t.Run("invalid_duration", func(t *testing.T) {
		dir := t.TempDir()
		entry := makeHistoryEntry(t, dir, "2025-01-15", "10-30-00")
		writeTestFile(t, filepath.Join(entry, "duration"), "invalid")

		items, _ := ListHistory(dir)
		if items[0].Duration != 0 {
			t.Errorf("duration = %v, want 0", items[0].Duration)
		}
	})

	t.Run("duration_rounding", func(t *testing.T) {
		dir := t.TempDir()
		entry := makeHistoryEntry(t, dir, "2025-01-15", "10-30-00")
		writeTestFile(t, filepath.Join(entry, "duration"), "5m30s500ms")

		items, _ := ListHistory(dir)
		want := 5*time.Minute + 31*time.Second
		if items[0].Duration != want {
			t.Errorf("duration = %v, want %v", items[0].Duration, want)
		}
	})

	for _, f := range []string{"start", "command", "files", "hosts.json", "duration"} {
		t.Run("unreadable_"+f, func(t *testing.T) {
			dir := t.TempDir()
			entry := makeHistoryEntry(t, dir, "2025-01-15", "10-30-00")
			fpath := filepath.Join(entry, f)
			writeTestFile(t, fpath, "content")
			os.Chmod(fpath, 0000)
			t.Cleanup(func() { os.Chmod(fpath, 0644) })

			items, err := ListHistory(dir)
			if err != nil {
				t.Fatalf("ListHistory: %v", err)
			}
			if len(items) != 1 {
				t.Errorf("got %d items, want 1", len(items))
			}
		})
	}
}

func TestEntryTime(t *testing.T) {
	tt := []struct {
		path string
		want time.Time
		err  bool
	}{
		{"2025-01-15/10-30-00", time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC), false},
		{"2025-01-15/10-30-00.12345", time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC), false},
		{"not-a-date/10-30-00", time.Time{}, true},
		{"2025-01-15/not-a-time", time.Time{}, true},
	}

	for _, tc := range tt {
		t.Run(tc.path, func(t *testing.T) {
			got, err := entryTime(tc.path)
			if tc.err && err == nil {
				t.Error("expected error")
			}
			if !tc.err && !got.Equal(tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEntryName(t *testing.T) {
	tt := []struct{ path, want string }{
		{"2025-01-15/10-30-00/command", "2025-01-15/10-30-00"},
		{"2025-01-15/10-30-00.12345/host.log", "2025-01-15/10-30-00.12345"},
		{"date/time/file", "date/time"},
	}

	for _, tc := range tt {
		t.Run(tc.path, func(t *testing.T) {
			if got := entryName(tc.path); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWriteHostsJSON(t *testing.T) {
	root := t.TempDir()
	entry := makeHistoryEntry(t, root, "2025-01-15", "10-30-00")

	archive := map[*Job]error{
		{host: "host1.example.com", tries: 1, duration: 10 * time.Second}: nil,
		{host: "host2.example.com", tries: 3, duration: 30 * time.Second}: ErrConnection,
		{host: "host3.example.com", tries: 2, duration: 20 * time.Second}: ErrFileTransfer,
		{host: "host4.example.com", tries: 1, duration: 5 * time.Second}:  ErrExecution,
	}

	if err := writeHostsJSON(entry, archive); err != nil {
		t.Fatalf("writeHostsJSON: %v", err)
	}

	// Read and parse the written file
	items, err := ListHistory(root)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}

	hosts := items[0].Hosts
	if len(hosts) != 4 {
		t.Fatalf("got %d hosts, want 4", len(hosts))
	}

	tests := []struct {
		host     string
		err      string
		tries    int
		duration time.Duration
	}{
		{"host1.example.com", "", 1, 10 * time.Second},
		{"host2.example.com", "connection", 3, 30 * time.Second},
		{"host3.example.com", "transfer", 2, 20 * time.Second},
		{"host4.example.com", "execution", 1, 5 * time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.host, func(t *testing.T) {
			r, ok := hosts[tc.host]
			if !ok {
				t.Fatalf("host %q not found", tc.host)
			}
			if r.Error != tc.err {
				t.Errorf("error = %q, want %q", r.Error, tc.err)
			}
			if r.Tries != tc.tries {
				t.Errorf("tries = %d, want %d", r.Tries, tc.tries)
			}
			if r.Duration != tc.duration {
				t.Errorf("duration = %v, want %v", r.Duration, tc.duration)
			}
		})
	}
}

func TestListHistoryInvalidHostsJSON(t *testing.T) {
	discardLogs(t)

	dir := t.TempDir()
	entry := makeHistoryEntry(t, dir, "2025-01-15", "10-30-00")
	writeTestFile(t, filepath.Join(entry, "hosts.json"), "invalid json")

	items, err := ListHistory(dir)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	// Hosts should be nil/empty when parsing fails
	if len(items[0].Hosts) != 0 {
		t.Errorf("hosts = %v, want empty", items[0].Hosts)
	}
}
