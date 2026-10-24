package runtimecmd

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

//go:embed telemetry_assets/lgtm.yaml
var telemetryManifestYAML []byte

type telemetryManifest struct {
	kind string
	data []byte
}

func telemetryManifests(c telemetryConfig) ([]telemetryManifest, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(telemetryManifestYAML))
	var manifests []telemetryManifest
	for {
		var doc map[string]any
		if err := decoder.Decode(&doc); err != nil {
			if err == io.EOF {
				return manifests, nil
			}
			return nil, fmt.Errorf("telemetry manifest: %w", err)
		}
		kind := doc["kind"].(string)
		if kind == "PersistentVolumeClaim" {
			spec := doc["spec"].(map[string]any)
			spec["storageClassName"] = c.StorageClass
			spec["resources"].(map[string]any)["requests"].(map[string]any)["storage"] = c.StorageSize
		}
		data, err := yaml.Marshal(doc)
		if err != nil {
			return nil, fmt.Errorf("telemetry manifest: %w", err)
		}
		manifests = append(manifests, telemetryManifest{kind, data})
	}
}
