package fscan

import (
	"bufio"
	"fmt"
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

	var targets []Target
	seen := map[string]bool{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		target, ok := ParseLine(scanner.Text(), lineNo)
		if !ok {
			continue
		}
		key := target.Type + "\x00" + target.Host + "\x00" + strconv.Itoa(target.Port) + "\x00" + target.User + "\x00" + target.Password
		if seen[key] {
			continue
		}
		seen[key] = true
		targets = append(targets, target)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return targets, nil
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
		if !ok {
			continue
		}
		return Target{Type: dbType, Host: host, Port: port, User: user, Password: pass, Line: lineNo, Raw: line}, true
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
		if !ok {
			continue
		}
		return Target{Type: dbType, Host: host, Port: port, User: user, Password: pass, Line: lineNo, Raw: line}, true
	}
	return Target{}, false
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

func normalizeType(s string) (string, bool) {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(s), "[]:+")) {
	case "mysql":
		return "mysql", true
	case "mariadb":
		return "mariadb", true
	case "postgres", "postgresql":
		return "postgres", true
	case "mssql", "sqlserver":
		return "mssql", true
	case "oracle":
		return "oracle", true
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
