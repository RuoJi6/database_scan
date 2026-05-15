package detector

import (
	"regexp"
	"strings"
)

type Kind string

const (
	Phone    Kind = "手机号"
	IDCard   Kind = "身份证"
	Address  Kind = "地址"
	Username Kind = "用户名/账号"
	Password Kind = "密码/密钥"
	Email    Kind = "邮箱"
	BankCard Kind = "银行卡"
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

var Rules = []Rule{
	{Kind: Phone, Level: LevelMedium, Keywords: []string{"phone", "mobile", "tel", "手机号", "电话", "联系方式"}, Pattern: regexp.MustCompile(`1[3-9]\d{9}`)},
	{Kind: IDCard, Level: LevelHigh, Keywords: []string{"id_card", "identity", "cert", "身份证", "证件"}, Pattern: regexp.MustCompile(`\b\d{17}[\dXx]\b`)},
	{Kind: Address, Level: LevelLow, Keywords: []string{"address", "addr", "地址", "住址"}, Pattern: regexp.MustCompile(`(省|市|区|县|镇|街道|路|号楼|小区)`)},
	{Kind: Username, Level: LevelLow, Keywords: []string{"user", "username", "login", "account", "账号", "用户", "用户名"}, Pattern: regexp.MustCompile(`^[A-Za-z0-9_.@-]{3,64}$`)},
	{Kind: Password, Level: LevelHigh, Keywords: []string{"password", "passwd", "pwd", "token", "secret", "key", "密码", "密钥", "令牌"}, Pattern: regexp.MustCompile(`.{6,}`)},
	{Kind: Email, Level: LevelMedium, Keywords: []string{"email", "mail", "邮箱"}, Pattern: regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)},
	{Kind: BankCard, Level: LevelHigh, Keywords: []string{"bank", "card", "银行卡", "卡号"}, Pattern: regexp.MustCompile(`\b\d{13,19}\b`)},
}

func FieldKinds(names ...string) []Kind {
	return FieldKindsByLevel(LevelAll, names...)
}

func FieldKindsByLevel(level Level, names ...string) []Kind {
	seen := map[Kind]bool{}
	var kinds []Kind
	joined := strings.ToLower(strings.Join(names, " "))
	for _, rule := range Rules {
		if !LevelMatches(level, rule.Level) {
			continue
		}
		for _, kw := range rule.Keywords {
			if strings.Contains(joined, strings.ToLower(kw)) {
				if !seen[rule.Kind] {
					seen[rule.Kind] = true
					kinds = append(kinds, rule.Kind)
				}
				break
			}
		}
	}
	return kinds
}

func ContentKinds(value string) []Kind {
	return ContentKindsByLevel(LevelAll, value)
}

func ContentKindsByLevel(level Level, value string) []Kind {
	seen := map[Kind]bool{}
	var kinds []Kind
	for _, rule := range Rules {
		if !LevelMatches(level, rule.Level) {
			continue
		}
		if rule.Pattern.MatchString(value) {
			seen[rule.Kind] = true
			kinds = append(kinds, rule.Kind)
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

func SQLPattern() string {
	return SQLPatternByLevel(LevelAll)
}

func SQLPatternByLevel(level Level) string {
	switch level {
	case LevelHigh:
		return `([0-9]{17}[0-9Xx]|[0-9]{13,19})`
	case LevelMedium:
		return `(1[3-9][0-9]{9}|[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,})`
	case LevelLow:
		return `(省|市|区|县|镇|街道|路|号楼|小区)`
	default:
	}
	return `(1[3-9][0-9]{9}|[0-9]{17}[0-9Xx]|[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}|[0-9]{13,19}|省|市|区|县|镇|街道|路|号楼|小区)`
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
	if len(value) <= 4 {
		return "****"
	}
	switch kind {
	case Phone:
		if len(value) >= 11 {
			return value[:3] + "****" + value[len(value)-4:]
		}
	case IDCard:
		if len(value) >= 18 {
			return value[:6] + "********" + value[len(value)-4:]
		}
	case Email:
		at := strings.IndexByte(value, '@')
		if at > 1 {
			return value[:1] + "****" + value[at:]
		}
	}
	return value[:2] + "****" + value[len(value)-2:]
}
