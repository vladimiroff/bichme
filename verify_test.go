package bichme

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skeema/knownhosts"
	"golang.org/x/crypto/ssh"
)

// fakeAddr is a minimal net.Addr for tests.
type fakeAddr struct{ s string }

func (a fakeAddr) Network() string { return "tcp" }
func (a fakeAddr) String() string  { return a.s }

// makeKnownHostsFile writes a known_hosts file containing the given public key
// for the given host and returns the file path.
func makeKnownHostsFile(t *testing.T, host string, key ssh.PublicKey) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	addr := fakeAddr{host}
	if err := knownhosts.WriteKnownHost(f, host, addr, key); err != nil {
		t.Fatal(err)
	}
	return path
}

// emptyKnownHostsCallback returns a knownhosts callback backed by an empty file.
func emptyKnownHostsCallback(t *testing.T) ssh.HostKeyCallback {
	t.Helper()
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := knownhosts.NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	return db.HostKeyCallback()
}

func TestKeyCollectorCallback(t *testing.T) {
	const host = "example.com:22"
	remote := fakeAddr{host}

	tt := []struct {
		name        string
		base        func() ssh.HostKeyCallback
		presentKey  string // key type to present to the callback
		wantErr     bool
		wantChanged bool // subset of wantErr: error must be IsHostKeyChanged
		wantPending int
	}{
		{
			name:        "unknown host",
			base:        func() ssh.HostKeyCallback { return emptyKnownHostsCallback(t) },
			presentKey:  "ed25519",
			wantErr:     false,
			wantPending: 1,
		},
		{
			name: "known host matching key",
			base: func() ssh.HostKeyCallback {
				db, err := knownhosts.NewDB(makeKnownHostsFile(t, host, testPublicKeys["ed25519"]))
				if err != nil {
					t.Fatal(err)
				}
				return db.HostKeyCallback()
			},
			presentKey:  "ed25519",
			wantErr:     false,
			wantPending: 0,
		},
		{
			name: "changed key",
			base: func() ssh.HostKeyCallback {
				db, err := knownhosts.NewDB(makeKnownHostsFile(t, host, testPublicKeys["rsa"]))
				if err != nil {
					t.Fatal(err)
				}
				return db.HostKeyCallback()
			},
			presentKey:  "ed25519",
			wantErr:     true,
			wantChanged: true,
			wantPending: 0,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			c := &KeyCollector{}
			wrapped := c.Callback(tc.base())

			err := wrapped(host, remote, testPublicKeys[tc.presentKey])

			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if tc.wantChanged && !knownhosts.IsHostKeyChanged(err) {
				t.Errorf("expected IsHostKeyChanged error, got: %v", err)
			}
			if got := len(c.Keys()); got != tc.wantPending {
				t.Errorf("pending keys = %d, want %d", got, tc.wantPending)
			}
		})
	}
}

func TestKeyCollectorCallback_Accumulates(t *testing.T) {
	c := &KeyCollector{}
	wrapped := c.Callback(emptyKnownHostsCallback(t))

	hosts := []string{"a.example.com:22", "b.example.com:22", "c.example.com:22"}
	for _, h := range hosts {
		if err := wrapped(h, fakeAddr{h}, testPublicKeys["ed25519"]); err != nil {
			t.Errorf("host %s: unexpected error: %v", h, err)
		}
	}

	if got := len(c.Keys()); got != len(hosts) {
		t.Errorf("pending keys = %d, want %d", got, len(hosts))
	}
}

func TestPrompt(t *testing.T) {
	oneKey := []PendingKey{
		{Host: "web1.example.com:22", Remote: fakeAddr{"web1.example.com:22"}, Key: testPublicKeys["ed25519"]},
	}
	twoKeys := []PendingKey{
		{Host: "web1.example.com:22", Remote: fakeAddr{"web1.example.com:22"}, Key: testPublicKeys["ed25519"]},
		{Host: "web2.example.com:22", Remote: fakeAddr{"web2.example.com:22"}, Key: testPublicKeys["rsa"]},
	}

	tt := []struct {
		name       string
		input      string
		keys       []PendingKey
		wantOK     bool
		wantOutput []string // substrings that must appear in the output
	}{
		{name: "yes", input: "y\n", keys: oneKey, wantOK: true},
		{name: "no", input: "n\n", keys: oneKey, wantOK: false},
		{name: "EOF", input: "", keys: oneKey, wantOK: false},
		{name: "Y uppercase", input: "Y\n", keys: oneKey, wantOK: true},
		{name: "yes full", input: "yes\n", keys: oneKey, wantOK: true},
		{name: "YES full uppercase", input: "YES\n", keys: oneKey, wantOK: true},
		{
			name:       "output format",
			input:      "n\n",
			keys:       twoKeys,
			wantOK:     false,
			wantOutput: []string{"web1.example.com:22", "web2.example.com:22", "SHA256:"},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			ok, err := Prompt(tc.keys, &out, strings.NewReader(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			if ok != tc.wantOK {
				t.Errorf("ok = %v, want %v", ok, tc.wantOK)
			}
			for _, s := range tc.wantOutput {
				if !strings.Contains(out.String(), s) {
					t.Errorf("output missing %q", s)
				}
			}
		})
	}
}

func TestWriteToKnownHosts(t *testing.T) {
	tt := []struct {
		name      string
		run       func(t *testing.T, tmpHome string)
		assertion func(t *testing.T, khPath string)
	}{
		{
			name: "writes keys and is parseable",
			run: func(t *testing.T, tmpHome string) {
				t.Helper()
				keys := []PendingKey{
					{Host: "web1.example.com:22", Remote: fakeAddr{"web1.example.com:22"}, Key: testPublicKeys["ed25519"]},
					{Host: "web2.example.com:22", Remote: fakeAddr{"web2.example.com:22"}, Key: testPublicKeys["rsa"]},
				}
				if err := WriteToKnownHosts(keys); err != nil {
					t.Fatal(err)
				}
			},
			assertion: func(t *testing.T, khPath string) {
				t.Helper()
				db, err := knownhosts.NewDB(khPath)
				if err != nil {
					t.Fatalf("written known_hosts is not parseable: %v", err)
				}
				cb := db.HostKeyCallback()
				if err := cb("web1.example.com:22", fakeAddr{"web1.example.com:22"}, testPublicKeys["ed25519"]); err != nil {
					t.Errorf("web1 key not recognized after write: %v", err)
				}
				if err := cb("web2.example.com:22", fakeAddr{"web2.example.com:22"}, testPublicKeys["rsa"]); err != nil {
					t.Errorf("web2 key not recognized after write: %v", err)
				}
			},
		},
		{
			name: "creates .ssh directory if missing",
			run: func(t *testing.T, tmpHome string) {
				t.Helper()
				if err := WriteToKnownHosts([]PendingKey{
					{Host: "new.example.com:22", Remote: fakeAddr{"new.example.com:22"}, Key: testPublicKeys["ed25519"]},
				}); err != nil {
					t.Fatal(err)
				}
			},
			assertion: func(t *testing.T, khPath string) {
				t.Helper()
				info, err := os.Stat(filepath.Dir(khPath))
				if err != nil {
					t.Fatalf(".ssh directory not created: %v", err)
				}
				if !info.IsDir() {
					t.Error(".ssh is not a directory")
				}
			},
		},
		{
			name: "appends on repeated calls",
			run: func(t *testing.T, tmpHome string) {
				t.Helper()
				if err := WriteToKnownHosts([]PendingKey{
					{Host: "host1.example.com:22", Remote: fakeAddr{"host1.example.com:22"}, Key: testPublicKeys["ed25519"]},
				}); err != nil {
					t.Fatal(err)
				}
				if err := WriteToKnownHosts([]PendingKey{
					{Host: "host2.example.com:22", Remote: fakeAddr{"host2.example.com:22"}, Key: testPublicKeys["rsa"]},
				}); err != nil {
					t.Fatal(err)
				}
			},
			assertion: func(t *testing.T, khPath string) {
				t.Helper()
				f, err := os.Open(khPath)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				var lines []string
				scanner := bufio.NewScanner(f)
				for scanner.Scan() {
					if line := scanner.Text(); strings.TrimSpace(line) != "" {
						lines = append(lines, line)
					}
				}
				if len(lines) != 2 {
					t.Errorf("expected 2 lines in known_hosts, got %d", len(lines))
				}
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			tmpHome := t.TempDir()
			t.Setenv("HOME", tmpHome)
			tc.run(t, tmpHome)
			tc.assertion(t, filepath.Join(tmpHome, ".ssh", "known_hosts"))
		})
	}
}

func TestFingerprint(t *testing.T) {
	fp := fingerprint(testPublicKeys["ed25519"])
	if !strings.HasPrefix(fp, "SHA256:") {
		t.Errorf("fingerprint %q does not start with SHA256:", fp)
	}
	if fp2 := fingerprint(testPublicKeys["ed25519"]); fp != fp2 {
		t.Error("fingerprint is not deterministic")
	}
}

// TestKeyCollectorConcurrency ensures KeyCollector is safe for concurrent use.
func TestKeyCollectorConcurrency(t *testing.T) {
	c := &KeyCollector{}
	wrapped := c.Callback(emptyKnownHostsCallback(t))

	const n = 50
	done := make(chan struct{})
	for i := range n {
		go func(i int) {
			host := net.JoinHostPort(fmt.Sprintf("10.0.%d.%d", i/256, i%256), "22")
			wrapped(host, fakeAddr{host}, testPublicKeys["ed25519"]) //nolint:errcheck
			done <- struct{}{}
		}(i)
	}
	for range n {
		<-done
	}

	if len(c.Keys()) != n {
		t.Errorf("expected %d keys, got %d", n, len(c.Keys()))
	}
}
