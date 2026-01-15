package bichme

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"

	"github.com/pkg/sftp"
)

// upload files via active ssh client to given directory.
func upload(ctx context.Context, c *sftp.Client, dir string, files ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if dir != "" {
		if err := c.MkdirAll(dir); err != nil {
			return fmt.Errorf("create upload dir: %w", err)
		}
	}

	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := uploadFile(c, dir, file); err != nil {
			return err
		}
	}

	return nil
}

func uploadFile(c *sftp.Client, dir, file string) (err error) {
	local, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("open %q: %w", file, err)
	}
	defer local.Close()

	filename := path.Join(dir, filepath.Base(file))
	tempname := fmt.Sprintf("%s_%s.tmp", filename, randHex(32))
	temp, err := c.Create(tempname)
	if err != nil {
		return fmt.Errorf("create %q: %w", tempname, err)
	}
	defer func() {
		if err != nil {
			temp.Close()
			c.Remove(tempname)
		}
	}()

	if err = c.Chmod(tempname, 0600); err != nil {
		return fmt.Errorf("chmod 0600 %q: %w", tempname, err)
	}

	if _, err = io.Copy(temp, local); err != nil {
		return fmt.Errorf("copy %q: %w", file, err)
	}

	if err = temp.Close(); err != nil {
		return fmt.Errorf("close %q: %w", tempname, err)
	}

	if err = c.PosixRename(tempname, filename); err != nil {
		return fmt.Errorf("rename %q to %q: %w", tempname, filename, err)
	}

	return nil
}

// makeExec makes a file executable.
func makeExec(ctx context.Context, c *sftp.Client, filename string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := c.Chmod(filename, 0700); err != nil {
		return fmt.Errorf("chmod 0700 %q: %w", filename, err)
	}

	return nil
}

func sftpIsAlive(c *sftp.Client) bool {
	var err error
	defer func() {
		if err != nil {
			slog.Debug("sftpsAlive failed", "error", err)
		}
	}()

	_, err = c.Getwd()
	return err == nil
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
