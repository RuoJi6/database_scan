package scanner

import (
	"testing"

	"database_scan/internal/db"
	"database_scan/internal/detector"
)

func TestModeLabel(t *testing.T) {
	if got := ModeLabel(FieldContent); got != "字段名命中+内容抽样" {
		t.Fatalf("unexpected field-content label: %s", got)
	}
	if got := ModeLabel(Content); got != "内容正则命中" {
		t.Fatalf("unexpected content label: %s", got)
	}
}

func TestSummaryRowsUsesDisplayModeLabel(t *testing.T) {
	rows := SummaryRows([]Summary{{
		Database: "app",
		Schema:   "dbo",
		Table:    "Users",
		Column:   "Phone",
		Kind:     detector.Phone,
		Mode:     FieldContent,
		Total:    3,
	}})
	if rows[0][6] != "字段名命中+内容抽样" {
		t.Fatalf("unexpected mode label: %#v", rows[0])
	}
}

func TestFindingsGroupsSamplesBySummary(t *testing.T) {
	result := Result{
		Summaries: []Summary{
			{Database: "app", Schema: "dbo", Table: "Users", Column: "Phone", Kind: detector.Phone, Mode: FieldContent, Total: 2},
			{Database: "app", Schema: "dbo", Table: "Users", Column: "Email", Kind: detector.Email, Mode: FieldContent, Total: 1},
		},
		Samples: []Sample{
			{Database: "app", Schema: "dbo", Table: "Users", Column: "Phone", Kind: detector.Phone, Mode: FieldContent, Value: "13800138000"},
			{Database: "app", Schema: "dbo", Table: "Users", Column: "Email", Kind: detector.Email, Mode: FieldContent, Value: "a@example.com"},
			{Database: "app", Schema: "dbo", Table: "Users", Column: "Phone", Kind: detector.Phone, Mode: FieldContent, Value: "13900139000"},
		},
	}
	findings := Findings(result)
	if len(findings) != 2 {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	if findings[1].Summary.Column != "Phone" || len(findings[1].Samples) != 2 {
		t.Fatalf("phone samples were not grouped correctly: %#v", findings)
	}
}

func TestSampleValueRows(t *testing.T) {
	rows := SampleValueRows([]string{"a", "b"})
	if rows[0][0] != "1" || rows[0][1] != "a" || rows[1][0] != "2" || rows[1][1] != "b" {
		t.Fatalf("unexpected sample rows: %#v", rows)
	}
}

func TestFindingsByDatabaseGroupsByDatabaseAndTable(t *testing.T) {
	result := Result{
		Summaries: []Summary{
			{Database: "app", Schema: "dbo", Table: "Users", Column: "Phone", Kind: detector.Phone, Mode: FieldContent, Total: 2},
			{Database: "app", Schema: "dbo", Table: "Users", Column: "Email", Kind: detector.Email, Mode: FieldContent, Total: 1},
			{Database: "app", Schema: "dbo", Table: "Orders", Column: "Address", Kind: detector.Address, Mode: FieldContent, Total: 3},
		},
	}
	groups := FindingsByDatabase(result)
	if len(groups) != 1 || groups[0].Name != "app" {
		t.Fatalf("unexpected database groups: %#v", groups)
	}
	if len(groups[0].Tables) != 2 {
		t.Fatalf("unexpected table groups: %#v", groups[0].Tables)
	}
}

func TestTableRowsKeepSameTableFindingsTogether(t *testing.T) {
	findings := []Finding{
		{
			Summary: Summary{Column: "Phone", Kind: detector.Phone, Mode: FieldContent, Total: 2},
			Samples: []string{"13800138000", "13900139000"},
		},
		{
			Summary: Summary{Column: "Email", Kind: detector.Email, Mode: FieldContent, Total: 1},
			Samples: []string{"a@example.com"},
		},
	}
	fieldRows := TableFieldRows(findings)
	if len(fieldRows) != 2 || fieldRows[0][0] != "Phone" || fieldRows[0][4] != "2" {
		t.Fatalf("unexpected field rows: %#v", fieldRows)
	}
	sampleRows := TableSampleRows(findings)
	if len(sampleRows) != 3 || sampleRows[0][0] != "Phone" || sampleRows[2][0] != "Email" {
		t.Fatalf("unexpected sample rows: %#v", sampleRows)
	}
}

func TestRowSampleRowsUsesRealTableColumns(t *testing.T) {
	table := TableResult{
		Columns: []string{"ID", "UserName", "Password", "Status"},
		Fields: []FieldResult{
			{Name: "UserName", Kinds: []detector.Kind{detector.Username}},
			{Name: "Password", Kinds: []detector.Kind{detector.Password}},
		},
		Rows: []RowSample{{Values: map[string]string{"ID": "1", "UserName": "admin", "Password": "secret", "Status": "0"}}},
	}
	headers, rows := RowSampleRows(table, false)
	if headers[0] != "ID" || headers[1] != "UserName" || headers[2] != "Password" || headers[3] != "Status" {
		t.Fatalf("unexpected headers: %#v", headers)
	}
	if rows[0][0] != "1" || rows[0][1] != "admin" || rows[0][2] != "secret" || rows[0][3] != "0" {
		t.Fatalf("unexpected rows: %#v", rows)
	}
}

func TestSensitiveFieldRowsOnlyColorsFieldName(t *testing.T) {
	rows := SensitiveFieldRows([]FieldResult{{
		Name:  "accbank",
		Kinds: []detector.Kind{detector.BankCard},
		Total: 698,
	}}, true)
	if rows[0][0][:len("银行卡（高敏）：")] != "银行卡（高敏）：" {
		t.Fatalf("kind label should not be colored: %q", rows[0][0])
	}
	if rows[0][0] == "银行卡：accbank" {
		t.Fatalf("field name should be colored when color is enabled")
	}
}

func TestSensitiveColumnsByLevel(t *testing.T) {
	columns := []db.Column{
		{Table: "Users", Name: "mobile_phone"},
		{Table: "Users", Name: "password_hash"},
		{Table: "Users", Name: "address"},
	}
	got := sensitiveColumnsByLevel(columns, detector.LevelHigh)
	if len(got) != 1 || got[0].Name != "password_hash" {
		t.Fatalf("unexpected high-level columns: %#v", got)
	}
}

func TestFilterColumnsByTableMatchesTableAndSchemaTable(t *testing.T) {
	columns := []db.Column{
		{Database: "app", Schema: "dbo", Table: "Users", Name: "ID"},
		{Database: "app", Schema: "dbo", Table: "Users", Name: "Password"},
		{Database: "app", Schema: "audit", Table: "Log", Name: "UserName"},
	}

	byTable := filterColumnsByTable(columns, "users")
	if len(byTable) != 2 || byTable[0].Name != "ID" || byTable[1].Name != "Password" {
		t.Fatalf("unexpected table match: %#v", byTable)
	}

	bySchemaTable := filterColumnsByTable(columns, "DBO.Users")
	if len(bySchemaTable) != 2 || bySchemaTable[0].Name != "ID" || bySchemaTable[1].Name != "Password" {
		t.Fatalf("unexpected schema.table match: %#v", bySchemaTable)
	}
}
