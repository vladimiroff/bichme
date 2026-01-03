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
		t.Format(time.TimeOnly), os.Getpid(),
	)
}

func sshIsAlive(c *ssh.Client) bool {
	var err error
	defer func() {
		if err != nil {
			slog.Debug("sshIsAlive failed", "error", err)
		}
	}()
	s, err := c.NewSession()
	if err != nil {
		return false
	}

	err = s.Wait()
	return err == nil
}
