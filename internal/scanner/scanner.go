package scanner

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
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
	Limit         int
	Workers       int
	Timeout       time.Duration
	Mask          bool
	IncludeSystem bool
}

type Summary struct {
	Database string
	Schema   string
	Table    string
	Column   string
	Kind     detector.Kind
	Mode     Mode
	Total    int64
}

type Sample struct {
	Database string
	Schema   string
	Table    string
	Column   string
	Kind     detector.Kind
	Mode     Mode
	Value    string
}

type Result struct {
	Summaries []Summary
	Samples   []Sample
	Errors    []string
}

type Reconnector func(ctx context.Context, database string) (*sql.DB, error)

func Scan(ctx context.Context, base *sql.DB, adapter db.Adapter, databases []string, opts Options, reconnect Reconnector) Result {
	if opts.Workers <= 0 {
		opts.Workers = 4
	}
	var result Result
	var sampleCount atomic.Int64
	var mu sync.Mutex

	for _, database := range databases {
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
		columns, err := adapter.ListColumns(ctx, queryDB, database, opts.IncludeSystem)
		if err != nil {
			mu.Lock()
			result.Errors = append(result.Errors, fmt.Sprintf("%s: list columns failed: %v", database, err))
			mu.Unlock()
			if reconnect != nil {
				_ = queryDB.Close()
			}
			continue
		}
		scanColumns(ctx, queryDB, adapter, columns, opts, &result, &mu, &sampleCount)
		if reconnect != nil {
			_ = queryDB.Close()
		}
	}
	return result
}

func scanColumns(ctx context.Context, sqlDB *sql.DB, adapter db.Adapter, columns []db.Column, opts Options, result *Result, mu *sync.Mutex, sampleCount *atomic.Int64) {
	jobs := make(chan db.Column)
	var wg sync.WaitGroup
	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for col := range jobs {
				scanColumn(ctx, sqlDB, adapter, col, opts, result, mu, sampleCount)
			}
		}()
	}
	for _, col := range columns {
		jobs <- col
	}
	close(jobs)
	wg.Wait()
}

func scanColumn(ctx context.Context, sqlDB *sql.DB, adapter db.Adapter, col db.Column, opts Options, result *Result, mu *sync.Mutex, sampleCount *atomic.Int64) {
	fieldKinds := detector.FieldKinds(col.Table, col.Name)
	if opts.Mode == FieldName || opts.Mode == All {
		for _, kind := range fieldKinds {
			addSummary(result, mu, Summary{Database: col.Database, Schema: col.Schema, Table: col.Table, Column: col.Name, Kind: kind, Mode: FieldName, Total: 1})
			addSample(result, mu, sampleCount, opts.Limit, Sample{Database: col.Database, Schema: col.Schema, Table: col.Table, Column: col.Name, Kind: kind, Mode: FieldName, Value: col.Name}, opts.Mask)
		}
	}
	if (opts.Mode == FieldContent || opts.Mode == All) && len(fieldKinds) > 0 {
		total, samples, err := queryNonEmpty(ctx, sqlDB, adapter, col, opts)
		if err != nil {
			addError(result, mu, col, FieldContent, err)
			return
		}
		for _, kind := range fieldKinds {
			addSummary(result, mu, Summary{Database: col.Database, Schema: col.Schema, Table: col.Table, Column: col.Name, Kind: kind, Mode: FieldContent, Total: total})
			for _, value := range samples {
				addSample(result, mu, sampleCount, opts.Limit, Sample{Database: col.Database, Schema: col.Schema, Table: col.Table, Column: col.Name, Kind: kind, Mode: FieldContent, Value: value}, opts.Mask)
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
			for _, kind := range detector.ContentKinds(value) {
				countByKind[kind]++
				valuesByKind[kind] = append(valuesByKind[kind], value)
			}
		}
		for kind, total := range countByKind {
			addSummary(result, mu, Summary{Database: col.Database, Schema: col.Schema, Table: col.Table, Column: col.Name, Kind: kind, Mode: Content, Total: total})
			for _, value := range valuesByKind[kind] {
				addSample(result, mu, sampleCount, opts.Limit, Sample{Database: col.Database, Schema: col.Schema, Table: col.Table, Column: col.Name, Kind: kind, Mode: Content, Value: value}, opts.Mask)
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

func queryContent(ctx context.Context, sqlDB *sql.DB, adapter db.Adapter, col db.Column, opts Options) ([]string, error) {
	qctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	query, args := adapter.ContentRegexSQL(col, detector.SQLPattern())
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

func addSample(result *Result, mu *sync.Mutex, sampleCount *atomic.Int64, limit int, sample Sample, mask bool) {
	if limit <= 0 || sampleCount.Load() >= int64(limit) {
		return
	}
	if mask {
		sample.Value = detector.Mask(sample.Kind, sample.Value)
	}
	if sampleCount.Add(1) > int64(limit) {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	result.Samples = append(result.Samples, sample)
}

func addError(result *Result, mu *sync.Mutex, col db.Column, mode Mode, err error) {
	mu.Lock()
	defer mu.Unlock()
	result.Errors = append(result.Errors, fmt.Sprintf("%s.%s.%s mode=%s: %v", col.Database, col.Table, col.Name, mode, err))
}

func SummaryRows(summaries []Summary) [][]string {
	rows := make([][]string, 0, len(summaries))
	for _, s := range summaries {
		rows = append(rows, []string{s.Database, s.Schema, s.Table, s.Column, string(s.Kind), string(s.Mode), strconv.FormatInt(s.Total, 10)})
	}
	return rows
}

func SampleRows(samples []Sample) [][]string {
	rows := make([][]string, 0, len(samples))
	for _, s := range samples {
		rows = append(rows, []string{s.Database, s.Schema, s.Table, s.Column, string(s.Kind), string(s.Mode), s.Value})
	}
	return rows
}
