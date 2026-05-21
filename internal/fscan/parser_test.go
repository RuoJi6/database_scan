package fscan

import "testing"

func TestParseLineOldFormat(t *testing.T) {
	cases := []struct {
		line   string
		dbType string
		host   string
		port   int
		user   string
		pass   string
	}{
		{"[+] mysql 10.211.55.16:33060:root ProdPass_2026!", "mysql", "10.211.55.16", 33060, "root", "ProdPass_2026!"},
		{"[+] mssql 10.211.55.16:14330:sa ProdPass_2026!Strong", "mssql", "10.211.55.16", 14330, "sa", "ProdPass_2026!Strong"},
		{"[+] Postgres:10.211.55.16:54320:appuser ProdPass_2026!", "postgres", "10.211.55.16", 54320, "appuser", "ProdPass_2026!"},
	}
	for _, tc := range cases {
		got, ok := ParseLine(tc.line, 1)
		if !ok {
			t.Fatalf("expected parse success for %q", tc.line)
		}
		if got.Type != tc.dbType || got.Host != tc.host || got.Port != tc.port || got.User != tc.user || got.Password != tc.pass {
			t.Fatalf("unexpected parse result: %#v", got)
		}
	}
}

func TestParseLineNewFormat(t *testing.T) {
	cases := []struct {
		line   string
		dbType string
		host   string
		port   int
		user   string
		pass   string
	}{
		{"[SUCCESS] MySQL 10.211.55.16:33060 root:ProdPass_2026!", "mysql", "10.211.55.16", 33060, "root", "ProdPass_2026!"},
		{"[VULN] PostgreSQL 10.211.55.16:54320 appuser:ProdPass_2026!", "postgres", "10.211.55.16", 54320, "appuser", "ProdPass_2026!"},
		{"MSSQL 10.211.55.16:14330 sa:ProdPass_2026!Strong", "mssql", "10.211.55.16", 14330, "sa", "ProdPass_2026!Strong"},
		{"[SUCCESS] Redis 10.211.55.16:63790 ProdPass_2026!", "redis", "10.211.55.16", 63790, "", "ProdPass_2026!"},
	}
	for _, tc := range cases {
		got, ok := ParseLine(tc.line, 1)
		if !ok {
			t.Fatalf("expected parse success for %q", tc.line)
		}
		if got.Type != tc.dbType || got.Host != tc.host || got.Port != tc.port || got.User != tc.user || got.Password != tc.pass {
			t.Fatalf("unexpected parse result: %#v", got)
		}
	}
}

func TestParseLineSavedV2Format(t *testing.T) {
	cases := []struct {
		line   string
		dbType string
		host   string
		port   int
		user   string
		pass   string
	}{
		{"127.0.0.1:33060 mysql root/ProdPass_2026!", "mysql", "127.0.0.1", 33060, "root", "ProdPass_2026!"},
		{"127.0.0.1:54320 postgresql appuser/ProdPass_2026!", "postgres", "127.0.0.1", 54320, "appuser", "ProdPass_2026!"},
		{"127.0.0.1:14330 mssql sa/ProdPass_2026!Strong", "mssql", "127.0.0.1", 14330, "sa", "ProdPass_2026!Strong"},
		{"127.0.0.1:63790 redis root/ProdPass_2026!", "redis", "127.0.0.1", 63790, "", "ProdPass_2026!"},
	}
	for _, tc := range cases {
		got, ok := ParseLine(tc.line, 1)
		if !ok {
			t.Fatalf("expected parse success for %q", tc.line)
		}
		if got.Type != tc.dbType || got.Host != tc.host || got.Port != tc.port || got.User != tc.user || got.Password != tc.pass {
			t.Fatalf("unexpected parse result: %#v", got)
		}
	}
}

func TestParseLineIgnoresNonDatabaseFindings(t *testing.T) {
	if got, ok := ParseLine("[+] SSH 10.0.0.1:22 root:toor", 1); ok {
		t.Fatalf("unexpected parse result: %#v", got)
	}
}

func TestParseTextDeduplicatesAndIgnoresNoise(t *testing.T) {
	targets, err := ParseText(`[SUCCESS] MySQL 10.211.55.16:13306 root:scanpass
[VULN] PostgreSQL 10.211.55.16:15432 audit:scanpass
[SUCCESS] Redis 10.211.55.16:16379 scanpass
[SUCCESS] MySQL 10.211.55.16:13306 root:scanpass
[+] SSH 10.211.55.16:22 root:kaliadmin`)
	if err != nil {
		t.Fatalf("ParseText returned error: %v", err)
	}
	if len(targets) != 3 {
		t.Fatalf("expected 3 unique database targets, got %#v", targets)
	}
	if targets[0].Type != "mysql" || targets[1].Type != "postgres" || targets[2].Type != "redis" {
		t.Fatalf("unexpected target order: %#v", targets)
	}
}

func TestParseTextManualTwoLineTargets(t *testing.T) {
	targets, err := ParseText(`mysql 10.211.55.16:13306
root:scanpass
postgresql 10.211.55.16:15432
audit:scanpass
redis 10.211.55.16:16379
scanpass`)
	if err != nil {
		t.Fatalf("ParseText returned error: %v", err)
	}
	if len(targets) != 3 {
		t.Fatalf("expected 3 manual targets, got %#v", targets)
	}
	if targets[0].Type != "mysql" || targets[0].User != "root" || targets[0].Password != "scanpass" {
		t.Fatalf("unexpected mysql target: %#v", targets[0])
	}
	if targets[1].Type != "postgres" || targets[1].Port != 15432 || targets[1].User != "audit" {
		t.Fatalf("unexpected postgres target: %#v", targets[1])
	}
	if targets[2].Type != "redis" || targets[2].User != "" || targets[2].Password != "scanpass" {
		t.Fatalf("unexpected redis target: %#v", targets[2])
	}
}

func TestParseTextManualTargetsAcceptAdapterAliases(t *testing.T) {
	targets, err := ParseText(`opengauss 10.211.55.16:15432
audit:scanpass
oceanbase 10.211.55.16:13306
root:scanpass
kingbase 10.211.55.16:15433
system:scanpass
go-ora 10.211.55.16:11521
system:scanpass`)
	if err != nil {
		t.Fatalf("ParseText returned error: %v", err)
	}
	if len(targets) != 4 {
		t.Fatalf("expected 4 manual alias targets, got %#v", targets)
	}
	if targets[0].Type != "opengauss" || targets[1].Type != "oceanbase" || targets[2].Type != "kingbase" || targets[3].Type != "oracle" {
		t.Fatalf("unexpected target types: %#v", targets)
	}
}
