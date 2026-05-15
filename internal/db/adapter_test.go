package db

import (
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
}

func TestAdapterSQLGeneration(t *testing.T) {
	col := Column{Database: "app", Schema: "public", Table: "users", Name: "phone"}
	cases := []string{
		(MySQLAdapter{}).SampleNonEmptySQL(col, 15),
		(PostgresAdapter{}).SampleNonEmptySQL(col, 15),
		(MSSQLAdapter{}).SampleNonEmptySQL(col, 15),
	}
	for _, sql := range cases {
		if !strings.Contains(strings.ToLower(sql), "users") || !strings.Contains(strings.ToLower(sql), "phone") {
			t.Fatalf("generated SQL does not reference table/column: %s", sql)
		}
	}
}
