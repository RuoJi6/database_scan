package app

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"database_scan/internal/db"
	"database_scan/internal/detector"
)

func TestParseArgsHelp(t *testing.T) {
	_, err := parseArgs([]string{"-h"})
	if !errors.Is(err, errHelp) {
		t.Fatalf("expected help error, got %v", err)
	}
}

func TestPrintHelpIncludesProjectAndColor(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf, true, true)
	body := buf.String()
	if !strings.Contains(body, projectName) || !strings.Contains(body, projectURL) {
		t.Fatalf("help missing project metadata: %s", body)
	}
	if !strings.Contains(body, "github.com/RuoJi6/database_scan") {
		t.Fatalf("help missing banner metadata: %s", body)
	}
	if !strings.Contains(body, "\x1b[") {
		t.Fatalf("expected colored help output: %q", body)
	}
}

func TestPrintHelpCanDisableColor(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf, false, true)
	body := buf.String()
	if strings.Contains(body, "\x1b[") {
		t.Fatalf("expected plain help output: %q", body)
	}
	if !strings.Contains(body, "--no-color") {
		t.Fatalf("help missing --no-color option: %s", body)
	}
	if !strings.Contains(body, "--no-banner") {
		t.Fatalf("help missing --no-banner option: %s", body)
	}
	if !strings.Contains(body, "--no-progress") {
		t.Fatalf("help missing --no-progress option: %s", body)
	}
	if !strings.Contains(body, "--fscan") {
		t.Fatalf("help missing --fscan option: %s", body)
	}
	if !strings.Contains(body, "--split-output") {
		t.Fatalf("help missing --split-output option: %s", body)
	}
}

func TestPrintHelpCanDisableBanner(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf, false, false)
	body := buf.String()
	if !strings.HasPrefix(body, projectName+"\n") {
		t.Fatalf("expected help without banner: %s", body)
	}
	if !strings.Contains(body, "Usage") {
		t.Fatalf("help missing usage: %s", body)
	}
}

func TestHelpColorEnabledHonorsNoColorFlag(t *testing.T) {
	if helpColorEnabled([]string{"--no-color", "-h"}) {
		t.Fatal("expected --no-color to disable help colors")
	}
}

func TestBannerEnabledHonorsNoBannerFlag(t *testing.T) {
	if bannerEnabled([]string{"--no-banner", "-h"}) {
		t.Fatal("expected --no-banner to disable help banner")
	}
}

func TestPrintBanner(t *testing.T) {
	var colored bytes.Buffer
	printBanner(&colored, true)
	coloredBody := colored.String()
	if !strings.Contains(coloredBody, projectName) || !strings.Contains(coloredBody, "github.com/RuoJi6/database_scan") {
		t.Fatalf("banner missing project metadata: %s", coloredBody)
	}
	if !strings.Contains(coloredBody, "\x1b[") {
		t.Fatalf("expected colored banner output: %q", coloredBody)
	}

	var plain bytes.Buffer
	printBanner(&plain, false)
	if strings.Contains(plain.String(), "\x1b[") {
		t.Fatalf("expected plain banner output: %q", plain.String())
	}
}

func TestBannerTemplates(t *testing.T) {
	banners := bannerTemplates()
	if len(banners) < 10 {
		t.Fatalf("expected at least 10 banner templates, got %d", len(banners))
	}
	for _, banner := range banners {
		if !strings.Contains(banner, projectName) && !strings.Contains(banner, "database_scan") {
			t.Fatalf("banner missing project name: %s", banner)
		}
	}
}

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

func TestParseArgsDefaultsPortForAliases(t *testing.T) {
	cases := map[string]int{
		"oceanbase": 3306,
		"opengauss": 5432,
		"kingbase":  5432,
		"oracle":    1521,
	}
	for kind, wantPort := range cases {
		cfg, err := parseArgs([]string{
			"--type", kind,
			"--host", "203.0.113.10",
			"--user", "dev",
			"--password", "secret",
		})
		if err != nil {
			t.Fatalf("%s: parseArgs returned error: %v", kind, err)
		}
		if cfg.Port != wantPort {
			t.Fatalf("%s: expected port %d, got %d", kind, wantPort, cfg.Port)
		}
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

func TestParseArgsNoProgress(t *testing.T) {
	cfg, err := parseArgs([]string{
		"--type", "mssql",
		"--host", "192.0.2.10",
		"--user", "sa",
		"--password", "secret",
		"--no-progress",
	})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if !cfg.NoProgress {
		t.Fatal("expected --no-progress to be enabled")
	}
}

func TestParseArgsDefaultsWorkersToSingleThread(t *testing.T) {
	cfg, err := parseArgs([]string{
		"--type", "mssql",
		"--host", "192.0.2.10",
		"--user", "sa",
		"--password", "secret",
	})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if cfg.Workers != 1 {
		t.Fatalf("expected default workers=1, got %d", cfg.Workers)
	}
}

func TestParseArgsAcceptsWorkers(t *testing.T) {
	cfg, err := parseArgs([]string{
		"--type", "mssql",
		"--host", "192.0.2.10",
		"--user", "sa",
		"--password", "secret",
		"--workers", "6",
	})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if cfg.Workers != 6 {
		t.Fatalf("expected workers=6, got %d", cfg.Workers)
	}
}

func TestParseArgsAcceptsFscanWithoutSingleTarget(t *testing.T) {
	cfg, err := parseArgs([]string{
		"--fscan", "result.txt",
		"--password", "should-not-be-required",
	})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if cfg.Fscan != "result.txt" {
		t.Fatalf("unexpected fscan path: %s", cfg.Fscan)
	}
}

func TestParseArgsAcceptsRedisWithoutUserOrPassword(t *testing.T) {
	cfg, err := parseArgs([]string{
		"--type", "redis",
		"--host", "192.0.2.10",
	})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if cfg.Port != 6379 {
		t.Fatalf("expected redis default port, got %d", cfg.Port)
	}
}

func TestParseArgsAcceptsExplicitEmptyPassword(t *testing.T) {
	cfg, err := parseArgs([]string{
		"--type", "mysql",
		"--host", "192.0.2.10",
		"--user", "root",
		"--password", "",
	})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if cfg.Password != "" {
		t.Fatalf("expected empty password to be preserved, got %q", cfg.Password)
	}
}

func TestParseArgsAcceptsSplitOutputWithOutput(t *testing.T) {
	cfg, err := parseArgs([]string{
		"--fscan", "result.txt",
		"--output", "all.xlsx",
		"--split-output",
	})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if !cfg.SplitOutput {
		t.Fatalf("expected split output to be enabled")
	}
}

func TestParseArgsRejectsSplitOutputWithoutOutput(t *testing.T) {
	_, err := parseArgs([]string{
		"--fscan", "result.txt",
		"--split-output",
	})
	if err == nil {
		t.Fatalf("expected split-output without output to fail")
	}
}

func TestParseArgsAcceptsLevel(t *testing.T) {
	cfg, err := parseArgs([]string{
		"--type", "mssql",
		"--host", "192.0.2.10",
		"--user", "sa",
		"--password", "secret",
		"--level", "high",
	})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if cfg.Level != detector.LevelHigh {
		t.Fatalf("unexpected level: %s", cfg.Level)
	}
}

func TestParseArgsRejectsUnknownLevel(t *testing.T) {
	_, err := parseArgs([]string{
		"--type", "mssql",
		"--host", "192.0.2.10",
		"--user", "sa",
		"--password", "secret",
		"--level", "unknown",
	})
	if err == nil {
		t.Fatal("expected unknown level to be rejected")
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
	got := scanDatabases(dbStub{family: "mysql"}, []string{"app", "audit"}, "target")
	if len(got) != 1 || got[0] != "target" {
		t.Fatalf("unexpected databases: %#v", got)
	}
}

func TestScanDatabasesUsesCommaSeparatedDatabases(t *testing.T) {
	got := scanDatabases(dbStub{family: "mysql"}, []string{"app", "audit"}, "app, audit,app")
	want := []string{"app", "audit"}
	if len(got) != len(want) {
		t.Fatalf("unexpected length: %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected databases: %#v", got)
		}
	}
}

func TestConnectionDatabaseIsEmptyForMultipleDatabases(t *testing.T) {
	got := connectionDatabase(dbStub{family: "mysql"}, "app, audit")
	if got != "" {
		t.Fatalf("expected empty initial database for multiple mysql databases, got %q", got)
	}
}

func TestConnectionDatabaseKeepsSingleDatabase(t *testing.T) {
	got := connectionDatabase(dbStub{family: "postgres"}, "app")
	if got != "app" {
		t.Fatalf("expected single initial database to be kept, got %q", got)
	}
}

func TestScanDatabasesSortsAllWhenDatabaseIsNotSpecified(t *testing.T) {
	got := scanDatabases(dbStub{family: "mysql"}, []string{"z", "a"}, "")
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

func TestScanDatabasesKeepsAllOracleSchemasWhenDatabaseIsServiceName(t *testing.T) {
	got := scanDatabases(dbStub{family: "oracle"}, []string{"Z", "A"}, "ORCL")
	want := []string{"A", "Z"}
	if len(got) != len(want) {
		t.Fatalf("unexpected length: %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected oracle schemas: %#v", got)
		}
	}
}

type dbStub struct {
	family string
}

func (s dbStub) Name() string                                                       { return s.family }
func (s dbStub) Family() string                                                     { return s.family }
func (s dbStub) DisplayName() string                                                { return s.family }
func (s dbStub) DefaultPort() int                                                   { return 0 }
func (s dbStub) NeedsDatabaseReconnect() bool                                       { return false }
func (s dbStub) Open(context.Context, db.Config, db.ContextDialer) (*sql.DB, error) { return nil, nil }
func (s dbStub) ServerInfo(context.Context, *sql.DB, db.Config) (db.ServerInfo, error) {
	return db.ServerInfo{}, nil
}
func (s dbStub) ListDatabases(context.Context, *sql.DB, bool) ([]string, error) { return nil, nil }
func (s dbStub) ListColumns(context.Context, *sql.DB, string, bool) ([]db.Column, error) {
	return nil, nil
}
func (s dbStub) QuoteIdent(...string) string                        { return "" }
func (s dbStub) CountNonEmptySQL(db.Column) string                  { return "" }
func (s dbStub) CountTableSQL(db.Column) string                     { return "" }
func (s dbStub) SampleNonEmptySQL(db.Column, int) string            { return "" }
func (s dbStub) SampleRowsSQL([]db.Column, []db.Column, int) string { return "" }
func (s dbStub) ContentRegexSQL(db.Column, string) (string, []any)  { return "", nil }
