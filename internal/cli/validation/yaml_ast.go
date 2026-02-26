package validation

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

func parseYAML(data []byte) (*yaml.Node, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil, fmt.Errorf("manifest must be a YAML document")
	}
	return root.Content[0], nil
}

func mappingLookup(node *yaml.Node, key string) (keyNode *yaml.Node, valueNode *yaml.Node, ok bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, nil, false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		k := node.Content[i]
		if k.Value == key {
			return k, node.Content[i+1], true
		}
	}
	return nil, nil, false
}

func keyPos(node *yaml.Node) position {
	if node == nil {
		return position{}
	}
	return position{line: node.Line, column: node.Column}
}
