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
