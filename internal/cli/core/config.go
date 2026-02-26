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
	Archetype   string            `yaml:"archetype"`
	Depends     []string          `yaml:"depends"`
	Dev         string            `yaml:"dev"`
	Commands    map[string]string `yaml:"commands"`
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
