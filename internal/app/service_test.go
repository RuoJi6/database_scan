package app

import (
	"testing"

	"database_scan/internal/detector"
)

func TestValidateScanRequestAppliesDefaults(t *testing.T) {
	cfg, err := ValidateScanRequest(ScanRequest{
		Type:     "mysql",
		Host:     "127.0.0.1",
		User:     "root",
		Password: "pass",
	})
	if err != nil {
		t.Fatalf("ValidateScanRequest returned error: %v", err)
	}
	if cfg.Mode != "field-content" || cfg.Level != detector.LevelAll || cfg.Limit != 15 || cfg.Workers != 1 {
		t.Fatalf("defaults were not applied: %#v", cfg)
	}
	if cfg.Port != 3306 {
		t.Fatalf("expected default mysql port, got %d", cfg.Port)
	}
}

func TestValidateScanRequestRejectsSplitOutputWithoutOutput(t *testing.T) {
	_, err := ValidateScanRequest(ScanRequest{Fscan: "fscan.txt", SplitOutput: true})
	if err == nil {
		t.Fatal("expected split-output validation error")
	}
}

func TestValidateScanRequestAllowsFscanWithoutSingleTarget(t *testing.T) {
	cfg, err := ValidateScanRequest(ScanRequest{Fscan: "fscan.txt", Output: "out.xlsx", SplitOutput: true})
	if err != nil {
		t.Fatalf("expected fscan request to validate: %v", err)
	}
	if cfg.Fscan != "fscan.txt" || !cfg.SplitOutput {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestValidateScanRequestAllowsFscanTextWithoutSingleTarget(t *testing.T) {
	cfg, err := ValidateScanRequest(ScanRequest{FscanText: "mysql 127.0.0.1:3306 root:pass"})
	if err != nil {
		t.Fatalf("expected fscan text request to validate: %v", err)
	}
	if cfg.FscanText == "" {
		t.Fatalf("expected fscan text to be preserved")
	}
}

func TestParseFscanTextPreviewDeduplicatesTargets(t *testing.T) {
	preview, err := ParseFscanTextPreview(`mysql 127.0.0.1:3306 root:pass
mysql 127.0.0.1:3306 root:pass
redis 127.0.0.1:6379 pass
ssh 127.0.0.1:22 root:pass`)
	if err != nil {
		t.Fatalf("ParseFscanTextPreview returned error: %v", err)
	}
	if preview.Total != 2 {
		t.Fatalf("expected 2 targets, got %#v", preview)
	}
}

func TestSupportedDatabaseTypesReturnsCanonicalGUIChoices(t *testing.T) {
	types := SupportedDatabaseTypes()
	seen := map[string]bool{}
	for _, typ := range types {
		seen[typ] = true
	}
	for _, typ := range []string{"mysql", "tidb", "oceanbase", "polardb-mysql", "postgres", "opengauss", "gaussdb", "kingbase", "mssql", "oracle", "redis", "mongodb", "elasticsearch", "clickhouse-http", "clickhouse-native"} {
		if !seen[typ] {
			t.Fatalf("SupportedDatabaseTypes missing %q: %#v", typ, types)
		}
	}
	for _, alias := range []string{"postgresql", "sqlserver", "go-ora", "kingbasees", "oceanbase-mysql", "mongo", "elastic", "clickhouse"} {
		if seen[alias] {
			t.Fatalf("GUI type list should not duplicate alias %q: %#v", alias, types)
		}
	}
}

func TestValidateScanRequestAppliesNewTypeDefaults(t *testing.T) {
	tests := []struct {
		name string
		req  ScanRequest
		port int
	}{
		{name: "mongodb", req: ScanRequest{Type: "mongo", Host: "127.0.0.1", User: "root", Password: "pass", AuthDatabase: "admin"}, port: 27017},
		{name: "elasticsearch", req: ScanRequest{Type: "es", Host: "127.0.0.1"}, port: 9200},
		{name: "clickhouse-http", req: ScanRequest{Type: "ch-http", Host: "127.0.0.1", User: "default", Password: "pass"}, port: 8123},
		{name: "clickhouse-native", req: ScanRequest{Type: "clickhouse", Host: "127.0.0.1", User: "default", Password: "pass"}, port: 9000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ValidateScanRequest(tt.req)
			if err != nil {
				t.Fatalf("ValidateScanRequest returned error: %v", err)
			}
			if cfg.Port != tt.port {
				t.Fatalf("expected default port %d, got %d", tt.port, cfg.Port)
			}
			if tt.name == "mongodb" && cfg.AuthDatabase != "admin" {
				t.Fatalf("expected auth database to be preserved, got %#v", cfg)
			}
		})
	}
}

func TestIsQuerySQLClassifiesReadOnlyStatements(t *testing.T) {
	if !isQuerySQL(" SELECT 1") || !isQuerySQL("with q as (select 1) select * from q") || !isQuerySQL("SHOW TABLES") {
		t.Fatal("expected read-style SQL to use Query")
	}
	if isQuerySQL("INSERT INTO t VALUES (1)") || isQuerySQL("UPDATE t SET a = 1") || isQuerySQL("DROP TABLE t") {
		t.Fatal("expected write-style SQL to use Exec")
	}
}
