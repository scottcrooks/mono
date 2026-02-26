package validation

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var parseLinePattern = regexp.MustCompile(`line\s+(\d+)`)

// Diagnostic is a single policy/schema validation issue.
type Diagnostic struct {
	Severity Severity
	Code     string
	Path     string
	Message  string
	Service  string
	Line     int
	Column   int
}

// Severity is the diagnostic level.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Report is the full deterministic validator output.
type Report struct {
	Diagnostics []Diagnostic
}

func (r *Report) add(diag Diagnostic) {
	r.Diagnostics = append(r.Diagnostics, diag)
}

func (r Report) HasErrors() bool {
	return r.ErrorCount() > 0
}

func (r Report) ErrorCount() int {
	count := 0
	for _, diag := range r.Diagnostics {
		if diag.Severity == SeverityError {
			count++
		}
	}
	return count
}

func (r Report) WarningCount() int {
	count := 0
	for _, diag := range r.Diagnostics {
		if diag.Severity == SeverityWarning {
			count++
		}
	}
	return count
}

func (r *Report) sort() {
	sort.SliceStable(r.Diagnostics, func(i, j int) bool {
		a := r.Diagnostics[i]
		b := r.Diagnostics[j]
		if a.Line != b.Line {
			if a.Line == 0 {
				return false
			}
			if b.Line == 0 {
				return true
			}
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			if a.Column == 0 {
				return false
			}
			if b.Column == 0 {
				return true
			}
			return a.Column < b.Column
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Severity != b.Severity {
			return a.Severity < b.Severity
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
}

type manifest struct {
	Services []manifestService `yaml:"services"`
	Local    *manifestLocal    `yaml:"local"`
}

type manifestService struct {
	Name      string          `yaml:"name"`
	Path      string          `yaml:"path"`
	Kind      string          `yaml:"kind"`
	Type      string          `yaml:"type"`
	Archetype string          `yaml:"archetype"`
	Runtime   string          `yaml:"runtime"`
	Owner     string          `yaml:"owner"`
	Depends   []string        `yaml:"depends"`
	Deploy    *manifestDeploy `yaml:"deploy"`
}

type manifestLocal struct {
	Resources []manifestLocalResource `yaml:"resources"`
}

type manifestLocalResource struct {
	Name string `yaml:"name"`
}

type manifestDeploy struct {
	ContainerPort int                    `yaml:"containerPort"`
	Probes        *manifestDeployProbes  `yaml:"probes"`
	Resources     *manifestDeployResSpec `yaml:"resources"`
}

type manifestDeployProbes struct {
	Readiness *manifestDeployProbe `yaml:"readiness"`
	Liveness  *manifestDeployProbe `yaml:"liveness"`
}

type manifestDeployProbe struct {
	Path string `yaml:"path"`
	Port int    `yaml:"port"`
}

type manifestDeployResSpec struct {
	Requests map[string]string `yaml:"requests"`
	Limits   map[string]string `yaml:"limits"`
}

type position struct {
	line   int
	column int
}

type serviceNodeInfo struct {
	index     int
	keyPos    map[string]position
	keyNode   map[string]*yaml.Node
	serviceAt position
}

func (s manifestService) projectKind() string {
	if strings.TrimSpace(s.Kind) != "" {
		return strings.TrimSpace(s.Kind)
	}
	return strings.TrimSpace(s.Type)
}

func serviceLabel(index int, svc manifestService) string {
	name := strings.TrimSpace(svc.Name)
	path := strings.TrimSpace(svc.Path)
	switch {
	case name != "" && path != "":
		return fmt.Sprintf("%s (%s)", name, path)
	case name != "":
		return name
	case path != "":
		return path
	default:
		return fmt.Sprintf("services[%d]", index)
	}
}

// ValidateServicesManifest validates services.yaml policy and manifest contract for G1.
func ValidateServicesManifest(path string) (Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, fmt.Errorf("read manifest: %w", err)
	}

	root, parseErr := parseYAML(data)
	if parseErr != nil {
		report := Report{}
		line := extractYAMLParseLine(parseErr)
		report.add(Diagnostic{
			Severity: SeverityError,
			Code:     "yaml.parse",
			Path:     "$",
			Message:  parseErr.Error(),
			Line:     line,
			Column:   1,
		})
		report.sort()
		return report, nil
	}

	var manifest manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		report := Report{}
		line := extractYAMLParseLine(err)
		report.add(Diagnostic{
			Severity: SeverityError,
			Code:     "yaml.unmarshal",
			Path:     "$",
			Message:  err.Error(),
			Line:     line,
			Column:   1,
		})
		report.sort()
		return report, nil
	}

	repoRoot := filepath.Dir(path)
	report := Report{}
	servicesInfo := validateSchema(root, &report)
	validateRequiredFields(manifest.Services, servicesInfo, &report)
	validatePathRules(repoRoot, manifest.Services, servicesInfo, &report)
	validateGraphRules(manifest.Services, manifest.Local, servicesInfo, &report)
	validateDeployRules(manifest.Services, servicesInfo, &report)
	report.sort()
	return report, nil
}

func extractYAMLParseLine(err error) int {
	if err == nil {
		return 0
	}
	match := parseLinePattern.FindStringSubmatch(err.Error())
	if len(match) != 2 {
		return 0
	}
	line, convErr := strconv.Atoi(match[1])
	if convErr != nil {
		return 0
	}
	return line
}
