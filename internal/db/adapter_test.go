package db

import (
	"errors"
	"net"
	"strings"
	"testing"
)

func TestQuoteIdent(t *testing.T) {
	if got := (MySQLAdapter{}).QuoteIdent("db", "ta`ble"); got != "`db`.`ta``ble`" {
		t.Fatalf("mysql quote mismatch: %s", got)
	}
	if got := (PostgresAdapter{}).QuoteIdent("sch", `ta"ble`); got != `"sch"."ta""ble"` {
		t.Fatalf("postgres quote mismatch: %s", got)
	}
	if got := (MSSQLAdapter{}).QuoteIdent("db", "ta]ble"); got != "[db].[ta]]ble]" {
		t.Fatalf("mssql quote mismatch: %s", got)
	}
	if got := (OracleAdapter{}).QuoteIdent("sch", `ta"ble`); got != `"sch"."ta""ble"` {
		t.Fatalf("oracle quote mismatch: %s", got)
	}
}

func TestAdapterSQLGeneration(t *testing.T) {
	col := Column{Database: "app", Schema: "public", Table: "users", Name: "phone"}
	cases := []string{
		(MySQLAdapter{}).SampleNonEmptySQL(col, 15),
		(PostgresAdapter{}).SampleNonEmptySQL(col, 15),
		(MSSQLAdapter{}).SampleNonEmptySQL(col, 15),
		(OracleAdapter{}).SampleNonEmptySQL(col, 15),
	}
	for _, sql := range cases {
		if !strings.Contains(strings.ToLower(sql), "users") || !strings.Contains(strings.ToLower(sql), "phone") {
			t.Fatalf("generated SQL does not reference table/column: %s", sql)
		}
	}
}

func TestSampleRowsSQLGeneration(t *testing.T) {
	selectCols := []Column{
		{Database: "app", Schema: "dbo", Table: "users", Name: "id"},
		{Database: "app", Schema: "dbo", Table: "users", Name: "username"},
		{Database: "app", Schema: "dbo", Table: "users", Name: "password"},
	}
	conditionCols := selectCols[1:]
	cases := []string{
		(MySQLAdapter{}).SampleRowsSQL(selectCols, conditionCols, 3),
		(PostgresAdapter{}).SampleRowsSQL(selectCols, conditionCols, 3),
		(MSSQLAdapter{}).SampleRowsSQL(selectCols, conditionCols, 3),
		(OracleAdapter{}).SampleRowsSQL(selectCols, conditionCols, 3),
	}
	for _, sql := range cases {
		lower := strings.ToLower(sql)
		if !strings.Contains(lower, "id") || !strings.Contains(lower, "username") || !strings.Contains(lower, "password") || !strings.Contains(lower, "users") {
			t.Fatalf("generated row sample SQL missing expected fields: %s", sql)
		}
	}
}

func TestNewAdapterAliases(t *testing.T) {
	cases := map[string]struct {
		family    string
		port      int
		reconnect bool
	}{
		"mysql":            {"mysql", 3306, false},
		"mariadb":          {"mysql", 3306, false},
		"tidb":             {"mysql", 3306, false},
		"oceanbase":        {"mysql", 3306, false},
		"oceanbase-mysql":  {"mysql", 3306, false},
		"polardb-mysql":    {"mysql", 3306, false},
		"doris":            {"mysql", 3306, false},
		"starrocks":        {"mysql", 3306, false},
		"gbase-mysql":      {"mysql", 3306, false},
		"mssql":            {"mssql", 1433, false},
		"sqlserver":        {"mssql", 1433, false},
		"postgres":         {"postgres", 5432, true},
		"postgresql":       {"postgres", 5432, true},
		"opengauss":        {"postgres", 5432, true},
		"gaussdb":          {"postgres", 5432, true},
		"kingbase":         {"postgres", 5432, true},
		"kingbasees":       {"postgres", 5432, true},
		"highgo":           {"postgres", 5432, true},
		"polardb-postgres": {"postgres", 5432, true},
		"oracle":           {"oracle", 1521, false},
		"go-ora":           {"oracle", 1521, false},
	}
	for kind, want := range cases {
		adapter, err := NewAdapter(kind)
		if err != nil {
			t.Fatalf("%s: NewAdapter returned error: %v", kind, err)
		}
		if adapter.Family() != want.family || adapter.DefaultPort() != want.port || adapter.NeedsDatabaseReconnect() != want.reconnect {
			t.Fatalf("%s: unexpected adapter metadata family=%s port=%d reconnect=%v", kind, adapter.Family(), adapter.DefaultPort(), adapter.NeedsDatabaseReconnect())
		}
	}
}

func TestMSSQLTLSHandshakeErrorDetection(t *testing.T) {
	err := errors.New("TLS Handshake failed: cannot read handshake packet: EOF")
	if !isMSSQLTLSHandshakeError(err) {
		t.Fatal("expected TLS handshake error to be detected")
	}
	if isMSSQLTLSHandshakeError(errors.New("login failed for user")) {
		t.Fatal("unexpected TLS handshake detection")
	}
}

func TestJoinHostPortSupportsIPv6(t *testing.T) {
	if got := net.JoinHostPort("::1", "1433"); got != "[::1]:1433" {
		t.Fatalf("unexpected IPv6 address: %s", got)
	}
}
