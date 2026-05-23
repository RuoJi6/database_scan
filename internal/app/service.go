package app

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"database_scan/internal/db"
	"database_scan/internal/detector"
	fscanparse "database_scan/internal/fscan"
	"database_scan/internal/output"
	iproxy "database_scan/internal/proxy"
	redisscan "database_scan/internal/redis"
	"database_scan/internal/scanner"
	"database_scan/internal/textfix"
)

type ScanRequest struct {
	Type          string
	Host          string
	Port          int
	User          string
	Password      string
	Database      string
	Table         string
	Proxy         string
	Mode          string
	Level         string
	Limit         int
	SQL           string
	Output        string
	Fscan         string
	FscanText     string
	SplitOutput   bool
	IncludeSystem bool
	Mask          bool
	TextEncoding  string
	Workers       int
	Timeout       string
}

type LogEntry struct {
	Time    string
	Level   string
	Message string
}

type ScanJobState struct {
	JobID       string
	Status      string
	Progress    int
	Message     string
	TargetLabel string
	Request     ScanRequest
	ServerInfo  *db.ServerInfo
	RedisInfo   *redisscan.Info
	Result      scanner.Result
	SQLResult   *CustomSQLResult
	Outputs     []string
	Logs        []LogEntry
	Errors      []string
	StartedAt   string
	FinishedAt  string
}

type CustomSQLResult struct {
	Columns  []string
	Rows     [][]string
	Total    int
	Shown    int
	Affected int64
	IsQuery  bool
}

type ConnectionTestResult struct {
	Success      bool
	Message      string
	Type         string
	Host         string
	Port         int
	Database     string
	User         string
	Proxy        string
	Version      string
	ResolvedAddr string
	ServerTime   string
}

type ServiceHooks struct {
	OnLog    func(LogEntry)
	OnTable  func(scanner.TableResult)
	OnTarget func(index int, total int, label string)
}

type FscanPreview struct {
	Targets []FscanTargetPreview
	Total   int
}

type FscanTargetPreview struct {
	Type     string
	Host     string
	Port     int
	User     string
	Line     int
	Raw      string
	Password bool
}

func DefaultScanRequest() ScanRequest {
	return ScanRequest{
		Type:         "mysql",
		Mode:         string(scanner.FieldContent),
		Level:        string(detector.LevelAll),
		Limit:        15,
		Workers:      1,
		Timeout:      "15s",
		TextEncoding: textfix.EncodingAuto,
	}
}

func SupportedDatabaseTypes() []string {
	return []string{
		"mysql", "mariadb", "tidb", "oceanbase", "polardb-mysql", "doris", "starrocks", "gbase-mysql",
		"mssql",
		"postgres", "opengauss", "gaussdb", "kingbase", "highgo", "polardb-postgres",
		"oracle",
		"redis",
	}
}

func ValidateScanRequest(req ScanRequest) (Config, error) {
	defaults := DefaultScanRequest()
	if strings.TrimSpace(req.Type) == "" {
		req.Type = defaults.Type
	}
	if strings.TrimSpace(req.Mode) == "" {
		req.Mode = defaults.Mode
	}
	if strings.TrimSpace(req.Level) == "" {
		req.Level = defaults.Level
	}
	if req.Limit == 0 {
		req.Limit = defaults.Limit
	}
	if req.Workers == 0 {
		req.Workers = defaults.Workers
	}
	if strings.TrimSpace(req.Timeout) == "" {
		req.Timeout = defaults.Timeout
	}
	req.TextEncoding = textfix.NormalizeEncoding(req.TextEncoding)
	if !textfix.IsSupportedEncoding(req.TextEncoding) {
		return Config{}, fmt.Errorf("unsupported text encoding %q", req.TextEncoding)
	}

	level, ok := detector.ParseLevel(req.Level)
	if !ok {
		return Config{}, fmt.Errorf("unsupported level %q", req.Level)
	}
	timeout, err := time.ParseDuration(req.Timeout)
	if err != nil {
		return Config{}, fmt.Errorf("parse timeout: %w", err)
	}
	cfg := Config{
		Type:          strings.ToLower(strings.TrimSpace(req.Type)),
		Host:          strings.TrimSpace(req.Host),
		Port:          req.Port,
		User:          strings.TrimSpace(req.User),
		Password:      req.Password,
		Database:      strings.TrimSpace(req.Database),
		Table:         strings.TrimSpace(req.Table),
		Proxy:         strings.TrimSpace(req.Proxy),
		Mode:          strings.ToLower(strings.TrimSpace(req.Mode)),
		Level:         level,
		Limit:         req.Limit,
		SQL:           strings.TrimSpace(req.SQL),
		Output:        strings.TrimSpace(req.Output),
		Fscan:         strings.TrimSpace(req.Fscan),
		FscanText:     strings.TrimSpace(req.FscanText),
		SplitOutput:   req.SplitOutput,
		IncludeSystem: req.IncludeSystem,
		Mask:          req.Mask,
		TextEncoding:  req.TextEncoding,
		Workers:       req.Workers,
		Timeout:       timeout,
		NoProgress:    true,
		NoColor:       true,
		NoBanner:      true,
	}
	if err := normalizeTarget(&cfg); err != nil {
		return Config{}, err
	}
	if cfg.Fscan == "" && cfg.FscanText == "" && (cfg.Type == "" || cfg.Host == "" || (cfg.User == "" && cfg.Type != "redis")) {
		return Config{}, fmt.Errorf("type, host and user are required")
	}
	if cfg.Port == 0 && cfg.Type != "" {
		adapter, err := db.NewAdapter(cfg.Type)
		if err != nil {
			return Config{}, err
		}
		cfg.Port = adapter.DefaultPort()
	}
	if cfg.Limit <= 0 {
		return Config{}, fmt.Errorf("limit must be greater than 0")
	}
	if cfg.Workers <= 0 {
		return Config{}, fmt.Errorf("workers must be greater than 0")
	}
	if cfg.Table != "" && cfg.Database == "" {
		return Config{}, fmt.Errorf("table requires database")
	}
	if cfg.SplitOutput && cfg.Output == "" {
		return Config{}, fmt.Errorf("split-output requires output")
	}
	switch cfg.Mode {
	case string(scanner.FieldContent), string(scanner.FieldName), string(scanner.Content), string(scanner.All):
	default:
		return Config{}, fmt.Errorf("unsupported mode %q", cfg.Mode)
	}
	return cfg, nil
}

func RunScan(ctx context.Context, req ScanRequest, hooks ServiceHooks) (ScanJobState, error) {
	cfg, err := ValidateScanRequest(req)
	if err != nil {
		return ScanJobState{}, err
	}
	state := ScanJobState{
		Status:    "running",
		Progress:  1,
		Request:   requestFromConfig(cfg),
		StartedAt: time.Now().Format(time.RFC3339),
	}
	log := func(level, message string) {
		entry := LogEntry{Time: time.Now().Format("15:04:05"), Level: level, Message: message}
		state.Logs = append(state.Logs, entry)
		if hooks.OnLog != nil {
			hooks.OnLog(entry)
		}
	}
	log("info", "扫描任务已启动")
	if cfg.Fscan != "" || cfg.FscanText != "" {
		return runFscanService(ctx, cfg, state, hooks, log)
	}
	if cfg.SQL != "" {
		sqlResult, err := RunCustomSQL(ctx, req)
		if err != nil {
			state.Status = "failed"
			state.Message = err.Error()
			state.Errors = append(state.Errors, err.Error())
			state.FinishedAt = time.Now().Format(time.RFC3339)
			return state, err
		}
		state.SQLResult = &sqlResult
		state.Status = "completed"
		state.Progress = 100
		state.Message = "自定义 SQL 执行完成"
		state.FinishedAt = time.Now().Format(time.RFC3339)
		return state, nil
	}
	result, serverInfo, redisInfo, err := scanAnyTargetService(ctx, cfg, hooks, log)
	if err != nil {
		state.Status = "failed"
		state.Message = err.Error()
		state.Errors = append(state.Errors, err.Error())
		state.FinishedAt = time.Now().Format(time.RFC3339)
		return state, err
	}
	state.Result = result
	state.ServerInfo = serverInfo
	state.RedisInfo = redisInfo
	state.Errors = append(state.Errors, result.Errors...)
	if cfg.Output != "" {
		path, err := writeOutput(cfg.Output, result)
		if err != nil {
			state.Status = "failed"
			state.Message = err.Error()
			state.Errors = append(state.Errors, err.Error())
			state.FinishedAt = time.Now().Format(time.RFC3339)
			return state, err
		}
		state.Outputs = append(state.Outputs, path)
		log("info", "Excel 报告已写入 "+path)
	}
	state.Status = "completed"
	state.Progress = 100
	state.Message = "扫描完成"
	state.FinishedAt = time.Now().Format(time.RFC3339)
	return state, nil
}

func runFscanService(ctx context.Context, cfg Config, state ScanJobState, hooks ServiceHooks, log func(string, string)) (ScanJobState, error) {
	targets, err := parseFscanTargets(cfg)
	if err != nil {
		return state, fmt.Errorf("parse fscan result: %w", err)
	}
	if len(targets) == 0 {
		return state, fmt.Errorf("parse fscan result: no supported database credentials found")
	}
	merged := scanner.Result{}
	log("info", fmt.Sprintf("fscan 解析到 %d 个数据库凭据", len(targets)))
	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			state.Status = "stopped"
			state.Message = "扫描已停止"
			state.FinishedAt = time.Now().Format(time.RFC3339)
			return state, nil
		}
		label := target.Label()
		state.TargetLabel = label
		state.Progress = int(float64(i) / float64(len(targets)) * 100)
		if hooks.OnTarget != nil {
			hooks.OnTarget(i+1, len(targets), label)
		}
		log("info", fmt.Sprintf("[%d/%d] %s", i+1, len(targets), label))
		next := cfg
		next.Type = target.Type
		next.Host = target.Host
		next.Port = target.Port
		next.User = target.User
		next.Password = target.Password
		next.Database = ""
		next.Table = ""
		next.SQL = ""
		next.Output = ""
		result, _, _, err := scanAnyTargetService(ctx, next, hooks, log)
		if err != nil {
			msg := fmt.Sprintf("%s: %v", label, err)
			merged.Errors = append(merged.Errors, msg)
			state.Errors = append(state.Errors, msg)
			log("error", msg)
			continue
		}
		prefixResultTables(result, target)
		if cfg.SplitOutput && cfg.Output != "" {
			path, err := writeOutput(splitOutputPath(cfg.Output, target), result)
			if err != nil {
				state.Status = "failed"
				state.Message = err.Error()
				state.FinishedAt = time.Now().Format(time.RFC3339)
				return state, err
			}
			state.Outputs = append(state.Outputs, path)
			log("info", "独立 Excel 报告已写入 "+path)
		}
		merged.Tables = append(merged.Tables, result.Tables...)
		merged.Summaries = append(merged.Summaries, result.Summaries...)
		merged.Samples = append(merged.Samples, result.Samples...)
		merged.Errors = append(merged.Errors, result.Errors...)
	}
	if cfg.Output != "" {
		path, err := writeOutput(cfg.Output, merged)
		if err != nil {
			state.Status = "failed"
			state.Message = err.Error()
			state.FinishedAt = time.Now().Format(time.RFC3339)
			return state, err
		}
		state.Outputs = append(state.Outputs, path)
		log("info", "汇总 Excel 报告已写入 "+path)
	}
	state.Result = merged
	state.Errors = append(state.Errors, merged.Errors...)
	state.Status = "completed"
	state.Progress = 100
	state.Message = "fscan 批量扫描完成"
	state.FinishedAt = time.Now().Format(time.RFC3339)
	return state, nil
}

func scanAnyTargetService(ctx context.Context, cfg Config, hooks ServiceHooks, log func(string, string)) (scanner.Result, *db.ServerInfo, *redisscan.Info, error) {
	if cfg.Type == "redis" {
		redisCfg := redisscan.Config{
			Host: cfg.Host, Port: cfg.Port, User: cfg.User, Password: cfg.Password, Database: cfg.Database, Proxy: cfg.Proxy,
			Timeout: cfg.Timeout, Limit: cfg.Limit, Level: cfg.Level, Mask: cfg.Mask, TextEncoding: cfg.TextEncoding, Progress: progressLogWriter{log: log},
		}
		info, result, err := scanRedisWithRetry(ctx, redisCfg, log)
		if err != nil {
			return scanner.Result{}, nil, nil, err
		}
		return result, nil, &info, nil
	}
	adapter, err := db.NewAdapter(cfg.Type)
	if err != nil {
		return scanner.Result{}, nil, nil, err
	}
	dialer, err := iproxy.FromURL(cfg.Proxy, cfg.Timeout)
	if err != nil {
		return scanner.Result{}, nil, nil, err
	}
	dbCfg := db.Config{
		Type: cfg.Type, Host: cfg.Host, Port: cfg.Port, User: cfg.User, Password: cfg.Password,
		Database: connectionDatabase(adapter, cfg.Database), Proxy: cfg.Proxy, IncludeSystem: cfg.IncludeSystem, Timeout: cfg.Timeout,
	}
	conn, err := openDatabaseWithRetry(ctx, adapter, dbCfg, dialer, log)
	if err != nil {
		return scanner.Result{}, nil, nil, fmt.Errorf("connect database: %w", err)
	}
	defer conn.Close()
	configureConnectionPool(conn, cfg.Workers)

	infoCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	info, err := adapter.ServerInfo(infoCtx, conn, dbCfg)
	cancel()
	if err != nil {
		return scanner.Result{}, nil, nil, fmt.Errorf("read server info: %w", err)
	}
	if addrs, err := net.LookupHost(cfg.Host); err == nil && len(addrs) > 0 {
		info.ResolvedAddr = strings.Join(addrs, ",")
	}
	log("info", fmt.Sprintf("已连接 %s %s:%d", cfg.Type, cfg.Host, cfg.Port))

	listCtx, listCancel := context.WithTimeout(ctx, cfg.Timeout)
	databases, err := adapter.ListDatabases(listCtx, conn, cfg.IncludeSystem)
	listCancel()
	if err != nil {
		return scanner.Result{}, nil, nil, fmt.Errorf("list databases: %w", err)
	}
	databases = scanDatabases(adapter, databases, cfg.Database)
	if len(databases) == 0 {
		return scanner.Result{}, &info, nil, nil
	}
	var reconnect scanner.Reconnector
	if adapter.NeedsDatabaseReconnect() {
		reconnect = func(ctx context.Context, database string) (*sql.DB, error) {
			nextCfg := dbCfg
			nextCfg.Database = database
			nextDB, err := openDatabaseWithRetry(ctx, adapter, nextCfg, dialer, log)
			if err != nil {
				return nil, err
			}
			configureConnectionPool(nextDB, cfg.Workers)
			return nextDB, nil
		}
	}
	result := scanner.Scan(ctx, conn, adapter, databases, scanner.Options{
		Mode: scanner.Mode(cfg.Mode), Limit: cfg.Limit, Workers: cfg.Workers, Timeout: cfg.Timeout,
		Level: cfg.Level, Mask: cfg.Mask, IncludeSystem: cfg.IncludeSystem, Table: cfg.Table,
		TextEncoding: cfg.TextEncoding,
		Progress:     progressLogWriter{log: log},
		OnTable: func(table scanner.TableResult) {
			if hooks.OnTable != nil {
				hooks.OnTable(table)
			}
		},
	}, reconnect)
	return result, &info, nil, nil
}

func RunCustomSQL(ctx context.Context, req ScanRequest) (CustomSQLResult, error) {
	cfg, err := ValidateScanRequest(req)
	if err != nil {
		return CustomSQLResult{}, err
	}
	if strings.TrimSpace(cfg.SQL) == "" {
		return CustomSQLResult{}, fmt.Errorf("sql is required")
	}
	if cfg.Type == "redis" || cfg.Fscan != "" || cfg.FscanText != "" {
		return CustomSQLResult{}, fmt.Errorf("custom SQL only supports SQL database targets")
	}
	adapter, err := db.NewAdapter(cfg.Type)
	if err != nil {
		return CustomSQLResult{}, err
	}
	dialer, err := iproxy.FromURL(cfg.Proxy, cfg.Timeout)
	if err != nil {
		return CustomSQLResult{}, err
	}
	conn, err := openDatabaseWithRetry(ctx, adapter, db.Config{
		Type: cfg.Type, Host: cfg.Host, Port: cfg.Port, User: cfg.User, Password: cfg.Password,
		Database: cfg.Database, Proxy: cfg.Proxy, IncludeSystem: cfg.IncludeSystem, Timeout: cfg.Timeout,
	}, dialer, nil)
	if err != nil {
		return CustomSQLResult{}, fmt.Errorf("connect database: %w", err)
	}
	defer conn.Close()
	return executeCustomSQL(ctx, conn, cfg.SQL, cfg.Limit, cfg.Timeout, cfg.TextEncoding)
}

func TestConnection(ctx context.Context, req ScanRequest) (ConnectionTestResult, error) {
	req.Fscan = ""
	req.FscanText = ""
	req.SQL = ""
	req.Output = ""
	req.SplitOutput = false
	cfg, err := ValidateScanRequest(req)
	if err != nil {
		return ConnectionTestResult{}, err
	}
	testCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	if cfg.Type == "redis" {
		info, err := redisscan.TestConnection(testCtx, redisscan.Config{
			Host: cfg.Host, Port: cfg.Port, User: cfg.User, Password: cfg.Password, Database: cfg.Database, Proxy: cfg.Proxy,
			Timeout: cfg.Timeout, Limit: 1, Level: cfg.Level, Mask: true, TextEncoding: cfg.TextEncoding,
		})
		if err != nil {
			return ConnectionTestResult{}, fmt.Errorf("test redis connection: %w", err)
		}
		return ConnectionTestResult{
			Success: true, Message: "Redis 连接测试通过", Type: cfg.Type, Host: cfg.Host, Port: cfg.Port,
			Database: info.DB, User: cfg.User, Proxy: cfg.Proxy, Version: info.Version, ResolvedAddr: info.ResolvedIP, ServerTime: info.ServerTime,
		}, nil
	}
	adapter, err := db.NewAdapter(cfg.Type)
	if err != nil {
		return ConnectionTestResult{}, err
	}
	dialer, err := iproxy.FromURL(cfg.Proxy, cfg.Timeout)
	if err != nil {
		return ConnectionTestResult{}, err
	}
	dbCfg := db.Config{
		Type: cfg.Type, Host: cfg.Host, Port: cfg.Port, User: cfg.User, Password: cfg.Password,
		Database: connectionDatabase(adapter, cfg.Database), Proxy: cfg.Proxy, IncludeSystem: cfg.IncludeSystem, Timeout: cfg.Timeout,
	}
	conn, err := openDatabaseWithRetry(testCtx, adapter, dbCfg, dialer, nil)
	if err != nil {
		return ConnectionTestResult{}, fmt.Errorf("test database connection: %w", err)
	}
	defer conn.Close()
	infoCtx, infoCancel := context.WithTimeout(testCtx, cfg.Timeout)
	info, err := adapter.ServerInfo(infoCtx, conn, dbCfg)
	infoCancel()
	if err != nil {
		return ConnectionTestResult{}, fmt.Errorf("read server info: %w", err)
	}
	if addrs, err := net.LookupHost(cfg.Host); err == nil && len(addrs) > 0 {
		info.ResolvedAddr = strings.Join(addrs, ",")
	}
	return ConnectionTestResult{
		Success: true, Message: "数据库连接测试通过", Type: cfg.Type, Host: cfg.Host, Port: cfg.Port,
		Database: info.CurrentDB, User: info.CurrentUser, Proxy: cfg.Proxy, Version: info.Version, ResolvedAddr: info.ResolvedAddr, ServerTime: info.ServerTime,
	}, nil
}

func openDatabaseWithRetry(ctx context.Context, adapter db.Adapter, cfg db.Config, dialer db.ContextDialer, log func(string, string)) (*sql.DB, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		conn, err := adapter.Open(ctx, cfg, dialer)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if attempt == 3 || !shouldRetryConnect(err) || ctx.Err() != nil {
			break
		}
		wait := time.Duration(attempt*350) * time.Millisecond
		if log != nil {
			log("warn", fmt.Sprintf("连接 %s %s:%d 失败：%v；%.1fs 后重试", cfg.Type, cfg.Host, cfg.Port, err, wait.Seconds()))
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, lastErr
}

func scanRedisWithRetry(ctx context.Context, cfg redisscan.Config, log func(string, string)) (redisscan.Info, scanner.Result, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		info, result, err := redisscan.Scan(ctx, cfg)
		if err == nil {
			return info, result, nil
		}
		lastErr = err
		if attempt == 3 || !shouldRetryConnect(err) || ctx.Err() != nil {
			break
		}
		wait := time.Duration(attempt*350) * time.Millisecond
		if log != nil {
			log("warn", fmt.Sprintf("连接 redis %s:%d 失败：%v；%.1fs 后重试", cfg.Host, cfg.Port, err, wait.Seconds()))
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return redisscan.Info{}, scanner.Result{}, ctx.Err()
		case <-timer.C:
		}
	}
	return redisscan.Info{}, scanner.Result{}, lastErr
}

func shouldRetryConnect(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{
		"no route to host",
		"network is unreachable",
		"host is down",
		"connection refused",
		"connection reset",
		"i/o timeout",
		"timeout",
		"temporary failure",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func executeCustomSQL(ctx context.Context, conn *sql.DB, query string, limit int, timeout time.Duration, textEncoding string) (CustomSQLResult, error) {
	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if isQuerySQL(query) {
		rows, err := conn.QueryContext(queryCtx, query)
		if err != nil {
			return CustomSQLResult{}, err
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			return CustomSQLResult{}, err
		}
		result := CustomSQLResult{Columns: cols, IsQuery: true}
		for rows.Next() {
			result.Total++
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return CustomSQLResult{}, err
			}
			if len(result.Rows) < limit {
				row := make([]string, len(cols))
				for i, v := range values {
					row[i] = stringifyWithEncoding(v, textEncoding)
				}
				result.Rows = append(result.Rows, row)
			}
		}
		result.Shown = len(result.Rows)
		return result, rows.Err()
	}
	execResult, err := conn.ExecContext(queryCtx, query)
	if err != nil {
		return CustomSQLResult{}, err
	}
	affected, _ := execResult.RowsAffected()
	return CustomSQLResult{Affected: affected, IsQuery: false}, nil
}

func isQuerySQL(query string) bool {
	fields := strings.Fields(strings.TrimSpace(query))
	if len(fields) == 0 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "select", "show", "with", "describe", "desc", "explain":
		return true
	default:
		return false
	}
}

func ParseFscanPreview(path string) (FscanPreview, error) {
	targets, err := fscanparse.ParseFile(path)
	if err != nil {
		return FscanPreview{}, err
	}
	return fscanPreviewFromTargets(targets), nil
}

func ParseFscanTextPreview(text string) (FscanPreview, error) {
	targets, err := fscanparse.ParseText(text)
	if err != nil {
		return FscanPreview{}, err
	}
	return fscanPreviewFromTargets(targets), nil
}

func fscanPreviewFromTargets(targets []fscanparse.Target) FscanPreview {
	out := FscanPreview{Total: len(targets), Targets: make([]FscanTargetPreview, 0, len(targets))}
	for _, target := range targets {
		out.Targets = append(out.Targets, FscanTargetPreview{
			Type: target.Type, Host: target.Host, Port: target.Port, User: target.User,
			Line: target.Line, Raw: target.Raw, Password: target.Password != "",
		})
	}
	return out
}

func parseFscanTargets(cfg Config) ([]fscanparse.Target, error) {
	if strings.TrimSpace(cfg.FscanText) != "" {
		return fscanparse.ParseText(cfg.FscanText)
	}
	return fscanparse.ParseFile(cfg.Fscan)
}

func writeOutput(path string, result scanner.Result) (string, error) {
	if filepath.Ext(path) == "" {
		path += ".xlsx"
	}
	if err := output.WriteXLSX(path, result); err != nil {
		return "", fmt.Errorf("write xlsx output: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path, nil
	}
	return abs, nil
}

func requestFromConfig(cfg Config) ScanRequest {
	return ScanRequest{
		Type: cfg.Type, Host: cfg.Host, Port: cfg.Port, User: cfg.User, Password: cfg.Password,
		Database: cfg.Database, Table: cfg.Table, Proxy: cfg.Proxy, Mode: cfg.Mode, Level: string(cfg.Level),
		Limit: cfg.Limit, SQL: cfg.SQL, Output: cfg.Output, Fscan: cfg.Fscan, SplitOutput: cfg.SplitOutput,
		FscanText:     cfg.FscanText,
		IncludeSystem: cfg.IncludeSystem, Mask: cfg.Mask, TextEncoding: cfg.TextEncoding, Workers: cfg.Workers, Timeout: cfg.Timeout.String(),
	}
}

type progressLogWriter struct {
	log func(string, string)
}

func (w progressLogWriter) Write(p []byte) (int, error) {
	if w.log != nil {
		for _, line := range strings.Split(strings.TrimSpace(string(p)), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				w.log("debug", line)
			}
		}
	}
	return len(p), nil
}

func sortedOutputPaths(paths []string) []string {
	out := append([]string(nil), paths...)
	sort.Strings(out)
	return out
}
