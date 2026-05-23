package app

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"database_scan/internal/db"
	fscanparse "database_scan/internal/fscan"
	"database_scan/internal/output"
	iproxy "database_scan/internal/proxy"
	redisscan "database_scan/internal/redis"
	"database_scan/internal/scanner"
	"database_scan/internal/textfix"
)

func Run(ctx context.Context, args []string) error {
	cfg, err := parseArgs(args)
	if err != nil {
		if isHelp(err) {
			return nil
		}
		return err
	}
	if !cfg.NoBanner {
		printBanner(os.Stdout, !cfg.NoColor && helpColorEnabled(nil))
	}
	if cfg.TestConnection {
		return runConnectionTest(ctx, cfg)
	}
	if cfg.Fscan != "" {
		return runFscan(ctx, cfg)
	}
	return runSingle(ctx, cfg)
}

func runConnectionTest(ctx context.Context, cfg Config) error {
	req := requestFromConfig(cfg)
	req.Fscan = ""
	req.FscanText = ""
	req.SQL = ""
	req.Output = ""
	result, err := TestConnection(ctx, req)
	if err != nil {
		return err
	}
	output.Section(os.Stdout, "连接测试")
	rows := [][]string{
		{"状态", "成功"},
		{"目标", fmt.Sprintf("%s:%d", result.Host, result.Port)},
		{"数据库类型", result.Type},
		{"当前库/DB", blank(result.Database)},
		{"当前用户", blank(result.User)},
		{"代理", blank(result.Proxy)},
		{"解析IP", blank(result.ResolvedAddr)},
		{"版本", blank(output.OneLine(result.Version))},
		{"信息", result.Message},
	}
	output.Table(os.Stdout, []string{"字段", "值"}, rows)
	return nil
}

func runSingle(ctx context.Context, cfg Config) error {
	if cfg.Type == "redis" {
		result, err := scanRedisTarget(ctx, cfg)
		if err != nil {
			return err
		}
		printRedisResult(result, !cfg.NoColor)
		if cfg.Output != "" {
			if err := output.WriteXLSX(cfg.Output, result); err != nil {
				return fmt.Errorf("write xlsx output: %w", err)
			}
			outputPath, err := filepath.Abs(cfg.Output)
			if err != nil {
				outputPath = cfg.Output
			}
			fmt.Fprintf(os.Stdout, "\n已写入表格文件: %s\n", outputPath)
		}
		return nil
	}
	adapter, err := db.NewAdapter(cfg.Type)
	if err != nil {
		return err
	}
	dialer, err := iproxy.FromURL(cfg.Proxy, cfg.Timeout)
	if err != nil {
		return err
	}
	dbCfg := db.Config{
		Type: cfg.Type, Host: cfg.Host, Port: cfg.Port, User: cfg.User, Password: cfg.Password,
		Database: connectionDatabase(adapter, cfg.Database), Proxy: cfg.Proxy, IncludeSystem: cfg.IncludeSystem, Timeout: cfg.Timeout,
	}
	conn, err := adapter.Open(ctx, dbCfg, dialer)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer conn.Close()
	configureConnectionPool(conn, cfg.Workers)

	infoCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	info, err := adapter.ServerInfo(infoCtx, conn, dbCfg)
	cancel()
	if err != nil {
		return fmt.Errorf("read server info: %w", err)
	}
	if addrs, err := net.LookupHost(cfg.Host); err == nil && len(addrs) > 0 {
		info.ResolvedAddr = strings.Join(addrs, ",")
	}
	printServerInfo(info)

	if cfg.SQL != "" {
		return runCustomSQL(ctx, conn, cfg)
	}

	listCtx, listCancel := context.WithTimeout(ctx, cfg.Timeout)
	databases, err := adapter.ListDatabases(listCtx, conn, cfg.IncludeSystem)
	listCancel()
	if err != nil {
		return fmt.Errorf("list databases: %w", err)
	}
	databases = scanDatabases(adapter, databases, cfg.Database)
	if len(databases) == 0 {
		output.Section(os.Stdout, "扫描结果")
		fmt.Fprintln(os.Stdout, "未发现可扫描数据库。")
		return nil
	}
	var reconnect scanner.Reconnector
	if adapter.NeedsDatabaseReconnect() {
		reconnect = func(ctx context.Context, database string) (*sql.DB, error) {
			nextCfg := dbCfg
			nextCfg.Database = database
			nextDB, err := adapter.Open(ctx, nextCfg, dialer)
			if err != nil {
				return nil, err
			}
			configureConnectionPool(nextDB, cfg.Workers)
			return nextDB, nil
		}
	}
	scanCtx, cancelScan := context.WithCancel(ctx)
	defer cancelScan()
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	scanDone := make(chan struct{})
	var interrupted atomic.Bool
	go func() {
		select {
		case <-ctx.Done():
			cancelScan()
			return
		case sig := <-signals:
			interrupted.Store(true)
			fmt.Fprintf(os.Stderr, "\n收到中断信号 %s，正在停止当前查询并输出已扫描结果...\n", sig)
			cancelScan()
			go func() {
				_ = conn.Close()
			}()
		case <-scanDone:
			return
		}

		select {
		case sig := <-signals:
			fmt.Fprintf(os.Stderr, "\n再次收到中断信号 %s，强制退出。\n", sig)
			os.Exit(130)
		case <-scanDone:
			return
		}
	}()
	var partialMu sync.Mutex
	partial := scanner.Result{}
	var progressWriter = os.Stderr
	if cfg.NoProgress {
		progressWriter = nil
	}
	result := scanner.Scan(scanCtx, conn, adapter, databases, scanner.Options{
		Mode: scanner.Mode(cfg.Mode), Limit: cfg.Limit, Workers: cfg.Workers, Timeout: cfg.Timeout,
		Level: cfg.Level, Mask: cfg.Mask, IncludeSystem: cfg.IncludeSystem, Table: cfg.Table, Progress: progressWriter,
		TextEncoding: cfg.TextEncoding,
		OnTable: func(table scanner.TableResult) {
			partialMu.Lock()
			partial.Tables = append(partial.Tables, table)
			partialMu.Unlock()
		},
	}, reconnect)
	close(scanDone)
	if scanCtx.Err() != nil || interrupted.Load() {
		partialMu.Lock()
		partial.Errors = append(partial.Errors, result.Errors...)
		result = partial
		partialMu.Unlock()
	}
	printScanResult(result, !cfg.NoColor)
	if cfg.Output != "" {
		if err := output.WriteXLSX(cfg.Output, result); err != nil {
			return fmt.Errorf("write xlsx output: %w", err)
		}
		outputPath, err := filepath.Abs(cfg.Output)
		if err != nil {
			outputPath = cfg.Output
		}
		fmt.Fprintf(os.Stdout, "\n已写入表格文件: %s\n", outputPath)
	}
	return nil
}

func runFscan(ctx context.Context, cfg Config) error {
	targets, err := fscanparse.ParseFile(cfg.Fscan)
	if err != nil {
		return fmt.Errorf("parse fscan result: %w", err)
	}
	if len(targets) == 0 {
		return fmt.Errorf("parse fscan result: no supported database credentials found")
	}
	fmt.Fprintf(os.Stdout, "从 fscan 结果中解析到 %d 个数据库凭据，开始批量接入扫描。\n", len(targets))
	merged := scanner.Result{}
	for i, target := range targets {
		fmt.Fprintf(os.Stdout, "\n[%d/%d] %s\n", i+1, len(targets), target.Label())
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
		result, err := scanAnyTarget(ctx, next)
		if err != nil {
			msg := fmt.Sprintf("%s: %v", target.Label(), err)
			fmt.Fprintf(os.Stdout, "扫描失败: %s\n", msg)
			merged.Errors = append(merged.Errors, msg)
			continue
		}
		prefixResultTables(result, target)
		if cfg.SplitOutput && cfg.Output != "" {
			splitPath := splitOutputPath(cfg.Output, target)
			if err := output.WriteXLSX(splitPath, result); err != nil {
				return fmt.Errorf("write split xlsx output: %w", err)
			}
			absSplitPath, err := filepath.Abs(splitPath)
			if err != nil {
				absSplitPath = splitPath
			}
			fmt.Fprintf(os.Stdout, "已写入独立表格文件: %s\n", absSplitPath)
		}
		merged.Tables = append(merged.Tables, result.Tables...)
		merged.Summaries = append(merged.Summaries, result.Summaries...)
		merged.Samples = append(merged.Samples, result.Samples...)
		merged.Errors = append(merged.Errors, result.Errors...)
		if target.Type == "redis" {
			printRedisResult(result, !cfg.NoColor)
		} else {
			printScanResult(result, !cfg.NoColor)
		}
	}
	if cfg.Output != "" {
		if err := output.WriteXLSX(cfg.Output, merged); err != nil {
			return fmt.Errorf("write xlsx output: %w", err)
		}
		outputPath, err := filepath.Abs(cfg.Output)
		if err != nil {
			outputPath = cfg.Output
		}
		fmt.Fprintf(os.Stdout, "\n已写入表格文件: %s\n", outputPath)
	}
	return nil
}

func scanAnyTarget(ctx context.Context, cfg Config) (scanner.Result, error) {
	if cfg.Type == "redis" {
		return scanRedisTarget(ctx, cfg)
	}
	return scanTarget(ctx, cfg)
}

func scanRedisTarget(ctx context.Context, cfg Config) (scanner.Result, error) {
	var progressWriter = os.Stderr
	if cfg.NoProgress {
		progressWriter = nil
	}
	info, result, err := redisscan.Scan(ctx, redisscan.Config{
		Host: cfg.Host, Port: cfg.Port, User: cfg.User, Password: cfg.Password, Database: cfg.Database, Proxy: cfg.Proxy,
		Timeout: cfg.Timeout, Limit: cfg.Limit, Level: cfg.Level, Mask: cfg.Mask, TextEncoding: cfg.TextEncoding, Progress: progressWriter,
	})
	if err != nil {
		return scanner.Result{}, err
	}
	printRedisInfo(info)
	return result, nil
}

func splitOutputPath(base string, target fscanparse.Target) string {
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if ext == "" {
		ext = ".xlsx"
	}
	suffix := sanitizeFilenamePart(fmt.Sprintf("%s_%s_%d_%s", target.Type, target.Host, target.Port, target.User))
	return stem + "-" + suffix + ext
}

func sanitizeFilenamePart(s string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '.' {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_.-")
	if out == "" {
		return "target"
	}
	return out
}

func printRedisInfo(info redisscan.Info) {
	output.Section(os.Stdout, "连接信息")
	rows := [][]string{
		{"目标", fmt.Sprintf("%s:%d", info.Host, info.Port)},
		{"解析IP", blank(info.ResolvedIP)},
		{"代理", blank(info.Proxy)},
		{"数据库类型", "Redis"},
		{"版本", blank(info.Version)},
		{"当前库", blank(info.DB)},
		{"服务端时间", blank(info.ServerTime)},
		{"模式", blank(info.Mode)},
		{"Keyspace", blank(info.Keyspace)},
		{"需要认证", strconv.FormatBool(info.RequireAuth)},
	}
	output.Table(os.Stdout, []string{"字段", "值"}, rows)
}

func scanTarget(ctx context.Context, cfg Config) (scanner.Result, error) {
	adapter, err := db.NewAdapter(cfg.Type)
	if err != nil {
		return scanner.Result{}, err
	}
	dialer, err := iproxy.FromURL(cfg.Proxy, cfg.Timeout)
	if err != nil {
		return scanner.Result{}, err
	}
	dbCfg := db.Config{
		Type: cfg.Type, Host: cfg.Host, Port: cfg.Port, User: cfg.User, Password: cfg.Password,
		Database: connectionDatabase(adapter, cfg.Database), Proxy: cfg.Proxy, IncludeSystem: cfg.IncludeSystem, Timeout: cfg.Timeout,
	}
	conn, err := adapter.Open(ctx, dbCfg, dialer)
	if err != nil {
		return scanner.Result{}, fmt.Errorf("connect database: %w", err)
	}
	defer conn.Close()
	configureConnectionPool(conn, cfg.Workers)

	infoCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	info, err := adapter.ServerInfo(infoCtx, conn, dbCfg)
	cancel()
	if err != nil {
		return scanner.Result{}, fmt.Errorf("read server info: %w", err)
	}
	if addrs, err := net.LookupHost(cfg.Host); err == nil && len(addrs) > 0 {
		info.ResolvedAddr = strings.Join(addrs, ",")
	}
	printServerInfo(info)

	listCtx, listCancel := context.WithTimeout(ctx, cfg.Timeout)
	databases, err := adapter.ListDatabases(listCtx, conn, cfg.IncludeSystem)
	listCancel()
	if err != nil {
		return scanner.Result{}, fmt.Errorf("list databases: %w", err)
	}
	databases = scanDatabases(adapter, databases, cfg.Database)
	if len(databases) == 0 {
		return scanner.Result{}, nil
	}
	var reconnect scanner.Reconnector
	if adapter.NeedsDatabaseReconnect() {
		reconnect = func(ctx context.Context, database string) (*sql.DB, error) {
			nextCfg := dbCfg
			nextCfg.Database = database
			nextDB, err := adapter.Open(ctx, nextCfg, dialer)
			if err != nil {
				return nil, err
			}
			configureConnectionPool(nextDB, cfg.Workers)
			return nextDB, nil
		}
	}
	var progressWriter = os.Stderr
	if cfg.NoProgress {
		progressWriter = nil
	}
	result := scanner.Scan(ctx, conn, adapter, databases, scanner.Options{
		Mode: scanner.Mode(cfg.Mode), Limit: cfg.Limit, Workers: cfg.Workers, Timeout: cfg.Timeout,
		Level: cfg.Level, Mask: cfg.Mask, IncludeSystem: cfg.IncludeSystem, Table: cfg.Table, TextEncoding: cfg.TextEncoding, Progress: progressWriter,
	}, reconnect)
	return result, nil
}

func prefixResultTables(result scanner.Result, target fscanparse.Target) {
	prefix := fmt.Sprintf("%s:%d", target.Host, target.Port)
	for i := range result.Tables {
		result.Tables[i].Database = prefix + "/" + result.Tables[i].Database
	}
	for i := range result.Summaries {
		result.Summaries[i].Database = prefix + "/" + result.Summaries[i].Database
	}
	for i := range result.Samples {
		result.Samples[i].Database = prefix + "/" + result.Samples[i].Database
	}
}

func configureConnectionPool(conn *sql.DB, workers int) {
	if workers < 1 {
		workers = 1
	}
	conn.SetMaxOpenConns(workers)
	conn.SetMaxIdleConns(workers)
}

func scanDatabases(adapter db.Adapter, available []string, wanted string) []string {
	wantedDatabases := splitCommaList(wanted)
	if len(wantedDatabases) > 0 && adapter.Family() != "oracle" {
		return wantedDatabases
	}
	if adapter.Family() == "oracle" {
		out := append([]string(nil), available...)
		sort.Strings(out)
		return out
	}
	out := append([]string(nil), available...)
	sort.Strings(out)
	return out
}

func connectionDatabase(adapter db.Adapter, wanted string) string {
	if adapter.Family() != "oracle" && len(splitCommaList(wanted)) > 1 {
		return ""
	}
	return strings.TrimSpace(wanted)
}

func splitCommaList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func printServerInfo(info db.ServerInfo) {
	output.Section(os.Stdout, "连接信息")
	rows := [][]string{
		{"目标", fmt.Sprintf("%s:%d", info.Host, info.Port)},
		{"解析IP", blank(info.ResolvedAddr)},
		{"代理", blank(info.Proxy)},
		{"数据库类型", info.DBType},
		{"版本", output.OneLine(info.Version)},
		{"当前用户", info.CurrentUser},
		{"当前库", blank(info.CurrentDB)},
		{"服务端时间", info.ServerTime},
		{"包含系统库", strconv.FormatBool(info.IncludeSystem)},
	}
	keys := make([]string, 0, len(info.Environment))
	for k := range info.Environment {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		rows = append(rows, []string{k, info.Environment[k]})
	}
	output.Table(os.Stdout, []string{"字段", "值"}, rows)
}

func printScanResult(result scanner.Result, color bool) {
	output.Section(os.Stdout, "敏感字段与样例值")
	if len(result.Tables) == 0 {
		fmt.Fprintln(os.Stdout, "未发现敏感信息命中。")
	} else {
		for i, table := range result.Tables {
			if i > 0 {
				fmt.Fprintln(os.Stdout)
			}
			fmt.Fprintf(os.Stdout, "[数据库] %s\n", table.Database)
			fmt.Fprintf(os.Stdout, "[表] %s【实际数据行数：%d】\n", tableDisplayName(table), table.Total)
			for _, row := range scanner.SensitiveFieldRows(table.Fields, color) {
				fmt.Fprintf(os.Stdout, "%s （存在行数：%s）\n", row[0], row[1])
			}
			headers, rows := scanner.RowSampleRows(table, color)
			if len(rows) == 0 {
				fmt.Fprintln(os.Stdout, "数据库真实样例值: 无")
			} else {
				output.Table(os.Stdout, headers, rows)
			}
		}
	}
	if len(result.Errors) > 0 {
		output.Section(os.Stdout, "扫描错误")
		rows := make([][]string, 0, len(result.Errors))
		for _, err := range result.Errors {
			rows = append(rows, []string{err})
		}
		output.Table(os.Stdout, []string{"错误"}, rows)
	}
}

func printRedisResult(result scanner.Result, color bool) {
	output.Section(os.Stdout, "Redis 敏感 Key 与样例值")
	if len(result.Tables) == 0 {
		fmt.Fprintln(os.Stdout, "未发现敏感 Redis key/value 命中。")
	} else {
		rows := make([][]string, 0, len(result.Tables))
		for _, table := range result.Tables {
			if len(table.Rows) == 0 {
				continue
			}
			sample := table.Rows[0].Values
			rows = append(rows, []string{
				sample["Target"],
				sample["DB"],
				sample["Key"],
				sample["Type"],
				sample["TTL"],
				sample["Path/Field"],
				colorRedisValue(sample["Value"], table.Fields, color),
				sample["命中类型"],
				sample["敏感级别"],
				sample["判断依据"],
			})
		}
		output.Table(os.Stdout, []string{"Target", "DB", "Key", "Type", "TTL", "Path/Field", "Value", "命中类型", "敏感级别", "判断依据"}, rows)
	}
	if len(result.Errors) > 0 {
		output.Section(os.Stdout, "扫描错误")
		rows := make([][]string, 0, len(result.Errors))
		for _, err := range result.Errors {
			rows = append(rows, []string{err})
		}
		output.Table(os.Stdout, []string{"错误"}, rows)
	}
}

func colorRedisValue(value string, fields []scanner.FieldResult, color bool) string {
	for _, field := range fields {
		if field.Name == "value" || field.Name == "Value" {
			return scanner.ColorizeValue(value, field.Kinds, color)
		}
	}
	return value
}

func tableDisplayName(table scanner.TableResult) string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(table.Schema) != "" {
		parts = append(parts, table.Schema)
	}
	if strings.TrimSpace(table.Name) != "" {
		parts = append(parts, table.Name)
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ".")
}

func blank(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func runCustomSQL(ctx context.Context, conn *sql.DB, cfg Config) error {
	output.Section(os.Stdout, "自定义SQL")
	result, err := executeCustomSQL(ctx, conn, cfg.SQL, cfg.Limit, cfg.Timeout, cfg.TextEncoding)
	if err != nil {
		return err
	}
	if result.IsQuery {
		output.Table(os.Stdout, result.Columns, result.Rows)
		fmt.Fprintf(os.Stdout, "总返回行数: %d，已显示: %d\n", result.Total, result.Shown)
		return nil
	}
	fmt.Fprintf(os.Stdout, "SQL执行完成，影响行数: %d\n", result.Affected)
	return nil
}

func stringify(v any) string {
	return stringifyWithEncoding(v, textfix.EncodingAuto)
}

func stringifyWithEncoding(v any, textEncoding string) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return textfix.RepairBytes(x, textEncoding)
	default:
		return textfix.RepairString(fmt.Sprint(x), textEncoding)
	}
}
