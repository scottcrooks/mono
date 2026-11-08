package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joho/godotenv"
)

// BuildEnvironment builds a child-only environment from root and service dotenv
// files, followed by inherited entries. Each file is parsed independently.
func BuildEnvironment(root, serviceDir, filename string, inherited []string) ([]string, error) {
	values := make(map[string]string)
	seen := make(map[string]bool)
	for _, dir := range []string{root, serviceDir} {
		path := filepath.Join(dir, filename)
		if seen[path] {
			continue
		}
		seen[path] = true
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read dotenv %s: %w", path, err)
		}
		parsed, err := godotenv.Unmarshal(string(data))
		if err != nil {
			// Parser errors can include source values; never expose them.
			return nil, fmt.Errorf("invalid dotenv file %s", path)
		}
		for key, value := range parsed {
			values[key] = value
		}
	}
	for _, entry := range inherited {
		if key, value, ok := strings.Cut(entry, "="); ok {
			values[key] = value
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env, nil
}
