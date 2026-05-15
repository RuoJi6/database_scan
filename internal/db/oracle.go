package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	go_ora "github.com/sijms/go-ora/v2"
)

type OracleAdapter struct{}

func (a OracleAdapter) Name() string { return "oracle" }

func (a OracleAdapter) Family() string { return "oracle" }

func (a OracleAdapter) DisplayName() string { return "Oracle" }

func (a OracleAdapter) DefaultPort() int { return 1521 }

func (a OracleAdapter) NeedsDatabaseReconnect() bool { return false }

func (a OracleAdapter) Open(ctx context.Context, cfg Config, dialer ContextDialer) (*sql.DB, error) {
	service := cfg.Database
	if service == "" {
		service = "ORCL"
	}
	connector := go_ora.NewConnector(go_ora.BuildUrl(cfg.Host, cfg.Port, service, cfg.User, cfg.Password, map[string]string{
		"CONNECT TIMEOUT": fmt.Sprintf("%d", int(cfg.Timeout.Seconds())),
	}))
	connector.(*go_ora.OracleConnector).Dialer(dialer)
	db := sql.OpenDB(connector)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func (a OracleAdapter) ServerInfo(ctx context.Context, sqlDB *sql.DB, cfg Config) (ServerInfo, error) {
	info := ServerInfo{Host: cfg.Host, Port: cfg.Port, DBType: a.DisplayName(), Proxy: cfg.Proxy, IncludeSystem: cfg.IncludeSystem, Environment: map[string]string{}}
	row := sqlDB.QueryRowContext(ctx, `SELECT banner, SYS_CONTEXT('USERENV','SESSION_USER'), SYS_CONTEXT('USERENV','SERVICE_NAME'), TO_CHAR(SYSTIMESTAMP, 'YYYY-MM-DD"T"HH24:MI:SS.FF TZH:TZM'), SYS_CONTEXT('USERENV','SERVER_HOST'), SYS_CONTEXT('USERENV','INSTANCE_NAME') FROM v$version WHERE ROWNUM = 1`)
	var host, instance sql.NullString
	if err := row.Scan(&info.Version, &info.CurrentUser, &info.CurrentDB, &info.ServerTime, &host, &instance); err != nil {
		return info, err
	}
	info.Environment["server_host"] = host.String
	info.Environment["instance_name"] = instance.String
	return info, nil
}

func (a OracleAdapter) ListDatabases(ctx context.Context, sqlDB *sql.DB, includeSystem bool) ([]string, error) {
	query := `SELECT username FROM all_users`
	if !includeSystem {
		query += ` WHERE username NOT IN ('SYS','SYSTEM','OUTLN','DBSNMP','APPQOSSYS','AUDSYS','CTXSYS','DVSYS','GSMADMIN_INTERNAL','LBACSYS','MDSYS','OJVMSYS','ORDDATA','ORDSYS','WMSYS','XDB')`
	}
	query += ` ORDER BY username`
	return scanStrings(ctx, sqlDB, query)
}

func (a OracleAdapter) ListColumns(ctx context.Context, sqlDB *sql.DB, database string, includeSystem bool) ([]Column, error) {
	rows, err := sqlDB.QueryContext(ctx, `SELECT owner, table_name, column_name, data_type
FROM all_tab_columns
WHERE owner = :1
  AND data_type NOT IN ('BLOB','RAW','LONG RAW','BFILE','XMLTYPE')
ORDER BY owner, table_name, column_id`, strings.ToUpper(database))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []Column
	for rows.Next() {
		var c Column
		if err := rows.Scan(&c.Schema, &c.Table, &c.Name, &c.DataType); err != nil {
			return nil, err
		}
		c.Database = c.Schema
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

func (a OracleAdapter) QuoteIdent(parts ...string) string {
	quoted := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		quoted = append(quoted, `"`+strings.ReplaceAll(p, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, ".")
}

func (a OracleAdapter) CountNonEmptySQL(c Column) string {
	qcol := a.QuoteIdent(c.Name)
	return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s IS NOT NULL AND TO_CHAR(%s) <> ''", a.QuoteIdent(c.Schema, c.Table), qcol, qcol)
}

func (a OracleAdapter) CountTableSQL(c Column) string {
	return fmt.Sprintf("SELECT COUNT(*) FROM %s", a.QuoteIdent(c.Schema, c.Table))
}

func (a OracleAdapter) SampleNonEmptySQL(c Column, limit int) string {
	qcol := a.QuoteIdent(c.Name)
	return fmt.Sprintf("SELECT TO_CHAR(%s) FROM %s WHERE %s IS NOT NULL AND TO_CHAR(%s) <> '' AND ROWNUM <= %d", qcol, a.QuoteIdent(c.Schema, c.Table), qcol, qcol, limit)
}

func (a OracleAdapter) SampleRowsSQL(selectCols []Column, conditionCols []Column, limit int) string {
	selects := make([]string, 0, len(selectCols))
	conditions := make([]string, 0, len(conditionCols))
	for _, col := range selectCols {
		qcol := a.QuoteIdent(col.Name)
		selects = append(selects, fmt.Sprintf("TO_CHAR(%s) AS %s", qcol, qcol))
	}
	for _, col := range conditionCols {
		qcol := a.QuoteIdent(col.Name)
		conditions = append(conditions, fmt.Sprintf("(%s IS NOT NULL AND TO_CHAR(%s) <> '')", qcol, qcol))
	}
	return fmt.Sprintf("SELECT %s FROM %s WHERE (%s) AND ROWNUM <= %d", strings.Join(selects, ", "), a.QuoteIdent(selectCols[0].Schema, selectCols[0].Table), strings.Join(conditions, " OR "), limit)
}

func (a OracleAdapter) ContentRegexSQL(c Column, pattern string) (string, []any) {
	qcol := a.QuoteIdent(c.Name)
	return fmt.Sprintf("SELECT TO_CHAR(%s) FROM %s WHERE REGEXP_LIKE(TO_CHAR(%s), :1) AND ROWNUM <= 50", qcol, a.QuoteIdent(c.Schema, c.Table), qcol), []any{pattern}
}
