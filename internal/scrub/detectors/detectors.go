package detectors

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Kind string

const (
	KindEmail      Kind = "email"
	KindJWT        Kind = "jwt"
	KindPhone      Kind = "phone"
	KindSSN        Kind = "ssn"
	KindCreditCard Kind = "credit_card"
	KindAPIKey     Kind = "api_key"
	KindPassword   Kind = "password"
)

type Match struct {
	Kind  Kind
	Value string
	Start int
	End   int
}

type Detector interface {
	Kind() Kind
	FindAll(text string) []Match
}

type Library struct {
	detectors []Detector
}

type Enabled struct {
	Email      bool
	JWT        bool
	Phone      bool
	SSN        bool
	CreditCard bool
	APIKey     bool
	Password   bool
}

type regexDetector struct {
	kind     Kind
	patterns []*regexp.Regexp
	validate func(string, int, int) bool
}

var (
	emailPattern    = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	jwtPattern      = regexp.MustCompile(`\b[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
	phonePattern    = regexp.MustCompile(`(?:\+?\d[\d\s().-]{8,}\d)`)
	ssnPattern      = regexp.MustCompile(`\b\d{3}-?\d{2}-?\d{4}\b`)
	cardPattern     = regexp.MustCompile(`\b\d(?:[ -]?\d){12,18}\b`)
	passwordPattern = regexp.MustCompile(
		`(?i)\b(?:password|passwd|pwd)\b(?:(?:[^:\n]{0,80}:|\s*(?:=|is))\s*([^\s,;]+)|\s*\(\s*([^\s,;)]+)\s*\))`,
	)
	apiPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\bsk-[A-Za-z0-9]{16,}\b`),
		regexp.MustCompile(`\bsk-proj-[A-Za-z0-9_-]{16,}\b`),
		regexp.MustCompile(`\b(?:sk|pk|rk)_(?:live|test)_[A-Za-z0-9]{16,}\b`),
		regexp.MustCompile(`\bAIza[0-9A-Za-z\-_]{35}\b`),
		regexp.MustCompile(`\bghp_[A-Za-z0-9]{20,}\b`),
	}
)

func DefaultLibrary() *Library {
	return LibraryForEnabled(Enabled{
		Email:      true,
		JWT:        true,
		Phone:      true,
		SSN:        true,
		CreditCard: true,
		APIKey:     true,
		Password:   true,
	})
}

func LibraryForEnabled(enabled Enabled) *Library {
	var configured []Detector

	if enabled.Email {
		configured = append(configured, newRegexDetector(KindEmail, []*regexp.Regexp{emailPattern}, validateEmail))
	}
	if enabled.JWT {
		configured = append(configured, newRegexDetector(KindJWT, []*regexp.Regexp{jwtPattern}, validateJWT))
	}
	if enabled.Phone {
		configured = append(configured, newRegexDetector(KindPhone, []*regexp.Regexp{phonePattern}, validatePhone))
	}
	if enabled.SSN {
		configured = append(configured, newRegexDetector(KindSSN, []*regexp.Regexp{ssnPattern}, validateSSN))
	}
	if enabled.CreditCard {
		configured = append(configured, newRegexDetector(KindCreditCard, []*regexp.Regexp{cardPattern}, validateCreditCard))
	}
	if enabled.APIKey {
		configured = append(configured, newRegexDetector(KindAPIKey, apiPatterns, nil))
	}
	if enabled.Password {
		configured = append(configured, passwordDetector{})
	}

	return NewLibrary(configured...)
}

func NewLibrary(detectors ...Detector) *Library {
	cloned := append([]Detector(nil), detectors...)
	return &Library{detectors: cloned}
}

func (l *Library) Scan(text string) []Match {
	if l == nil || len(l.detectors) == 0 || text == "" {
		return nil
	}

	seen := map[string]struct{}{}
	var matches []Match

	for _, detector := range l.detectors {
		for _, match := range detector.FindAll(text) {
			key := string(match.Kind) + "|" + match.Value + "|" + stringifyIndex(match.Start) + "|" + stringifyIndex(match.End)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			matches = append(matches, match)
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Start != matches[j].Start {
			return matches[i].Start < matches[j].Start
		}
		if matches[i].End != matches[j].End {
			return matches[i].End < matches[j].End
		}
		if matches[i].Kind != matches[j].Kind {
			return matches[i].Kind < matches[j].Kind
		}
		return matches[i].Value < matches[j].Value
	})

	return matches
}

func newRegexDetector(kind Kind, patterns []*regexp.Regexp, validate func(string, int, int) bool) Detector {
	return regexDetector{
		kind:     kind,
		patterns: append([]*regexp.Regexp(nil), patterns...),
		validate: validate,
	}
}

func (d regexDetector) Kind() Kind {
	return d.kind
}

func (d regexDetector) FindAll(text string) []Match {
	if text == "" {
		return nil
	}

	seen := map[string]struct{}{}
	var matches []Match
	for _, pattern := range d.patterns {
		indexes := pattern.FindAllStringIndex(text, -1)
		for _, idx := range indexes {
			value := text[idx[0]:idx[1]]
			if d.validate != nil && !d.validate(text, idx[0], idx[1]) {
				continue
			}

			key := value + "|" + stringifyIndex(idx[0]) + "|" + stringifyIndex(idx[1])
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}

			matches = append(matches, Match{
				Kind:  d.kind,
				Value: value,
				Start: idx[0],
				End:   idx[1],
			})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Start != matches[j].Start {
			return matches[i].Start < matches[j].Start
		}
		if matches[i].End != matches[j].End {
			return matches[i].End < matches[j].End
		}
		return matches[i].Value < matches[j].Value
	})

	return matches
}

func validateEmail(text string, start int, end int) bool {
	value := text[start:end]
	at := strings.LastIndex(value, "@")
	if at <= 0 || at == len(value)-1 {
		return false
	}

	domain := value[at+1:]
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}

	return strings.Count(domain, ".") >= 1
}

func validateJWT(text string, start int, end int) bool {
	value := text[start:end]
	segments := strings.Split(value, ".")
	if len(segments) != 3 {
		return false
	}

	headerBytes, err := decodeBase64URL(segments[0])
	if err != nil {
		return false
	}

	payloadBytes, err := decodeBase64URL(segments[1])
	if err != nil {
		return false
	}

	var header map[string]any
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return false
	}

	if _, ok := header["alg"]; !ok {
		return false
	}

	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return false
	}

	return len(payload) > 0
}

func validatePhone(text string, start int, end int) bool {
	value := text[start:end]
	digits := digitsOnly(value)
	if len(digits) < 10 || len(digits) > 15 {
		return false
	}

	if start > 0 {
		if r, ok := previousRune(text, start); ok && isPhoneTokenBoundary(r) {
			return false
		}
	}

	if end < len(text) {
		if r, ok := nextRune(text, end); ok && isPhoneTokenBoundary(r) {
			return false
		}
	}

	if strings.Count(value, "@") > 0 {
		return false
	}

	return true
}

func validateSSN(text string, start int, end int) bool {
	value := text[start:end]
	digits := digitsOnly(value)
	if len(digits) != 9 {
		return false
	}

	if digits[:3] == "000" || digits[:3] == "666" || digits[0] == '9' {
		return false
	}

	if digits[3:5] == "00" || digits[5:] == "0000" {
		return false
	}

	return true
}

func validateCreditCard(text string, start int, end int) bool {
	value := text[start:end]
	digits := digitsOnly(value)
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}

	return passesLuhn(digits)
}

func decodeBase64URL(value string) ([]byte, error) {
	if len(value)%4 != 0 {
		value += strings.Repeat("=", 4-len(value)%4)
	}

	return base64.URLEncoding.DecodeString(value)
}

func digitsOnly(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		if r >= '0' && r <= '9' {
			builder.WriteRune(r)
		}
	}

	return builder.String()
}

func passesLuhn(digits string) bool {
	sum := 0
	double := false

	for idx := len(digits) - 1; idx >= 0; idx-- {
		value := int(digits[idx] - '0')
		if double {
			value *= 2
			if value > 9 {
				value -= 9
			}
		}
		sum += value
		double = !double
	}

	return sum > 0 && sum%10 == 0
}

func isPhoneTokenBoundary(r rune) bool {
	switch {
	case unicode.IsLetter(r):
		return true
	case unicode.IsDigit(r):
		return true
	case r == '_':
		return true
	default:
		return false
	}
}

func previousRune(text string, start int) (rune, bool) {
	if start <= 0 || start > len(text) {
		return 0, false
	}

	r, _ := utf8.DecodeLastRuneInString(text[:start])
	if r == utf8.RuneError {
		return r, false
	}

	return r, true
}

func nextRune(text string, end int) (rune, bool) {
	if end < 0 || end >= len(text) {
		return 0, false
	}

	r, _ := utf8.DecodeRuneInString(text[end:])
	if r == utf8.RuneError {
		return r, false
	}

	return r, true
}

func stringifyIndex(value int) string {
	return strconv.Itoa(value)
}

type passwordDetector struct{}

func (passwordDetector) Kind() Kind {
	return KindPassword
}

func (passwordDetector) FindAll(text string) []Match {
	if text == "" {
		return nil
	}

	indexes := passwordPattern.FindAllStringSubmatchIndex(text, -1)
	matches := make([]Match, 0, len(indexes))
	for _, idx := range indexes {
		start, end := firstCapturedRange(idx)
		if start < 0 || end < 0 {
			continue
		}

		value := text[start:end]
		if len(value) < 4 {
			continue
		}

		matches = append(matches, Match{
			Kind:  KindPassword,
			Value: value,
			Start: start,
			End:   end,
		})
	}

	return matches
}

func firstCapturedRange(indexes []int) (int, int) {
	for idx := 2; idx+1 < len(indexes); idx += 2 {
		if indexes[idx] >= 0 && indexes[idx+1] >= 0 {
			return indexes[idx], indexes[idx+1]
		}
	}
	return -1, -1
}
