package main

import (
	"bytes"
	"io"
	"sync"
)

func newPrefixWriter(w io.Writer, prefix string) io.Writer {
	return &prefixWriter{
		w:      w,
		prefix: prefix,
		atBOL:  true,
		mu:     new(sync.Mutex),
	}
}

type prefixWriter struct {
	w      io.Writer
	prefix string
	atBOL  bool
	mu     *sync.Mutex
}

func (pw *prefixWriter) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	pw.mu.Lock()
	defer pw.mu.Unlock()

	var buf bytes.Buffer
	for _, b := range p {
		if pw.atBOL {
			buf.WriteString(pw.prefix)
			pw.atBOL = false
		}
		buf.WriteByte(b)
		if b == '\n' {
			pw.atBOL = true
		}
	}
	_, err = pw.w.Write(buf.Bytes())
	return len(p), err
}
