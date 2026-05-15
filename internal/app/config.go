package app

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

type Config struct {
	Type          string
	Host          string
	Port          int
	User          string
	Password      string
	Database      string
	Proxy         string
	Mode          string
	Limit         int
	SQL           string
	IncludeSystem bool
	Mask          bool
	Workers       int
	Timeout       time.Duration
}

func parseArgs(args []string) (Config, error) {
	cfg := Config{Mode: "field-content", Limit: 15, Workers: 4, Timeout: 15 * time.Second}
	fs := flag.NewFlagSet("database_scan", flag.ContinueOnError)
	fs.StringVar(&cfg.Type, "type", "", "database type: mysql, mssql, postgres")
	fs.StringVar(&cfg.Host, "host", "", "database host")
	fs.IntVar(&cfg.Port, "port", 0, "database port")
	fs.StringVar(&cfg.User, "user", "", "database username")
	fs.StringVar(&cfg.Password, "password", "", "database password")
	fs.StringVar(&cfg.Database, "database", "", "initial database")
	fs.StringVar(&cfg.Proxy, "proxy", "", "proxy url: socks5://user:pass@host:port or http://user:pass@host:port")
	fs.StringVar(&cfg.Mode, "mode", "field-content", "scan mode: field-content, field-name, content, all")
	fs.IntVar(&cfg.Limit, "limit", 15, "max sample rows to display")
	fs.StringVar(&cfg.SQL, "sql", "", "custom SQL to execute")
	fs.BoolVar(&cfg.IncludeSystem, "include-system", false, "include system databases")
	fs.BoolVar(&cfg.Mask, "mask", false, "mask sensitive sample values")
	fs.IntVar(&cfg.Workers, "workers", 4, "scan workers")
	fs.DurationVar(&cfg.Timeout, "timeout", 15*time.Second, "single query timeout")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	cfg.Type = strings.ToLower(strings.TrimSpace(cfg.Type))
	cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	if cfg.Port == 0 {
		switch cfg.Type {
		case "mysql":
			cfg.Port = 3306
		case "mssql":
			cfg.Port = 1433
		case "postgres", "postgresql":
			cfg.Port = 5432
		}
	}
	if cfg.Type == "" || cfg.Host == "" || cfg.User == "" {
		return cfg, fmt.Errorf("--type, --host and --user are required")
	}
	if cfg.Password == "" {
		fmt.Fprint(os.Stderr, "Password: ")
		pass, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return cfg, fmt.Errorf("read password: %w", err)
		}
		cfg.Password = string(pass)
	}
	if cfg.Limit <= 0 {
		return cfg, fmt.Errorf("--limit must be greater than 0")
	}
	if cfg.Workers <= 0 {
		return cfg, fmt.Errorf("--workers must be greater than 0")
	}
	switch cfg.Mode {
	case "field-content", "field-name", "content", "all":
	default:
		return cfg, fmt.Errorf("unsupported --mode %q", cfg.Mode)
	}
	return cfg, nil
}
