package validation

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var projectMarkerFiles = []string{"go.mod", "package.json", "pyproject.toml", "Cargo.toml"}

func validatePathRules(repoRoot string, services []manifestService, info map[int]serviceNodeInfo, report *Report) {
	repoRootAbs, _ := filepath.Abs(repoRoot)
	for i, svc := range services {
		path := strings.TrimSpace(svc.Path)
		if path == "" {
			continue
		}
		p := position{}
		if sInfo, ok := info[i]; ok {
			p = requiredFieldPos(sInfo, "path")
		}

		if filepath.IsAbs(path) {
			report.add(Diagnostic{
				Severity: SeverityError,
				Code:     "path.absolute",
				Path:     fmt.Sprintf("services[%d].path", i),
				Message:  "service path must be relative to repository root",
				Line:     p.line,
				Column:   p.column,
			})
			continue
		}
		clean := filepath.Clean(path)
		if strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
			report.add(Diagnostic{
				Severity: SeverityError,
				Code:     "path.escape",
				Path:     fmt.Sprintf("services[%d].path", i),
				Message:  "service path cannot escape repository root",
				Line:     p.line,
				Column:   p.column,
			})
			continue
		}

		absPath := filepath.Join(repoRoot, path)
		absPath, _ = filepath.Abs(absPath)
		if repoRootAbs != "" && !strings.HasPrefix(absPath, repoRootAbs+string(filepath.Separator)) && absPath != repoRootAbs {
			report.add(Diagnostic{
				Severity: SeverityError,
				Code:     "path.escape",
				Path:     fmt.Sprintf("services[%d].path", i),
				Message:  "service path resolves outside repository root",
				Line:     p.line,
				Column:   p.column,
			})
			continue
		}
		stat, err := os.Stat(absPath)
		if err != nil {
			report.add(Diagnostic{
				Severity: SeverityError,
				Code:     "path.missing",
				Path:     fmt.Sprintf("services[%d].path", i),
				Message:  fmt.Sprintf("service path does not exist: %s", path),
				Line:     p.line,
				Column:   p.column,
			})
			continue
		}

		if !stat.IsDir() {
			report.add(Diagnostic{
				Severity: SeverityError,
				Code:     "path.not_directory",
				Path:     fmt.Sprintf("services[%d].path", i),
				Message:  fmt.Sprintf("service path is not a directory: %s", path),
				Line:     p.line,
				Column:   p.column,
			})
			continue
		}

		if !hasProjectMarker(absPath) {
			report.add(Diagnostic{
				Severity: SeverityWarning,
				Code:     "path.project_marker",
				Path:     fmt.Sprintf("services[%d].path", i),
				Message:  "service directory has no recognized project manifest (go.mod/package.json/pyproject.toml/Cargo.toml)",
				Line:     p.line,
				Column:   p.column,
			})
		}
	}
}

func hasProjectMarker(absPath string) bool {
	for _, marker := range projectMarkerFiles {
		if _, err := os.Stat(filepath.Join(absPath, marker)); err == nil {
			return true
		}
	}
	return false
}
