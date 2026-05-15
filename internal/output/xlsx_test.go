package output

import (
	"archive/zip"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"database_scan/internal/detector"
	"database_scan/internal/scanner"
)

func TestWriteXLSX(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan.xlsx")
	result := scanner.Result{Tables: []scanner.TableResult{{
		Database: "app",
		Schema:   "dbo",
		Name:     "Users",
		Total:    2,
		Columns:  []string{"ID", "UserName", "Password"},
		Fields: []scanner.FieldResult{
			{Name: "UserName", Kinds: []detector.Kind{detector.Username}, Mode: scanner.FieldContent, Total: 2},
			{Name: "Password", Kinds: []detector.Kind{detector.Password}, Mode: scanner.FieldContent, Total: 1},
		},
		Rows: []scanner.RowSample{{Values: map[string]string{"ID": "1", "UserName": "admin", "Password": "secret"}}},
	}}}
	if err := WriteXLSX(path, result); err != nil {
		t.Fatalf("WriteXLSX returned error: %v", err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open xlsx zip: %v", err)
	}
	defer zr.Close()
	seenWorkbook := false
	seenSheet := false
	seenDetailSheet := false
	seenStyle := false
	for _, f := range zr.File {
		if f.Name == "xl/workbook.xml" {
			seenWorkbook = true
		}
		if f.Name == "xl/worksheets/sheet1.xml" {
			seenSheet = true
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open sheet: %v", err)
			}
			data, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				t.Fatalf("read sheet: %v", err)
			}
			body := string(data)
			if !strings.Contains(body, "敏感信息汇总") || !strings.Contains(body, "[数据库]") || !strings.Contains(body, "dbo.Users") || !strings.Contains(body, "（存在行数：2）") {
				t.Fatalf("summary sheet missing expected content: %s", body)
			}
			if !strings.Contains(body, `<cols>`) || !strings.Contains(body, `customWidth="1"`) {
				t.Fatalf("summary sheet missing custom column widths: %s", body)
			}
			dimensionPos := strings.Index(body, "<dimension ")
			sheetViewsPos := strings.Index(body, "<sheetViews>")
			formatPos := strings.Index(body, "<sheetFormatPr ")
			colsPos := strings.Index(body, "<cols>")
			sheetDataPos := strings.Index(body, "<sheetData>")
			if dimensionPos < 0 || sheetViewsPos < 0 || formatPos < 0 || colsPos < 0 || sheetDataPos < 0 {
				t.Fatalf("sheet missing expected worksheet metadata: %s", body)
			}
			if !(dimensionPos < sheetViewsPos && sheetViewsPos < formatPos && formatPos < colsPos && colsPos < sheetDataPos) {
				t.Fatalf("worksheet metadata is not in the expected order: %s", body)
			}
			seenStyle = strings.Contains(body, `s="1"`) && strings.Contains(body, `s="3"`)
		}
		if f.Name == "xl/worksheets/sheet2.xml" {
			seenDetailSheet = true
		}
	}
	if !seenWorkbook || !seenSheet || !seenDetailSheet {
		t.Fatalf("xlsx missing workbook or sheets: workbook=%v summary=%v detail=%v", seenWorkbook, seenSheet, seenDetailSheet)
	}
	if !seenStyle {
		t.Fatal("expected sensitive cells to include style ids")
	}
}

func TestWriteXLSXRedisLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "redis.xlsx")
	result := scanner.Result{Tables: []scanner.TableResult{{
		Database: "redis-db0",
		Schema:   "redis-key",
		Name:     "user:1001:mobile",
		Total:    1,
		Columns:  []string{"Target", "DB", "Key", "Type", "TTL", "Path/Field", "Value", "命中类型", "敏感级别", "判断依据"},
		Fields: []scanner.FieldResult{
			{Name: "value", Kinds: []detector.Kind{detector.Phone}, Mode: scanner.Content, Total: 1},
		},
		Rows: []scanner.RowSample{{Values: map[string]string{
			"Target":     "127.0.0.1:6379",
			"DB":         "0",
			"Key":        "user:1001:mobile",
			"Type":       "string",
			"TTL":        "-1",
			"Path/Field": "value",
			"Value":      "13800138001",
			"命中类型":       "手机号",
			"敏感级别":       "中敏",
			"判断依据":       "key+value",
		}}},
	}}}
	if err := WriteXLSX(path, result); err != nil {
		t.Fatalf("WriteXLSX returned error: %v", err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open xlsx zip: %v", err)
	}
	defer zr.Close()
	var body strings.Builder
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/") && strings.HasSuffix(f.Name, ".xml") {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open %s: %v", f.Name, err)
			}
			data, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				t.Fatalf("read %s: %v", f.Name, err)
			}
			body.Write(data)
		}
	}
	text := body.String()
	for _, want := range []string{"Redis 汇总", "Redis Keys", "Redis 敏感 Key 明细", "Target", "Path/Field", "127.0.0.1:6379", "user:1001:mobile", "13800138001"} {
		if !strings.Contains(text, want) {
			t.Fatalf("redis workbook missing %q: %s", want, text)
		}
	}
}

func TestWriteXLSXMixedSQLAndRedisLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mixed.xlsx")
	result := scanner.Result{Tables: []scanner.TableResult{
		{
			Database: "app",
			Schema:   "dbo",
			Name:     "Users",
			Total:    1,
			Columns:  []string{"mobile"},
			Fields:   []scanner.FieldResult{{Name: "mobile", Kinds: []detector.Kind{detector.Phone}, Mode: scanner.FieldContent, Total: 1}},
			Rows:     []scanner.RowSample{{Values: map[string]string{"mobile": "13800138001"}}},
		},
		{
			Database: "redis-db0",
			Schema:   "redis-key",
			Name:     "session:token",
			Total:    1,
			Columns:  []string{"Target", "DB", "Key", "Type", "TTL", "Path/Field", "Value", "命中类型", "敏感级别", "判断依据"},
			Fields:   []scanner.FieldResult{{Name: "value", Kinds: []detector.Kind{detector.Password}, Mode: scanner.Content, Total: 1}},
			Rows: []scanner.RowSample{{Values: map[string]string{
				"Target": "127.0.0.1:6379", "DB": "0", "Key": "session:token", "Type": "string", "TTL": "-1", "Path/Field": "value",
				"Value": "sk_live_redis_abc", "命中类型": "密码/密钥", "敏感级别": "高敏", "判断依据": "key+value",
			}}},
		},
	}}
	if err := WriteXLSX(path, result); err != nil {
		t.Fatalf("WriteXLSX returned error: %v", err)
	}
	names := SheetNamesForTest(result)
	for _, want := range []string{"敏感信息汇总", "dbo.Users", "Redis 汇总", "Redis Keys"} {
		found := false
		for _, name := range names {
			if name == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing sheet %q in %#v", want, names)
		}
	}
}

func TestColumnWidths(t *testing.T) {
	rows := [][]xlsxCell{
		cells("短", "ascii"),
		cells("中文列宽", strings.Repeat("x", 100)),
	}
	widths := columnWidths(rows)
	if len(widths) != 2 {
		t.Fatalf("unexpected widths: %#v", widths)
	}
	if widths[0] < 10 {
		t.Fatalf("expected Chinese display width to be considered, got %.1f", widths[0])
	}
	if widths[1] != 60 {
		t.Fatalf("expected long text width to be capped at 60, got %.1f", widths[1])
	}
}

func TestDisplayWidth(t *testing.T) {
	if got := displayWidth("abc"); got != 3 {
		t.Fatalf("unexpected ascii width: %d", got)
	}
	if got := displayWidth("中文"); got != 4 {
		t.Fatalf("unexpected Chinese width: %d", got)
	}
}

func TestSheetNamesForTest(t *testing.T) {
	result := scanner.Result{Tables: []scanner.TableResult{{Schema: "dbo", Name: "Users"}, {Schema: "dbo", Name: "Users"}}}
	names := SheetNamesForTest(result)
	if len(names) != 3 || names[0] != "dbo.Users" || names[1] != "dbo.Users 2" || names[2] != "敏感信息汇总" {
		t.Fatalf("unexpected sheet names: %#v", names)
	}
}
