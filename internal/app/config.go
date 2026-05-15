package app

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"database_scan/internal/db"
	"database_scan/internal/detector"

	"golang.org/x/term"
)

type Config struct {
	Type          string
	Host          string
	Port          int
	User          string
	Password      string
	Database      string
	Table         string
	Proxy         string
	Mode          string
	Level         detector.Level
	Limit         int
	SQL           string
	Output        string
	IncludeSystem bool
	Mask          bool
	NoColor       bool
	Workers       int
	Timeout       time.Duration
}

func parseArgs(args []string) (Config, error) {
	cfg := Config{Mode: "field-content", Level: detector.LevelAll, Limit: 15, Workers: 4, Timeout: 15 * time.Second}
	args, target := splitTargetArg(args)
	fs := flag.NewFlagSet("database_scan", flag.ContinueOnError)
	fs.StringVar(&cfg.Type, "type", "", "database type: mysql, mssql, postgres")
	fs.StringVar(&cfg.Host, "host", "", "database host")
	fs.IntVar(&cfg.Port, "port", 0, "database port")
	fs.StringVar(&cfg.User, "user", "", "database username")
	fs.StringVar(&cfg.Password, "password", "", "database password")
	fs.StringVar(&cfg.Database, "database", "", "initial database")
	fs.StringVar(&cfg.Table, "table", "", "scan only this table; supports table or schema.table")
	fs.StringVar(&cfg.Proxy, "proxy", "", "proxy url: socks5://user:pass@host:port or http://user:pass@host:port")
	fs.StringVar(&cfg.Mode, "mode", "field-content", "scan mode: field-content, field-name, content, all")
	level := string(cfg.Level)
	fs.StringVar(&level, "level", string(detector.LevelAll), "sensitive level: all, high, medium, low")
	fs.IntVar(&cfg.Limit, "limit", 15, "max sample rows to display")
	fs.StringVar(&cfg.SQL, "sql", "", "custom SQL to execute")
	fs.StringVar(&cfg.Output, "output", "", "write scan result to .xlsx file")
	fs.BoolVar(&cfg.IncludeSystem, "include-system", false, "include system databases")
	fs.BoolVar(&cfg.Mask, "mask", false, "mask sensitive sample values")
	fs.BoolVar(&cfg.NoColor, "no-color", false, "disable colored output")
	fs.IntVar(&cfg.Workers, "workers", 4, "scan workers")
	fs.DurationVar(&cfg.Timeout, "timeout", 15*time.Second, "single query timeout")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if cfg.Host == "" {
		if target != "" {
			cfg.Host = target
		} else if fs.NArg() > 0 {
			cfg.Host = fs.Arg(0)
		}
	}
	cfg.Type = strings.ToLower(strings.TrimSpace(cfg.Type))
	cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	parsedLevel, ok := detector.ParseLevel(level)
	if !ok {
		return cfg, fmt.Errorf("unsupported --level %q", level)
	}
	cfg.Level = parsedLevel
	if err := normalizeTarget(&cfg); err != nil {
		return cfg, err
	}
	if cfg.Type == "" || cfg.Host == "" || cfg.User == "" {
		return cfg, fmt.Errorf("--type, --host and --user are required")
	}
	if cfg.Port == 0 {
		adapter, err := db.NewAdapter(cfg.Type)
		if err != nil {
			return cfg, err
		}
		cfg.Port = adapter.DefaultPort()
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
	if strings.TrimSpace(cfg.Table) != "" && strings.TrimSpace(cfg.Database) == "" {
		return cfg, fmt.Errorf("--table requires --database")
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

func normalizeTarget(cfg *Config) error {
	cfg.Host = strings.TrimSpace(cfg.Host)
	if cfg.Host == "" {
		return nil
	}
	host, portText, err := net.SplitHostPort(cfg.Host)
	if err != nil {
		if strings.Contains(err.Error(), "missing port in address") {
			return nil
		}
		if net.ParseIP(cfg.Host) != nil {
			return nil
		}
		return fmt.Errorf("parse --host: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return fmt.Errorf("parse --host: invalid port %q", portText)
	}
	if cfg.Port != 0 && cfg.Port != port {
		return fmt.Errorf("--host port %d conflicts with --port %d", port, cfg.Port)
	}
	cfg.Host = host
	cfg.Port = port
	return nil
}

func splitTargetArg(args []string) ([]string, string) {
	valueFlags := map[string]bool{
		"type": true, "host": true, "port": true, "user": true, "password": true,
		"database": true, "table": true, "proxy": true, "mode": true, "level": true, "limit": true, "output": true, "workers": true,
		"timeout": true, "sql": true,
	}
	out := make([]string, 0, len(args))
	var target string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			out = append(out, args[i:]...)
			break
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			out = append(out, arg)
			name := strings.TrimLeft(arg, "-")
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				name = name[:eq]
			}
			if valueFlags[name] && !strings.Contains(arg, "=") && i+1 < len(args) {
				i++
				out = append(out, args[i])
			}
			continue
		}
		if target == "" {
			target = arg
			continue
		}
		out = append(out, arg)
	}
	return out, target
}
