package runtimecmd

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"unicode"
)

type telemetryPorts [6]int

var telemetryPortNames = [6]string{"grafana", "loki", "tempo", "otlp-grpc", "otlp-http", "prometheus"}
var defaultTelemetryPorts = telemetryPorts{3000, 3100, 3200, 4317, 4318, 9090}

func (p *telemetryPorts) flags(f *flag.FlagSet) {
	for i, name := range telemetryPortNames {
		f.IntVar(&p[i], name+"-port", defaultTelemetryPorts[i], "Host loopback port")
	}
}

func (p telemetryPorts) validate() error {
	seen := make(map[int]bool)
	for i, port := range p {
		if port < 1 || port > 65535 || seen[port] {
			return fmt.Errorf("telemetry ports: --%s-port=%d must be in 1–65535 and distinct from all other ports", telemetryPortNames[i], port)
		}
		seen[port] = true
	}
	return nil
}

func (p telemetryPorts) url(i int) string { return fmt.Sprintf("http://127.0.0.1:%d", p[i]) }

func writeTelemetryEnv(w io.Writer, o telemetryOptions) error {
	settings := [][2]string{
		{"OTEL_EXPORTER_OTLP_ENDPOINT", o.ports.url(4)},
		{"OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf"},
		{"GRAFANA_URL", o.ports.url(0)},
		{"LOKI_URL", o.ports.url(1)},
		{"TEMPO_URL", o.ports.url(2)},
		{"PROMETHEUS_URL", o.ports.url(5)},
	}
	if o.serviceName != "" {
		settings = append(settings, [2]string{"OTEL_SERVICE_NAME", o.serviceName})
	}
	if o.project != "" {
		settings = append(settings, [2]string{"OTEL_RESOURCE_ATTRIBUTES", "service.namespace=" + o.project + ",deployment.environment.name=local"})
	}
	for _, kv := range settings {
		if _, err := fmt.Fprintf(w, "export %s='%s'\n", kv[0], strings.ReplaceAll(kv[1], "'", "'\"'\"'")); err != nil {
			return fmt.Errorf("telemetry env: %w", err)
		}
	}
	return nil
}

func validateTelemetryIdentity(o telemetryOptions) error {
	if strings.ContainsAny(o.project, ",=") || strings.ContainsFunc(o.project, unicode.IsControl) {
		return fmt.Errorf("telemetry env: --project must not contain comma, equal or control characters")
	}
	if strings.ContainsRune(o.serviceName, 0) {
		return fmt.Errorf("telemetry env: --service-name must not contain NUL")
	}
	return nil
}
