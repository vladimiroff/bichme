package bichme

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

func TestOutput(t *testing.T) {
	tt := []struct {
		name  string
		input string
	}{
		{name: "one line", input: "hello world"},
		{name: "one line trailin endl", input: "hello world\n"},
		{name: "one line with prefix endl", input: "\nhello world"},
		{name: "two lines", input: "hello world\nbye world"},
		{name: "empty lines", input: "\n\n\n\n"},
		{name: "empty lines with content", input: "\n\na\n\n\n\n\nb\n\n\nc\n\nd"},
	}

	for _, tc := range tt {
		for _, useFile := range []bool{true, false} {
			var (
				f      *os.File
				err    error
				suffix string
			)
			if useFile {
				f, err = os.CreateTemp("", "bichme_outut")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Remove(f.Name()) })
			} else {
				suffix = "_nofile"
			}

			t.Run(fmt.Sprintf("%s%s", tc.name, suffix), func(t *testing.T) {
				stdout := new(bytes.Buffer)
				out := NewOutput(tc.name)
				out.SetStdout(stdout)
				defer out.Close()

				if f != nil {
					out.SetFile(f)
				}
				fmt.Fprint(out, tc.input)
				out.Flush()
				if f != nil {
					f.Seek(0, io.SeekStart)
				}

				outBytes := stdout.Bytes()
				inLines := readLines(strings.NewReader(tc.input))
				outLines := readLines(stdout)
				if len(inLines) != len(outLines) {
					t.Errorf("received %d lines; dumped %d to stdout", len(inLines), len(outLines))
					t.Logf("\n### input:\n%v\n### output:\n%v\n###", []byte(tc.input), outBytes)
				}
				for i := range len(inLines) {
					if outLine := strings.TrimPrefix(outLines[i], tc.name+":\t"); inLines[i] != outLine {
						t.Errorf("line %d differs in output from input\nin:\t%v\nout:\t%v", i, inLines[i], outLine)
						t.Logf("\n### input:\n%v\n### output:\n%v\n###", []byte(tc.input), outBytes)
					}
				}

				if f != nil {
					content, err := io.ReadAll(f)
					if err != nil {
						t.Fatal(err)
					}

					if all := string(content); all != tc.input {
						t.Errorf("expected file to be %q; got %q", tc.input, all)
					}
				}
			})
		}
	}
}
