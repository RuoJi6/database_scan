package db

import (
	"context"
	"database/sql"
	"net"
	"time"
)

type DialContextFunc func(ctx context.Context, network, address string) (any, error)

type Config struct {
	Type          string
	Host          string
	Port          int
	User          string
	Password      string
	Database      string
	Proxy         string
	IncludeSystem bool
	Timeout       time.Duration
}

type ServerInfo struct {
	Host          string
	Port          int
	DBType        string
	Version       string
	CurrentUser   string
	CurrentDB     string
	ServerTime    string
	Environment   map[string]string
	ResolvedAddr  string
	Proxy         string
	IncludeSystem bool
}

type Column struct {
	Database string
	Schema   string
	Table    string
	Name     string
	DataType string
}

type Adapter interface {
	Name() string
	Open(ctx context.Context, cfg Config, dialer ContextDialer) (*sql.DB, error)
	ServerInfo(ctx context.Context, db *sql.DB, cfg Config) (ServerInfo, error)
	ListDatabases(ctx context.Context, db *sql.DB, includeSystem bool) ([]string, error)
	ListColumns(ctx context.Context, db *sql.DB, database string, includeSystem bool) ([]Column, error)
	QuoteIdent(parts ...string) string
	CountNonEmptySQL(col Column) string
	SampleNonEmptySQL(col Column, limit int) string
	ContentRegexSQL(col Column, pattern string) (string, []any)
}

type ContextDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}
