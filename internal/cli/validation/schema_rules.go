package validation

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

var allowedTopLevelKeys = map[string]struct{}{
	"services": {},
	"local":    {},
}

var allowedServiceKeys = map[string]struct{}{
	"name":        {},
	"path":        {},
	"description": {},
	"kind":        {},
	"type":        {},
	"archetype":   {},
	"runtime":     {},
	"owner":       {},
	"depends":     {},
	"dev":         {},
	"commands":    {},
	"deploy":      {},
}

var allowedLocalKeys = map[string]struct{}{
	"namespace": {},
	"resources": {},
}

var allowedLocalResourceKeys = map[string]struct{}{
	"name":        {},
	"description": {},
	"manifest":    {},
	"readyCheck":  {},
	"portForward": {},
}

var allowedReadyCheckKeys = map[string]struct{}{
	"selector": {},
}

var allowedPortForwardKeys = map[string]struct{}{
	"localPort":  {},
	"targetPort": {},
	"target":     {},
}

var allowedDeployKeys = map[string]struct{}{
	"containerPort": {},
	"probes":        {},
	"resources":     {},
	"ingress":       {},
	"env":           {},
}

var allowedProbesKeys = map[string]struct{}{
	"readiness": {},
	"liveness":  {},
}

var allowedProbeKeys = map[string]struct{}{
	"path": {},
	"port": {},
}

var allowedResourcesKeys = map[string]struct{}{
	"requests": {},
	"limits":   {},
}

var allowedIngressKeys = map[string]struct{}{
	"enabled": {},
	"host":    {},
}

func validateSchema(root *yaml.Node, report *Report) map[int]serviceNodeInfo {
	serviceInfo := map[int]serviceNodeInfo{}
	foundServices := false
	if root == nil || root.Kind != yaml.MappingNode {
		report.add(Diagnostic{
			Severity: SeverityError,
			Code:     "schema.root",
			Path:     "$",
			Message:  "manifest root must be a mapping",
			Line:     keyPos(root).line,
			Column:   keyPos(root).column,
		})
		return serviceInfo
	}

	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		v := root.Content[i+1]
		if _, ok := allowedTopLevelKeys[k.Value]; !ok {
			report.add(Diagnostic{
				Severity: SeverityError,
				Code:     "schema.unknown_key",
				Path:     k.Value,
				Message:  fmt.Sprintf("unknown top-level key %q", k.Value),
				Line:     k.Line,
				Column:   k.Column,
			})
			continue
		}
		if k.Value == "services" {
			foundServices = true
			serviceInfo = validateServicesSection(v, report)
		}
		if k.Value == "local" {
			validateKnownMappingKeys(v, "local", allowedLocalKeys, "schema.unknown_local_key", report)
			validateLocalNestedKeys(v, report)
		}
	}
	if !foundServices {
		report.add(Diagnostic{
			Severity: SeverityError,
			Code:     "schema.required",
			Path:     "services",
			Message:  "missing required top-level key: services",
			Line:     keyPos(root).line,
			Column:   keyPos(root).column,
		})
	}

	return serviceInfo
}

func validateServicesSection(node *yaml.Node, report *Report) map[int]serviceNodeInfo {
	result := map[int]serviceNodeInfo{}
	if node == nil {
		return result
	}
	if node.Kind != yaml.SequenceNode {
		report.add(Diagnostic{
			Severity: SeverityError,
			Code:     "schema.services_type",
			Path:     "services",
			Message:  "services must be a sequence",
			Line:     node.Line,
			Column:   node.Column,
		})
		return result
	}

	for i, serviceNode := range node.Content {
		pathPrefix := fmt.Sprintf("services[%d]", i)
		info := serviceNodeInfo{
			index:     i,
			keyPos:    map[string]position{},
			keyNode:   map[string]*yaml.Node{},
			serviceAt: keyPos(serviceNode),
		}

		if serviceNode.Kind != yaml.MappingNode {
			report.add(Diagnostic{
				Severity: SeverityError,
				Code:     "schema.service_type",
				Path:     pathPrefix,
				Message:  "service entry must be a mapping",
				Line:     serviceNode.Line,
				Column:   serviceNode.Column,
			})
			result[i] = info
			continue
		}

		for j := 0; j+1 < len(serviceNode.Content); j += 2 {
			k := serviceNode.Content[j]
			v := serviceNode.Content[j+1]
			info.keyPos[k.Value] = keyPos(k)
			info.keyNode[k.Value] = v
			if _, ok := allowedServiceKeys[k.Value]; !ok {
				report.add(Diagnostic{
					Severity: SeverityError,
					Code:     "schema.unknown_service_key",
					Path:     pathPrefix + "." + k.Value,
					Message:  fmt.Sprintf("unknown service key %q", k.Value),
					Line:     k.Line,
					Column:   k.Column,
				})
			}
			if k.Value == "deploy" {
				validateKnownMappingKeys(v, pathPrefix+".deploy", allowedDeployKeys, "schema.unknown_deploy_key", report)
				validateDeployNestedKeys(v, pathPrefix+".deploy", report)
			}
		}

		result[i] = info
	}

	return result
}

func addMissingFieldDiagnostic(report *Report, path string, field string, where position) {
	report.add(Diagnostic{
		Severity: SeverityError,
		Code:     "schema.required",
		Path:     path,
		Message:  fmt.Sprintf("missing required field: %s", field),
		Line:     where.line,
		Column:   where.column,
	})
}

func requiredFieldPos(info serviceNodeInfo, field string) position {
	if p, ok := info.keyPos[field]; ok {
		return p
	}
	return info.serviceAt
}

func isFilled(v string) bool {
	return strings.TrimSpace(v) != ""
}

func validateKnownMappingKeys(node *yaml.Node, path string, allowed map[string]struct{}, code string, report *Report) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		k := node.Content[i]
		if _, ok := allowed[k.Value]; ok {
			continue
		}
		report.add(Diagnostic{
			Severity: SeverityError,
			Code:     code,
			Path:     path + "." + k.Value,
			Message:  fmt.Sprintf("unknown key %q", k.Value),
			Line:     k.Line,
			Column:   k.Column,
		})
	}
}

func validateLocalNestedKeys(localNode *yaml.Node, report *Report) {
	if localNode == nil || localNode.Kind != yaml.MappingNode {
		return
	}
	_, resourcesNode, ok := mappingLookup(localNode, "resources")
	if !ok || resourcesNode == nil || resourcesNode.Kind != yaml.SequenceNode {
		return
	}
	for i, resourceNode := range resourcesNode.Content {
		pathPrefix := fmt.Sprintf("local.resources[%d]", i)
		validateKnownMappingKeys(resourceNode, pathPrefix, allowedLocalResourceKeys, "schema.unknown_local_resource_key", report)
		if _, readyCheckNode, ok := mappingLookup(resourceNode, "readyCheck"); ok {
			validateKnownMappingKeys(readyCheckNode, pathPrefix+".readyCheck", allowedReadyCheckKeys, "schema.unknown_readycheck_key", report)
		}
		if _, portForwardNode, ok := mappingLookup(resourceNode, "portForward"); ok {
			validateKnownMappingKeys(portForwardNode, pathPrefix+".portForward", allowedPortForwardKeys, "schema.unknown_portforward_key", report)
		}
	}
}

func validateDeployNestedKeys(deployNode *yaml.Node, path string, report *Report) {
	if deployNode == nil || deployNode.Kind != yaml.MappingNode {
		return
	}
	if _, probesNode, ok := mappingLookup(deployNode, "probes"); ok {
		validateKnownMappingKeys(probesNode, path+".probes", allowedProbesKeys, "schema.unknown_probes_key", report)
		if _, readinessNode, ok := mappingLookup(probesNode, "readiness"); ok {
			validateKnownMappingKeys(readinessNode, path+".probes.readiness", allowedProbeKeys, "schema.unknown_probe_key", report)
		}
		if _, livenessNode, ok := mappingLookup(probesNode, "liveness"); ok {
			validateKnownMappingKeys(livenessNode, path+".probes.liveness", allowedProbeKeys, "schema.unknown_probe_key", report)
		}
	}
	if _, resourcesNode, ok := mappingLookup(deployNode, "resources"); ok {
		validateKnownMappingKeys(resourcesNode, path+".resources", allowedResourcesKeys, "schema.unknown_deploy_resources_key", report)
	}
	if _, ingressNode, ok := mappingLookup(deployNode, "ingress"); ok {
		validateKnownMappingKeys(ingressNode, path+".ingress", allowedIngressKeys, "schema.unknown_ingress_key", report)
	}
}

func validateRequiredFields(services []manifestService, info map[int]serviceNodeInfo, report *Report) {
	for i, svc := range services {
		prefix := fmt.Sprintf("services[%d]", i)
		label := serviceLabel(i, svc)
		nodeInfo, ok := info[i]
		if !ok {
			nodeInfo = serviceNodeInfo{serviceAt: position{}}
		}

		if !isFilled(svc.Name) {
			addMissingFieldDiagnostic(report, prefix+".name", "name", requiredFieldPos(nodeInfo, "name"))
		}
		if !isFilled(svc.Path) {
			addMissingFieldDiagnostic(report, prefix+".path", "path", requiredFieldPos(nodeInfo, "path"))
		}
		if !isFilled(svc.Kind) && !isFilled(svc.Type) {
			addMissingFieldDiagnostic(report, prefix+".kind", "kind/type", requiredFieldPos(nodeInfo, "kind"))
		}
		if !isFilled(svc.Archetype) && !isFilled(svc.Runtime) {
			addMissingFieldDiagnostic(report, prefix+".archetype", "archetype/runtime", requiredFieldPos(nodeInfo, "archetype"))
		}
		if !isFilled(svc.Owner) {
			ownerPos := requiredFieldPos(nodeInfo, "owner")
			report.add(Diagnostic{
				Severity: SeverityError,
				Code:     "schema.required",
				Path:     prefix + ".owner",
				Message:  "missing required field: owner",
				Service:  label,
				Line:     ownerPos.line,
				Column:   ownerPos.column,
			})
		}
	}
}
