package output

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"database_scan/internal/detector"
	"database_scan/internal/scanner"
)

func WriteXLSX(path string, result scanner.Result) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	zw := zip.NewWriter(file)
	defer zw.Close()

	sheets := buildSheets(result)
	if len(sheets) == 0 {
		sheets = []xlsxSheet{{Name: "No Findings", Rows: [][]xlsxCell{cells("结果"), cells("未发现敏感信息命中")}}}
	}
	files := map[string]string{
		"[Content_Types].xml":        contentTypesXML(len(sheets)),
		"_rels/.rels":                packageRelsXML(),
		"xl/workbook.xml":            workbookXML(sheets),
		"xl/_rels/workbook.xml.rels": workbookRelsXML(len(sheets)),
		"xl/styles.xml":              stylesXML(),
	}
	for name, body := range files {
		if err := writeZipFile(zw, name, body); err != nil {
			return err
		}
	}
	for i, sheet := range sheets {
		if err := writeZipFile(zw, fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), worksheetXML(sheet.Rows)); err != nil {
			return err
		}
	}
	return nil
}

type xlsxSheet struct {
	Name string
	Rows [][]xlsxCell
}

type xlsxCell struct {
	Value string
	Style int
}

func buildSheets(result scanner.Result) []xlsxSheet {
	used := map[string]int{}
	sheets := make([]xlsxSheet, 0, len(result.Tables)+1)
	if len(result.Tables) > 0 {
		sheets = append(sheets, xlsxSheet{Name: uniqueSheetName("敏感信息汇总", used), Rows: summaryRows(result.Tables)})
	}
	for _, table := range result.Tables {
		rows := [][]xlsxCell{
			cells("数据库", table.Database),
			cells("Schema", table.Schema),
			cells("表名", table.Name),
			cells("实际数据行数", strconv.FormatInt(table.Total, 10)),
			{},
			cells("敏感字段清单"),
			cells("字段名", "疑似类型", "敏感级别", "判断依据", "字段非空行数"),
		}
		for _, field := range table.Fields {
			rows = append(rows, []xlsxCell{{Value: field.Name, Style: styleForKinds(field.Kinds)}, {Value: scanner.KindLabel(field.Kinds)}, {Value: scanner.LevelLabelForField(field)}, {Value: scanner.ModeLabel(field.Mode)}, {Value: strconv.FormatInt(field.Total, 10)}})
		}
		rows = append(rows, []xlsxCell{}, cells("真实样例数据"))
		headers, sampleRows := scanner.RowSampleRows(table, false)
		rows = append(rows, styledHeaderCells(headers, table))
		rows = append(rows, styledSampleRows(headers, sampleRows, table)...)
		sheets = append(sheets, xlsxSheet{Name: uniqueSheetName(table.Schema+"."+table.Name, used), Rows: rows})
	}
	return sheets
}

func summaryRows(tables []scanner.TableResult) [][]xlsxCell {
	rows := [][]xlsxCell{
		cells("敏感信息汇总"),
		{},
	}
	for i, table := range tables {
		if i > 0 {
			rows = append(rows, []xlsxCell{})
		}
		rows = append(rows,
			cells("[数据库]", table.Database),
			cells("[表]", fmt.Sprintf("%s.%s【实际数据行数：%d】", table.Schema, table.Name, table.Total)),
		)
		for _, field := range table.Fields {
			rows = append(rows, []xlsxCell{
				{Value: scanner.KindLabel(field.Kinds) + "（" + scanner.LevelLabelForField(field) + "）："},
				{Value: field.Name, Style: styleForKinds(field.Kinds)},
				{Value: fmt.Sprintf("（存在行数：%d）", field.Total)},
			})
		}
	}
	return rows
}

func cells(values ...string) []xlsxCell {
	out := make([]xlsxCell, 0, len(values))
	for _, value := range values {
		out = append(out, xlsxCell{Value: value})
	}
	return out
}

func styledHeaderCells(headers []string, table scanner.TableResult) []xlsxCell {
	fieldStyles := fieldStyles(table)
	out := make([]xlsxCell, 0, len(headers))
	for _, header := range headers {
		out = append(out, xlsxCell{Value: header, Style: fieldStyles[header]})
	}
	return out
}

func styledSampleRows(headers []string, rows [][]string, table scanner.TableResult) [][]xlsxCell {
	fieldStyles := fieldStyles(table)
	out := make([][]xlsxCell, 0, len(rows))
	for _, row := range rows {
		next := make([]xlsxCell, 0, len(row))
		for i, value := range row {
			style := 0
			if i < len(headers) {
				style = fieldStyles[headers[i]]
			}
			next = append(next, xlsxCell{Value: value, Style: style})
		}
		out = append(out, next)
	}
	return out
}

func fieldStyles(table scanner.TableResult) map[string]int {
	styles := map[string]int{}
	for _, field := range table.Fields {
		styles[field.Name] = styleForKinds(field.Kinds)
	}
	return styles
}

func styleForKinds(kinds []detector.Kind) int {
	for _, kind := range kinds {
		switch kind {
		case detector.Password, detector.IDCard, detector.BankCard:
			return 1
		case detector.Phone, detector.Email:
			return 2
		}
	}
	if len(kinds) > 0 {
		return 3
	}
	return 0
}

func uniqueSheetName(name string, used map[string]int) string {
	base := sanitizeSheetName(name)
	count := used[base]
	used[base] = count + 1
	if count == 0 {
		return base
	}
	suffix := fmt.Sprintf(" %d", count+1)
	return truncateRunes(base, 31-len(suffix)) + suffix
}

func sanitizeSheetName(name string) string {
	replacer := strings.NewReplacer("[", "_", "]", "_", ":", "_", "*", "_", "?", "_", "/", "_", "\\", "_")
	name = strings.TrimSpace(replacer.Replace(name))
	if name == "" {
		name = "Sheet"
	}
	return truncateRunes(name, 31)
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}

func writeZipFile(zw *zip.Writer, name, body string) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write([]byte(body))
	return err
}

func contentTypesXML(sheetCount int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	b.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`)
	b.WriteString(`<Default Extension="xml" ContentType="application/xml"/>`)
	b.WriteString(`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`)
	b.WriteString(`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`)
	for i := 1; i <= sheetCount; i++ {
		b.WriteString(fmt.Sprintf(`<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i))
	}
	b.WriteString(`</Types>`)
	return b.String()
}

func packageRelsXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`
}

func workbookXML(sheets []xlsxSheet) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	for i, sheet := range sheets {
		b.WriteString(fmt.Sprintf(`<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xmlEscape(sheet.Name), i+1, i+1))
	}
	b.WriteString(`</sheets></workbook>`)
	return b.String()
}

func workbookRelsXML(sheetCount int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i := 1; i <= sheetCount; i++ {
		b.WriteString(fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i, i))
	}
	b.WriteString(`<Relationship Id="rIdStyles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`)
	b.WriteString(`</Relationships>`)
	return b.String()
}

func stylesXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="4"><font><sz val="11"/><name val="Calibri"/></font><font><sz val="11"/><color rgb="FFFF0000"/><name val="Calibri"/></font><font><sz val="11"/><color rgb="FFFFC000"/><name val="Calibri"/></font><font><sz val="11"/><color rgb="FF00A6A6"/><name val="Calibri"/></font></fonts><fills count="1"><fill><patternFill patternType="none"/></fill></fills><borders count="1"><border/></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="4"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/><xf numFmtId="0" fontId="2" fillId="0" borderId="0" xfId="0" applyFont="1"/><xf numFmtId="0" fontId="3" fillId="0" borderId="0" xfId="0" applyFont="1"/></cellXfs></styleSheet>`
}

func worksheetXML(rows [][]xlsxCell) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for r, row := range rows {
		b.WriteString(fmt.Sprintf(`<row r="%d">`, r+1))
		for c, cell := range row {
			ref := cellRef(c+1, r+1)
			style := ""
			if cell.Style > 0 {
				style = fmt.Sprintf(` s="%d"`, cell.Style)
			}
			b.WriteString(fmt.Sprintf(`<c r="%s"%s t="inlineStr"><is><t>%s</t></is></c>`, ref, style, xmlEscape(cell.Value)))
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}

func cellRef(col, row int) string {
	var letters []byte
	for col > 0 {
		col--
		letters = append([]byte{byte('A' + col%26)}, letters...)
		col /= 26
	}
	return string(letters) + strconv.Itoa(row)
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func SheetNamesForTest(result scanner.Result) []string {
	sheets := buildSheets(result)
	names := make([]string, 0, len(sheets))
	for _, sheet := range sheets {
		names = append(names, sheet.Name)
	}
	sort.Strings(names)
	return names
}
