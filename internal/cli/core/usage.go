package core

import "fmt"

func PrintUsage() {
	fmt.Println("mono - Monorepo orchestration tool")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  mono <command> [service...]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  affected [--base <ref>] [--explain]  List impacted services from git changes")
	fmt.Println("  check [--base <ref>] [--no-cache] [--concurrency N]  Run pending lint/typecheck/test for impacted services")
	fmt.Println("  list                  List all services and their commands")
	fmt.Println("  status [--base <ref>] Show changed/impacted services and planned check tasks")
	fmt.Println("  dev [service...]      Run services with hot reload (concurrent)")
	fmt.Println("  doctor                Check environment and validate services manifest policy")
	fmt.Println("  hosts <subcommand>    Manage local hosts entries (see: mono hosts)")
	fmt.Println("  infra <subcommand>    Manage local infrastructure (see: mono infra)")
	fmt.Println("  metadata              Show generated metadata overview")
	fmt.Println("  migrate [service...]  Apply/create migrations for services")
	fmt.Println("  worktree <subcommand> Manage git worktrees")
	fmt.Println()
	fmt.Println("Orchestrated tasks:")
	fmt.Println("  build|lint|typecheck|test|audit|package|deploy [service...] [--no-cache] [--concurrency N]")
	fmt.Println()
	fmt.Println("Global flags:")
	fmt.Println("  --help, -h            Show help")
	fmt.Println("  --version             Show version")
}
