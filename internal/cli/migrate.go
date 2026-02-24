package cli

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // postgres driver for migrations
	_ "github.com/golang-migrate/migrate/v4/source/file"       // file source for migrations
	_ "github.com/jackc/pgx/v5/stdlib"                         // pgx driver for database/sql
)

type migrateCommand struct{}

func init() {
	registerCommand("migrate", &migrateCommand{})
}

// Run dispatches migrate subcommands.
// Usage: mono migrate <service> <subcommand> [args]
func (c *migrateCommand) Run(args []string) error {
	if len(args) < 4 {
		printMigrateUsage()
		return fmt.Errorf("missing arguments: mono migrate <service> <subcommand>")
	}

	serviceName := args[2]
	subcommand := args[3]

	config, err := loadConfig()
	if err != nil {
		return err
	}

	svc := findService(config, serviceName)
	if svc == nil {
		return fmt.Errorf("unknown service: %s", serviceName)
	}

	absPath, err := filepath.Abs(svc.Path)
	if err != nil {
		return fmt.Errorf("failed to resolve path for %s: %w", serviceName, err)
	}

	migrationsPath := filepath.Join(absPath, "migrations")

	if _, statErr := os.Stat(migrationsPath); os.IsNotExist(statErr) {
		return fmt.Errorf("migrations directory not found: %s", migrationsPath)
	}

	// create subcommand does not need a DSN
	if subcommand == "create" {
		if len(args) < 5 {
			return fmt.Errorf("usage: mono migrate <service> create <name>")
		}
		return migrateCreate(migrationsPath, args[4])
	}

	dsn, err := loadDSN(absPath)
	if err != nil {
		return fmt.Errorf("failed to load DSN: %w", err)
	}

	switch subcommand {
	case "up":
		if err := ensureDatabase(dsn); err != nil {
			return fmt.Errorf("failed to ensure database exists: %w", err)
		}
		return migrateUp(migrationsPath, dsn)
	case "down":
		steps := 1
		if len(args) > 4 {
			n, parseErr := strconv.Atoi(args[4])
			if parseErr != nil {
				return fmt.Errorf("invalid steps value %q: must be an integer", args[4])
			}
			steps = n
		}
		return migrateDown(migrationsPath, dsn, steps)
	case "status":
		return migrateStatus(migrationsPath, dsn)
	default:
		printMigrateUsage()
		return fmt.Errorf("unknown subcommand: %s", subcommand)
	}
}

// loadDSN resolves the database DSN using the following priority:
//  1. MONO_DATABASE_DSN environment variable
//  2. MONO_DATABASE_DSN key in <servicePath>/.env file
func loadDSN(servicePath string) (string, error) {
	if dsn := os.Getenv("MONO_DATABASE_DSN"); dsn != "" {
		return dsn, nil
	}

	envFile := filepath.Join(servicePath, ".env")
	//nolint:gosec // G304: envFile is constructed from a validated service path from services.yaml
	data, err := os.ReadFile(envFile)
	if err != nil {
		return "", fmt.Errorf("MONO_DATABASE_DSN not set and no .env file found at %s", envFile)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		// Strip surrounding quotes
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if key == "MONO_DATABASE_DSN" {
			return val, nil
		}
	}

	return "", fmt.Errorf("MONO_DATABASE_DSN not found in %s", envFile)
}

// ensureDatabase connects to the postgres maintenance database and creates the
// target database if it does not exist. This enables a smooth first-run local
// dev experience without requiring manual database creation.
func ensureDatabase(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("invalid DSN: %w", err)
	}

	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		return fmt.Errorf("no database name found in DSN")
	}

	if !isValidIdentifier(dbName) {
		return fmt.Errorf("unsafe database name in DSN: %q", dbName)
	}

	// Connect to the postgres maintenance database on the same host
	adminURL := *u
	adminURL.Path = "/postgres"
	adminDSN := adminURL.String()

	db, err := sql.Open("pgx", adminDSN)
	if err != nil {
		return fmt.Errorf("failed to open maintenance connection: %w", err)
	}
	defer func() { _ = db.Close() }()

	var exists bool
	row := db.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName)
	if scanErr := row.Scan(&exists); scanErr != nil {
		return fmt.Errorf("failed to query pg_database: %w", scanErr)
	}

	if !exists {
		fmt.Printf("==> [migrate] Creating database %q...\n", dbName)
		//nolint:gosec // dbName is validated by isValidIdentifier above
		if _, execErr := db.Exec(fmt.Sprintf(`CREATE DATABASE "%s"`, dbName)); execErr != nil {
			return fmt.Errorf("failed to create database %q: %w", dbName, execErr)
		}
		fmt.Printf("✓ Database %q created\n", dbName)
	}

	return nil
}

// isValidIdentifier returns true if s contains only alphanumeric characters,
// underscores, or hyphens — safe for use as a quoted SQL identifier.
func isValidIdentifier(s string) bool {
	if s == "" {
		return false
	}
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_\-]+$`, s)
	return matched
}

func migrateUp(migrationsPath, dsn string) error {
	m, err := newMigrate(migrationsPath, dsn)
	if err != nil {
		return err
	}
	defer closeMigrate(m)

	fmt.Println("==> [migrate] Applying pending migrations...")
	if upErr := m.Up(); upErr != nil {
		if errors.Is(upErr, migrate.ErrNoChange) {
			fmt.Println("✓ No new migrations to apply")
			return nil
		}
		return fmt.Errorf("migration up failed: %w", upErr)
	}

	version, dirty, _ := m.Version()
	fmt.Printf("✓ Migrations applied (version %d, dirty: %v)\n", version, dirty)
	return nil
}

func migrateDown(migrationsPath, dsn string, steps int) error {
	m, err := newMigrate(migrationsPath, dsn)
	if err != nil {
		return err
	}
	defer closeMigrate(m)

	fmt.Printf("==> [migrate] Rolling back %d migration(s)...\n", steps)
	if downErr := m.Steps(-steps); downErr != nil {
		if errors.Is(downErr, migrate.ErrNoChange) {
			fmt.Println("✓ Nothing to roll back")
			return nil
		}
		return fmt.Errorf("migration down failed: %w", downErr)
	}

	version, dirty, vErr := m.Version()
	if errors.Is(vErr, migrate.ErrNilVersion) {
		fmt.Println("✓ Rolled back (no migrations applied)")
	} else {
		fmt.Printf("✓ Rolled back (version %d, dirty: %v)\n", version, dirty)
	}
	return nil
}

func migrateStatus(migrationsPath, dsn string) error {
	m, err := newMigrate(migrationsPath, dsn)
	if err != nil {
		return err
	}
	defer closeMigrate(m)

	version, dirty, vErr := m.Version()
	if errors.Is(vErr, migrate.ErrNilVersion) {
		fmt.Println("Status: no migrations applied")
		return nil
	}
	if vErr != nil {
		return fmt.Errorf("failed to get migration version: %w", vErr)
	}

	fmt.Printf("Current version : %d\n", version)
	fmt.Printf("Dirty           : %v\n", dirty)
	return nil
}

// migrateCreate generates a new NNN_name.{up,down}.sql file pair in migrationsPath.
// The sequence number is one higher than the current maximum.
func migrateCreate(migrationsPath, name string) error {
	// Normalise name
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")

	if !isValidIdentifier(name) {
		return fmt.Errorf("invalid migration name %q: use only alphanumeric characters, underscores, or hyphens", name)
	}

	entries, err := os.ReadDir(migrationsPath)
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	seqPattern := regexp.MustCompile(`^(\d+)_`)
	highest := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := seqPattern.FindStringSubmatch(entry.Name())
		if len(matches) < 2 {
			continue
		}
		n, parseErr := strconv.Atoi(matches[1])
		if parseErr != nil {
			continue
		}
		if n > highest {
			highest = n
		}
	}

	next := highest + 1
	prefix := fmt.Sprintf("%03d_%s", next, name)
	upFile := filepath.Join(migrationsPath, prefix+".up.sql")
	downFile := filepath.Join(migrationsPath, prefix+".down.sql")

	upContent := fmt.Sprintf("-- Migration %03d: %s\n-- TODO: Add your up migration SQL here\n", next, name)
	downContent := fmt.Sprintf("-- Migration %03d: %s (rollback)\n-- TODO: Add your down migration SQL here\n", next, name)

	if writeErr := os.WriteFile(upFile, []byte(upContent), 0600); writeErr != nil {
		return fmt.Errorf("failed to create up migration file: %w", writeErr)
	}
	if writeErr := os.WriteFile(downFile, []byte(downContent), 0600); writeErr != nil {
		return fmt.Errorf("failed to create down migration file: %w", writeErr)
	}

	fmt.Println("✓ Created migration files:")
	fmt.Printf("  %s\n", upFile)
	fmt.Printf("  %s\n", downFile)
	return nil
}

// newMigrate creates a golang-migrate instance for the given migrations directory and DSN.
func newMigrate(migrationsPath, dsn string) (*migrate.Migrate, error) {
	m, err := migrate.New(fmt.Sprintf("file://%s", migrationsPath), dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to create migrate instance: %w", err)
	}
	return m, nil
}

// closeMigrate closes a migrate instance, logging any errors.
func closeMigrate(m *migrate.Migrate) {
	srcErr, dbErr := m.Close()
	if srcErr != nil {
		fmt.Fprintf(os.Stderr, "migrate: source close error: %v\n", srcErr)
	}
	if dbErr != nil {
		fmt.Fprintf(os.Stderr, "migrate: db close error: %v\n", dbErr)
	}
}

func printMigrateUsage() {
	fmt.Println("mono migrate - Database migration management")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  mono migrate <service> <subcommand> [args]")
	fmt.Println()
	fmt.Println("Subcommands:")
	fmt.Println("  up              Apply all pending migrations")
	fmt.Println("  down [N]        Roll back N migrations (default: 1)")
	fmt.Println("  status          Show current migration version and dirty state")
	fmt.Println("  create <name>   Create a new migration file pair")
	fmt.Println()
	fmt.Println("DSN Resolution (priority order):")
	fmt.Println("  1. MONO_DATABASE_DSN environment variable")
	fmt.Println("  2. MONO_DATABASE_DSN=... in <service_path>/.env")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  mono migrate pythia up")
	fmt.Println("  mono migrate pythia down 2")
	fmt.Println("  mono migrate pythia status")
	fmt.Println("  mono migrate pythia create add_user_table")
}
