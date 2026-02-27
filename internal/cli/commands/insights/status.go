package insights

import (
	"fmt"
	"strings"

	"github.com/scottcrooks/mono/internal/cli/output"
)

type statusCommand struct{}

func init() {
	registerCommand("status", &statusCommand{})
}

func (c *statusCommand) Run(args []string) error {
	printer := output.DefaultPrinter()

	baseRef, err := parseStatusArgs(args[2:])
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	report, err := buildImpactReport(cfg, baseRef, false)
	if err != nil {
		return err
	}
	rows := buildCheckTaskPreview(cfg, report.Impacted)

	printStatusSection(printer, "Changed services", report.Changed)
	printer.Blank()
	printStatusSection(printer, "Impacted services", report.Impacted)
	printer.Blank()
	printCheckTaskSection(printer, rows)
	return nil
}

func parseStatusArgs(args []string) (baseRef string, err error) {
	sawBase := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--base":
			sawBase = true
			if i+1 >= len(args) {
				return "", fmt.Errorf("--base requires a value")
			}
			baseRef = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(arg, "--base="):
			sawBase = true
			baseRef = strings.TrimSpace(strings.TrimPrefix(arg, "--base="))
		default:
			return "", fmt.Errorf("unknown argument %q (usage: mono status [--base <ref>])", arg)
		}
	}
	if sawBase && strings.TrimSpace(baseRef) == "" {
		return "", fmt.Errorf("--base requires a non-empty value")
	}
	return baseRef, nil
}

func printStatusSection(printer output.Printer, title string, items []string) {
	printer.Summary(title + ":")
	if len(items) == 0 {
		printer.Summary("  (none)")
		return
	}
	for _, item := range items {
		printer.Summary("  - " + item)
	}
}

func printCheckTaskSection(printer output.Printer, rows []CheckTaskPreview) {
	printer.Summary("Planned check tasks:")
	if len(rows) == 0 {
		printer.Summary("  (none)")
		return
	}
	for _, row := range rows {
		present := "none"
		missing := "none"
		if len(row.Present) > 0 {
			present = strings.Join(row.Present, ", ")
		}
		if len(row.Missing) > 0 {
			missing = strings.Join(row.Missing, ", ")
		}
		printer.Summary(fmt.Sprintf("  - %s: run [%s], skip [%s]", row.Service, present, missing))
	}
}
