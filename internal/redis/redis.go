package redis

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"database_scan/internal/detector"
	iproxy "database_scan/internal/proxy"
	"database_scan/internal/scanner"
)

const maxValueBytes = 4096

type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
	Proxy    string
	Timeout  time.Duration
	Limit    int
	Level    detector.Level
	Mask     bool
	Progress io.Writer
}

type Info struct {
	Host        string
	Port        int
	Version     string
	Mode        string
	DB          string
	Keyspace    string
	ResolvedIP  string
	Proxy       string
	ServerTime  string
	RequireAuth bool
}

type client struct {
	conn net.Conn
	r    *bufio.Reader
}

func Scan(ctx context.Context, cfg Config) (Info, scanner.Result, error) {
	dialer, err := iproxy.FromURL(cfg.Proxy, cfg.Timeout)
	if err != nil {
		return Info{}, scanner.Result{}, err
	}
	if cfg.Port == 0 {
		cfg.Port = 6379
	}
	address := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dialCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	conn, err := dialer.DialContext(dialCtx, "tcp", address)
	if err != nil {
		return Info{}, scanner.Result{}, fmt.Errorf("connect redis: %w", err)
	}
	defer conn.Close()
	c := &client{conn: conn, r: bufio.NewReader(conn)}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if cfg.Timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(cfg.Timeout))
	}
	if cfg.Password != "" {
		args := []string{"AUTH", cfg.Password}
		if cfg.User != "" {
			args = []string{"AUTH", cfg.User, cfg.Password}
		}
		if _, err := c.command(args...); err != nil {
			return Info{}, scanner.Result{}, fmt.Errorf("redis auth: %w", err)
		}
	}
	if cfg.Database != "" {
		if _, err := c.command("SELECT", cfg.Database); err != nil {
			return Info{}, scanner.Result{}, fmt.Errorf("redis select db %s: %w", cfg.Database, err)
		}
	}
	info, err := readInfo(c, cfg)
	if err != nil {
		return Info{}, scanner.Result{}, err
	}
	keys, err := scanKeys(ctx, c, cfg)
	if err != nil {
		return info, scanner.Result{}, err
	}
	result := scanKeyValues(ctx, c, cfg, keys)
	return info, result, nil
}

func readInfo(c *client, cfg Config) (Info, error) {
	reply, err := c.command("INFO")
	if err != nil {
		return Info{}, fmt.Errorf("redis info: %w", err)
	}
	text, _ := reply.(string)
	values := parseInfo(text)
	info := Info{
		Host:        cfg.Host,
		Port:        cfg.Port,
		Version:     values["redis_version"],
		Mode:        values["redis_mode"],
		Keyspace:    keyspaceSummary(values),
		Proxy:       cfg.Proxy,
		RequireAuth: cfg.Password != "" || (values["requirepass"] != "" && values["requirepass"] != "0"),
	}
	if cfg.Database == "" {
		info.DB = "0"
	} else {
		info.DB = cfg.Database
	}
	if addrs, err := net.LookupHost(cfg.Host); err == nil && len(addrs) > 0 {
		info.ResolvedIP = strings.Join(addrs, ",")
	}
	if t, err := c.command("TIME"); err == nil {
		if parts, ok := t.([]any); ok && len(parts) >= 1 {
			info.ServerTime = fmt.Sprint(parts[0])
		}
	}
	return info, nil
}

func scanKeys(ctx context.Context, c *client, cfg Config) ([]string, error) {
	var cursor int64
	seen := map[string]bool{}
	var keys []string
	for {
		if err := ctx.Err(); err != nil {
			return keys, err
		}
		reply, err := c.command("SCAN", strconv.FormatInt(cursor, 10), "COUNT", "200")
		if err != nil {
			return keys, fmt.Errorf("redis scan: %w", err)
		}
		parts, ok := reply.([]any)
		if !ok || len(parts) != 2 {
			return keys, fmt.Errorf("redis scan: unexpected reply %T", reply)
		}
		next, err := strconv.ParseInt(fmt.Sprint(parts[0]), 10, 64)
		if err != nil {
			return keys, fmt.Errorf("redis scan cursor: %w", err)
		}
		if batch, ok := parts[1].([]any); ok {
			for _, item := range batch {
				key := fmt.Sprint(item)
				if key == "" || seen[key] {
					continue
				}
				seen[key] = true
				keys = append(keys, key)
			}
		}
		cursor = next
		if cursor == 0 {
			return keys, nil
		}
	}
}

func scanKeyValues(ctx context.Context, c *client, cfg Config, keys []string) scanner.Result {
	var result scanner.Result
	for i, key := range keys {
		if err := ctx.Err(); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("scan interrupted: %v", err))
			return result
		}
		progressf(cfg.Progress, "扫描 Redis key %d/%d %s...\n", i+1, len(keys), key)
		entry, err := readKey(c, cfg, key)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("redis key %s: %v", key, err))
			continue
		}
		kinds := redisKinds(cfg.Level, key, entry.Value)
		if len(kinds) == 0 {
			continue
		}
		fields := []scanner.FieldResult{
			{Name: "key", Kinds: detector.FieldKindsByLevel(cfg.Level, key), Level: highestLevel(detector.FieldKindsByLevel(cfg.Level, key)), Mode: scanner.FieldName, Total: 1},
			{Name: "value", Kinds: kinds, Level: highestLevel(kinds), Mode: scanner.Content, Total: 1},
		}
		fields = compactFields(fields)
		row := scanner.RowSample{Values: map[string]string{
			"Target":     net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
			"DB":         entry.DB,
			"Key":        key,
			"Type":       entry.Type,
			"TTL":        entry.TTL,
			"Path/Field": entry.Path,
			"Value":      maskValue(kinds, entry.Value, cfg.Mask),
			"命中类型":       scanner.KindLabel(kinds),
			"敏感级别":       detector.LevelLabel(highestLevel(kinds)),
			"判断依据":       redisReason(cfg.Level, key, entry.Value),
		}}
		table := scanner.TableResult{
			Database: "redis-db" + entry.DB,
			Schema:   "redis-key",
			Name:     key,
			Total:    1,
			Columns:  []string{"Target", "DB", "Key", "Type", "TTL", "Path/Field", "Value", "命中类型", "敏感级别", "判断依据"},
			Fields:   fields,
			Rows:     []scanner.RowSample{row},
		}
		result.Tables = append(result.Tables, table)
		for _, kind := range kinds {
			result.Summaries = append(result.Summaries, scanner.Summary{Database: table.Database, Schema: table.Schema, Table: table.Name, Column: "value", Kind: kind, Level: detector.LevelOf(kind), Mode: scanner.Content, Total: 1})
		}
		if len(result.Tables) >= cfg.Limit && cfg.Limit > 0 {
			break
		}
	}
	return result
}

type keyEntry struct {
	DB    string
	Type  string
	TTL   string
	Path  string
	Value string
}

func readKey(c *client, cfg Config, key string) (keyEntry, error) {
	typ, err := c.command("TYPE", key)
	if err != nil {
		return keyEntry{}, err
	}
	ttl, _ := c.command("TTL", key)
	db := cfg.Database
	if db == "" {
		db = "0"
	}
	entry := keyEntry{DB: db, Type: fmt.Sprint(typ), TTL: fmt.Sprint(ttl)}
	switch entry.Type {
	case "string":
		value, err := c.command("GET", key)
		if err != nil {
			return entry, err
		}
		entry.Path = "value"
		entry.Value = truncate(fmt.Sprint(value), maxValueBytes)
	case "hash":
		value, err := c.command("HGETALL", key)
		if err != nil {
			return entry, err
		}
		entry.Path, entry.Value = flattenHash(value)
	case "list":
		value, err := c.command("LRANGE", key, "0", "20")
		if err != nil {
			return entry, err
		}
		entry.Path = "[0..20]"
		entry.Value = flattenArray(value)
	case "set":
		value, err := c.command("SSCAN", key, "0", "COUNT", "20")
		if err != nil {
			return entry, err
		}
		entry.Path = "members"
		entry.Value = flattenArray(value)
	case "zset":
		value, err := c.command("ZRANGE", key, "0", "20", "WITHSCORES")
		if err != nil {
			return entry, err
		}
		entry.Path = "members"
		entry.Value = flattenArray(value)
	default:
		entry.Path = "value"
		entry.Value = "<unsupported redis type>"
	}
	return entry, nil
}

func (c *client) command(args ...string) (any, error) {
	if _, err := c.conn.Write(encodeCommand(args...)); err != nil {
		return nil, err
	}
	return readRESP(c.r)
}

func encodeCommand(args ...string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(arg), arg)
	}
	return b.Bytes()
}

func readRESP(r *bufio.Reader) (any, error) {
	prefix, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	switch prefix {
	case '+':
		return line, nil
	case '-':
		return nil, errors.New(line)
	case ':':
		return strconv.ParseInt(line, 10, 64)
	case '$':
		n, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return "", nil
		}
		buf := make([]byte, n+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return string(buf[:n]), nil
	case '*':
		n, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return []any(nil), nil
		}
		out := make([]any, 0, n)
		for i := 0; i < n; i++ {
			item, err := readRESP(r)
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unexpected redis reply prefix %q", prefix)
	}
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}

func parseInfo(s string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if ok {
			out[key] = value
		}
	}
	return out
}

func keyspaceSummary(values map[string]string) string {
	var parts []string
	for key, value := range values {
		if strings.HasPrefix(key, "db") {
			parts = append(parts, key+"="+value)
		}
	}
	return strings.Join(parts, " ")
}

func flattenArray(v any) string {
	items, ok := v.([]any)
	if !ok {
		return truncate(fmt.Sprint(v), maxValueBytes)
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if nested, ok := item.([]any); ok {
			parts = append(parts, flattenArray(nested))
			continue
		}
		parts = append(parts, fmt.Sprint(item))
	}
	return truncate(strings.Join(parts, " "), maxValueBytes)
}

func flattenHash(v any) (string, string) {
	items, ok := v.([]any)
	if !ok {
		return "fields", truncate(fmt.Sprint(v), maxValueBytes)
	}
	var fields []string
	var pairs []string
	for i := 0; i+1 < len(items); i += 2 {
		field := fmt.Sprint(items[i])
		value := fmt.Sprint(items[i+1])
		fields = append(fields, field)
		pairs = append(pairs, field+"="+value)
	}
	return strings.Join(fields, ","), truncate(strings.Join(pairs, " "), maxValueBytes)
}

func redisReason(level detector.Level, key, value string) string {
	var reasons []string
	if len(detector.FieldKindsByLevel(level, key)) > 0 {
		reasons = append(reasons, "key")
	}
	if len(detector.ContentKindsByLevel(level, value)) > 0 {
		reasons = append(reasons, "value")
	}
	if len(reasons) == 0 {
		return "-"
	}
	return strings.Join(reasons, "+")
}

func redisKinds(level detector.Level, key, value string) []detector.Kind {
	seen := map[detector.Kind]bool{}
	var kinds []detector.Kind
	for _, kind := range detector.FieldKindsByLevel(level, key) {
		if !seen[kind] {
			seen[kind] = true
			kinds = append(kinds, kind)
		}
	}
	for _, kind := range detector.ContentKindsByLevel(level, value) {
		if !seen[kind] {
			seen[kind] = true
			kinds = append(kinds, kind)
		}
	}
	return kinds
}

func highestLevel(kinds []detector.Kind) detector.Level {
	level := detector.LevelLow
	for _, kind := range kinds {
		switch detector.LevelOf(kind) {
		case detector.LevelHigh:
			return detector.LevelHigh
		case detector.LevelMedium:
			level = detector.LevelMedium
		}
	}
	return level
}

func compactFields(fields []scanner.FieldResult) []scanner.FieldResult {
	out := fields[:0]
	for _, field := range fields {
		if len(field.Kinds) > 0 {
			out = append(out, field)
		}
	}
	return out
}

func maskValue(kinds []detector.Kind, value string, mask bool) string {
	if !mask || len(kinds) == 0 {
		return value
	}
	return detector.Mask(kinds[0], value)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...<truncated>"
}

func progressf(w io.Writer, format string, args ...any) {
	if w != nil {
		fmt.Fprintf(w, format, args...)
	}
}
