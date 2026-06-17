package db

import (
	"fmt"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
)

func NewAdapter(kind string) (Adapter, error) {
	switch kind {
	case "mysql":
		return NewMySQLAdapter("mysql", "MySQL"), nil
	case "mariadb":
		return NewMySQLAdapter("mariadb", "MariaDB"), nil
	case "tidb":
		return NewMySQLAdapter("tidb", "TiDB(MySQL)"), nil
	case "oceanbase", "oceanbase-mysql":
		return NewMySQLAdapter(kind, "OceanBase(MySQL)"), nil
	case "polardb-mysql":
		return NewMySQLAdapter("polardb-mysql", "PolarDB(MySQL)"), nil
	case "doris":
		return NewMySQLAdapter("doris", "Apache Doris(MySQL)"), nil
	case "starrocks":
		return NewMySQLAdapter("starrocks", "StarRocks(MySQL)"), nil
	case "gbase-mysql":
		return NewMySQLAdapter("gbase-mysql", "GBase(MySQL)"), nil
	case "mssql", "sqlserver":
		return MSSQLAdapter{}, nil
	case "postgres", "postgresql":
		return NewPostgresAdapter(kind, "PostgreSQL"), nil
	case "opengauss":
		return NewPostgresAdapter("opengauss", "openGauss"), nil
	case "gaussdb":
		return NewPostgresAdapter("gaussdb", "GaussDB(PostgreSQL)"), nil
	case "kingbase", "kingbasees":
		return NewPostgresAdapter(kind, "KingbaseES(PostgreSQL)"), nil
	case "highgo":
		return NewPostgresAdapter("highgo", "HighGo(PostgreSQL)"), nil
	case "polardb-postgres":
		return NewPostgresAdapter("polardb-postgres", "PolarDB(PostgreSQL)"), nil
	case "oracle", "go-ora":
		return OracleAdapter{}, nil
	case "redis":
		return RedisAdapter{}, nil
	case "clickhouse", "clickhouse-native":
		return NewClickHouseAdapter("clickhouse", clickhouse.Native, "ClickHouse Native"), nil
	case "clickhouse-http":
		return NewClickHouseAdapter("clickhouse-http", clickhouse.HTTP, "ClickHouse HTTP"), nil
	default:
		return nil, fmt.Errorf("unsupported database type %q", kind)
	}
}
