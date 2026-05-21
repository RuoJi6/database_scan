package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"database_scan/internal/db"
	"database_scan/internal/detector"

	"golang.org/x/term"
)

const (
	projectName = "database_scan"
	projectURL  = "https://github.com/RuoJi6/database_scan"
)

var errHelp = errors.New("help requested")

type Config struct {
	Type           string
	Host           string
	Port           int
	User           string
	Password       string
	Database       string
	Table          string
	Proxy          string
	Mode           string
	Level          detector.Level
	Limit          int
	SQL            string
	Output         string
	Fscan          string
	FscanText      string
	SplitOutput    bool
	IncludeSystem  bool
	Mask           bool
	TestConnection bool
	NoColor        bool
	NoBanner       bool
	NoProgress     bool
	Workers        int
	Timeout        time.Duration
}

func parseArgs(args []string) (Config, error) {
	cfg := Config{Mode: "field-content", Level: detector.LevelAll, Limit: 15, Workers: 1, Timeout: 15 * time.Second}
	args, target := splitTargetArg(args)
	fs := flag.NewFlagSet("database_scan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.Type, "type", "", "database type: mysql, mssql, postgres, redis")
	fs.StringVar(&cfg.Host, "host", "", "database host")
	fs.IntVar(&cfg.Port, "port", 0, "database port")
	fs.StringVar(&cfg.User, "user", "", "database username")
	fs.StringVar(&cfg.Password, "password", "", "database password")
	fs.StringVar(&cfg.Database, "database", "", "initial database; comma-separated values are supported")
	fs.StringVar(&cfg.Table, "table", "", "scan only these tables; supports table, schema.table, and comma-separated values")
	fs.StringVar(&cfg.Proxy, "proxy", "", "proxy url: socks5://user:pass@host:port or http://user:pass@host:port")
	fs.StringVar(&cfg.Mode, "mode", "field-content", "scan mode: field-content, field-name, content, all")
	level := string(cfg.Level)
	fs.StringVar(&level, "level", string(detector.LevelAll), "sensitive level: all, high, medium, low")
	fs.IntVar(&cfg.Limit, "limit", 15, "max sample rows to display")
	fs.StringVar(&cfg.SQL, "sql", "", "custom SQL to execute")
	fs.StringVar(&cfg.Output, "output", "", "write scan result to .xlsx file")
	fs.StringVar(&cfg.Fscan, "fscan", "", "parse fscan result file and scan discovered database credentials")
	fs.BoolVar(&cfg.SplitOutput, "split-output", false, "with --fscan and --output, also write one .xlsx per discovered database credential")
	fs.BoolVar(&cfg.IncludeSystem, "include-system", false, "include system databases")
	fs.BoolVar(&cfg.Mask, "mask", false, "mask sensitive sample values")
	fs.BoolVar(&cfg.TestConnection, "test-connection", false, "test database connection, including proxy when --proxy is set, then exit")
	fs.BoolVar(&cfg.NoColor, "no-color", false, "disable colored output")
	fs.BoolVar(&cfg.NoBanner, "no-banner", false, "disable startup banner")
	fs.BoolVar(&cfg.NoProgress, "no-progress", false, "disable scan progress output")
	fs.IntVar(&cfg.Workers, "workers", 1, "scan workers; 1 disables parallel table scanning")
	fs.DurationVar(&cfg.Timeout, "timeout", 15*time.Second, "single query timeout")
	fs.Usage = func() {
		printHelp(os.Stdout, helpColorEnabled(args), bannerEnabled(args))
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return cfg, errHelp
		}
		return cfg, err
	}
	if cfg.Host == "" {
		if target != "" {
			cfg.Host = target
		} else if fs.NArg() > 0 {
			cfg.Host = fs.Arg(0)
		}
	}
	cfg.Type = strings.ToLower(strings.TrimSpace(cfg.Type))
	cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	parsedLevel, ok := detector.ParseLevel(level)
	if !ok {
		return cfg, fmt.Errorf("unsupported --level %q", level)
	}
	cfg.Level = parsedLevel
	if err := normalizeTarget(&cfg); err != nil {
		return cfg, err
	}
	if cfg.Fscan == "" && (cfg.Type == "" || cfg.Host == "" || (cfg.User == "" && cfg.Type != "redis")) {
		return cfg, fmt.Errorf("--type, --host and --user are required")
	}
	if cfg.Port == 0 && cfg.Type != "" {
		adapter, err := db.NewAdapter(cfg.Type)
		if err != nil {
			return cfg, err
		}
		cfg.Port = adapter.DefaultPort()
	}
	if cfg.Fscan == "" && cfg.Password == "" && cfg.Type != "redis" {
		fmt.Fprint(os.Stderr, "Password: ")
		pass, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return cfg, fmt.Errorf("read password: %w", err)
		}
		cfg.Password = string(pass)
	}
	if cfg.Limit <= 0 {
		return cfg, fmt.Errorf("--limit must be greater than 0")
	}
	if strings.TrimSpace(cfg.Table) != "" && strings.TrimSpace(cfg.Database) == "" {
		return cfg, fmt.Errorf("--table requires --database")
	}
	if cfg.SplitOutput && strings.TrimSpace(cfg.Output) == "" {
		return cfg, fmt.Errorf("--split-output requires --output")
	}
	if cfg.Workers <= 0 {
		return cfg, fmt.Errorf("--workers must be greater than 0")
	}
	switch cfg.Mode {
	case "field-content", "field-name", "content", "all":
	default:
		return cfg, fmt.Errorf("unsupported --mode %q", cfg.Mode)
	}
	return cfg, nil
}

func isHelp(err error) bool {
	return errors.Is(err, errHelp)
}

func helpColorEnabled(args []string) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	for _, arg := range args {
		if arg == "--no-color" || arg == "-no-color" {
			return false
		}
	}
	return true
}

func bannerEnabled(args []string) bool {
	for _, arg := range args {
		if arg == "--no-banner" || arg == "-no-banner" {
			return false
		}
	}
	return true
}

func printHelp(w io.Writer, color bool, banner bool) {
	if banner {
		printBanner(w, color)
	}
	c := func(code, s string) string {
		if !color {
			return s
		}
		return "\x1b[" + code + "m" + s + "\x1b[0m"
	}
	title := c("1;36", projectName)
	url := c("4;34", projectURL)
	section := func(s string) string { return c("1;33", s) }
	flagName := func(s string) string { return c("36", s) }
	fmt.Fprintf(w, "%s\n", title)
	fmt.Fprintln(w, c("90", "数据库敏感信息扫描与整行样例导出工具"))
	fmt.Fprintf(w, "项目地址: %s\n\n", url)
	fmt.Fprintf(w, "%s\n", section("Usage"))
	fmt.Fprintf(w, "  %s --type <mysql|mssql|postgres|redis|oracle|oceanbase|opengauss|kingbase> <host:port> --user <user> [options]\n\n", projectName)
	fmt.Fprintf(w, "%s\n", section("Examples"))
	fmt.Fprintf(w, "  %s --type mssql 192.0.2.10:1433 --user sa --password pass --database appdb\n", projectName)
	fmt.Fprintf(w, "  %s --type mssql 192.0.2.10:1433 --user sa --password pass --database appdb --table dbo.Users --output result.xlsx\n", projectName)
	fmt.Fprintf(w, "  %s --type mysql 192.0.2.20:3306 --user root --password pass --database app,audit --table users,orders\n", projectName)
	fmt.Fprintf(w, "  %s --type postgres --host 198.51.100.10 --user dev --password pass --level high --workers 4\n\n", projectName)
	fmt.Fprintf(w, "%s\n", section("Target"))
	helpFlag(w, flagName("--type"), "数据库类型：mysql、mssql、postgres、redis、oracle、oceanbase、opengauss、kingbase")
	helpFlag(w, flagName("--host"), "目标地址；也支持把 host:port 作为位置参数")
	helpFlag(w, flagName("--port"), "目标端口，不填时使用数据库默认端口")
	helpFlag(w, flagName("--proxy"), "代理地址：socks5://... 或 http://...")
	helpFlag(w, flagName("--test-connection"), "只测试数据库连接；设置 --proxy 时同时验证代理链路")
	fmt.Fprintf(w, "\n%s\n", section("Auth"))
	helpFlag(w, flagName("--user"), "数据库用户名")
	helpFlag(w, flagName("--password"), "数据库密码；不填时隐藏交互输入")
	fmt.Fprintf(w, "\n%s\n", section("Scan"))
	helpFlag(w, flagName("--database"), "指定数据库；多个用逗号分隔；不指定时扫描全部可访问数据库")
	helpFlag(w, flagName("--table"), "只扫描指定表，需要同时指定 --database；支持 Users、dbo.Users，多个用逗号分隔")
	helpFlag(w, flagName("--fscan"), "解析 fscan 扫描结果中的数据库凭据并批量接入扫描")
	helpFlag(w, flagName("--mode"), "扫描模式：field-content、field-name、content、all；默认 field-content")
	helpFlag(w, flagName("--level"), "敏感级别：all、high、medium、low；默认 all")
	helpFlag(w, flagName("--limit"), "每张命中表最多展示整行样例数量；默认 15")
	helpFlag(w, flagName("--workers"), "按表并发扫描数量；默认 1 表示不启用并发")
	helpFlag(w, flagName("--timeout"), "单查询超时；默认 15s")
	helpFlag(w, flagName("--include-system"), "包含系统库")
	fmt.Fprintf(w, "\n%s\n", section("Output"))
	helpFlag(w, flagName("--output"), "写入 Excel 文件；第一个 Sheet 为敏感信息汇总")
	helpFlag(w, flagName("--split-output"), "配合 --fscan 和 --output 使用，额外为每个数据库凭据生成独立 Excel")
	helpFlag(w, flagName("--mask"), "样例值脱敏显示")
	helpFlag(w, flagName("--no-color"), "关闭终端颜色；也可设置 NO_COLOR=1")
	helpFlag(w, flagName("--no-banner"), "关闭启动随机颜文字 banner")
	helpFlag(w, flagName("--no-progress"), "关闭运行状态/扫描进度输出")
	helpFlag(w, flagName("--sql"), "执行自定义 SQL")
	helpFlag(w, flagName("-h, --help"), "显示此帮助")
}

func helpFlag(w io.Writer, name, desc string) {
	fmt.Fprintf(w, "  %-18s %s\n", name, desc)
}

func printBanner(w io.Writer, color bool) {
	banners := bannerTemplates()
	palette := []string{"1;36", "1;35", "1;32", "1;34"}
	idx := rand.Intn(len(banners))
	body := banners[idx]
	if color {
		body = "\x1b[" + palette[idx%len(palette)] + "m" + body + "\x1b[0m"
	}
	fmt.Fprintln(w, body)
	fmt.Fprintln(w)
}

func bannerTemplates() []string {
	return []string{
		"   ( •_•)   database_scan\n   / >*    sensitive data mapper\n          github.com/RuoJi6/database_scan",
		"   ╭( ･ㅂ･)و ̑̑  database_scan\n   ├─ table-aware sensitive scanner\n   ╰─ github.com/RuoJi6/database_scan",
		"   (ง •̀_•́)ง  database_scan\n   [sql] -> [fields] -> [full-row proof]\n   https://github.com/RuoJi6/database_scan",
		"   (づ｡◕‿‿◕｡)づ  database_scan\n   dumping rows, not dignity\n   https://github.com/RuoJi6/database_scan",
		"   (⌐■_■)  database_scan\n   ===[ schema recon :: sensitive proof ]===\n   github.com/RuoJi6/database_scan",
		"   (｀･ω･´)ゞ  database_scan\n   tables locked, samples loaded\n   github.com/RuoJi6/database_scan",
		"   (｡•̀ᴗ-)✧  database_scan\n   field names lie; rows confess\n   github.com/RuoJi6/database_scan",
		"   (ﾉ◕ヮ◕)ﾉ*:･ﾟ✧  database_scan\n   xlsx summaries with colored risk\n   github.com/RuoJi6/database_scan",
		"      _      _        _\n   __| | ___| |_ __ _| |__   __ _ ___  ___\n  / _` |/ _ \\ __/ _` | '_ \\ / _` / __|/ _ \\\n | (_| |  __/ || (_| | |_) | (_| \\__ \\  __/\n  \\__,_|\\___|\\__\\__,_|_.__/ \\__,_|___/\\___|\n        database_scan  ::  github.com/RuoJi6/database_scan",
		"   =[ database_scan ]====================\n   + target: tables, columns, rows\n   + output: terminal, xlsx, proof\n   + repo  : github.com/RuoJi6/database_scan",
		"   .: database_scan :.\n   [ enumerate ] -> [ classify ] -> [ sample rows ]\n   repo: github.com/RuoJi6/database_scan",
		"   ┌─[ database_scan ]─[ sensitive mapper ]\n   ├─ risk levels: high / medium / low\n   └─ github.com/RuoJi6/database_scan",
	}
}

func normalizeTarget(cfg *Config) error {
	cfg.Host = strings.TrimSpace(cfg.Host)
	if cfg.Host == "" {
		return nil
	}
	host, portText, err := net.SplitHostPort(cfg.Host)
	if err != nil {
		if strings.Contains(err.Error(), "missing port in address") {
			return nil
		}
		if net.ParseIP(cfg.Host) != nil {
			return nil
		}
		return fmt.Errorf("parse --host: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return fmt.Errorf("parse --host: invalid port %q", portText)
	}
	if cfg.Port != 0 && cfg.Port != port {
		return fmt.Errorf("--host port %d conflicts with --port %d", port, cfg.Port)
	}
	cfg.Host = host
	cfg.Port = port
	return nil
}

func splitTargetArg(args []string) ([]string, string) {
	valueFlags := map[string]bool{
		"type": true, "host": true, "port": true, "user": true, "password": true,
		"database": true, "table": true, "proxy": true, "mode": true, "level": true, "limit": true, "output": true, "workers": true,
		"timeout": true, "sql": true,
		"fscan": true,
	}
	out := make([]string, 0, len(args))
	var target string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			out = append(out, args[i:]...)
			break
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			out = append(out, arg)
			name := strings.TrimLeft(arg, "-")
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				name = name[:eq]
			}
			if valueFlags[name] && !strings.Contains(arg, "=") && i+1 < len(args) {
				i++
				out = append(out, args[i])
			}
			continue
		}
		if target == "" {
			target = arg
			continue
		}
		out = append(out, arg)
	}
	return out, target
}
