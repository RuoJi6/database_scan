package db

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"strings"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
)

type ClickHouseAdapter struct {
	protocol    clickhouse.Protocol
	dbType      string
	displayName string
}

func NewClickHouseAdapter(dbType string, protocol clickhouse.Protocol, displayName string) ClickHouseAdapter {
	if dbType == "" {
		dbType = "clickhouse"
	}
	if displayName == "" {
		displayName = "ClickHouse"
	}
	return ClickHouseAdapter{dbType: dbType, protocol: protocol, displayName: displayName}
}

func (a ClickHouseAdapter) Name() string { return a.dbType }

func (a ClickHouseAdapter) Family() string { return "clickhouse" }

func (a ClickHouseAdapter) DisplayName() string { return a.displayName }

func (a ClickHouseAdapter) DefaultPort() int {
	if a.protocol == clickhouse.HTTP {
		return 8123
	}
	return 9000
}

func (a ClickHouseAdapter) NeedsDatabaseReconnect() bool { return false }

func (a ClickHouseAdapter) Open(ctx context.Context, cfg Config, dialer ContextDialer) (*sql.DB, error) {
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("invalid clickhouse port %d", cfg.Port)
	}
	options := &clickhouse.Options{
		Protocol:     a.protocol,
		Addr:         []string{net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))},
		Auth:         clickhouse.Auth{Database: clickhouseDatabase(cfg.Database), Username: cfg.User, Password: cfg.Password},
		DialTimeout:  cfg.Timeout,
		ReadTimeout:  cfg.Timeout,
		DialContext:  func(ctx context.Context, addr string) (net.Conn, error) { return dialer.DialContext(ctx, "tcp", addr) },
		MaxOpenConns: 5,
		MaxIdleConns: 5,
	}
	db := clickhouse.OpenDB(options)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func clickhouseDatabase(database string) string {
	if strings.TrimSpace(database) == "" {
		return "default"
	}
	return database
}

func (a ClickHouseAdapter) ServerInfo(ctx context.Context, db *sql.DB, cfg Config) (ServerInfo, error) {
	info := ServerInfo{Host: cfg.Host, Port: cfg.Port, DBType: a.DisplayName(), Proxy: cfg.Proxy, IncludeSystem: cfg.IncludeSystem, Environment: map[string]string{}}
	row := db.QueryRowContext(ctx, `SELECT version(), currentUser(), currentDatabase(), now(), hostName()`)
	var hostname string
	if err := row.Scan(&info.Version, &info.CurrentUser, &info.CurrentDB, &info.ServerTime, &hostname); err != nil {
		return info, err
	}
	info.Environment["hostname"] = hostname
	info.Environment["protocol"] = a.protocol.String()
	return info, nil
}

func (a ClickHouseAdapter) ListDatabases(ctx context.Context, db *sql.DB, includeSystem bool) ([]string, error) {
	query := "SELECT name FROM system.databases"
	if !includeSystem {
		query += " WHERE name NOT IN ('system','INFORMATION_SCHEMA','information_schema')"
	}
	query += " ORDER BY name"
	return scanStrings(ctx, db, query)
}

func (a ClickHouseAdapter) ListColumns(ctx context.Context, db *sql.DB, database string, includeSystem bool) ([]Column, error) {
	rows, err := db.QueryContext(ctx, `SELECT database, table, name, type
FROM system.columns
WHERE database = ?
  AND (
    positionCaseInsensitive(type, 'String') > 0
    OR positionCaseInsensitive(type, 'UUID') > 0
    OR positionCaseInsensitive(type, 'IPv4') > 0
    OR positionCaseInsensitive(type, 'IPv6') > 0
    OR positionCaseInsensitive(type, 'Enum') > 0
    OR positionCaseInsensitive(type, 'JSON') > 0
    OR positionCaseInsensitive(type, 'Object') > 0
  )
ORDER BY database, table, position`, database)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []Column
	for rows.Next() {
		var c Column
		if err := rows.Scan(&c.Database, &c.Table, &c.Name, &c.DataType); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

func (a ClickHouseAdapter) QuoteIdent(parts ...string) string {
	quoted := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		quoted = append(quoted, "`"+strings.ReplaceAll(p, "`", "``")+"`")
	}
	return strings.Join(quoted, ".")
}

func (a ClickHouseAdapter) CountNonEmptySQL(c Column) string {
	qcol := a.QuoteIdent(c.Name)
	return fmt.Sprintf("SELECT count() FROM %s WHERE isNotNull(%s) AND notEmpty(toString(%s)) SETTINGS max_threads = 1", a.QuoteIdent(c.Database, c.Table), qcol, qcol)
}

func (a ClickHouseAdapter) CountTableSQL(c Column) string {
	return fmt.Sprintf("SELECT count() FROM %s SETTINGS max_threads = 1", a.QuoteIdent(c.Database, c.Table))
}

func (a ClickHouseAdapter) SampleNonEmptySQL(c Column, limit int) string {
	qcol := a.QuoteIdent(c.Name)
	return fmt.Sprintf("SELECT toString(%s) FROM %s WHERE isNotNull(%s) AND notEmpty(toString(%s)) LIMIT %d", qcol, a.QuoteIdent(c.Database, c.Table), qcol, qcol, limit)
}

func (a ClickHouseAdapter) SampleRowsSQL(selectCols []Column, conditionCols []Column, limit int) string {
	selects := make([]string, 0, len(selectCols))
	conditions := make([]string, 0, len(conditionCols))
	for _, col := range selectCols {
		qcol := a.QuoteIdent(col.Name)
		selects = append(selects, fmt.Sprintf("toString(%s) AS %s", qcol, qcol))
	}
	for _, col := range conditionCols {
		qcol := a.QuoteIdent(col.Name)
		conditions = append(conditions, fmt.Sprintf("(isNotNull(%s) AND notEmpty(toString(%s)))", qcol, qcol))
	}
	return fmt.Sprintf("SELECT %s FROM %s WHERE %s LIMIT %d", strings.Join(selects, ", "), a.QuoteIdent(selectCols[0].Database, selectCols[0].Table), strings.Join(conditions, " OR "), limit)
}

func (a ClickHouseAdapter) ContentRegexSQL(c Column, pattern string) (string, []any) {
	qcol := a.QuoteIdent(c.Name)
	return fmt.Sprintf("SELECT toString(%s) FROM %s WHERE match(toString(%s), ?) LIMIT 50", qcol, a.QuoteIdent(c.Database, c.Table), qcol), []any{pattern}
}
