package output

import (
	"fmt"
	"hash/fnv"
	"io"
	"sync"
)

// PrefixWriter prefixes each complete line with a scope label.
type PrefixWriter struct {
	prefix string
	writer io.Writer
	mu     sync.Mutex
	buffer []byte
}

func NewPrefixWriter(prefix string, writer io.Writer) *PrefixWriter {
	return &PrefixWriter{prefix: prefix, writer: writer}
}

func NewServicePrefixWriter(serviceName string, writer io.Writer, mode Mode) *PrefixWriter {
	prefix := fmt.Sprintf("[%s]", serviceName)
	return NewPrefixWriter(colorizeServicePrefix(prefix, serviceName, mode), writer)
}

func (pw *PrefixWriter) Write(p []byte) (n int, err error) {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	pw.buffer = append(pw.buffer, p...)

	for {
		idx := -1
		for i, b := range pw.buffer {
			if b == '\n' {
				idx = i
				break
			}
		}
		if idx == -1 {
			break
		}
		line := pw.buffer[:idx+1]
		pw.buffer = pw.buffer[idx+1:]
		if _, err := pw.writer.Write([]byte(fmt.Sprintf("%s %s", pw.prefix, string(line)))); err != nil {
			return 0, err
		}
	}

	return len(p), nil
}

func (pw *PrefixWriter) Flush() error {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	if len(pw.buffer) == 0 {
		return nil
	}
	if _, err := pw.writer.Write([]byte(fmt.Sprintf("%s %s\n", pw.prefix, string(pw.buffer)))); err != nil {
		return err
	}
	pw.buffer = nil
	return nil
}

func colorizeServicePrefix(prefix, serviceName string, mode Mode) string {
	if mode != ModeInteractive {
		return prefix
	}

	// Curated non-red/non-green accents so these don't clash with status colors.
	palette := []string{
		"\x1b[38;5;39m",  // blue
		"\x1b[38;5;45m",  // cyan
		"\x1b[38;5;141m", // lavender
		"\x1b[38;5;214m", // amber
		"\x1b[38;5;118m", // lime
		"\x1b[38;5;205m", // pink
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(serviceName))
	color := palette[h.Sum32()%uint32(len(palette))]
	return color + prefix + ansiReset
}
