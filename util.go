package bichme

import (
	"fmt"
	"log/slog"
	"os"
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
