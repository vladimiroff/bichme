package bichme

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kevinburke/ssh_config"
)

// makeSSHConfig writes an SSH config file under a temp home dir and returns
// a resolver backed by a UserSettings pointing at that file.
func makeSSHConfig(t *testing.T, content string) SSHConfigResolver {
	t.Helper()
	dir := t.TempDir()
	sshDir := filepath.Join(dir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	settings = ssh_config.UserSettings{IgnoreErrors: true}
	settings.ConfigFinder(func() string { return filepath.Join(dir, ".ssh", "config") })
	t.Cleanup(func() { settings = ssh_config.UserSettings{IgnoreErrors: true} })
	return loadSSHConfig()
}

const (
	sshConfigBasicHost = `
Host myhost
  HostName 10.0.0.1
  Port 2222
  User deploy
`
	sshConfigNoMatch = `
Host myhost
  HostName 10.0.0.1
  Port 2222
`
	sshConfigWildcard = `
Host *.example.com
  User admin
  Port 8022
`
	sshConfigIdentityFile = `
Host myhost
  IdentityFile ~/.ssh/mykey
  IdentityFile /absolute/path/key
`
	sshConfigPort22 = `
Host myhost
  Port 22
`
	sshConfigGlobFallback = `
Host prod-*
  User deployer

Host *
  User default
`
)

func TestSSHConfigResolver(t *testing.T) {
	home, _ := os.UserHomeDir()
	tests := []struct {
		name   string
		config string
		checks map[string]HostConfig
	}{
		{
			name:   "basic host",
			config: sshConfigBasicHost,
			checks: map[string]HostConfig{
				"myhost": {Hostname: "10.0.0.1", Port: 2222, User: "deploy"},
			},
		},
		{
			name:   "no match",
			config: sshConfigNoMatch,
			checks: map[string]HostConfig{
				"other": {},
			},
		},
		{
			name:   "wildcard match",
			config: sshConfigWildcard,
			checks: map[string]HostConfig{
				"web.example.com": {User: "admin", Port: 8022},
				"web.other.com":   {},
			},
		},
		{
			name:   "identity file",
			config: sshConfigIdentityFile,
			checks: map[string]HostConfig{
				"myhost": {IdentityFiles: []string{
					filepath.Join(home, ".ssh", "mykey"),
					"/absolute/path/key",
				}},
			},
		},
		{
			name:   "no identity file",
			config: sshConfigPort22,
			checks: map[string]HostConfig{
				"myhost": {},
			},
		},
		{
			name:   "default port not set",
			config: sshConfigPort22,
			checks: map[string]HostConfig{
				"myhost": {},
			},
		},
		{
			name:   "empty config",
			config: "",
			checks: map[string]HostConfig{
				"baba": {},
			},
		},
		{
			name:   "fallback to glob",
			config: sshConfigGlobFallback,
			checks: map[string]HostConfig{
				"prod-web01": {User: "deployer"},
				"dev-web01":  {User: "default"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolve := makeSSHConfig(t, tt.config)
			for host, want := range tt.checks {
				hc := resolve(host)
				if !reflect.DeepEqual(hc, want) {
					t.Errorf("resolve(%q) = %+v, want %+v", host, hc, want)
				}
			}
		})
	}
}

func TestExpandTilde(t *testing.T) {
	home, _ := os.UserHomeDir()
	tests := []struct {
		path string
		want string
	}{
		{"~", home},
		{"~/foo", filepath.Join(home, "foo")},
		{"~/foo/bar", filepath.Join(home, "foo/bar")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"~other", "~other"}, // ~user not expanded
	}
	for _, tc := range tests {
		got := expandTilde(home, tc.path)
		if got != tc.want {
			t.Errorf("expandTilde(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestParseServer(t *testing.T) {
	tests := []struct {
		entry string
		user  string
		host  string
		port  int
	}{
		{"host", "", "host", 0},
		{"host:2222", "", "host", 2222},
		{"user@host", "user", "host", 0},
		{"user@host:2222", "user", "host", 2222},
		{"admin@192.168.1.1:22", "admin", "192.168.1.1", 22},
	}
	for _, tc := range tests {
		u, h, p := parseServer(tc.entry)
		if u != tc.user || h != tc.host || p != tc.port {
			t.Errorf("parseServer(%q) = (%q, %q, %d), want (%q, %q, %d)",
				tc.entry, u, h, p, tc.user, tc.host, tc.port)
		}
	}
}
