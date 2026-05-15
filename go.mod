module database_scan

go 1.23.0

toolchain go1.23.5

require (
	github.com/go-sql-driver/mysql v1.9.3
	github.com/jackc/pgx/v5 v5.7.6
	github.com/microsoft/go-mssqldb v1.8.2
	golang.org/x/net v0.38.0
	golang.org/x/term v0.31.0
)

replace (
	golang.org/x/crypto => golang.org/x/crypto v0.36.0
	golang.org/x/sync => golang.org/x/sync v0.12.0
	golang.org/x/sys => golang.org/x/sys v0.31.0
	golang.org/x/text => golang.org/x/text v0.23.0
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/golang-sql/civil v0.0.0-20220223132316-b832511892a9 // indirect
	github.com/golang-sql/sqlexp v0.1.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/crypto v0.43.0 // indirect
	golang.org/x/sync v0.17.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.30.0 // indirect
)
