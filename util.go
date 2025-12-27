package bichme

import (
	"fmt"
	"os"
	"time"
)

func runID() string {
	t := time.Now()
	return fmt.Sprintf("%s/%s.%d",
		t.Format(time.DateOnly),
		t.Format(time.TimeOnly), os.Getpid(),
	)
}
