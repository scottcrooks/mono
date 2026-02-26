package insights

import (
	"fmt"
	"strings"
)

type statusCommand struct{}

func init() {
	registerCommand("status", &statusCommand{})
}

func (c *statusCommand) Run(args []string) error {
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

	printStatusSection("Changed services", report.Changed)
	fmt.Println()
	printStatusSection("Impacted services", report.Impacted)
	fmt.Println()
	printCheckTaskSection(rows)
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

func printStatusSection(title string, items []string) {
	fmt.Printf("%s:\n", title)
	if len(items) == 0 {
		fmt.Println("  (none)")
		return
	}
	for _, item := range items {
		fmt.Printf("  - %s\n", item)
	}
}

func printCheckTaskSection(rows []CheckTaskPreview) {
	fmt.Println("Planned check tasks:")
	if len(rows) == 0 {
		fmt.Println("  (none)")
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
		fmt.Printf("  - %s: run [%s], skip [%s]\n", row.Service, present, missing)
	}
}
