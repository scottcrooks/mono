package runtimecmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

type telemetryConfig struct {
	Version      int    `json:"version"`
	Context      string `json:"context"`
	StorageClass string `json:"storageClass"`
	StorageSize  string `json:"storageSize"`
}

var telemetryStorageSizeRE = regexp.MustCompile(`^[1-9][0-9]*(Mi|Gi)$`)

func validTelemetryStorageSize(s string) bool { return telemetryStorageSizeRE.MatchString(s) }

func telemetryConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("telemetry config: locate user configuration directory: %w", err)
	}
	return filepath.Join(dir, "mono", "telemetry.json"), nil
}

func validateTelemetryConfig(c telemetryConfig) error {
	if c.Version != 1 || c.Context == "" || !validTelemetryStorageSize(c.StorageSize) {
		return fmt.Errorf("expected version 1, context, and positive Mi/Gi storageSize")
	}
	return nil
}

func loadTelemetryConfig() (telemetryConfig, error) {
	var c telemetryConfig
	path, err := telemetryConfigPath()
	if err != nil {
		return c, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err == nil {
		err = json.Unmarshal(data, &c)
		if err == nil {
			err = validateTelemetryConfig(c)
		}
	}
	if err != nil {
		return telemetryConfig{}, fmt.Errorf("telemetry config: read %s: %w; repair or move this file before retrying", path, err)
	}
	return c, nil
}

func saveTelemetryConfig(c telemetryConfig) error {
	if err := validateTelemetryConfig(c); err != nil {
		return fmt.Errorf("telemetry config: %w", err)
	}
	path, err := telemetryConfigPath()
	if err != nil {
		return err
	}
	// Never overwrite malformed or unsupported configuration, even on explicit setup.
	if _, err := loadTelemetryConfig(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("telemetry config: create directory: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return fmt.Errorf("telemetry config: make directory private: %w", err)
	}
	f, err := os.CreateTemp(dir, ".telemetry-*")
	if err != nil {
		return fmt.Errorf("telemetry config: create temporary file: %w", err)
	}
	defer os.Remove(f.Name())
	data, _ := json.MarshalIndent(c, "", "  ")
	if _, err = f.Write(append(data, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		return fmt.Errorf("telemetry config: save %s: %w", path, err)
	}
	return nil
}
