package detector

import (
	"regexp"
	"strings"
)

type Kind string

const (
	Phone              Kind = "手机号"
	IDCard             Kind = "身份证"
	Address            Kind = "地址"
	Username           Kind = "用户名/账号"
	Password           Kind = "密码/密钥"
	Email              Kind = "邮箱"
	BankCard           Kind = "银行卡"
	CloudKey           Kind = "云厂商密钥"
	JWT                Kind = "JWT令牌"
	Auth               Kind = "认证凭据"
	JDBC               Kind = "JDBC连接"
	WeComKey           Kind = "企微密钥"
	TokenSecret        Kind = "令牌/密钥"
	Account            Kind = "账号"
	IPAddress          Kind = "IP地址"
	BusinessIdentifier Kind = "业务标识"
	RiskEvidence       Kind = "风险证据"
	Secret             Kind = "敏感密钥"
	PrivateKey         Kind = "私钥"
	Webhook            Kind = "Webhook"
	ConnectionString   Kind = "连接串"
)

type Level string

const (
	LevelAll    Level = "all"
	LevelHigh   Level = "high"
	LevelMedium Level = "medium"
	LevelLow    Level = "low"
)

type Rule struct {
	Kind     Kind
	Level    Level
	Keywords []string
	Pattern  *regexp.Regexp
}

var legacyRules = []Rule{
	{Kind: Phone, Level: LevelMedium, Keywords: []string{"phone", "mobile", "tel", "手机号", "电话", "联系方式"}, Pattern: regexp.MustCompile(`1[3-9]\d{9}`)},
	{Kind: IDCard, Level: LevelHigh, Keywords: []string{"id_card", "identity", "cert", "身份证", "证件"}, Pattern: regexp.MustCompile(`\b\d{17}[\dXx]\b`)},
	{Kind: Address, Level: LevelLow, Keywords: []string{"address", "addr", "地址", "住址"}, Pattern: regexp.MustCompile(`(省|市|区|县|镇|街道|路|号楼|小区)`)},
	{Kind: Username, Level: LevelLow, Keywords: []string{"user", "username", "login", "account", "账号", "用户", "用户名"}, Pattern: regexp.MustCompile(`^[A-Za-z0-9_.@-]{3,64}$`)},
	{Kind: Password, Level: LevelHigh, Keywords: []string{"password", "passwd", "pwd", "db_password", "database_password", "api_key", "apikey", "api_secret", "secret", "token", "key", "config", "access", "admin", "ticket", "密码", "密钥", "令牌"}, Pattern: regexp.MustCompile(`(?i)[\w.\-]{0,32}(pass|pwd|passwd|password|key|secret|token|config|access|admin|ticket)[\w.\-]{0,32}\s*[:=]\s*["']?[^"'\s,;]{6,}`)},
	{Kind: Email, Level: LevelMedium, Keywords: []string{"email", "mail", "邮箱"}, Pattern: regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)},
	{Kind: BankCard, Level: LevelHigh, Keywords: []string{"bank", "card", "银行卡", "卡号"}, Pattern: regexp.MustCompile(`\b\d{13,19}\b`)},
	{Kind: CloudKey, Level: LevelHigh, Keywords: []string{"access_key", "access-key", "accesskey", "access_key_id", "access_key_secret", "aws_access_key_id", "aws_secret_access_key", "cloud_api_key", "alicloud_access_key", "aliyun_access_key", "oss_access_key", "LTAI", "AKIA", "ASIA"}, Pattern: regexp.MustCompile(`(?i)(access[-_]?key[-_]?(id|secret)|LTAI[a-z0-9]{12,20}|\b(AKIA|ASIA)[0-9A-Z]{16}\b)`)},
	{Kind: JWT, Level: LevelHigh, Keywords: []string{"jwt", "json_web_token", "id_token"}, Pattern: regexp.MustCompile(`eyJ[A-Za-z0-9_\-/+]{10,}\.[A-Za-z0-9._\-/+]{10,}(?:\.[A-Za-z0-9._\-/+]{10,})?`)},
	{Kind: Auth, Level: LevelHigh, Keywords: []string{"authorization", "auth_header", "auth_token", "bearer", "basic_auth"}, Pattern: regexp.MustCompile(`(?i)\b(basic|bearer)\s+[a-z0-9_.=:_+/\-]{5,200}\b`)},
	{Kind: JDBC, Level: LevelHigh, Keywords: []string{"jdbc", "jdbc_url", "database_url", "connection_string", "datasource", "db_url"}, Pattern: regexp.MustCompile(`(?i)jdbc:[a-z0-9:]+://[a-z0-9.\-_:;=/@?,&%]+`)},
	{Kind: WeComKey, Level: LevelHigh, Keywords: []string{"corpid", "corpsecret", "wecom", "wechat_work", "企业微信"}, Pattern: regexp.MustCompile(`(?i)\bcorp(id|secret)\b`)},
}

// Rules keeps the original static rule table available for existing callers.
// New matching also uses the embedded dbx/found YAML rule engine.
var Rules = legacyRules

var kindLevels = map[Kind]Level{
	Phone:              LevelMedium,
	IDCard:             LevelHigh,
	Address:            LevelLow,
	Username:           LevelLow,
	Password:           LevelHigh,
	Email:              LevelMedium,
	BankCard:           LevelHigh,
	CloudKey:           LevelHigh,
	JWT:                LevelHigh,
	Auth:               LevelHigh,
	JDBC:               LevelHigh,
	WeComKey:           LevelHigh,
	TokenSecret:        LevelHigh,
	Account:            LevelLow,
	IPAddress:          LevelMedium,
	BusinessIdentifier: LevelLow,
	RiskEvidence:       LevelMedium,
	Secret:             LevelHigh,
	PrivateKey:         LevelHigh,
	Webhook:            LevelHigh,
	ConnectionString:   LevelHigh,
}

func FieldKinds(names ...string) []Kind {
	return FieldKindsByLevel(LevelAll, names...)
}

func FieldKindsByLevel(level Level, names ...string) []Kind {
	seen := map[Kind]bool{}
	var kinds []Kind
	for _, matched := range builtinEngine().scanField(fieldScanText(names...), level) {
		appendKind(&kinds, seen, matched.Kind)
	}
	for _, kind := range legacyFieldKindsByLevel(level, names...) {
		appendKind(&kinds, seen, kind)
	}
	return kinds
}

func FieldMatchesByLevel(level Level, names ...string) []RuleMatch {
	return builtinEngine().scanField(fieldScanText(names...), level)
}

func ContentKinds(value string) []Kind {
	return ContentKindsByLevel(LevelAll, value)
}

func ContentKindsByLevel(level Level, value string) []Kind {
	seen := map[Kind]bool{}
	var kinds []Kind
	for _, matched := range builtinEngine().scanContent(value, level) {
		appendKind(&kinds, seen, matched.Kind)
	}
	for _, kind := range legacyContentKindsByLevel(level, value) {
		appendKind(&kinds, seen, kind)
	}
	return kinds
}

func ContentMatchesByLevel(level Level, value string) []RuleMatch {
	return builtinEngine().scanContent(value, level)
}

func SQLPattern() string {
	return SQLPatternByLevel(LevelAll)
}

func SQLPatternByLevel(level Level) string {
	high := `[0-9]{17}[0-9Xx]|[0-9]{13,19}|eyJ[A-Za-z0-9_\-/+]{10,}\.[A-Za-z0-9._\-/+]{10,}|LTAI[A-Za-z0-9]{12,20}|(AKIA|ASIA)[0-9A-Z]{16}|[Aa]ccess[-_]?[Kk]ey[-_]?([Ii][Dd]|[Ss]ecret)|[Bb]earer[[:space:]]+[A-Za-z0-9_.=:_+/\-]{5,200}|[Bb]asic[[:space:]]+[A-Za-z0-9=:_+/\-]{5,100}|jdbc:[A-Za-z0-9:]+://[A-Za-z0-9.\-_:;=/@?,&%]+|(mysql|postgres|postgresql|mongodb|redis|oracle|sqlserver)://[^[:space:]'"]{8,}|sk-[A-Za-z0-9]{24,}|BEGIN [A-Z ]*PRIVATE KEY|hooks\.[A-Za-z0-9.\-]+|([Pp]assword|[Pp]asswd|[Pp]wd|[Tt]oken|[Ss]ecret|[Aa]pi[-_]?[Kk]ey|[Cc]orpsecret|[Cc]orpid)[[:space:]]*[:=]`
	medium := `1[3-9][0-9]{9}|[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}|([0-9]{1,3}\.){3}[0-9]{1,3}`
	low := `account|acct|username|user_name|nickname|realname|customer_id|order_id|trace_id|request_id|session_id|address|addr|省|市|区|县|镇|街道|路|号楼|小区`
	switch level {
	case LevelHigh:
		return "(" + high + ")"
	case LevelMedium:
		return "(" + medium + ")"
	case LevelLow:
		return "(" + low + ")"
	default:
		return "(" + high + "|" + medium + "|" + low + ")"
	}
}

func LevelMatches(filter Level, actual Level) bool {
	return filter == "" || filter == LevelAll || filter == actual
}

func ParseLevel(s string) (Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "all", "全部":
		return LevelAll, true
	case "high", "critical", "highest", "高", "高敏", "最敏感":
		return LevelHigh, true
	case "medium", "middle", "中", "中敏":
		return LevelMedium, true
	case "low", "低", "低敏":
		return LevelLow, true
	default:
		return "", false
	}
}

func LevelOf(kind Kind) Level {
	if level, ok := kindLevels[kind]; ok {
		return level
	}
	for _, rule := range Rules {
		if rule.Kind == kind {
			return rule.Level
		}
	}
	return LevelLow
}

func LevelLabel(level Level) string {
	switch level {
	case LevelHigh:
		return "高敏"
	case LevelMedium:
		return "中敏"
	case LevelLow:
		return "低敏"
	default:
		return "全部"
	}
}

func Mask(kind Kind, value string) string {
	runes := []rune(value)
	if len(runes) <= 4 {
		if len(runes) == 0 {
			return ""
		}
		return strings.Repeat("*", len(runes))
	}
	switch kind {
	case Phone:
		if len(runes) >= 11 {
			return string(runes[:3]) + "****" + string(runes[len(runes)-4:])
		}
	case IDCard:
		if len(runes) >= 18 {
			return string(runes[:6]) + "********" + string(runes[len(runes)-4:])
		}
	case Email:
		at := strings.IndexRune(value, '@')
		if at > 1 {
			return string(runes[:1]) + "****" + value[at:]
		}
	}
	return string(runes[:2]) + strings.Repeat("*", min(len(runes)-4, 12)) + string(runes[len(runes)-2:])
}

func fieldScanText(names ...string) string {
	if len(names) == 0 {
		return ""
	}
	column := strings.TrimSpace(names[len(names)-1])
	return "column=" + column
}

func appendKind(kinds *[]Kind, seen map[Kind]bool, kind Kind) {
	if kind == "" || seen[kind] {
		return
	}
	seen[kind] = true
	*kinds = append(*kinds, kind)
}

func legacyFieldKindsByLevel(level Level, names ...string) []Kind {
	seen := map[Kind]bool{}
	var kinds []Kind
	joined := strings.ToLower(strings.Join(names, " "))
	for _, rule := range Rules {
		if !LevelMatches(level, rule.Level) {
			continue
		}
		for _, kw := range rule.Keywords {
			if strings.Contains(joined, strings.ToLower(kw)) {
				appendKind(&kinds, seen, rule.Kind)
				break
			}
		}
	}
	return kinds
}

func legacyContentKindsByLevel(level Level, value string) []Kind {
	seen := map[Kind]bool{}
	var kinds []Kind
	for _, rule := range Rules {
		if !LevelMatches(level, rule.Level) {
			continue
		}
		if rule.Pattern.MatchString(value) {
			seen[rule.Kind] = true
		}
	}
	for _, rule := range Rules {
		if !LevelMatches(level, rule.Level) {
			continue
		}
		if seen[rule.Kind] {
			kinds = append(kinds, rule.Kind)
		}
	}
	return kinds
}
