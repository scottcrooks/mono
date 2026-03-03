package core

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the services.yaml structure.
type Config struct {
	Services []Service  `yaml:"services"`
	Local    *InfraSpec `yaml:"local"`
}

// Service represents a single service configuration.
type Service struct {
	Name        string            `yaml:"name"`
	Path        string            `yaml:"path"`
	Description string            `yaml:"description"`
	Kind        string            `yaml:"kind"`
	Type        string            `yaml:"type"`
	Archetype   string            `yaml:"archetype"`
	Runtime     string            `yaml:"runtime"`
	Owner       string            `yaml:"owner"`
	Depends     []string          `yaml:"depends"`
	DevDepends  []string          `yaml:"devDepends"`
	Dev         string            `yaml:"dev"`
	Commands    map[string]string `yaml:"commands"`
	Deploy      *DeploySpec       `yaml:"deploy"`
}

// DeploySpec defines minimal deploy contract settings for a service.
type DeploySpec struct {
	ContainerPort int             `yaml:"containerPort"`
	Probes        *ProbeGroupSpec `yaml:"probes"`
	Resources     *ResourcesSpec  `yaml:"resources"`
	Ingress       *IngressSpec    `yaml:"ingress"`
	Env           map[string]any  `yaml:"env"`
	Extra         map[string]any  `yaml:",inline"`
}

// ProbeGroupSpec groups readiness and liveness probes.
type ProbeGroupSpec struct {
	Readiness *ProbeSpec `yaml:"readiness"`
	Liveness  *ProbeSpec `yaml:"liveness"`
}

// ProbeSpec defines a simple HTTP probe contract.
type ProbeSpec struct {
	Path string `yaml:"path"`
	Port int    `yaml:"port"`
}

// ResourcesSpec defines request/limit presence for deploy policy validation.
type ResourcesSpec struct {
	Requests map[string]string `yaml:"requests"`
	Limits   map[string]string `yaml:"limits"`
}

// IngressSpec defines minimal ingress configuration.
type IngressSpec struct {
	Enabled bool   `yaml:"enabled"`
	Host    string `yaml:"host"`
}

// InfraSpec contains the infrastructure configuration.
type InfraSpec struct {
	Namespace string          `yaml:"namespace"`
	Resources []InfraResource `yaml:"resources"`
}

// InfraResource represents a single infrastructure resource.
type InfraResource struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Manifest    string           `yaml:"manifest"`
	ReadyCheck  ReadyCheckSpec   `yaml:"readyCheck"`
	PortForward *PortForwardSpec `yaml:"portForward"`
}

// ReadyCheckSpec defines how to check if a resource is ready.
type ReadyCheckSpec struct {
	Selector string `yaml:"selector"`
}

// PortForwardSpec defines port forwarding configuration.
type PortForwardSpec struct {
	LocalPort  int    `yaml:"localPort"`
	TargetPort int    `yaml:"targetPort"`
	Target     string `yaml:"target"`
}

// LoadConfig reads and parses services.yaml from the repo root.
func LoadConfig() (*Config, error) {
	configPath := "services.yaml"

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", configPath, err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", configPath, err)
	}

	return &config, nil
}

// FindService searches for a service by name.
func FindService(config *Config, name string) *Service {
	for _, svc := range config.Services {
		if svc.Name == name {
			return &svc
		}
	}
	return nil
}
