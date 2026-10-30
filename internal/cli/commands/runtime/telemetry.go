package runtimecmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/scottcrooks/mono/internal/cli/output"
)

type telemetryOptions struct {
	context, storageClass, storageSize string
	check, help                        bool
	ports                              telemetryPorts
	serviceName, project               string
}

type telemetryCommand struct{}

func init() { registerCommand("telemetry", &telemetryCommand{}) }

func (*telemetryCommand) Run(args []string) error {
	if len(args) < 3 {
		printTelemetryUsage()
		return fmt.Errorf("telemetry: missing subcommand")
	}
	if args[2] == "help" || args[2] == "--help" || args[2] == "-h" {
		printTelemetryUsage()
		return nil
	}
	opts, err := parseTelemetryOptions(args[2], args[3:])
	if err != nil {
		return err
	}
	if opts.help {
		printTelemetryUsage()
		return nil
	}
	if args[2] == "env" {
		return writeTelemetryEnv(os.Stdout, opts)
	}
	cfg, err := loadTelemetryConfig()
	if err != nil {
		return err
	}
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signalCtx, 7*time.Minute)
	defer cancel()
	kube := telemetryKube{run: runTelemetryKubectl}
	selected, err := kube.selectContext(ctx, opts.context, cfg.Context)
	if err != nil {
		return err
	}
	switch args[2] {
	case "connect":
		if err := kube.checkInstallation(ctx, selected, opts); err != nil {
			return err
		}
		return connectTelemetry(signalCtx, selected, opts.ports)
	case "down":
		return kube.down(ctx, selected)
	case "status":
		return kube.status(ctx, selected)
	case "doctor", "up":
		if opts.check {
			err = kube.checkInstallation(ctx, selected, opts)
		} else {
			err = kube.install(ctx, selected, opts, cfg)
		}
	}
	if err != nil {
		return err
	}
	output.DefaultPrinter().StepOK("telemetry", "Healthy installation in context "+selected+"; run mono telemetry connect")
	return nil
}

func ensureTelemetryForDev() (string, error) {
	cfg, err := loadTelemetryConfig()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()
	kube := telemetryKube{run: runTelemetryKubectl}
	selected, err := kube.selectContext(ctx, "", cfg.Context)
	if err != nil {
		return "", err
	}
	if err := kube.checkInstallation(ctx, selected, telemetryOptions{}); err != nil {
		return "", err
	}
	return connectTelemetryEndpoint(ctx, selected, defaultTelemetryPorts, true)
}

var ensureTelemetryForDevHook = ensureTelemetryForDev

func parseTelemetryOptions(sub string, args []string) (telemetryOptions, error) {
	var o telemetryOptions
	f := flag.NewFlagSet("telemetry "+sub, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.BoolVar(&o.help, "help", false, "Show help")
	f.BoolVar(&o.help, "h", false, "Show help")
	switch sub {
	case "doctor", "up":
		f.StringVar(&o.storageClass, "storage-class", "", "StorageClass")
		f.StringVar(&o.storageSize, "storage-size", "", "Positive integer Mi or Gi")
		if sub == "doctor" {
			f.BoolVar(&o.check, "check", false, "Inspect without repair")
		}
	case "status", "connect", "down", "env":
	default:
		return o, fmt.Errorf("telemetry: unknown subcommand %q (see mono telemetry --help)", sub)
	}
	if sub != "env" {
		f.StringVar(&o.context, "context", "", "Kubeconfig context")
	}
	if sub == "env" || sub == "connect" {
		o.ports.flags(f)
	}
	if sub == "env" {
		f.StringVar(&o.serviceName, "service-name", "", "Explicit application service identity")
		f.StringVar(&o.project, "project", "", "Explicit service namespace")
	}
	if err := f.Parse(args); err != nil {
		return o, fmt.Errorf("telemetry %s: %w", sub, err)
	}
	if f.NArg() != 0 {
		return o, fmt.Errorf("telemetry %s: unexpected argument %q", sub, f.Arg(0))
	}
	if o.storageSize != "" && !validTelemetryStorageSize(o.storageSize) {
		return o, fmt.Errorf("telemetry storage: --storage-size must be a positive integer followed by Mi or Gi")
	}
	if sub == "env" || sub == "connect" {
		if err := o.ports.validate(); err != nil {
			return o, err
		}
	}
	if err := validateTelemetryIdentity(o); err != nil {
		return o, err
	}
	return o, nil
}

func printTelemetryUsage() {
	p := output.DefaultPrinter()
	p.Summary("mono telemetry - Shared local telemetry (namespace mono-telemetry)")
	p.Summary("Usage: mono telemetry <doctor|up|status|connect|env|down> [options]")
	p.Summary("  doctor [--check]      Check prerequisites and install/repair; --check only inspects")
	p.Summary("  up                    Install/repair and start the shared backend")
	p.Summary("  status                Inspect the selected installation")
	p.Summary("  connect               Start forwarding and return after endpoints are ready")
	p.Summary("  env                   Print application settings without cluster access")
	p.Summary("  down                  Stop the backend and retain storage")
	p.Summary("  --context NAME        Select context for cluster commands")
	p.Summary("  --storage-class NAME  StorageClass for doctor/up")
	p.Summary("  --storage-size SIZE   Positive integer Mi/Gi for doctor/up (default 10Gi)")
	p.Summary("  connect/env ports: --grafana-port, --loki-port, --tempo-port,")
	p.Summary("    --otlp-grpc-port, --otlp-http-port, --prometheus-port (distinct integers 1–65535)")
	p.Summary("  env: --service-name NAME --project NAME (explicit SDK identity)")
}
