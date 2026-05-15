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
