package scanner

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"database_scan/internal/db"
	"database_scan/internal/detector"
)

type Mode string

const (
	FieldContent Mode = "field-content"
	FieldName    Mode = "field-name"
	Content      Mode = "content"
	All          Mode = "all"
)

type Options struct {
	Mode          Mode
	Level         detector.Level
	Limit         int
	Workers       int
	Timeout       time.Duration
	Mask          bool
	IncludeSystem bool
	Table         string
	Progress      io.Writer
	OnTable       func(TableResult)
}

type Summary struct {
	Database string
	Schema   string
	Table    string
	Column   string
	Kind     detector.Kind
	Level    detector.Level
	Mode     Mode
	Total    int64
}

type Sample struct {
	Database string
	Schema   string
	Table    string
	Column   string
	Kind     detector.Kind
	Level    detector.Level
	Mode     Mode
	Value    string
}

type Result struct {
	Summaries []Summary
	Samples   []Sample
	Tables    []TableResult
	Errors    []string
}

type TableResult struct {
	Database string
	Schema   string
	Name     string
	Total    int64
	Columns  []string
	Fields   []FieldResult
	Rows     []RowSample
}

type FieldResult struct {
	Name  string
	Kinds []detector.Kind
	Level detector.Level
	Mode  Mode
	Total int64
}

type RowSample struct {
	Values map[string]string
}

type Finding struct {
	Summary Summary
	Samples []string
}

type DatabaseFindings struct {
	Name   string
	Tables []TableFindings
}

type TableFindings struct {
	Schema   string
	Name     string
	Findings []Finding
}

type Reconnector func(ctx context.Context, database string) (*sql.DB, error)

type tableJob struct {
	Index   int
	Total   int
	Columns []db.Column
}

type tableOutcome struct {
	Index     int
	Table     TableResult
	Summaries []Summary
	Errors    []string
	Hit       bool
}

func Scan(ctx context.Context, base *sql.DB, adapter db.Adapter, databases []string, opts Options, reconnect Reconnector) Result {
	if opts.Workers <= 0 {
		opts.Workers = 1
	}
	var result Result
	var mu sync.Mutex

	for _, database := range databases {
		if err := ctx.Err(); err != nil {
			addScanError(&result, &mu, fmt.Sprintf("scan interrupted: %v", err))
			return result
		}
		progressf(opts.Progress, "正在枚举数据库 %s 的字段...\n", database)
		queryDB := base
		if reconnect != nil {
			nextDB, err := reconnect(ctx, database)
			if err != nil {
				mu.Lock()
				result.Errors = append(result.Errors, fmt.Sprintf("%s: reconnect failed: %v", database, err))
				mu.Unlock()
				continue
			}
			queryDB = nextDB
		}
		stopCloseOnCancel := closeDBOnCancel(ctx, queryDB)
		listCtx, listCancel := context.WithTimeout(ctx, opts.Timeout)
		columns, err := adapter.ListColumns(listCtx, queryDB, database, opts.IncludeSystem)
		listCancel()
		if err != nil {
			stopCloseOnCancel()
			mu.Lock()
			result.Errors = append(result.Errors, fmt.Sprintf("%s: list columns failed: %v", database, err))
			mu.Unlock()
			if reconnect != nil {
				_ = queryDB.Close()
			}
			continue
		}
		columns = filterColumnsByTable(columns, opts.Table)
		if opts.Table != "" && len(columns) == 0 {
			stopCloseOnCancel()
			mu.Lock()
			result.Errors = append(result.Errors, fmt.Sprintf("%s: table %q not found or has no scannable columns", database, opts.Table))
			mu.Unlock()
			if reconnect != nil {
				_ = queryDB.Close()
			}
			continue
		}
		progressf(opts.Progress, "数据库 %s: 发现 %d 个可扫描字段，%d 个字段名命中规则，开始扫描...\n", database, len(columns), matchingFieldCountByLevel(columns, opts.Level))
		scanTables(ctx, queryDB, adapter, columns, opts, &result, &mu)
		progressf(opts.Progress, "数据库 %s: 扫描完成。\n", database)
		stopCloseOnCancel()
		if reconnect != nil {
			_ = queryDB.Close()
		}
	}
	return result
}

func matchingFieldCount(columns []db.Column) int {
	return matchingFieldCountByLevel(columns, detector.LevelAll)
}

func matchingFieldCountByLevel(columns []db.Column, level detector.Level) int {
	total := 0
	for _, col := range columns {
		if len(detector.FieldKindsByLevel(level, col.Table, col.Name)) > 0 {
			total++
		}
	}
	return total
}

func filterColumnsByTable(columns []db.Column, wanted string) []db.Column {
	wanted = strings.TrimSpace(wanted)
	if wanted == "" {
		return columns
	}
	wanted = strings.ToLower(wanted)
	var out []db.Column
	for _, col := range columns {
		table := strings.ToLower(col.Table)
		schemaTable := strings.ToLower(col.Schema + "." + col.Table)
		if wanted == table || wanted == schemaTable {
			out = append(out, col)
		}
	}
	return out
}

func progressf(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, format, args...)
}

func closeDBOnCancel(ctx context.Context, sqlDB *sql.DB) func() {
	done := make(chan struct{})
	var once sync.Once
	go func() {
		select {
		case <-ctx.Done():
			_ = sqlDB.Close()
		case <-done:
		}
	}()
	return func() {
		once.Do(func() {
			close(done)
		})
	}
}

func scanColumns(ctx context.Context, sqlDB *sql.DB, adapter db.Adapter, columns []db.Column, opts Options, result *Result, mu *sync.Mutex) {
	jobs := make(chan db.Column)
	var wg sync.WaitGroup
	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for col := range jobs {
				scanColumn(ctx, sqlDB, adapter, col, opts, result, mu)
			}
		}()
	}
	for _, col := range columns {
		jobs <- col
	}
	close(jobs)
	wg.Wait()
}

func scanTables(ctx context.Context, sqlDB *sql.DB, adapter db.Adapter, columns []db.Column, opts Options, result *Result, mu *sync.Mutex) {
	tableCols := groupColumnsByTable(columns)
	keys := make([]string, 0, len(tableCols))
	for key := range tableCols {
		if len(sensitiveColumnsByLevel(tableCols[key], opts.Level)) > 0 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if opts.Workers <= 1 || len(keys) <= 1 {
		for i, key := range keys {
			outcome := scanTable(ctx, sqlDB, adapter, tableJob{Index: i, Total: len(keys), Columns: tableCols[key]}, opts)
			applyTableOutcome(result, mu, outcome, opts.OnTable)
			if ctx.Err() != nil {
				addScanError(result, mu, fmt.Sprintf("scan interrupted: %v", ctx.Err()))
				return
			}
		}
		return
	}

	progressf(opts.Progress, "启用按表并发扫描：workers=%d\n", opts.Workers)
	jobs := make(chan tableJob)
	outcomes := make(chan tableOutcome)
	var wg sync.WaitGroup
	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				outcome := scanTable(ctx, sqlDB, adapter, job, opts)
				select {
				case outcomes <- outcome:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i, key := range keys {
			if ctx.Err() != nil {
				return
			}
			select {
			case jobs <- tableJob{Index: i, Total: len(keys), Columns: tableCols[key]}:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(outcomes)
	}()

	outcomeByIndex := map[int]tableOutcome{}
	for outcome := range outcomes {
		outcomeByIndex[outcome.Index] = outcome
		applyTableOutcome(result, mu, outcome, opts.OnTable)
	}
	sortResultTables(result, mu)
	if ctx.Err() != nil {
		addScanError(result, mu, fmt.Sprintf("scan interrupted: %v", ctx.Err()))
		return
	}
	for i := range keys {
		if _, ok := outcomeByIndex[i]; !ok {
			addScanError(result, mu, "scan interrupted before all tables completed")
			return
		}
	}
}

func scanTable(ctx context.Context, sqlDB *sql.DB, adapter db.Adapter, job tableJob, opts Options) tableOutcome {
	outcome := tableOutcome{Index: job.Index}
	allCols := job.Columns
	if len(allCols) == 0 {
		return outcome
	}
	conditionCols := sensitiveColumnsByLevel(allCols, opts.Level)
	tableName := allCols[0].Schema + "." + allCols[0].Table
	progressf(opts.Progress, "扫描进度 %s: 表 %d/%d %s（字段 %d，敏感候选 %d）\n", allCols[0].Database, job.Index+1, job.Total, tableName, len(allCols), len(conditionCols))
	tableResult := TableResult{Database: allCols[0].Database, Schema: allCols[0].Schema, Name: allCols[0].Table, Columns: columnNames(allCols)}
	totalRows, err := queryTableCount(ctx, sqlDB, adapter, allCols[0], opts)
	if err != nil {
		outcome.Errors = append(outcome.Errors, formatColumnError(allCols[0], FieldContent, err))
	} else {
		tableResult.Total = totalRows
	}
	for j, col := range conditionCols {
		if err := ctx.Err(); err != nil {
			outcome.Errors = append(outcome.Errors, fmt.Sprintf("scan interrupted: %v", err))
			return outcome
		}
		kinds := detector.FieldKindsByLevel(opts.Level, col.Table, col.Name)
		progressf(opts.Progress, "  字段 %d/%d %s.%s: 统计中...\n", j+1, len(conditionCols), tableName, col.Name)
		total, err := queryNonEmptyCount(ctx, sqlDB, adapter, col, opts)
		if err != nil {
			outcome.Errors = append(outcome.Errors, formatColumnError(col, FieldContent, err))
			continue
		}
		progressf(opts.Progress, "  字段 %d/%d %s.%s: 存在行数 %d\n", j+1, len(conditionCols), tableName, col.Name, total)
		if total <= 0 {
			continue
		}
		tableResult.Fields = append(tableResult.Fields, FieldResult{Name: col.Name, Kinds: kinds, Level: highestLevel(kinds), Mode: FieldContent, Total: total})
		for _, kind := range kinds {
			outcome.Summaries = append(outcome.Summaries, Summary{Database: col.Database, Schema: col.Schema, Table: col.Table, Column: col.Name, Kind: kind, Level: detector.LevelOf(kind), Mode: FieldContent, Total: total})
		}
	}
	if len(tableResult.Fields) == 0 {
		progressf(opts.Progress, "扫描进度 %s: 表 %d/%d %s 完成，无有效命中。\n", allCols[0].Database, job.Index+1, job.Total, tableName)
		return outcome
	}
	progressf(opts.Progress, "  样例 %s: 抽取最多 %d 行整行数据...\n", tableName, opts.Limit)
	rows, err := querySampleRows(ctx, sqlDB, adapter, allCols, conditionCols, opts)
	if err != nil {
		outcome.Errors = append(outcome.Errors, formatColumnError(allCols[0], FieldContent, err))
	} else {
		tableResult.Rows = rows
	}
	outcome.Table = tableResult
	outcome.Hit = true
	progressf(opts.Progress, "扫描进度 %s: 表 %d/%d %s 完成，命中字段 %d，样例行 %d。\n", allCols[0].Database, job.Index+1, job.Total, tableName, len(tableResult.Fields), len(tableResult.Rows))
	return outcome
}

func applyTableOutcome(result *Result, mu *sync.Mutex, outcome tableOutcome, onTable func(TableResult)) {
	mu.Lock()
	result.Errors = append(result.Errors, outcome.Errors...)
	for _, summary := range outcome.Summaries {
		if summary.Total > 0 {
			result.Summaries = append(result.Summaries, summary)
		}
	}
	if outcome.Hit {
		result.Tables = append(result.Tables, outcome.Table)
	}
	mu.Unlock()
	if outcome.Hit && onTable != nil {
		onTable(outcome.Table)
	}
}

func sortResultTables(result *Result, mu *sync.Mutex) {
	mu.Lock()
	defer mu.Unlock()
	sort.SliceStable(result.Tables, func(i, j int) bool {
		a := result.Tables[i]
		b := result.Tables[j]
		return a.Database+"\x00"+a.Schema+"\x00"+a.Name < b.Database+"\x00"+b.Schema+"\x00"+b.Name
	})
}

func columnNames(columns []db.Column) []string {
	names := make([]string, 0, len(columns))
	for _, col := range columns {
		names = append(names, col.Name)
	}
	return names
}

func groupColumnsByTable(columns []db.Column) map[string][]db.Column {
	grouped := map[string][]db.Column{}
	for _, col := range columns {
		key := col.Database + "\x00" + col.Schema + "\x00" + col.Table
		grouped[key] = append(grouped[key], col)
	}
	return grouped
}

func sensitiveColumns(columns []db.Column) []db.Column {
	return sensitiveColumnsByLevel(columns, detector.LevelAll)
}

func sensitiveColumnsByLevel(columns []db.Column, level detector.Level) []db.Column {
	var out []db.Column
	for _, col := range columns {
		if len(detector.FieldKindsByLevel(level, col.Table, col.Name)) > 0 {
			out = append(out, col)
		}
	}
	return out
}

func scanColumn(ctx context.Context, sqlDB *sql.DB, adapter db.Adapter, col db.Column, opts Options, result *Result, mu *sync.Mutex) {
	fieldKinds := detector.FieldKindsByLevel(opts.Level, col.Table, col.Name)
	if opts.Mode == FieldName || opts.Mode == All {
		for _, kind := range fieldKinds {
			addSummary(result, mu, Summary{Database: col.Database, Schema: col.Schema, Table: col.Table, Column: col.Name, Kind: kind, Level: detector.LevelOf(kind), Mode: FieldName, Total: 1})
			addSample(result, mu, Sample{Database: col.Database, Schema: col.Schema, Table: col.Table, Column: col.Name, Kind: kind, Level: detector.LevelOf(kind), Mode: FieldName, Value: col.Name}, opts.Mask)
		}
	}
	if (opts.Mode == FieldContent || opts.Mode == All) && len(fieldKinds) > 0 {
		total, samples, err := queryNonEmpty(ctx, sqlDB, adapter, col, opts)
		if err != nil {
			addError(result, mu, col, FieldContent, err)
			return
		}
		for _, kind := range fieldKinds {
			addSummary(result, mu, Summary{Database: col.Database, Schema: col.Schema, Table: col.Table, Column: col.Name, Kind: kind, Level: detector.LevelOf(kind), Mode: FieldContent, Total: total})
			for _, value := range samples {
				addSample(result, mu, Sample{Database: col.Database, Schema: col.Schema, Table: col.Table, Column: col.Name, Kind: kind, Level: detector.LevelOf(kind), Mode: FieldContent, Value: value}, opts.Mask)
			}
		}
	}
	if opts.Mode == Content || opts.Mode == All {
		samples, err := queryContent(ctx, sqlDB, adapter, col, opts)
		if err != nil {
			addError(result, mu, col, Content, err)
			return
		}
		countByKind := map[detector.Kind]int64{}
		valuesByKind := map[detector.Kind][]string{}
		for _, value := range samples {
			for _, kind := range detector.ContentKindsByLevel(opts.Level, value) {
				countByKind[kind]++
				valuesByKind[kind] = append(valuesByKind[kind], value)
			}
		}
		for kind, total := range countByKind {
			addSummary(result, mu, Summary{Database: col.Database, Schema: col.Schema, Table: col.Table, Column: col.Name, Kind: kind, Level: detector.LevelOf(kind), Mode: Content, Total: total})
			for _, value := range limitStrings(valuesByKind[kind], opts.Limit) {
				addSample(result, mu, Sample{Database: col.Database, Schema: col.Schema, Table: col.Table, Column: col.Name, Kind: kind, Level: detector.LevelOf(kind), Mode: Content, Value: value}, opts.Mask)
			}
		}
	}
}

func queryNonEmpty(ctx context.Context, sqlDB *sql.DB, adapter db.Adapter, col db.Column, opts Options) (int64, []string, error) {
	qctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	var total int64
	if err := sqlDB.QueryRowContext(qctx, adapter.CountNonEmptySQL(col)).Scan(&total); err != nil {
		return 0, nil, err
	}
	rows, err := sqlDB.QueryContext(qctx, adapter.SampleNonEmptySQL(col, opts.Limit))
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	values, err := scanRowStrings(rows)
	return total, values, err
}

func queryNonEmptyCount(ctx context.Context, sqlDB *sql.DB, adapter db.Adapter, col db.Column, opts Options) (int64, error) {
	qctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	var total int64
	if err := sqlDB.QueryRowContext(qctx, adapter.CountNonEmptySQL(col)).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func queryTableCount(ctx context.Context, sqlDB *sql.DB, adapter db.Adapter, col db.Column, opts Options) (int64, error) {
	qctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	var total int64
	if err := sqlDB.QueryRowContext(qctx, adapter.CountTableSQL(col)).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func querySampleRows(ctx context.Context, sqlDB *sql.DB, adapter db.Adapter, selectCols []db.Column, conditionCols []db.Column, opts Options) ([]RowSample, error) {
	if len(selectCols) == 0 || len(conditionCols) == 0 || opts.Limit <= 0 {
		return nil, nil
	}
	qctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	rows, err := sqlDB.QueryContext(qctx, adapter.SampleRowsSQL(selectCols, conditionCols, opts.Limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var samples []RowSample
	for rows.Next() {
		values := make([]sql.NullString, len(names))
		ptrs := make([]any, len(names))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		sample := RowSample{Values: map[string]string{}}
		for i, name := range names {
			if values[i].Valid {
				sample.Values[name] = values[i].String
			}
		}
		samples = append(samples, sample)
	}
	return samples, rows.Err()
}

func queryContent(ctx context.Context, sqlDB *sql.DB, adapter db.Adapter, col db.Column, opts Options) ([]string, error) {
	qctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	query, args := adapter.ContentRegexSQL(col, detector.SQLPatternByLevel(opts.Level))
	rows, err := sqlDB.QueryContext(qctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRowStrings(rows)
}

func scanRowStrings(rows *sql.Rows) ([]string, error) {
	var values []string
	for rows.Next() {
		var v sql.NullString
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		if v.Valid {
			values = append(values, v.String)
		}
	}
	return values, rows.Err()
}

func addSummary(result *Result, mu *sync.Mutex, summary Summary) {
	if summary.Total <= 0 {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	result.Summaries = append(result.Summaries, summary)
}

func addSample(result *Result, mu *sync.Mutex, sample Sample, mask bool) {
	if mask {
		sample.Value = detector.Mask(sample.Kind, sample.Value)
	}
	mu.Lock()
	defer mu.Unlock()
	result.Samples = append(result.Samples, sample)
}

func formatColumnError(col db.Column, mode Mode, err error) string {
	return fmt.Sprintf("%s.%s.%s mode=%s: %v", col.Database, col.Table, col.Name, mode, err)
}

func addError(result *Result, mu *sync.Mutex, col db.Column, mode Mode, err error) {
	mu.Lock()
	defer mu.Unlock()
	result.Errors = append(result.Errors, formatColumnError(col, mode, err))
}

func addScanError(result *Result, mu *sync.Mutex, message string) {
	mu.Lock()
	defer mu.Unlock()
	result.Errors = append(result.Errors, message)
}

func SummaryRows(summaries []Summary) [][]string {
	rows := make([][]string, 0, len(summaries))
	for _, s := range summaries {
		rows = append(rows, []string{s.Database, s.Schema, s.Table, s.Column, string(s.Kind), detector.LevelLabel(summaryLevel(s)), ModeLabel(s.Mode), strconv.FormatInt(s.Total, 10)})
	}
	return rows
}

func SampleRows(samples []Sample) [][]string {
	rows := make([][]string, 0, len(samples))
	for _, s := range samples {
		rows = append(rows, []string{s.Database, s.Schema, s.Table, s.Column, string(s.Kind), detector.LevelLabel(sampleLevel(s)), ModeLabel(s.Mode), s.Value})
	}
	return rows
}

func Findings(result Result) []Finding {
	sampleByKey := map[string][]string{}
	for _, sample := range result.Samples {
		key := findingKey(sample.Database, sample.Schema, sample.Table, sample.Column, sample.Kind, sample.Mode)
		sampleByKey[key] = append(sampleByKey[key], sample.Value)
	}
	findings := make([]Finding, 0, len(result.Summaries))
	for _, summary := range result.Summaries {
		findings = append(findings, Finding{
			Summary: summary,
			Samples: sampleByKey[findingKey(summary.Database, summary.Schema, summary.Table, summary.Column, summary.Kind, summary.Mode)],
		})
	}
	sortFindings(findings)
	return findings
}

func FindingsByDatabase(result Result) []DatabaseFindings {
	findings := Findings(result)
	dbIndex := map[string]int{}
	tableIndex := map[string]map[string]int{}
	var groups []DatabaseFindings
	for _, finding := range findings {
		summary := finding.Summary
		dbPos, ok := dbIndex[summary.Database]
		if !ok {
			dbPos = len(groups)
			dbIndex[summary.Database] = dbPos
			tableIndex[summary.Database] = map[string]int{}
			groups = append(groups, DatabaseFindings{Name: summary.Database})
		}
		tableKey := summary.Schema + "\x00" + summary.Table
		tablePos, ok := tableIndex[summary.Database][tableKey]
		if !ok {
			tablePos = len(groups[dbPos].Tables)
			tableIndex[summary.Database][tableKey] = tablePos
			groups[dbPos].Tables = append(groups[dbPos].Tables, TableFindings{Schema: summary.Schema, Name: summary.Table})
		}
		groups[dbPos].Tables[tablePos].Findings = append(groups[dbPos].Tables[tablePos].Findings, finding)
	}
	return groups
}

func FindingSummaryRows(summary Summary) [][]string {
	return [][]string{{summary.Database, summary.Schema, summary.Table, summary.Column, string(summary.Kind), detector.LevelLabel(summaryLevel(summary)), ModeLabel(summary.Mode), strconv.FormatInt(summary.Total, 10)}}
}

func TableFieldRows(findings []Finding) [][]string {
	rows := make([][]string, 0, len(findings))
	for _, finding := range findings {
		summary := finding.Summary
		rows = append(rows, []string{summary.Column, string(summary.Kind), detector.LevelLabel(summaryLevel(summary)), ModeLabel(summary.Mode), strconv.FormatInt(summary.Total, 10)})
	}
	return rows
}

func SensitiveFieldRows(fields []FieldResult, color bool) [][]string {
	rows := make([][]string, 0, len(fields))
	for _, field := range fields {
		rows = append(rows, []string{KindLabel(field.Kinds) + "（" + detector.LevelLabel(fieldLevel(field)) + "）：" + ColorizeField(field.Name, field.Kinds, color), strconv.FormatInt(field.Total, 10)})
	}
	return rows
}

func RowSampleRows(table TableResult, color bool) (headers []string, rows [][]string) {
	fieldByName := fieldResultByName(table.Fields)
	headers = make([]string, 0, len(table.Columns))
	for _, column := range table.Columns {
		field, ok := fieldByName[column]
		if ok {
			headers = append(headers, ColorizeField(column, field.Kinds, color))
		} else {
			headers = append(headers, column)
		}
	}
	for _, sample := range table.Rows {
		row := make([]string, 0, len(table.Columns))
		for _, column := range table.Columns {
			field, ok := fieldByName[column]
			if ok {
				row = append(row, ColorizeValue(sample.Values[column], field.Kinds, color))
			} else {
				row = append(row, sample.Values[column])
			}
		}
		rows = append(rows, row)
	}
	return headers, rows
}

func fieldResultByName(fields []FieldResult) map[string]FieldResult {
	out := make(map[string]FieldResult, len(fields))
	for _, field := range fields {
		out[field.Name] = field
	}
	return out
}

func KindLabel(kinds []detector.Kind) string {
	if len(kinds) == 0 {
		return "未知"
	}
	labels := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		labels = append(labels, string(kind))
	}
	return strings.Join(labels, "/")
}

func LevelLabelForField(field FieldResult) string {
	return detector.LevelLabel(fieldLevel(field))
}

func fieldLevel(field FieldResult) detector.Level {
	if field.Level != "" {
		return field.Level
	}
	return highestLevel(field.Kinds)
}

func summaryLevel(summary Summary) detector.Level {
	if summary.Level != "" {
		return summary.Level
	}
	return detector.LevelOf(summary.Kind)
}

func sampleLevel(sample Sample) detector.Level {
	if sample.Level != "" {
		return sample.Level
	}
	return detector.LevelOf(sample.Kind)
}

func highestLevel(kinds []detector.Kind) detector.Level {
	level := detector.LevelLow
	for _, kind := range kinds {
		switch detector.LevelOf(kind) {
		case detector.LevelHigh:
			return detector.LevelHigh
		case detector.LevelMedium:
			level = detector.LevelMedium
		}
	}
	if len(kinds) == 0 {
		return ""
	}
	return level
}

func TableSampleRows(findings []Finding) [][]string {
	var rows [][]string
	for _, finding := range findings {
		for i, value := range finding.Samples {
			rows = append(rows, []string{finding.Summary.Column, strconv.Itoa(i + 1), value})
		}
	}
	return rows
}

func ColorizeKind(s string, kinds []detector.Kind, enabled bool) string {
	return colorize(s, kinds, enabled)
}

func ColorizeField(s string, kinds []detector.Kind, enabled bool) string {
	return colorize(s, kinds, enabled)
}

func ColorizeValue(s string, kinds []detector.Kind, enabled bool) string {
	return colorize(s, kinds, enabled)
}

func colorize(s string, kinds []detector.Kind, enabled bool) string {
	if !enabled || s == "" {
		return s
	}
	return riskColor(kinds) + s + "\x1b[0m"
}

func riskColor(kinds []detector.Kind) string {
	for _, kind := range kinds {
		switch kind {
		case detector.Password, detector.IDCard, detector.BankCard:
			return "\x1b[31m"
		case detector.Phone, detector.Email:
			return "\x1b[33m"
		}
	}
	return "\x1b[36m"
}

func SampleValueRows(values []string) [][]string {
	rows := make([][]string, 0, len(values))
	for i, value := range values {
		rows = append(rows, []string{strconv.Itoa(i + 1), value})
	}
	return rows
}

func findingKey(database, schema, table, column string, kind detector.Kind, mode Mode) string {
	return database + "\x00" + schema + "\x00" + table + "\x00" + column + "\x00" + string(kind) + "\x00" + string(mode)
}

func limitStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		a := findings[i].Summary
		b := findings[j].Summary
		return findingKey(a.Database, a.Schema, a.Table, a.Column, a.Kind, a.Mode) < findingKey(b.Database, b.Schema, b.Table, b.Column, b.Kind, b.Mode)
	})
}

func ModeLabel(mode Mode) string {
	switch mode {
	case FieldContent:
		return "字段名命中+内容抽样"
	case FieldName:
		return "字段名命中"
	case Content:
		return "内容正则命中"
	case All:
		return "全部模式"
	default:
		return string(mode)
	}
}
