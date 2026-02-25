package cli

import (
	"fmt"
	"strings"
)

type affectedCommand struct{}

func init() {
	registerCommand("affected", &affectedCommand{})
}

func (c *affectedCommand) Run(args []string) error {
	baseRef, explain, err := parseAffectedArgs(args[2:])
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	report, err := buildImpactReport(cfg, baseRef, explain)
	if err != nil {
		return err
	}
	if len(report.Impacted) == 0 {
		fmt.Println("No affected projects.")
		return nil
	}

	if explain {
		for _, svc := range report.Impacted {
			chains := report.Explain[svc]
			if len(chains) == 0 {
				fmt.Println(svc)
				continue
			}
			for _, chain := range chains {
				fmt.Println(chain)
			}
		}
		return nil
	}

	for _, svc := range report.Impacted {
		fmt.Println(svc)
	}
	return nil
}

func parseAffectedArgs(args []string) (baseRef string, explain bool, err error) {
	sawBase := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--explain":
			explain = true
		case arg == "--base":
			sawBase = true
			if i+1 >= len(args) {
				return "", false, fmt.Errorf("--base requires a value")
			}
			baseRef = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(arg, "--base="):
			sawBase = true
			baseRef = strings.TrimSpace(strings.TrimPrefix(arg, "--base="))
		default:
			return "", false, fmt.Errorf("unknown argument %q (usage: mono affected [--base <ref>] [--explain])", arg)
		}
	}
	if sawBase && strings.TrimSpace(baseRef) == "" {
		return "", false, fmt.Errorf("--base requires a non-empty value")
	}
	return baseRef, explain, nil
}
