package db

import (
	"context"
	"database/sql"
	"errors"
)

type RedisAdapter struct{}

func (RedisAdapter) Name() string                      { return "redis" }
func (RedisAdapter) Family() string                    { return "redis" }
func (RedisAdapter) DisplayName() string               { return "Redis" }
func (RedisAdapter) DefaultPort() int                  { return 6379 }
func (RedisAdapter) NeedsDatabaseReconnect() bool      { return false }
func (RedisAdapter) QuoteIdent(parts ...string) string { return "" }

func (RedisAdapter) Open(context.Context, Config, ContextDialer) (*sql.DB, error) {
	return nil, errors.New("redis uses native key scanner")
}

func (RedisAdapter) ServerInfo(context.Context, *sql.DB, Config) (ServerInfo, error) {
	return ServerInfo{}, errors.New("redis uses native key scanner")
}

func (RedisAdapter) ListDatabases(context.Context, *sql.DB, bool) ([]string, error) {
	return nil, errors.New("redis uses native key scanner")
}

func (RedisAdapter) ListColumns(context.Context, *sql.DB, string, bool) ([]Column, error) {
	return nil, errors.New("redis uses native key scanner")
}

func (RedisAdapter) CountNonEmptySQL(Column) string { return "" }
func (RedisAdapter) CountTableSQL(Column) string    { return "" }
func (RedisAdapter) SampleNonEmptySQL(Column, int) string {
	return ""
}
func (RedisAdapter) SampleRowsSQL([]Column, []Column, int) string {
	return ""
}
func (RedisAdapter) ContentRegexSQL(Column, string) (string, []any) {
	return "", nil
}
