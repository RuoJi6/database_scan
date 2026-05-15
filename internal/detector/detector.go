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

type Rule struct {
	Kind     Kind
	Keywords []string
	Pattern  *regexp.Regexp
}

var Rules = []Rule{
	{Kind: Phone, Keywords: []string{"phone", "mobile", "tel", "手机号", "电话", "联系方式"}, Pattern: regexp.MustCompile(`1[3-9]\d{9}`)},
	{Kind: IDCard, Keywords: []string{"id_card", "identity", "cert", "身份证", "证件"}, Pattern: regexp.MustCompile(`\b\d{17}[\dXx]\b`)},
	{Kind: Address, Keywords: []string{"address", "addr", "地址", "住址"}, Pattern: regexp.MustCompile(`(省|市|区|县|镇|街道|路|号楼|小区)`)},
	{Kind: Username, Keywords: []string{"user", "username", "login", "account", "账号", "用户", "用户名"}, Pattern: regexp.MustCompile(`^[A-Za-z0-9_.@-]{3,64}$`)},
	{Kind: Password, Keywords: []string{"password", "passwd", "pwd", "token", "secret", "key", "密码", "密钥", "令牌"}, Pattern: regexp.MustCompile(`.{6,}`)},
	{Kind: Email, Keywords: []string{"email", "mail", "邮箱"}, Pattern: regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)},
	{Kind: BankCard, Keywords: []string{"bank", "card", "银行卡", "卡号"}, Pattern: regexp.MustCompile(`\b\d{13,19}\b`)},
}

func FieldKinds(names ...string) []Kind {
	seen := map[Kind]bool{}
	var kinds []Kind
	joined := strings.ToLower(strings.Join(names, " "))
	for _, rule := range Rules {
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
	seen := map[Kind]bool{}
	var kinds []Kind
	for _, rule := range Rules {
		if rule.Pattern.MatchString(value) {
			seen[rule.Kind] = true
			kinds = append(kinds, rule.Kind)
		}
	}
	for _, rule := range Rules {
		if seen[rule.Kind] {
			kinds = append(kinds, rule.Kind)
		}
	}
	return kinds
}

func SQLPattern() string {
	return `(1[3-9][0-9]{9}|[0-9]{17}[0-9Xx]|[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}|[0-9]{13,19}|省|市|区|县|镇|街道|路|号楼|小区)`
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
