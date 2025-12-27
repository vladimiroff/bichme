package bichme

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"
)

// Output is used to relay remove execution's output into the history (if
// enabled), while also teeing the output to stdout.
type Output struct {
	mu  sync.Mutex
	buf bytes.Buffer

	// printing-related
	prefix string
	stdout io.Writer

	file io.WriteCloser // file to write through if set
}

var newline = []byte{'\n'}

// NewOutput creates new output with given prefix, without underlaying file,
// printing to os.Stdout.
func NewOutput(prefix string) *Output {
	return &Output{prefix: prefix, stdout: os.Stdout}
}

// SetFile sets f as underlaying file.
func (o *Output) SetFile(f io.WriteCloser) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.file = f
}

// SetStdout changes where to tee the output, instead of os.Stdout. w MUST NOT
// be nil, otherwise Output will eventually panic on Write or Flush.
func (o *Output) SetStdout(w io.Writer) { o.stdout = w }

func (o *Output) bufferOut(p []byte) {
	i := bytes.Index(p, newline)
	if i < 0 {
		o.buf.Write(p)
		return
	}

	o.buf.Write(p[:i+1])
	fmt.Fprintf(o.stdout, "%s:\t%s", o.prefix, o.buf.String())
	o.buf.Reset()
	o.bufferOut(p[i+1:])
}

// Write to underlaying file (if any) while buffering p until a newline is
// received in order to print to standard output. Returns the result from
// file's Write.
func (o *Output) Write(p []byte) (n int, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.file != nil {
		n, err = o.file.Write(p)
	}
	o.bufferOut(p)
	return n, err
}

// Close the underlaying file (if any).
func (o *Output) Close() error {
	if o.file != nil {
		return o.file.Close()
	}
	return nil
}

// Flush writes any buffered data to stdout with a trailing newline.
func (o *Output) Flush() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.buf.Len() > 0 {
		o.bufferOut(newline)
	}
	return nil
}
