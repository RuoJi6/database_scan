package fscan

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
)

type Target struct {
	Type     string
	Host     string
	Port     int
	User     string
	Password string
	Line     int
	Raw      string
}

func ParseFile(path string) ([]Target, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return Parse(file)
}

func ParseText(text string) ([]Target, error) {
	return Parse(strings.NewReader(text))
}

func Parse(r io.Reader) ([]Target, error) {
	var targets []Target
	seen := map[string]bool{}
	var pendingManual Target
	hasPendingManual := false
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		target, ok := ParseLine(line, lineNo)
		if !ok && hasPendingManual {
			target, ok = completeManualTarget(pendingManual, line)
			if ok {
				hasPendingManual = false
			}
		}
		if !ok {
			if target, ok = parseManualHeader(line, lineNo); ok {
				pendingManual = target
				hasPendingManual = true
			}
			continue
		}
		addTarget(&targets, seen, target)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return targets, nil
}

func addTarget(targets *[]Target, seen map[string]bool, target Target) {
	key := target.Type + "\x00" + target.Host + "\x00" + strconv.Itoa(target.Port) + "\x00" + target.User + "\x00" + target.Password
	if seen[key] {
		return
	}
	seen[key] = true
	*targets = append(*targets, target)
}

func ParseLine(line string, lineNo int) (Target, bool) {
	raw := strings.TrimSpace(stripANSI(line))
	if raw == "" {
		return Target{}, false
	}
	if target, ok := parseOldLine(raw, lineNo); ok {
		return target, true
	}
	if target, ok := parseSavedV2Line(raw, lineNo); ok {
		return target, true
	}
	return parseNewLine(raw, lineNo)
}

func parseOldLine(line string, lineNo int) (Target, bool) {
	text := strings.TrimSpace(strings.TrimPrefix(line, "[+]"))
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return Target{}, false
	}
	dbType, ok := normalizeType(fields[0])
	if !ok {
		dbName, rest, cut := strings.Cut(fields[0], ":")
		if !cut {
			return Target{}, false
		}
		dbType, ok = normalizeType(dbName)
		if !ok {
			return Target{}, false
		}
		fields = append([]string{dbName, rest}, fields[1:]...)
	}
	if len(fields) < 3 {
		return Target{}, false
	}
	host, port, user, ok := splitOldTarget(fields[1])
	if !ok {
		return Target{}, false
	}
	return Target{Type: dbType, Host: host, Port: port, User: user, Password: fields[2], Line: lineNo, Raw: line}, true
}

func parseSavedV2Line(line string, lineNo int) (Target, bool) {
	fields := strings.Fields(line)
	for i := 0; i+2 < len(fields); i++ {
		host, port, ok := splitHostPort(fields[i])
		if !ok {
			continue
		}
		dbType, ok := normalizeType(fields[i+1])
		if !ok {
			continue
		}
		user, pass, ok := splitCredential(fields[i+2])
		if ok {
			user, pass = normalizeRedisCredential(dbType, user, pass)
			return Target{Type: dbType, Host: host, Port: port, User: user, Password: pass, Line: lineNo, Raw: line}, true
		}
	}
	return Target{}, false
}

func parseNewLine(line string, lineNo int) (Target, bool) {
	fields := strings.Fields(line)
	for i := 0; i+2 < len(fields); i++ {
		dbType, ok := normalizeType(fields[i])
		if !ok {
			continue
		}
		host, port, ok := splitHostPort(fields[i+1])
		if !ok {
			continue
		}
		user, pass, ok := splitCredential(fields[i+2])
		if !ok && dbType == "redis" {
			user, pass, ok = "", fields[i+2], true
		}
		if ok {
			user, pass = normalizeRedisCredential(dbType, user, pass)
			return Target{Type: dbType, Host: host, Port: port, User: user, Password: pass, Line: lineNo, Raw: line}, true
		}
	}
	return Target{}, false
}

func parseManualHeader(line string, lineNo int) (Target, bool) {
	raw := strings.TrimSpace(stripANSI(line))
	fields := strings.Fields(raw)
	if len(fields) != 2 {
		return Target{}, false
	}
	dbType, ok := normalizeType(fields[0])
	if !ok {
		return Target{}, false
	}
	host, port, ok := splitHostPort(fields[1])
	if !ok {
		return Target{}, false
	}
	return Target{Type: dbType, Host: host, Port: port, Line: lineNo, Raw: raw}, true
}

func completeManualTarget(target Target, line string) (Target, bool) {
	raw := strings.TrimSpace(stripANSI(line))
	fields := strings.Fields(raw)
	if len(fields) != 1 {
		return Target{}, false
	}
	user, pass, ok := splitCredential(fields[0])
	if !ok && target.Type == "redis" {
		user, pass, ok = "", fields[0], fields[0] != ""
	}
	if !ok {
		return Target{}, false
	}
	user, pass = normalizeRedisCredential(target.Type, user, pass)
	target.User = user
	target.Password = pass
	target.Raw = strings.TrimSpace(target.Raw + "\n" + raw)
	return target, true
}

func splitOldTarget(s string) (string, int, string, bool) {
	parts := strings.Split(s, ":")
	if len(parts) < 3 {
		return "", 0, "", false
	}
	user := parts[len(parts)-1]
	portText := parts[len(parts)-2]
	host := strings.Join(parts[:len(parts)-2], ":")
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 || host == "" || user == "" {
		return "", 0, "", false
	}
	return strings.Trim(host, "[]"), port, user, true
}

func splitHostPort(s string) (string, int, bool) {
	host, portText, err := net.SplitHostPort(s)
	if err != nil {
		idx := strings.LastIndexByte(s, ':')
		if idx <= 0 || idx == len(s)-1 {
			return "", 0, false
		}
		host, portText = s[:idx], s[idx+1:]
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 || host == "" {
		return "", 0, false
	}
	return strings.Trim(host, "[]"), port, true
}

func splitCredential(s string) (string, string, bool) {
	user, pass, ok := strings.Cut(s, ":")
	if !ok {
		user, pass, ok = strings.Cut(s, "/")
	}
	if !ok || user == "" {
		return "", "", false
	}
	return user, pass, true
}

func normalizeRedisCredential(dbType, user, pass string) (string, string) {
	if dbType == "redis" && strings.EqualFold(user, "root") {
		return "", pass
	}
	return user, pass
}

func normalizeType(s string) (string, bool) {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(s), "[]:+")) {
	case "mysql":
		return "mysql", true
	case "mariadb":
		return "mariadb", true
	case "tidb":
		return "tidb", true
	case "oceanbase", "oceanbase-mysql":
		return "oceanbase", true
	case "polardb-mysql":
		return "polardb-mysql", true
	case "doris":
		return "doris", true
	case "starrocks":
		return "starrocks", true
	case "gbase-mysql":
		return "gbase-mysql", true
	case "postgres", "postgresql":
		return "postgres", true
	case "opengauss":
		return "opengauss", true
	case "gaussdb":
		return "gaussdb", true
	case "kingbase", "kingbasees":
		return "kingbase", true
	case "highgo":
		return "highgo", true
	case "polardb-postgres":
		return "polardb-postgres", true
	case "mssql", "sqlserver":
		return "mssql", true
	case "oracle", "go-ora":
		return "oracle", true
	case "redis":
		return "redis", true
	default:
		return "", false
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < '@' || s[i] > '~') {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func (t Target) Label() string {
	return fmt.Sprintf("%s %s:%d %s", t.Type, t.Host, t.Port, t.User)
}
