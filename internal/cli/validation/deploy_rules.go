package validation

import (
	"fmt"
	"strings"
)

func validateDeployRules(services []manifestService, info map[int]serviceNodeInfo, report *Report) {
	for i, svc := range services {
		if svc.projectKind() != "service" {
			continue
		}
		prefix := fmt.Sprintf("services[%d].deploy", i)
		sInfo, ok := info[i]
		if !ok {
			sInfo = serviceNodeInfo{serviceAt: position{}}
		}
		deployPos := requiredFieldPos(sInfo, "deploy")

		if svc.Deploy == nil {
			report.add(Diagnostic{
				Severity: SeverityError,
				Code:     "deploy.required",
				Path:     prefix,
				Message:  "missing required deploy contract for service",
				Line:     deployPos.line,
				Column:   deployPos.column,
			})
			continue
		}

		if svc.Deploy.ContainerPort <= 0 {
			report.add(Diagnostic{
				Severity: SeverityError,
				Code:     "deploy.container_port",
				Path:     prefix + ".containerPort",
				Message:  "containerPort is required and must be > 0",
				Line:     deployPos.line,
				Column:   deployPos.column,
			})
		}

		if svc.Deploy.Probes == nil || svc.Deploy.Probes.Readiness == nil || svc.Deploy.Probes.Liveness == nil {
			report.add(Diagnostic{
				Severity: SeverityError,
				Code:     "deploy.probes",
				Path:     prefix + ".probes",
				Message:  "probes.readiness and probes.liveness are required",
				Line:     deployPos.line,
				Column:   deployPos.column,
			})
		} else {
			if strings.TrimSpace(svc.Deploy.Probes.Readiness.Path) == "" || svc.Deploy.Probes.Readiness.Port <= 0 {
				report.add(Diagnostic{
					Severity: SeverityError,
					Code:     "deploy.readiness",
					Path:     prefix + ".probes.readiness",
					Message:  "readiness probe requires path and port",
					Line:     deployPos.line,
					Column:   deployPos.column,
				})
			}
			if strings.TrimSpace(svc.Deploy.Probes.Liveness.Path) == "" || svc.Deploy.Probes.Liveness.Port <= 0 {
				report.add(Diagnostic{
					Severity: SeverityError,
					Code:     "deploy.liveness",
					Path:     prefix + ".probes.liveness",
					Message:  "liveness probe requires path and port",
					Line:     deployPos.line,
					Column:   deployPos.column,
				})
			}
		}

		if svc.Deploy.Resources == nil || len(svc.Deploy.Resources.Requests) == 0 || len(svc.Deploy.Resources.Limits) == 0 {
			report.add(Diagnostic{
				Severity: SeverityError,
				Code:     "deploy.resources",
				Path:     prefix + ".resources",
				Message:  "resources.requests and resources.limits are required",
				Line:     deployPos.line,
				Column:   deployPos.column,
			})
		}
	}
}
