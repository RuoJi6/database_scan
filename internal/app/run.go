package app

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"database_scan/internal/db"
	"database_scan/internal/output"
	iproxy "database_scan/internal/proxy"
	"database_scan/internal/scanner"
)

func Run(ctx context.Context, args []string) error {
	cfg, err := parseArgs(args)
	if err != nil {
		return err
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
		Database: cfg.Database, Proxy: cfg.Proxy, IncludeSystem: cfg.IncludeSystem, Timeout: cfg.Timeout,
	}
	conn, err := adapter.Open(ctx, dbCfg, dialer)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer conn.Close()

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
			return adapter.Open(ctx, nextCfg, dialer)
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
	result := scanner.Scan(scanCtx, conn, adapter, databases, scanner.Options{
		Mode: scanner.Mode(cfg.Mode), Limit: cfg.Limit, Workers: cfg.Workers, Timeout: cfg.Timeout,
		Level: cfg.Level, Mask: cfg.Mask, IncludeSystem: cfg.IncludeSystem, Table: cfg.Table, Progress: os.Stderr,
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
		fmt.Fprintf(os.Stdout, "\n已写入表格文件: %s\n", cfg.Output)
	}
	return nil
}

func scanDatabases(adapter db.Adapter, available []string, wanted string) []string {
	if adapter.Family() == "oracle" {
		out := append([]string(nil), available...)
		sort.Strings(out)
		return out
	}
	if strings.TrimSpace(wanted) != "" {
		return []string{wanted}
	}
	out := append([]string(nil), available...)
	sort.Strings(out)
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
			fmt.Fprintf(os.Stdout, "[表] %s.%s【实际数据行数：%d】\n", table.Schema, table.Name, table.Total)
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

func blank(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func runCustomSQL(ctx context.Context, conn *sql.DB, cfg Config) error {
	output.Section(os.Stdout, "自定义SQL")
	queryCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	rows, err := conn.QueryContext(queryCtx, cfg.SQL)
	if err == nil {
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			return err
		}
		var tableRows [][]string
		total := 0
		for rows.Next() {
			total++
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return err
			}
			if len(tableRows) < cfg.Limit {
				row := make([]string, len(cols))
				for i, v := range values {
					row[i] = stringify(v)
				}
				tableRows = append(tableRows, row)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		output.Table(os.Stdout, cols, tableRows)
		fmt.Fprintf(os.Stdout, "总返回行数: %d，已显示: %d\n", total, len(tableRows))
		return nil
	}
	result, execErr := conn.ExecContext(queryCtx, cfg.SQL)
	if execErr != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	fmt.Fprintf(os.Stdout, "SQL执行完成，影响行数: %d\n", affected)
	return nil
}

func stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return string(x)
	default:
		return fmt.Sprint(x)
	}
}
