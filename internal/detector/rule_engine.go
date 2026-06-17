package detector

import (
	"bytes"
	"embed"
	"io"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"

	"go.yaml.in/yaml/v3"
)

//go:embed rules/fields/*.yaml rules/content/*.yaml rules/content/found/keys/*.yaml rules/content/found/keys/*/*.yaml
var embeddedRules embed.FS

type ruleTarget int

const (
	ruleTargetField ruleTarget = iota
	ruleTargetContent
)

type RuleMatch struct {
	Kind     Kind
	Level    Level
	RuleID   string
	RuleName string
	Severity string
	Tags     []string
	Value    string
}

type ruleEngine struct {
	fields  ruleGroup
	content ruleGroup
}

type ruleGroup struct {
	rules       []compiledRule
	literalRefs map[string][]int
	fallback    []int
	literals    []string
}

type compiledRule struct {
	id       string
	name     string
	severity string
	tags     []string
	kind     Kind
	level    Level
	regexes  []lazyRegex
	literals []string
}

type lazyRegex struct {
	pattern string
	once    sync.Once
	re      *regexp.Regexp
}

type templateRule struct {
	ID   string        `yaml:"id"`
	Info *templateInfo `yaml:"info"`
	DBX  *dbxRule      `yaml:"dbx"`
	File []fileRule    `yaml:"file"`
}

type templateInfo struct {
	Name     string    `yaml:"name"`
	Severity string    `yaml:"severity"`
	Tags     yaml.Node `yaml:"tags"`
}

type dbxRule struct {
	Target string `yaml:"target"`
	Kind   string `yaml:"kind"`
}

type fileRule struct {
	Matchers   []ruleOperator `yaml:"matchers"`
	Extractors []ruleOperator `yaml:"extractors"`
}

type ruleOperator struct {
	Type  string   `yaml:"type"`
	Words []string `yaml:"words"`
	Regex []string `yaml:"regex"`
}

var builtinEngine = sync.OnceValue(loadBuiltinRuleEngine)

func loadBuiltinRuleEngine() *ruleEngine {
	var rules []targetRule
	rules = append(rules, loadEmbeddedRules("rules/fields", ruleTargetField)...)
	rules = append(rules, loadEmbeddedRules("rules/content", ruleTargetContent)...)
	return newRuleEngine(rules)
}

type targetRule struct {
	target ruleTarget
	rule   compiledRule
}

func loadEmbeddedRules(root string, forcedTarget ruleTarget) []targetRule {
	var rules []targetRule
	_ = fs.WalkDir(embeddedRules, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isYAMLPath(path) {
			return nil
		}
		data, err := embeddedRules.ReadFile(path)
		if err != nil {
			return nil
		}
		rules = append(rules, parseRuleDocuments(data, &forcedTarget)...)
		return nil
	})
	return rules
}

func newRuleEngine(rules []targetRule) *ruleEngine {
	var fields []compiledRule
	var content []compiledRule
	for _, item := range rules {
		switch item.target {
		case ruleTargetField:
			fields = append(fields, item.rule)
		case ruleTargetContent:
			content = append(content, item.rule)
		}
	}
	return &ruleEngine{fields: newRuleGroup(fields), content: newRuleGroup(content)}
}

func newRuleGroup(rules []compiledRule) ruleGroup {
	literalRefs := make(map[string][]int)
	var fallback []int
	for ruleIndex, rule := range rules {
		if len(rule.literals) == 0 {
			fallback = append(fallback, ruleIndex)
			continue
		}
		for _, literal := range rule.literals {
			literal = strings.ToLower(literal)
			literalRefs[literal] = append(literalRefs[literal], ruleIndex)
		}
	}
	literals := make([]string, 0, len(literalRefs))
	for literal := range literalRefs {
		literals = append(literals, literal)
	}
	sort.Slice(literals, func(i, j int) bool {
		return len(literals[i]) > len(literals[j])
	})
	return ruleGroup{rules: rules, literalRefs: literalRefs, fallback: fallback, literals: literals}
}

func (e *ruleEngine) scanField(text string, level Level) []RuleMatch {
	return e.fields.scan(text, level)
}

func (e *ruleEngine) scanContent(text string, level Level) []RuleMatch {
	return e.content.scan(text, level)
}

func (g ruleGroup) scan(text string, level Level) []RuleMatch {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	candidates := g.candidateRules(text)
	if len(candidates) == 0 {
		return nil
	}
	var matches []RuleMatch
	for _, ruleIndex := range candidates {
		if ruleIndex < 0 || ruleIndex >= len(g.rules) {
			continue
		}
		rule := g.rules[ruleIndex]
		if !LevelMatches(level, rule.level) {
			continue
		}
		for i := range rule.regexes {
			re := rule.regexes[i].get()
			if re == nil {
				continue
			}
			for _, indexes := range re.FindAllStringSubmatchIndex(text, -1) {
				value := matchedValue(text, indexes)
				matches = append(matches, RuleMatch{
					Kind:     rule.kind,
					Level:    rule.level,
					RuleID:   rule.id,
					RuleName: rule.name,
					Severity: rule.severity,
					Tags:     append([]string(nil), rule.tags...),
					Value:    value,
				})
			}
		}
	}
	return dedupeRuleMatches(matches)
}

func (g ruleGroup) candidateRules(text string) []int {
	seen := make(map[int]bool)
	for _, ruleIndex := range g.fallback {
		seen[ruleIndex] = true
	}
	lower := strings.ToLower(text)
	for _, literal := range g.literals {
		if strings.Contains(lower, literal) {
			for _, ruleIndex := range g.literalRefs[literal] {
				seen[ruleIndex] = true
			}
		}
	}
	out := make([]int, 0, len(seen))
	for ruleIndex := range seen {
		out = append(out, ruleIndex)
	}
	sort.Ints(out)
	return out
}

func (r *lazyRegex) get() *regexp.Regexp {
	r.once.Do(func() {
		re, err := regexp.Compile(normalizeRegexpPattern(r.pattern))
		if err == nil {
			r.re = re
		}
	})
	return r.re
}

func normalizeRegexpPattern(pattern string) string {
	return strings.ReplaceAll(pattern, "(?x)", "")
}

func matchedValue(text string, indexes []int) string {
	if len(indexes) >= 4 && indexes[2] >= 0 && indexes[3] >= indexes[2] {
		return text[indexes[2]:indexes[3]]
	}
	if len(indexes) >= 2 && indexes[0] >= 0 && indexes[1] >= indexes[0] {
		return text[indexes[0]:indexes[1]]
	}
	return ""
}

func parseRuleDocuments(data []byte, forcedTarget *ruleTarget) []targetRule {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var rules []targetRule
	for {
		var tpl templateRule
		err := dec.Decode(&tpl)
		if err == io.EOF {
			break
		}
		if err != nil || strings.TrimSpace(tpl.ID) == "" {
			continue
		}
		if compiled, ok := compileTemplateRule(tpl, forcedTarget); ok {
			rules = append(rules, compiled)
		}
	}
	return rules
}

func compileTemplateRule(tpl templateRule, forcedTarget *ruleTarget) (targetRule, bool) {
	target := ruleTargetContent
	if forcedTarget != nil {
		target = *forcedTarget
	} else if tpl.DBX != nil {
		if parsed, ok := ruleTargetFromString(tpl.DBX.Target); ok {
			target = parsed
		}
	}

	name := tpl.ID
	severity := "medium"
	var tags []string
	if tpl.Info != nil {
		if strings.TrimSpace(tpl.Info.Name) != "" {
			name = tpl.Info.Name
		}
		if strings.TrimSpace(tpl.Info.Severity) != "" {
			severity = strings.ToLower(strings.TrimSpace(tpl.Info.Severity))
		}
		tags = parseTags(tpl.Info.Tags)
	}

	kind := kindFromTemplate(tpl, name, tags)
	level := levelFromSeverity(severity)
	if level == "" {
		level = LevelOf(kind)
	}

	var patterns []string
	for _, file := range tpl.File {
		ops := append([]ruleOperator(nil), file.Extractors...)
		ops = append(ops, file.Matchers...)
		for _, op := range ops {
			switch strings.ToLower(strings.TrimSpace(op.Type)) {
			case "", "regex":
				patterns = append(patterns, op.Regex...)
			case "word":
				for _, word := range op.Words {
					patterns = append(patterns, regexp.QuoteMeta(word))
				}
			}
		}
	}
	if len(patterns) == 0 {
		return targetRule{}, false
	}

	literals := map[string]bool{}
	regexes := make([]lazyRegex, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		for _, literal := range extractLiterals(pattern) {
			literals[literal] = true
		}
		regexes = append(regexes, lazyRegex{pattern: pattern})
	}
	if len(regexes) == 0 {
		return targetRule{}, false
	}

	dbxRule := tpl.DBX != nil || containsFold(tags, "dbx")
	if !dbxRule {
		pruneGenericLiterals(literals)
		metadata := metadataLiterals(tpl.ID, name, tags)
		if (len(literals) == 0 || len(literals) > 32) && len(metadata) > 0 {
			literals = setFromSlice(metadata)
		} else {
			for _, literal := range metadata {
				literals[literal] = true
			}
			pruneGenericLiterals(literals)
		}
	}
	if len(literals) == 0 && !dbxRule {
		literals = setFromSlice(metadataLiterals(tpl.ID, name, tags))
	}

	return targetRule{
		target: target,
		rule: compiledRule{
			id:       tpl.ID,
			name:     name,
			severity: severity,
			tags:     tags,
			kind:     kind,
			level:    level,
			regexes:  regexes,
			literals: sortedSet(literals),
		},
	}, true
}

func isYAMLPath(path string) bool {
	ext := filepath.Ext(path)
	return strings.EqualFold(ext, ".yaml") || strings.EqualFold(ext, ".yml")
}

func parseTags(node yaml.Node) []string {
	switch node.Kind {
	case yaml.ScalarNode:
		return splitTagString(node.Value)
	case yaml.SequenceNode:
		var tags []string
		for _, item := range node.Content {
			tags = append(tags, splitTagString(item.Value)...)
		}
		return tags
	default:
		return nil
	}
}

func splitTagString(value string) []string {
	parts := strings.Split(value, ",")
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			tags = append(tags, part)
		}
	}
	return tags
}

func ruleTargetFromString(value string) (ruleTarget, bool) {
	switch normalizeKey(value) {
	case "field", "fields", "fieldname", "metadata":
		return ruleTargetField, true
	case "content", "value", "values":
		return ruleTargetContent, true
	default:
		return ruleTargetContent, false
	}
}

func kindFromTemplate(tpl templateRule, name string, tags []string) Kind {
	if tpl.DBX != nil {
		if kind := kindFromString(tpl.DBX.Kind); kind != "" {
			return kind
		}
	}
	return inferKind(tpl.ID, name, tags)
}

func kindFromString(value string) Kind {
	switch normalizeKey(value) {
	case "phone", "mobile":
		return Phone
	case "email", "mail":
		return Email
	case "idcard", "identity", "idcardnumber":
		return IDCard
	case "bankcard", "card":
		return BankCard
	case "passwordsecret", "password", "passwd", "pwd":
		return Password
	case "tokensecret", "token", "apikey":
		return TokenSecret
	case "address":
		return Address
	case "username", "user":
		return Username
	case "account":
		return Account
	case "ipaddress", "ip":
		return IPAddress
	case "businessidentifier", "businessid":
		return BusinessIdentifier
	case "riskevidence", "risk", "evidence":
		return RiskEvidence
	case "secret":
		return Secret
	case "privatekey":
		return PrivateKey
	case "cloudcredential", "cloudkey", "cloudsecret":
		return CloudKey
	case "webhook":
		return Webhook
	case "connectionstring", "connection":
		return ConnectionString
	default:
		return ""
	}
}

func inferKind(id, name string, tags []string) Kind {
	haystack := normalizeKey(id + " " + name + " " + strings.Join(tags, " "))
	switch {
	case strings.Contains(haystack, "privatekey") || strings.Contains(haystack, "sshkey"):
		return PrivateKey
	case strings.Contains(haystack, "webhook"):
		return Webhook
	case strings.Contains(haystack, "connectionstring") || strings.Contains(haystack, "odbc") || strings.Contains(haystack, "jdbc"):
		return ConnectionString
	case strings.Contains(haystack, "aws") ||
		strings.Contains(haystack, "amazon") ||
		strings.Contains(haystack, "azure") ||
		strings.Contains(haystack, "google") ||
		strings.Contains(haystack, "alibaba") ||
		strings.Contains(haystack, "cloud"):
		return CloudKey
	case strings.Contains(haystack, "password") || strings.Contains(haystack, "passwd") || strings.Contains(haystack, "pwd"):
		return Password
	case strings.Contains(haystack, "token") || strings.Contains(haystack, "secret") || strings.Contains(haystack, "key"):
		return TokenSecret
	default:
		return Secret
	}
}

func levelFromSeverity(value string) Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical", "high":
		return LevelHigh
	case "medium":
		return LevelMedium
	case "low", "info", "informational":
		return LevelLow
	default:
		return ""
	}
}

func normalizeKey(value string) string {
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}

func metadataLiterals(id, name string, tags []string) []string {
	var parts []string
	sources := append([]string{id, name}, tags...)
	for _, source := range sources {
		parts = append(parts, splitMetadataParts(source)...)
	}
	set := setFromSlice(parts)
	filtered := make(map[string]bool)
	for literal := range set {
		if !isGenericLiteral(literal) {
			filtered[literal] = true
		}
	}
	if len(filtered) > 0 {
		return sortedSet(filtered)
	}
	return sortedSet(set)
}

func splitMetadataParts(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.ToLower(strings.TrimSpace(field))
		if len([]rune(field)) >= 3 {
			out = append(out, field)
		}
	}
	return out
}

func extractLiterals(pattern string) []string {
	var literals []string
	var current strings.Builder
	inClass := false
	inQuantifier := false
	runes := []rune(pattern)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		if inClass {
			if ch == ']' {
				inClass = false
			}
			continue
		}
		if inQuantifier {
			if ch == '}' {
				inQuantifier = false
			}
			continue
		}
		switch ch {
		case '[':
			pushLiteral(&literals, &current)
			inClass = true
		case '{':
			pushLiteral(&literals, &current)
			inQuantifier = true
		case '\\':
			if i+1 >= len(runes) {
				pushLiteral(&literals, &current)
				continue
			}
			i++
			next := runes[i]
			if strings.ContainsRune("bBdDsSwWAzZ", next) {
				pushLiteral(&literals, &current)
			} else if isLiteralChar(next) {
				current.WriteRune(next)
			} else {
				pushLiteral(&literals, &current)
			}
		default:
			if isLiteralChar(ch) {
				current.WriteRune(ch)
			} else {
				pushLiteral(&literals, &current)
			}
		}
	}
	pushLiteral(&literals, &current)
	return literals
}

func isLiteralChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_-:/@.", r)
}

func pushLiteral(literals *[]string, current *strings.Builder) {
	literal := strings.Trim(current.String(), "_-:/.")
	current.Reset()
	if len([]rune(literal)) >= 3 || strings.EqualFold(literal, "ip") || strings.EqualFold(literal, "sk") || strings.EqualFold(literal, "ak") {
		*literals = append(*literals, literal)
	}
}

func pruneGenericLiterals(literals map[string]bool) {
	nonGeneric := false
	for literal := range literals {
		if !isGenericLiteral(literal) {
			nonGeneric = true
			break
		}
	}
	if !nonGeneric {
		return
	}
	for literal := range literals {
		if isGenericLiteral(literal) {
			delete(literals, literal)
		}
	}
}

func isGenericLiteral(value string) bool {
	parts := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		switch part {
		case "api", "app", "application", "auth", "client", "consumer", "customer", "detect", "file", "key", "keys", "oauth", "pass", "password", "sec", "secret", "token", "true", "verified":
		default:
			return false
		}
	}
	return true
}

func setFromSlice(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			set[value] = true
		}
	}
	return set
}

func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func dedupeRuleMatches(matches []RuleMatch) []RuleMatch {
	seen := make(map[string]bool, len(matches))
	out := make([]RuleMatch, 0, len(matches))
	for _, item := range matches {
		key := item.RuleID + "\x00" + item.Value
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}
