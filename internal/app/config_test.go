package app

import "testing"

func TestParseArgsAcceptsHostPortInHostFlag(t *testing.T) {
	cfg, err := parseArgs([]string{
		"--type", "mssql",
		"--host", "192.0.2.10:1433",
		"--user", "sa",
		"--password", "secret",
	})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if cfg.Host != "192.0.2.10" || cfg.Port != 1433 {
		t.Fatalf("unexpected target: %s:%d", cfg.Host, cfg.Port)
	}
}

func TestParseArgsAcceptsHostPortAsPositionalTarget(t *testing.T) {
	cfg, err := parseArgs([]string{
		"--type", "mysql",
		"198.51.100.10:3307",
		"--user", "root",
		"--password", "secret",
	})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if cfg.Host != "198.51.100.10" || cfg.Port != 3307 {
		t.Fatalf("unexpected target: %s:%d", cfg.Host, cfg.Port)
	}
}

func TestParseArgsDefaultsPortWhenTargetHasNoPort(t *testing.T) {
	cfg, err := parseArgs([]string{
		"--type", "postgres",
		"--host", "203.0.113.10",
		"--user", "dev",
		"--password", "secret",
	})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if cfg.Host != "203.0.113.10" || cfg.Port != 5432 {
		t.Fatalf("unexpected target: %s:%d", cfg.Host, cfg.Port)
	}
}

func TestParseArgsRejectsConflictingPorts(t *testing.T) {
	_, err := parseArgs([]string{
		"--type", "mssql",
		"--host", "192.0.2.10:1433",
		"--port", "1444",
		"--user", "sa",
		"--password", "secret",
	})
	if err == nil {
		t.Fatal("expected conflicting ports error")
	}
}

func TestParseArgsAcceptsBracketedIPv6HostPort(t *testing.T) {
	cfg, err := parseArgs([]string{
		"--type", "postgres",
		"--host", "[::1]:5433",
		"--user", "dev",
		"--password", "secret",
	})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if cfg.Host != "::1" || cfg.Port != 5433 {
		t.Fatalf("unexpected target: %s:%d", cfg.Host, cfg.Port)
	}
}

func TestParseArgsNoColor(t *testing.T) {
	cfg, err := parseArgs([]string{
		"--type", "mssql",
		"--host", "192.0.2.10",
		"--user", "sa",
		"--password", "secret",
		"--no-color",
	})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if !cfg.NoColor {
		t.Fatal("expected --no-color to be enabled")
	}
}

func TestParseArgsAcceptsDatabaseTable(t *testing.T) {
	cfg, err := parseArgs([]string{
		"--type", "mssql",
		"--host", "192.0.2.10",
		"--user", "sa",
		"--password", "secret",
		"--database", "appdb",
		"--table", "dbo.Users",
	})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if cfg.Database != "appdb" || cfg.Table != "dbo.Users" {
		t.Fatalf("unexpected database/table: %s / %s", cfg.Database, cfg.Table)
	}
}

func TestParseArgsRejectsTableWithoutDatabase(t *testing.T) {
	_, err := parseArgs([]string{
		"--type", "mssql",
		"--host", "192.0.2.10",
		"--user", "sa",
		"--password", "secret",
		"--table", "Users",
	})
	if err == nil {
		t.Fatal("expected --table without --database to be rejected")
	}
}

func TestScanDatabasesUsesSpecifiedDatabaseOnly(t *testing.T) {
	got := scanDatabases([]string{"app", "audit"}, "target")
	if len(got) != 1 || got[0] != "target" {
		t.Fatalf("unexpected databases: %#v", got)
	}
}

func TestScanDatabasesSortsAllWhenDatabaseIsNotSpecified(t *testing.T) {
	got := scanDatabases([]string{"z", "a"}, "")
	want := []string{"a", "z"}
	if len(got) != len(want) {
		t.Fatalf("unexpected length: %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected databases: %#v", got)
		}
	}
}
