package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	mssql "github.com/microsoft/go-mssqldb"
)

type MSSQLAdapter struct{}

func (a MSSQLAdapter) Name() string { return "mssql" }

func (a MSSQLAdapter) Open(ctx context.Context, cfg Config, dialer ContextDialer) (*sql.DB, error) {
	q := url.Values{}
	q.Set("database", cfg.Database)
	q.Set("encrypt", "disable")
	q.Set("connection timeout", fmt.Sprintf("%d", int(cfg.Timeout.Seconds())))
	u := &url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		RawQuery: q.Encode(),
	}
	connector, err := mssql.NewConnector(u.String())
	if err != nil {
		return nil, err
	}
	connector.Dialer = dialer
	db := sql.OpenDB(connector)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func (a MSSQLAdapter) ServerInfo(ctx context.Context, db *sql.DB, cfg Config) (ServerInfo, error) {
	info := ServerInfo{Host: cfg.Host, Port: cfg.Port, DBType: "mssql", Proxy: cfg.Proxy, IncludeSystem: cfg.IncludeSystem, Environment: map[string]string{}}
	row := db.QueryRowContext(ctx, `SELECT @@VERSION, SYSTEM_USER, DB_NAME(), CONVERT(varchar(30), SYSDATETIME(), 126), SERVERPROPERTY('MachineName'), SERVERPROPERTY('Edition'), SERVERPROPERTY('Collation')`)
	var machine, edition, collation sql.NullString
	if err := row.Scan(&info.Version, &info.CurrentUser, &info.CurrentDB, &info.ServerTime, &machine, &edition, &collation); err != nil {
		return info, err
	}
	info.Environment["machine"] = machine.String
	info.Environment["edition"] = edition.String
	info.Environment["collation"] = collation.String
	return info, nil
}

func (a MSSQLAdapter) ListDatabases(ctx context.Context, db *sql.DB, includeSystem bool) ([]string, error) {
	query := "SELECT name FROM sys.databases WHERE state_desc = 'ONLINE'"
	if !includeSystem {
		query += " AND name NOT IN ('master','model','msdb','tempdb')"
	}
	query += " ORDER BY name"
	return scanStrings(ctx, db, query)
}

func (a MSSQLAdapter) ListColumns(ctx context.Context, db *sql.DB, database string, includeSystem bool) ([]Column, error) {
	query := fmt.Sprintf(`SELECT TABLE_CATALOG, TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, DATA_TYPE
FROM %s.INFORMATION_SCHEMA.COLUMNS
WHERE DATA_TYPE NOT IN ('binary','varbinary','image','geography','geometry','hierarchyid','xml')
ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION`, a.QuoteIdent(database))
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []Column
	for rows.Next() {
		var c Column
		if err := rows.Scan(&c.Database, &c.Schema, &c.Table, &c.Name, &c.DataType); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

func (a MSSQLAdapter) QuoteIdent(parts ...string) string {
	quoted := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		quoted = append(quoted, "["+strings.ReplaceAll(p, "]", "]]")+"]")
	}
	return strings.Join(quoted, ".")
}

func (a MSSQLAdapter) CountNonEmptySQL(c Column) string {
	qcol := a.QuoteIdent(c.Name)
	return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s IS NOT NULL AND CONVERT(nvarchar(max), %s) <> ''", a.QuoteIdent(c.Database, c.Schema, c.Table), qcol, qcol)
}

func (a MSSQLAdapter) SampleNonEmptySQL(c Column, limit int) string {
	qcol := a.QuoteIdent(c.Name)
	return fmt.Sprintf("SELECT TOP (%d) CONVERT(nvarchar(max), %s) FROM %s WHERE %s IS NOT NULL AND CONVERT(nvarchar(max), %s) <> ''", limit, qcol, a.QuoteIdent(c.Database, c.Schema, c.Table), qcol, qcol)
}

func (a MSSQLAdapter) ContentRegexSQL(c Column, pattern string) (string, []any) {
	qcol := a.QuoteIdent(c.Name)
	like := "%"
	if strings.Contains(pattern, "1[3-9]") {
		like = "%1%"
	}
	return fmt.Sprintf("SELECT TOP (50) CONVERT(nvarchar(max), %s) FROM %s WHERE CONVERT(nvarchar(max), %s) LIKE @p1", qcol, a.QuoteIdent(c.Database, c.Schema, c.Table), qcol), []any{like}
}
