package core

import "github.com/scottcrooks/mono/internal/cli/output"

func PrintUsage() {
	p := output.DefaultPrinter()
	p.Summary("mono - Monorepo orchestration tool")
	p.Blank()
	p.Summary("Usage:")
	p.Summary("  mono <command> [service...]")
	p.Blank()
	p.Summary("Commands:")
	p.Summary("  affected [--base <ref>] [--explain]  List impacted services from git changes")
	p.Summary("  check [--base <ref>] [--no-cache] [--concurrency N]  Run pending lint/typecheck/test for impacted services")
	p.Summary("  list                  List all services and their commands")
	p.Summary("  status [--base <ref>] Show changed/impacted services and planned check tasks")
	p.Summary("  dev [service...]      Run services with hot reload (concurrent)")
	p.Summary("  doctor                Check environment and validate services manifest policy")
	p.Summary("  hosts <subcommand>    Manage local hosts entries (see: mono hosts)")
	p.Summary("  infra <subcommand>    Manage local infrastructure (see: mono infra)")
	p.Summary("  metadata              Show generated metadata overview")
	p.Summary("  migrate [service...]  Apply/create migrations for services")
	p.Summary("  worktree <subcommand> Manage git worktrees")
	p.Blank()
	p.Summary("Orchestrated tasks:")
	p.Summary("  build|lint|typecheck|test|audit|package|deploy [service...] [--no-cache] [--concurrency N]")
	p.Blank()
	p.Summary("Global flags:")
	p.Summary("  --help, -h            Show help")
	p.Summary("  --version             Show version")
}
