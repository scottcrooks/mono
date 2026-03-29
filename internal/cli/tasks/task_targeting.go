package tasks

import (
	"fmt"

	"github.com/scottcrooks/mono/internal/cli/output"
	"github.com/scottcrooks/mono/internal/cli/selection"
)

func resolveTaskTargetServices(cfg *Config, task TaskName, explicit []string, opts TaskRunOptions) ([]string, bool, error) {
	if len(explicit) > 0 {
		if opts.All {
			return nil, false, fmt.Errorf("--all cannot be combined with explicit service names")
		}
		return explicit, false, nil
	}

	services, err := selection.ResolveTargetServices(cfg, nil, opts.BaseRef, opts.All)
	if err != nil {
		return nil, false, err
	}
	return services, true, nil
}

func printNoTaskTargets(task TaskName, usedAll bool) {
	printer := output.DefaultPrinter()
	if usedAll {
		printer.Summary(fmt.Sprintf("No services configured. Nothing to %s.", task))
		return
	}
	printer.Summary(fmt.Sprintf("No impacted services. Nothing to %s.", task))
}
