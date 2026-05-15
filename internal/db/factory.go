package db

import "fmt"

func NewAdapter(kind string) (Adapter, error) {
	switch kind {
	case "mysql":
		return MySQLAdapter{}, nil
	case "mssql", "sqlserver":
		return MSSQLAdapter{}, nil
	case "postgres", "postgresql":
		return PostgresAdapter{}, nil
	default:
		return nil, fmt.Errorf("unsupported database type %q", kind)
	}
}
