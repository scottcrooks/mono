package tasks

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"

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
