package bichme

import (
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"time"

	"golang.org/x/crypto/ssh"
)

func runID() string {
	t := time.Now()
	return fmt.Sprintf("%s/%s.%d",
		t.Format(time.DateOnly),
		t.Format("15-04-05"), os.Getpid(),
	)
}

func sshIsAlive(c *ssh.Client) bool {
	s, err := c.NewSession()
	if err != nil {
		slog.Debug("sshIsAlive failed", "error", err)
		return false
	}
	s.Close()
	return true
}

// currentUser makes best effort to get currently used user. If
// os/user.Current() fails, it tries to use the USER environment variable or
// falls back to 'root'.
func currentUser() string {
	u, err := user.Current()
	if err != nil {
		fallback := os.Getenv("USER")
		slog.Debug("Failed to get current user", "error", err, "env", fallback)
		if fallback == "" {
			fallback = "root"
		}
		return fallback
	}
	return u.Username
}

// pick returns the first non-zero value of type T.
func pick[T comparable](values ...T) T {
	var zero T
	for _, v := range values {
		if v != zero {
			return v
		}
	}
	return zero
}
