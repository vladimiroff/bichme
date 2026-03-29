package bichme

import (
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kevinburke/ssh_config"
)

// HostConfig holds parsed SSH settings for a single host.
type HostConfig struct {
	Hostname      string
	Port          int
	User          string
	IdentityFiles []string
}

// SSHConfigResolver looks up per-host SSH config settings.
type SSHConfigResolver func(alias string) HostConfig

// settings used by loadSSHConfig. Nothing should ever change this, outside of tests.
var settings = ssh_config.UserSettings{IgnoreErrors: true}

// loadSSHConfig reads ~/.ssh/config and /etc/ssh/ssh_config and returns a
// resolver for per-host SSH settings.
func loadSSHConfig() SSHConfigResolver {
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Debug("Failed to get home directory for SSH config tilde expansion", "error", err)
	}

	return func(alias string) HostConfig {
		var hc HostConfig

		if v, err := settings.GetStrict(alias, "HostName"); err == nil && v != "" {
			hc.Hostname = v
		}

		if v, err := settings.GetStrict(alias, "Port"); err == nil {
			if v != ssh_config.Default("Port") {
				if p, err := strconv.Atoi(v); err == nil {
					hc.Port = p
				}
			}
		}

		if v, err := settings.GetStrict(alias, "User"); err == nil && v != "" {
			hc.User = v
		}

		if files, err := settings.GetAllStrict(alias, "IdentityFile"); err == nil && len(files) > 0 {
			defaultID := ssh_config.Default("IdentityFile")
			if len(files) == 1 && files[0] == defaultID {
				return hc // got the SSH default (~/.ssh/identity)
			}
			for _, f := range files {
				hc.IdentityFiles = append(hc.IdentityFiles, expandTilde(home, f))
			}
		}

		return hc
	}
}

// parseServer splits a server entry into user, host alias, and port.
// The format is [user@]host[:port]. Empty user or zero port means not specified.
func parseServer(entry string) (user, host string, port int) {
	if i := strings.Index(entry, "@"); i >= 0 {
		user = entry[:i]
		entry = entry[i+1:]
	}

	host, portStr, err := net.SplitHostPort(entry)
	if err != nil {
		return user, entry, 0
	}

	port, _ = strconv.Atoi(portStr)
	return user, host, port
}

// expandTilde replaces a leading ~ with the home directory.
func expandTilde(home, path string) string {
	if home == "" || !strings.HasPrefix(path, "~") {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
