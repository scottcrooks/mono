package core

import (
	"path/filepath"
	"strings"
)

// IsMonoTimingArtifact reports whether a project-root-relative path is one of
// the generated timing-history files maintained by mono.
func IsMonoTimingArtifact(path string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	if path == ".mono" || path == ".mono.lock" {
		return true
	}
	return strings.HasPrefix(path, ".mono.tmp-") && !strings.Contains(path, "/")
}
