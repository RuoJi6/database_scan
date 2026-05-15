package db

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strings"
	"sync/atomic"

	mysql "github.com/go-sql-driver/mysql"
)

var mysqlDialID atomic.Uint64

type MySQLAdapter struct{}

func (a MySQLAdapter) Name() string { return "mysql" }

func (a MySQLAdapter) Open(ctx context.Context, cfg Config, dialer ContextDialer) (*sql.DB, error) {
	netName := "tcp"
	if cfg.Proxy != "" {
		netName = fmt.Sprintf("database_scan_mysql_%d", mysqlDialID.Add(1))
		mysql.RegisterDialContext(netName, func(ctx context.Context, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", addr)
		})
	}
	mcfg := mysql.NewConfig()
	mcfg.Net = netName
	mcfg.Addr = fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	mcfg.User = cfg.User
	mcfg.Passwd = cfg.Password
	mcfg.DBName = cfg.Database
	mcfg.ParseTime = true
	mcfg.Timeout = cfg.Timeout
	mcfg.ReadTimeout = cfg.Timeout
	mcfg.WriteTimeout = cfg.Timeout
	connector, err := mysql.NewConnector(mcfg)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func (a MySQLAdapter) ServerInfo(ctx context.Context, db *sql.DB, cfg Config) (ServerInfo, error) {
	info := ServerInfo{Host: cfg.Host, Port: cfg.Port, DBType: "mysql", Proxy: cfg.Proxy, IncludeSystem: cfg.IncludeSystem, Environment: map[string]string{}}
	row := db.QueryRowContext(ctx, "SELECT VERSION(), CURRENT_USER(), DATABASE(), NOW(), @@version_comment, @@hostname, @@character_set_server")
	var currentDB sql.NullString
	var versionComment, hostname, charset string
	if err := row.Scan(&info.Version, &info.CurrentUser, &currentDB, &info.ServerTime, &versionComment, &hostname, &charset); err != nil {
		return info, err
	}
	info.CurrentDB = currentDB.String
	info.Environment["version_comment"] = versionComment
	info.Environment["hostname"] = hostname
	info.Environment["character_set_server"] = charset
	return info, nil
}

func (a MySQLAdapter) ListDatabases(ctx context.Context, db *sql.DB, includeSystem bool) ([]string, error) {
	query := "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA"
	if !includeSystem {
		query += " WHERE SCHEMA_NAME NOT IN ('information_schema','mysql','performance_schema','sys')"
	}
	query += " ORDER BY SCHEMA_NAME"
	return scanStrings(ctx, db, query)
}

func (a MySQLAdapter) ListColumns(ctx context.Context, db *sql.DB, database string, includeSystem bool) ([]Column, error) {
	rows, err := db.QueryContext(ctx, `SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, DATA_TYPE
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = ? AND DATA_TYPE NOT IN ('blob','binary','varbinary','geometry','json')
ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION`, database)
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

func (a MySQLAdapter) QuoteIdent(parts ...string) string {
	quoted := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		quoted = append(quoted, "`"+strings.ReplaceAll(p, "`", "``")+"`")
	}
	return strings.Join(quoted, ".")
}

func (a MySQLAdapter) CountNonEmptySQL(c Column) string {
	return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE `%s` IS NOT NULL AND CAST(`%s` AS CHAR) <> ''", a.QuoteIdent(c.Database, c.Table), escapeMySQLIdent(c.Name), escapeMySQLIdent(c.Name))
}

func (a MySQLAdapter) SampleNonEmptySQL(c Column, limit int) string {
	return fmt.Sprintf("SELECT CAST(`%s` AS CHAR) FROM %s WHERE `%s` IS NOT NULL AND CAST(`%s` AS CHAR) <> '' LIMIT %d", escapeMySQLIdent(c.Name), a.QuoteIdent(c.Database, c.Table), escapeMySQLIdent(c.Name), escapeMySQLIdent(c.Name), limit)
}

func (a MySQLAdapter) ContentRegexSQL(c Column, pattern string) (string, []any) {
	return fmt.Sprintf("SELECT CAST(`%s` AS CHAR) FROM %s WHERE CAST(`%s` AS CHAR) REGEXP ? LIMIT 50", escapeMySQLIdent(c.Name), a.QuoteIdent(c.Database, c.Table), escapeMySQLIdent(c.Name)), []any{pattern}
}

func escapeMySQLIdent(s string) string { return strings.ReplaceAll(s, "`", "``") }
