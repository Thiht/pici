package mask

import (
	"bytes"
	"io"
	"strings"
)

const replacement = "***"

type Writer struct {
	w       io.Writer
	secrets []string
	buf     []byte
}

func NewWriter(w io.Writer, secrets []string) *Writer {
	var cleaned []string
	for _, s := range secrets {
		if s != "" {
			cleaned = append(cleaned, s)
		}
	}
	return &Writer{w: w, secrets: cleaned}
}

func (m *Writer) Write(p []byte) (int, error) {
	n := len(p)
	m.buf = append(m.buf, p...)
	for {
		idx := bytes.IndexByte(m.buf, '\n')
		if idx < 0 {
			break
		}
		line := m.buf[:idx+1]
		m.buf = m.buf[idx+1:]
		if _, err := m.w.Write(m.redact(line)); err != nil {
			return n, err
		}
	}
	return n, nil
}

func (m *Writer) Flush() error {
	if len(m.buf) == 0 {
		return nil
	}
	_, err := m.w.Write(m.redact(m.buf))
	m.buf = nil
	return err
}

func (m *Writer) redact(line []byte) []byte {
	return []byte(Redact(string(line), m.secrets))
}

// Redact replaces every occurrence of a secret with the redaction marker.
func Redact(s string, secrets []string) string {
	for _, sec := range secrets {
		if sec == "" {
			continue
		}
		s = strings.ReplaceAll(s, sec, replacement)
	}
	return s
}
