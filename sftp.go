package bichme

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pkg/sftp"
)

// Upload files via active ssh client to given directory.
func Upload(c *sftp.Client, dir string, files ...string) error {
	if err := c.MkdirAll(dir); err != nil {
		return fmt.Errorf("create upload dir: %w", err)
	}

	if err := c.Chmod(dir, 0700); err != nil {
		return fmt.Errorf("chmod 0700 %q: %w", dir, err)
	}

	for _, file := range files {
		local, err := os.Open(file)
		if err != nil {
			return fmt.Errorf("open %q: %w", file, err)
		}
		defer local.Close()

		filename := filepath.Join(dir, filepath.Base(file))
		remote, err := c.Create(filename)
		if err != nil {
			return fmt.Errorf("create %q: %w", file, err)
		}
		defer remote.Close()

		if err := c.Chmod(filename, 0600); err != nil {
			return fmt.Errorf("chmod 0600 %q: %w", filename, err)
		}

		if _, err := io.Copy(remote, local); err != nil {
			return fmt.Errorf("copy %q: %w", file, err)
		}
	}

	return nil
}

// MakeExec makes a file executable.
func MakeExec(c *sftp.Client, filename string) error {
	if err := c.Chmod(filename, 0700); err != nil {
		return fmt.Errorf("chmod 0700 %q: %w", filename, err)
	}

	return nil
}
