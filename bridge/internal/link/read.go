package link

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

const maxLine = 4096

// readLines calls fn with a private copy of every newline-terminated line of r
// until r fails and returns that error. Blank lines and lines of maxLine bytes or
// more are dropped.
func readLines(r io.Reader, fn func([]byte)) error {
	br := bufio.NewReaderSize(r, maxLine)
	skipping := false
	for {
		line, err := br.ReadSlice('\n')
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			skipping = true
		case err != nil:
			return err
		case skipping:
			skipping = false
		default:
			if line = bytes.TrimRight(line, "\r\n"); len(line) > 0 {
				fn(append([]byte(nil), line...))
			}
		}
	}
}
