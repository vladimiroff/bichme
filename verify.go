package bichme

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/skeema/knownhosts"
	"golang.org/x/crypto/ssh"
)

// PendingKey holds an unknown host key encountered during an SSH dial.
type PendingKey struct {
	Host   string // "host:port"
	Remote net.Addr
	Key    ssh.PublicKey
}

// preferredHostKeyAlgos is the host key algorithm preference order used when
// connecting to an unknown host. It mirrors OpenSSH's default order so that
// bichme and ssh(1) negotiate the same key type and show consistent fingerprints.
var preferredHostKeyAlgos = []string{
	ssh.KeyAlgoED25519,
	ssh.KeyAlgoECDSA256,
	ssh.KeyAlgoECDSA384,
	ssh.KeyAlgoECDSA521,
	ssh.KeyAlgoRSASHA256,
	ssh.KeyAlgoRSASHA512,
}

// fingerprint returns the SHA256 fingerprint of the key, matching OpenSSH's
// display format (e.g. "SHA256:abc123...").
func fingerprint(key ssh.PublicKey) string {
	sum := sha256.Sum256(key.Marshal())
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// KeyCollector accumulates unknown host keys encountered during SSH dials.
// It is safe for concurrent use.
type KeyCollector struct {
	mu      sync.Mutex
	pending []PendingKey
}

// Callback wraps an existing ssh.HostKeyCallback. The returned callback:
//   - passes through nil (key is known and matches)
//   - records unknown hosts in the collector and returns nil so the dial succeeds
//   - returns the error unchanged for changed keys (potential MITM)
func (c *KeyCollector) Callback(base ssh.HostKeyCallback) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := base(hostname, remote, key)
		if err == nil {
			return nil
		}
		if knownhosts.IsHostKeyChanged(err) {
			return err
		}
		if knownhosts.IsHostUnknown(err) {
			c.mu.Lock()
			c.pending = append(c.pending, PendingKey{Host: hostname, Remote: remote, Key: key})
			c.mu.Unlock()
			return nil
		}
		return err
	}
}

// Keys returns all collected unknown-host keys. Safe to call after Run returns.
func (c *KeyCollector) Keys() []PendingKey {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pending
}

// Prompt prints the pending keys to w and asks the user to confirm via r.
// Returns true if the user accepted.
func Prompt(keys []PendingKey, w io.Writer, r io.Reader) (bool, error) {
	fmt.Fprintf(w, "\n%d unknown host key(s) were encountered:\n\n", len(keys))
	for _, k := range keys {
		fmt.Fprintf(w, "  %-40s  %-10s  %s\n", k.Host, k.Key.Type(), fingerprint(k.Key))
	}
	fmt.Fprintf(w, "\nAdd these keys to ~/.ssh/known_hosts? [y/N]: ")

	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return false, err
		}
		// EOF — treat as "no"
		fmt.Fprintln(w)
		return false, nil
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return answer == "y" || answer == "yes", nil
}

// WriteToKnownHosts appends keys to ~/.ssh/known_hosts, creating the file
// (and ~/.ssh/ directory) if they do not yet exist.
func WriteToKnownHosts(keys []PendingKey) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home directory: %w", err)
	}

	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return fmt.Errorf("create ~/.ssh: %w", err)
	}

	khPath := filepath.Join(sshDir, "known_hosts")
	f, err := os.OpenFile(khPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open known_hosts: %w", err)
	}
	defer f.Close()

	for _, k := range keys {
		if err := knownhosts.WriteKnownHost(f, k.Host, k.Remote, k.Key); err != nil {
			return fmt.Errorf("write key for %s: %w", k.Host, err)
		}
	}
	return nil
}
