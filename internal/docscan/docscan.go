package docscan

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"database_scan/internal/db"
	"database_scan/internal/detector"
	"database_scan/internal/scanner"
)

type Config struct {
	Type          string
	Host          string
	Port          int
	User          string
	Password      string
	Database      string
	AuthDatabase  string
	Proxy         string
	Timeout       time.Duration
	Limit         int
	Level         detector.Level
	Mask          bool
	TextEncoding  string
	IncludeSystem bool
	Progress      io.Writer
}

type valueEntry struct {
	Path  string
	Value string
}

func progressf(w io.Writer, format string, args ...any) {
	if w != nil {
		fmt.Fprintf(w, format, args...)
	}
}

func targetLabel(cfg Config) string {
	return net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
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

func combineKinds(level detector.Level, path, value string) ([]detector.Kind, string) {
	seen := map[detector.Kind]bool{}
	var kinds []detector.Kind
	var reasons []string
	fieldKinds := detector.FieldKindsByLevel(level, path)
	if len(fieldKinds) > 0 {
		reasons = append(reasons, "field")
	}
	for _, kind := range fieldKinds {
		if !seen[kind] {
			seen[kind] = true
			kinds = append(kinds, kind)
		}
	}
	contentKinds := detector.ContentKindsByLevel(level, value)
	if len(contentKinds) > 0 {
		reasons = append(reasons, "value")
	}
	for _, kind := range contentKinds {
		if !seen[kind] {
			seen[kind] = true
			kinds = append(kinds, kind)
		}
	}
	if len(reasons) == 0 {
		return nil, "-"
	}
	return kinds, strings.Join(reasons, "+")
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

func maskValue(kinds []detector.Kind, value string, mask bool) string {
	if !mask || len(kinds) == 0 {
		return value
	}
	return detector.Mask(kinds[0], value)
}

func addSummary(result *scanner.Result, table scanner.TableResult, path string, kinds []detector.Kind) {
	for _, kind := range kinds {
		result.Summaries = append(result.Summaries, scanner.Summary{
			Database: table.Database,
			Schema:   table.Schema,
			Table:    table.Name,
			Column:   path,
			Kind:     kind,
			Level:    detector.LevelOf(kind),
			Mode:     scanner.Content,
			Total:    1,
		})
	}
}

func addField(fields map[string]*scanner.FieldResult, path string, kinds []detector.Kind) {
	if len(kinds) == 0 {
		return
	}
	field, ok := fields[path]
	if !ok {
		fields[path] = &scanner.FieldResult{Name: path, Kinds: append([]detector.Kind(nil), kinds...), Level: highestLevel(kinds), Mode: scanner.Content, Total: 1}
		return
	}
	field.Total++
	seen := map[detector.Kind]bool{}
	for _, kind := range field.Kinds {
		seen[kind] = true
	}
	for _, kind := range kinds {
		if !seen[kind] {
			field.Kinds = append(field.Kinds, kind)
		}
	}
	field.Level = highestLevel(field.Kinds)
}

func fieldSlice(fields map[string]*scanner.FieldResult) []scanner.FieldResult {
	out := make([]scanner.FieldResult, 0, len(fields))
	for _, field := range fields {
		out = append(out, *field)
	}
	return out
}

func withResolvedAddr(info *db.ServerInfo, host string) {
	if addrs, err := net.LookupHost(host); err == nil && len(addrs) > 0 {
		info.ResolvedAddr = strings.Join(addrs, ",")
	}
}
