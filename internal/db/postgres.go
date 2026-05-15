package db

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type PostgresAdapter struct {
	dbType      string
	displayName string
}

func NewPostgresAdapter(dbType, displayName string) PostgresAdapter {
	if dbType == "" {
		dbType = "postgres"
	}
	if displayName == "" {
		displayName = "PostgreSQL"
	}
	return PostgresAdapter{dbType: dbType, displayName: displayName}
}

func (a PostgresAdapter) Name() string {
	if a.dbType == "" {
		return "postgres"
	}
	return a.dbType
}

func (a PostgresAdapter) Family() string { return "postgres" }

func (a PostgresAdapter) DisplayName() string {
	if a.displayName == "" {
		return "PostgreSQL"
	}
	return a.displayName
}

func (a PostgresAdapter) DefaultPort() int { return 5432 }

func (a PostgresAdapter) NeedsDatabaseReconnect() bool { return true }

func (a PostgresAdapter) Open(ctx context.Context, cfg Config, dialer ContextDialer) (*sql.DB, error) {
	pcfg, err := pgx.ParseConfig("")
	if err != nil {
		return nil, err
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("invalid postgres port %d", cfg.Port)
	}
	pcfg.Host = cfg.Host
	pcfg.Port = uint16(cfg.Port)
	pcfg.User = cfg.User
	pcfg.Password = cfg.Password
	pcfg.Database = postgresDatabase(cfg.Database)
	pcfg.ConnectTimeout = cfg.Timeout
	pcfg.TLSConfig = nil
	pcfg.Fallbacks = nil
	pcfg.Config.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, addr)
	}
	db := stdlib.OpenDB(*pcfg)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func postgresDatabase(database string) string {
	if strings.TrimSpace(database) == "" {
		return "postgres"
	}
	return database
}

func (a PostgresAdapter) ServerInfo(ctx context.Context, db *sql.DB, cfg Config) (ServerInfo, error) {
	info := ServerInfo{Host: cfg.Host, Port: cfg.Port, DBType: a.DisplayName(), Proxy: cfg.Proxy, IncludeSystem: cfg.IncludeSystem, Environment: map[string]string{}}
	row := db.QueryRowContext(ctx, `SELECT version(), current_user, current_database(), now()::text, inet_server_addr()::text, inet_server_port()::text, current_setting('server_encoding')`)
	var serverAddr, serverPort, encoding sql.NullString
	if err := row.Scan(&info.Version, &info.CurrentUser, &info.CurrentDB, &info.ServerTime, &serverAddr, &serverPort, &encoding); err != nil {
		return info, err
	}
	info.Environment["server_addr"] = serverAddr.String
	info.Environment["server_port"] = serverPort.String
	info.Environment["server_encoding"] = encoding.String
	return info, nil
}

func (a PostgresAdapter) ListDatabases(ctx context.Context, db *sql.DB, includeSystem bool) ([]string, error) {
	query := "SELECT datname FROM pg_database WHERE datallowconn"
	if !includeSystem {
		query += " AND datname NOT IN ('template0','template1','postgres')"
	}
	query += " ORDER BY datname"
	return scanStrings(ctx, db, query)
}

func (a PostgresAdapter) ListColumns(ctx context.Context, db *sql.DB, database string, includeSystem bool) ([]Column, error) {
	rows, err := db.QueryContext(ctx, `SELECT table_catalog, table_schema, table_name, column_name, data_type
FROM information_schema.columns
WHERE table_catalog = $1 AND table_schema NOT IN ('pg_catalog','information_schema')
  AND data_type NOT IN ('bytea','json','jsonb')
ORDER BY table_schema, table_name, ordinal_position`, database)
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

func (a PostgresAdapter) QuoteIdent(parts ...string) string {
	quoted := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		quoted = append(quoted, `"`+strings.ReplaceAll(p, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, ".")
}

func (a PostgresAdapter) CountNonEmptySQL(c Column) string {
	qcol := a.QuoteIdent(c.Name)
	return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s IS NOT NULL AND %s::text <> ''", a.QuoteIdent(c.Schema, c.Table), qcol, qcol)
}

func (a PostgresAdapter) CountTableSQL(c Column) string {
	return fmt.Sprintf("SELECT COUNT(*) FROM %s", a.QuoteIdent(c.Schema, c.Table))
}

func (a PostgresAdapter) SampleNonEmptySQL(c Column, limit int) string {
	qcol := a.QuoteIdent(c.Name)
	return fmt.Sprintf("SELECT %s::text FROM %s WHERE %s IS NOT NULL AND %s::text <> '' LIMIT %d", qcol, a.QuoteIdent(c.Schema, c.Table), qcol, qcol, limit)
}

func (a PostgresAdapter) SampleRowsSQL(selectCols []Column, conditionCols []Column, limit int) string {
	selects := make([]string, 0, len(selectCols))
	conditions := make([]string, 0, len(conditionCols))
	for _, col := range selectCols {
		qcol := a.QuoteIdent(col.Name)
		selects = append(selects, fmt.Sprintf("%s::text AS %s", qcol, qcol))
	}
	for _, col := range conditionCols {
		qcol := a.QuoteIdent(col.Name)
		conditions = append(conditions, fmt.Sprintf("(%s IS NOT NULL AND %s::text <> '')", qcol, qcol))
	}
	return fmt.Sprintf("SELECT %s FROM %s WHERE %s LIMIT %d", strings.Join(selects, ", "), a.QuoteIdent(selectCols[0].Schema, selectCols[0].Table), strings.Join(conditions, " OR "), limit)
}

func (a PostgresAdapter) ContentRegexSQL(c Column, pattern string) (string, []any) {
	qcol := a.QuoteIdent(c.Name)
	return fmt.Sprintf("SELECT %s::text FROM %s WHERE %s::text ~ $1 LIMIT 50", qcol, a.QuoteIdent(c.Schema, c.Table), qcol), []any{pattern}
}
