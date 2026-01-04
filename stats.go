package bichme

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
)

// WriteStats summary by reading the jobs' errors and performing some deductions.
func WriteStats(w io.Writer, archive map[*Job]error) error {
	statuses := make(map[string]int, 5)
	maxTry := 0
	for job, err := range archive {
		maxTry = max(maxTry, job.tries)
		switch {
		case err == nil:
			if job.tasks.Done() {
				statuses["done"] += 1
			} else {
				statuses["running"] += 1
			}
		case errors.Is(err, ErrConnection):
			statuses["conn"] += 1
		case errors.Is(err, ErrFileTransfer):
			statuses["file"] += 1
		case errors.Is(err, ErrExection):
			statuses["exec"] += 1
		default:
			slog.Debug("Job is in a bad state", "host", job.host, "error", err)
		}
	}

	fmt.Fprintf(w, "\n============== %d =============\n", maxTry)
	if statuses["conn"] > 0 {
		fmt.Fprintf(w, " Connection failed:\t%d\n", statuses["conn"])
	}
	if statuses["file"] > 0 {
		fmt.Fprintf(w, " File Transfer failed:\t%d\n", statuses["file"])
	}
	if statuses["exec"] > 0 {
		fmt.Fprintf(w, " Execution failed:\t%d\n", statuses["exec"])
	}
	if statuses["running"] > 0 {
		fmt.Fprintf(w, " Running:\t\t%d\n", statuses["running"])
	}
	if statuses["done"] > 0 {
		fmt.Fprintf(w, " Done:\t\t\t%d\n", statuses["done"])
	}
	fmt.Fprintf(w, "===============================\n")
	fmt.Fprintf(w, " Total:\t%d\n", len(archive))
	fmt.Fprintf(w, "===============================\n\n")
	return nil
}
