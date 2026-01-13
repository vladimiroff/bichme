package bichme

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// loadSSHAuth returns SSH auth methods by trying the SSH agent first,
// then identity files inside ~/.ssh/ (just like OpenSSH does).
func loadSSHAuth() []ssh.AuthMethod {
	var signers []ssh.Signer
	signers = append(signers, loadSSHAgent()...)
	signers = append(signers, loadIdentityFiles()...)
	if len(signers) == 0 {
		slog.Warn("No valid SSH signers found")
		return nil
	}
	return []ssh.AuthMethod{ssh.PublicKeys(signers...)}
}

// loadSSHAgent loads signers from the SSH agent via SSH_AUTH_SOCK.
func loadSSHAgent() []ssh.Signer {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		slog.Debug("SSH_AUTH_SOCK not set, skipping agent")
		return nil
	}
	c, err := net.Dial("unix", sock)
	if err != nil {
		slog.Debug("Failed to connect to SSH agent", "socket", sock, "error", err)
		return nil
	}
	ag := agent.NewClient(c)
	// Fetch signers once upfront. Using PublicKeysCallback would call
	// ag.Signers() during each SSH handshake, but agent.Client is not
	// thread-safe for concurrent use across multiple goroutines.
	signers, err := ag.Signers()
	if err != nil {
		slog.Warn("Failed to get signers from SSH agent", "error", err)
		return nil
	}
	return signers
}

// loadIdentityFiles loads private keys from ~/.ssh/ default identity files.
func loadIdentityFiles() []ssh.Signer {
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("Failed to get home directory, skipping identity files", "error", err)
		return nil
	}

	defaultIdentityFiles := [...]string{
		"id_rsa",
		"id_ecdsa",
		"id_ecdsa_sk",
		"id_ed25519",
		"id_ed25519_sk",
	}
	var signers []ssh.Signer
	sshDir := filepath.Join(home, ".ssh")
	for _, name := range defaultIdentityFiles {
		keyPath := filepath.Join(sshDir, name)
		signer, err := loadPrivateKey(keyPath)
		if err != nil {
			slog.Debug("Skip private key", "path", keyPath, "error", err)
			continue
		}
		signers = append(signers, signer)
	}
	return signers
}

// loadPrivateKey loads a private key from a file. Returns an error if the
// file doesn't exist or the key is encrypted (passphrase-protected).
func loadPrivateKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(data)
}

// loadHostKeyCallback returns an SSH host key callback. If insecure is true,
// it returns a callback that accepts any host key. Otherwise, it reads
// ~/.ssh/known_hosts and /etc/ssh/ssh_known_hosts for verification.
func loadHostKeyCallback(insecure bool) (ssh.HostKeyCallback, error) {
	if insecure {
		return ssh.InsecureIgnoreHostKey(), nil
	}

	var files []string
	if home, err := os.UserHomeDir(); err == nil {
		userKnownHosts := filepath.Join(home, ".ssh", "known_hosts")
		if _, err := os.Stat(userKnownHosts); err == nil {
			files = append(files, userKnownHosts)
		}
	}
	systemKnownHosts := "/etc/ssh/ssh_known_hosts"
	if _, err := os.Stat(systemKnownHosts); err == nil {
		files = append(files, systemKnownHosts)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no ssh known_hosts files found")
	}

	return knownhosts.New(files...)
}
