package mask

import (
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
		idx := strings.IndexByte(string(m.buf), '\n')
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
	s := string(line)
	for _, sec := range m.secrets {
		s = strings.ReplaceAll(s, sec, replacement)
	}
	return []byte(s)
}
