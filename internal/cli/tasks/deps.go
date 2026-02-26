package tasks

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/scottcrooks/mono/internal/cli/core"
)

type (
	Config  = core.Config
	Service = core.Service
)

func loadConfig() (*Config, error) {
	return core.LoadConfig()
}

func findService(config *Config, name string) *Service {
	return core.FindService(config, name)
}

func commandFromParts(ctx context.Context, parts []string) (*exec.Cmd, error) {
	return core.CommandFromParts(ctx, parts)
}

func normalizeServicePath(path string) string {
	path = filepath.ToSlash(path)
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "./")
	path = strings.Trim(path, "/")
	return path
}

type PrefixWriter struct {
	prefix string
	writer io.Writer
	mu     sync.Mutex
	buffer []byte
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
