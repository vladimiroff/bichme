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

	info, err := local.Stat()
	if err != nil {
		return fmt.Errorf("stat %q: %w", file, err)
	}

	filename := path.Join(dir, filepath.Base(file))
	tempname := fmt.Sprintf("%s_%s.tmp", filename, randHex(32))
	temp, err := c.Create(tempname)
	if err != nil {
		return fmt.Errorf("create %q: %w", tempname, err)
	}
	defer func() {
		temp.Close()
		if err != nil {
			c.Remove(tempname)
		}
	}()

	if err = c.Chmod(tempname, info.Mode().Perm()); err != nil {
		return fmt.Errorf("chmod %q: %w", tempname, err)
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

// makeExec adds execute permission to a file.
func makeExec(ctx context.Context, c *sftp.Client, filename string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	info, err := c.Stat(filename)
	if err != nil {
		return fmt.Errorf("stat %q: %w", filename, err)
	}

	mode := info.Mode().Perm() | 0111
	if err := c.Chmod(filename, mode); err != nil {
		return fmt.Errorf("chmod +x %q: %w", filename, err)
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

// download files from remote host to local directory. Patterns support glob
// expansion on the remote side. Directories are downloaded recursively.
// Symlinks are recreated locally with their original target (not followed).
// Full remote paths are preserved to avoid conflicts.
func download(ctx context.Context, c *sftp.Client, localDir string, patterns ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	for _, pattern := range patterns {
		if err := ctx.Err(); err != nil {
			return err
		}

		matches, err := c.Glob(pattern)
		if err != nil {
			return fmt.Errorf("glob %q: %w", pattern, err)
		}

		for _, remotePath := range matches {
			if err := ctx.Err(); err != nil {
				return err
			}

			if err := downloadPath(ctx, c, localDir, remotePath); err != nil {
				return err
			}
		}
	}

	return nil
}

// downloadPath downloads a single path (file, directory, or symlink).
func downloadPath(ctx context.Context, c *sftp.Client, localDir, remotePath string) error {
	info, err := c.Lstat(remotePath)
	if err != nil {
		return fmt.Errorf("lstat %q: %w", remotePath, err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return downloadSymlink(c, localDir, remotePath)
	}

	if info.IsDir() {
		return downloadDir(ctx, c, localDir, remotePath)
	}

	return downloadFile(c, localDir, remotePath)
}

// downloadDir recursively downloads a directory and its contents.
func downloadDir(ctx context.Context, c *sftp.Client, localDir, remoteDir string) error {
	walker := c.Walk(remoteDir)
	for walker.Step() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := walker.Err(); err != nil {
			return fmt.Errorf("walk %q: %w", remoteDir, err)
		}

		remotePath := walker.Path()
		info := walker.Stat()

		if info.Mode()&os.ModeSymlink != 0 {
			if err := downloadSymlink(c, localDir, remotePath); err != nil {
				return err
			}
			walker.SkipDir() // don't follow symlinks to directories
			continue
		}

		if info.IsDir() {
			localPath := filepath.Join(localDir, remotePath)
			if err := os.MkdirAll(localPath, info.Mode().Perm()); err != nil {
				return fmt.Errorf("create dir %q: %w", localPath, err)
			}
		} else {
			if err := downloadFile(c, localDir, remotePath); err != nil {
				return err
			}
		}
	}
	return nil
}

// downloadSymlink recreates a symlink locally with its original target.
func downloadSymlink(c *sftp.Client, localDir, remotePath string) error {
	localPath := filepath.Join(localDir, remotePath)

	if _, err := os.Lstat(localPath); err == nil {
		return nil // already fetched
	}

	target, err := c.ReadLink(remotePath)
	if err != nil {
		return fmt.Errorf("readlink %q: %w", remotePath, err)
	}

	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir %q: %w", dir, err)
	}

	if err := os.Symlink(target, localPath); err != nil {
		return fmt.Errorf("symlink %q -> %q: %w", localPath, target, err)
	}

	return nil
}

// downloadFile downloads a single file, preserving the full remote path structure.
func downloadFile(c *sftp.Client, localDir, remotePath string) (err error) {
	localPath := filepath.Join(localDir, remotePath)
	if _, err := os.Lstat(localPath); err == nil {
		return nil // already fetched
	}

	remote, err := c.Open(remotePath)
	if err != nil {
		return fmt.Errorf("open %q: %w", remotePath, err)
	}
	defer remote.Close()

	info, err := remote.Stat()
	if err != nil {
		return fmt.Errorf("stat %q: %w", remotePath, err)
	}

	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir %q: %w", dir, err)
	}

	tempname := fmt.Sprintf("%s_%s.tmp", localPath, randHex(32))
	temp, err := os.Create(tempname)
	if err != nil {
		return fmt.Errorf("create %q: %w", tempname, err)
	}
	defer func() {
		if err != nil {
			temp.Close()
			os.Remove(tempname)
		}
	}()

	if err = os.Chmod(tempname, info.Mode().Perm()); err != nil {
		return fmt.Errorf("chmod %q: %w", tempname, err)
	}

	if _, err = io.Copy(temp, remote); err != nil {
		return fmt.Errorf("copy %q: %w", remotePath, err)
	}

	if err = temp.Close(); err != nil {
		return fmt.Errorf("close %q: %w", tempname, err)
	}

	if err = os.Rename(tempname, localPath); err != nil {
		return fmt.Errorf("rename %q to %q: %w", tempname, localPath, err)
	}

	return nil
}
