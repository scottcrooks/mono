package cli

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	hostsBlockBegin = "# BEGIN ARGUS HOSTS"
	hostsBlockEnd   = "# END ARGUS HOSTS"
	hostsDomain     = "argus.local"
	hostsIP         = "127.0.0.1"
)

var (
	hostsFilePath = "/etc/hosts"

	dnsLabelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

type hostsCommand struct{}

func init() {
	registerCommand("hosts", &hostsCommand{})
}

func (c *hostsCommand) Run(args []string) error {
	if len(args) < 3 {
		printHostsUsage()
		return fmt.Errorf("missing hosts subcommand")
	}

	switch args[2] {
	case "sync":
		return runHostsSync(args)
	case "remove":
		return runHostsRemove()
	default:
		printHostsUsage()
		return fmt.Errorf("unknown hosts subcommand: %s", args[2])
	}
}

func runHostsSync(args []string) error {
	config, err := loadConfig()
	if err != nil {
		return err
	}

	services, err := selectServices(config, args[3:])
	if err != nil {
		return err
	}

	entries, err := serviceHostEntries(services)
	if err != nil {
		return err
	}

	managedBlock := buildHostsManagedBlock(entries)
	current, err := os.ReadFile(hostsFilePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", hostsFilePath, err)
	}

	updated, changed, err := upsertManagedHostsBlock(string(current), managedBlock)
	if err != nil {
		return err
	}
	if !changed {
		fmt.Println("hosts block already up to date")
		return nil
	}

	if err := writeHostsFile(updated); err != nil {
		return err
	}

	fmt.Printf("updated %s with %d service host entrie(s)\n", hostsFilePath, len(entries))
	return nil
}

func runHostsRemove() error {
	current, err := os.ReadFile(hostsFilePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", hostsFilePath, err)
	}

	updated, changed, err := removeManagedHostsBlock(string(current))
	if err != nil {
		return err
	}
	if !changed {
		fmt.Println("no managed argus hosts block found")
		return nil
	}

	if err := writeHostsFile(updated); err != nil {
		return err
	}

	fmt.Printf("removed managed argus hosts block from %s\n", hostsFilePath)
	return nil
}

func selectServices(config *Config, requested []string) ([]Service, error) {
	if len(requested) == 0 {
		return config.Services, nil
	}

	seen := make(map[string]bool)
	services := make([]Service, 0, len(requested))
	for _, name := range requested {
		if seen[name] {
			continue
		}
		svc := findService(config, name)
		if svc == nil {
			return nil, fmt.Errorf("unknown service %q", name)
		}
		seen[name] = true
		services = append(services, *svc)
	}
	return services, nil
}

func serviceHostEntries(services []Service) ([]string, error) {
	entries := make([]string, 0, len(services))
	for _, svc := range services {
		if err := validateServiceDNSLabel(svc.Name); err != nil {
			return nil, fmt.Errorf("service %q: %w", svc.Name, err)
		}
		entries = append(entries, fmt.Sprintf("%s %s.%s", hostsIP, svc.Name, hostsDomain))
	}
	return entries, nil
}

func validateServiceDNSLabel(name string) error {
	if !dnsLabelRe.MatchString(name) {
		return fmt.Errorf("invalid DNS label %q", name)
	}
	return nil
}

func buildHostsManagedBlock(entries []string) string {
	lines := make([]string, 0, len(entries)+2)
	lines = append(lines, hostsBlockBegin)
	lines = append(lines, entries...)
	lines = append(lines, hostsBlockEnd)
	return strings.Join(lines, "\n") + "\n"
}

func upsertManagedHostsBlock(existing, managedBlock string) (string, bool, error) {
	start := strings.Index(existing, hostsBlockBegin)
	if start == -1 {
		trimmed := strings.TrimRight(existing, "\n")
		if trimmed == "" {
			return managedBlock, true, nil
		}
		return trimmed + "\n\n" + managedBlock, true, nil
	}

	end, err := findManagedBlockEnd(existing, start)
	if err != nil {
		return "", false, err
	}

	updated := existing[:start] + managedBlock + existing[end:]
	return updated, updated != existing, nil
}

func removeManagedHostsBlock(existing string) (string, bool, error) {
	start := strings.Index(existing, hostsBlockBegin)
	if start == -1 {
		return existing, false, nil
	}

	end, err := findManagedBlockEnd(existing, start)
	if err != nil {
		return "", false, err
	}

	before := strings.TrimRight(existing[:start], "\n")
	after := strings.TrimLeft(existing[end:], "\n")

	switch {
	case before == "" && after == "":
		return "", true, nil
	case before == "":
		if strings.HasSuffix(after, "\n") {
			return after, true, nil
		}
		return after + "\n", true, nil
	case after == "":
		return before + "\n", true, nil
	default:
		if strings.HasSuffix(after, "\n") {
			return before + "\n\n" + after, true, nil
		}
		return before + "\n\n" + after + "\n", true, nil
	}
}

func findManagedBlockEnd(content string, start int) (int, error) {
	searchFrom := start + len(hostsBlockBegin)
	relativeEnd := strings.Index(content[searchFrom:], hostsBlockEnd)
	if relativeEnd == -1 {
		return 0, errors.New("managed argus hosts block start found without end marker")
	}

	end := searchFrom + relativeEnd + len(hostsBlockEnd)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return end, nil
}

func writeHostsFile(content string) error {
	info, err := os.Stat(hostsFilePath)
	if err != nil {
		return fmt.Errorf("stat %s: %w", hostsFilePath, err)
	}

	if err := os.WriteFile(hostsFilePath, []byte(content), info.Mode().Perm()); err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("permission denied writing %s; rerun with sudo (for example: sudo ./bin/mono hosts sync)", hostsFilePath)
		}
		return fmt.Errorf("write %s: %w", hostsFilePath, err)
	}
	return nil
}

func printHostsUsage() {
	fmt.Println("mono hosts - Manage local hosts entries for argus services")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  mono hosts sync [service...]")
	fmt.Println("  mono hosts remove")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  mono hosts sync")
	fmt.Println("  mono hosts sync mallos daedalus")
	fmt.Println("  mono hosts remove")
}
