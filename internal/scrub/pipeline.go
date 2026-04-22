package scrub

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"stagehand/internal/recorder"
	"stagehand/internal/scrub/detectors"
	"stagehand/internal/scrub/session_salt"
)

type Action string

const (
	ActionDrop     Action = "drop"
	ActionMask     Action = "mask"
	ActionHash     Action = "hash"
	ActionPreserve Action = "preserve"
)

type Rule struct {
	Name    string
	Pattern string
	Action  Action
}

type Options struct {
	PolicyVersion   string
	SessionSaltID   string
	HashSalt        []byte
	Rules           []Rule
	DetectorLibrary *detectors.Library
}

type Pipeline struct {
	policyVersion string
	sessionSaltID string
	hashSalt      []byte
	rules         []compiledRule
	detectors     *detectors.Library
}

type compiledRule struct {
	name        string
	pattern     string
	action      Action
	re          *regexp.Regexp
	specificity int
	order       int
}

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	if len(e.Problems) == 0 {
		return "invalid scrub configuration"
	}

	return fmt.Sprintf("invalid scrub configuration: %s", strings.Join(e.Problems, "; "))
}

func (e *ValidationError) add(format string, args ...any) {
	e.Problems = append(e.Problems, fmt.Sprintf(format, args...))
}

func (e *ValidationError) err() error {
	if len(e.Problems) == 0 {
		return nil
	}

	return e
}

func DefaultRules() []Rule {
	return []Rule{
		{
			Name:    "request-authorization-header",
			Pattern: "request.headers.authorization",
			Action:  ActionDrop,
		},
		{
			Name:    "request-cookie-header",
			Pattern: "request.headers.cookie",
			Action:  ActionDrop,
		},
	}
}

func NewPipeline(opts Options) (*Pipeline, error) {
	verr := &ValidationError{}

	if strings.TrimSpace(opts.PolicyVersion) == "" {
		verr.add("policy_version is required")
	}

	if strings.TrimSpace(opts.SessionSaltID) == "" {
		verr.add("session_salt_id is required")
	}

	if opts.DetectorLibrary != nil && len(opts.HashSalt) == 0 {
		verr.add("detector-based scrubbing requires hash_salt")
	}

	compiledRules := make([]compiledRule, 0, len(opts.Rules))
	for idx, rule := range opts.Rules {
		if strings.TrimSpace(rule.Pattern) == "" {
			verr.add("rules[%d].pattern is required", idx)
			continue
		}

		if !isValidAction(rule.Action) {
			verr.add("rules[%d].action must be one of %q, %q, %q, %q", idx, ActionDrop, ActionMask, ActionHash, ActionPreserve)
			continue
		}

		re, err := compilePattern(rule.Pattern)
		if err != nil {
			verr.add("rules[%d].pattern %q is invalid: %v", idx, rule.Pattern, err)
			continue
		}

		if rule.Action == ActionHash && len(opts.HashSalt) == 0 {
			verr.add("rules[%d] uses action %q but hash_salt is empty", idx, ActionHash)
			continue
		}

		compiledRules = append(compiledRules, compiledRule{
			name:        rule.Name,
			pattern:     rule.Pattern,
			action:      rule.Action,
			re:          re,
			specificity: patternSpecificity(rule.Pattern),
			order:       idx,
		})
	}

	if err := verr.err(); err != nil {
		return nil, err
	}

	return &Pipeline{
		policyVersion: opts.PolicyVersion,
		sessionSaltID: opts.SessionSaltID,
		hashSalt:      append([]byte(nil), opts.HashSalt...),
		rules:         compiledRules,
		detectors:     opts.DetectorLibrary,
	}, nil
}

func (p *Pipeline) ScrubInteraction(interaction recorder.Interaction) (recorder.Interaction, error) {
	scrubbed := interaction
	report := newReportCollector()

	scrubbedHeaders, err := p.scrubHeaders(interaction.Request.Headers, report)
	if err != nil {
		return recorder.Interaction{}, err
	}
	scrubbed.Request.Headers = scrubbedHeaders

	scrubbedURL, err := p.scrubQuery(interaction.Request.URL, report)
	if err != nil {
		return recorder.Interaction{}, err
	}
	scrubbed.Request.URL = scrubbedURL

	scrubbedBody, removed, err := p.scrubValue("request.body", interaction.Request.Body, report)
	if err != nil {
		return recorder.Interaction{}, err
	}
	if removed {
		scrubbed.Request.Body = nil
	} else {
		scrubbed.Request.Body = scrubbedBody
	}

	scrubbed.Events = make([]recorder.Event, len(interaction.Events))
	for idx, event := range interaction.Events {
		copiedEvent := event
		if event.Data != nil {
			path := fmt.Sprintf("events[%d].data", idx)
			scrubbedData, removed, err := p.scrubValue(path, event.Data, report)
			if err != nil {
				return recorder.Interaction{}, err
			}

			switch {
			case removed || scrubbedData == nil:
				copiedEvent.Data = nil
			default:
				typedData, ok := scrubbedData.(map[string]any)
				if !ok {
					return recorder.Interaction{}, fmt.Errorf("%s scrubbed to %T, want map[string]any", path, scrubbedData)
				}
				copiedEvent.Data = typedData
			}
		}

		scrubbed.Events[idx] = copiedEvent
	}

	scrubbed.ScrubReport = recorder.ScrubReport{
		RedactedPaths:      report.paths(),
		ScrubPolicyVersion: p.policyVersion,
		SessionSaltID:      p.sessionSaltID,
	}

	return scrubbed, nil
}

func (p *Pipeline) scrubHeaders(headers map[string][]string, report *reportCollector) (map[string][]string, error) {
	if len(headers) == 0 {
		return nil, nil
	}

	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)

	scrubbed := make(map[string][]string, len(headers))
	for _, originalName := range names {
		path := fmt.Sprintf("request.headers.%s", strings.ToLower(originalName))
		values := append([]string(nil), headers[originalName]...)
		rule, ok := p.ruleForPath(path)
		if !ok {
			updated, changed, err := p.scrubStringSliceWithDetectors(path, values, report)
			if err != nil {
				return nil, fmt.Errorf("scrub header %s: %w", path, err)
			}
			if changed {
				scrubbed[originalName] = updated
				continue
			}
			scrubbed[originalName] = values
			continue
		}

		updated, remove, err := p.applyStringSliceAction(path, values, rule.action, report)
		if err != nil {
			return nil, fmt.Errorf("scrub header %s: %w", path, err)
		}
		if remove {
			continue
		}
		scrubbed[originalName] = updated
	}

	if len(scrubbed) == 0 {
		return nil, nil
	}

	return scrubbed, nil
}

func (p *Pipeline) scrubQuery(rawURL string, report *reportCollector) (string, error) {
	if strings.TrimSpace(rawURL) == "" {
		return rawURL, nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse request url %q: %w", rawURL, err)
	}

	query := parsed.Query()
	if len(query) == 0 {
		return rawURL, nil
	}

	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	changed := false
	scrubbedQuery := url.Values{}
	for _, key := range keys {
		path := fmt.Sprintf("request.query.%s", key)
		values := append([]string(nil), query[key]...)
		rule, ok := p.ruleForPath(path)
		if !ok {
			updated, detectorChanged, err := p.scrubStringSliceWithDetectors(path, values, report)
			if err != nil {
				return "", fmt.Errorf("scrub query %s: %w", path, err)
			}
			if detectorChanged {
				changed = true
				scrubbedQuery[key] = updated
				continue
			}
			scrubbedQuery[key] = values
			continue
		}

		updated, remove, err := p.applyStringSliceAction(path, values, rule.action, report)
		if err != nil {
			return "", err
		}
		changed = true
		if remove {
			continue
		}
		scrubbedQuery[key] = updated
	}

	if !changed {
		return rawURL, nil
	}

	parsed.RawQuery = scrubbedQuery.Encode()
	return parsed.String(), nil
}

func (p *Pipeline) scrubValue(path string, value any, report *reportCollector) (any, bool, error) {
	if value == nil {
		return nil, false, nil
	}

	rule, ok := p.ruleForPath(path)
	if ok {
		return p.applyAction(path, value, rule.action, report)
	}

	switch typed := value.(type) {
	case map[string]any:
		return p.scrubMap(path, typed, report)
	case []any:
		return p.scrubSlice(path, typed, report)
	case []string:
		return p.scrubStringSlice(path, typed, report)
	case string:
		updated, _, err := p.scrubStringWithDetectors(path, typed, report)
		if err != nil {
			return nil, false, err
		}
		return updated, false, nil
	default:
		return typed, false, nil
	}
}

func (p *Pipeline) scrubMap(path string, value map[string]any, report *reportCollector) (any, bool, error) {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	scrubbed := make(map[string]any, len(value))
	for _, key := range keys {
		childPath := joinObjectPath(path, key)
		updated, remove, err := p.scrubValue(childPath, value[key], report)
		if err != nil {
			return nil, false, err
		}
		if remove {
			continue
		}
		scrubbed[key] = updated
	}

	return scrubbed, false, nil
}

func (p *Pipeline) scrubSlice(path string, value []any, report *reportCollector) (any, bool, error) {
	scrubbed := make([]any, 0, len(value))
	for idx, item := range value {
		childPath := fmt.Sprintf("%s[%d]", path, idx)
		updated, remove, err := p.scrubValue(childPath, item, report)
		if err != nil {
			return nil, false, err
		}
		if remove {
			continue
		}
		scrubbed = append(scrubbed, updated)
	}

	return scrubbed, false, nil
}

func (p *Pipeline) scrubStringSlice(path string, value []string, report *reportCollector) (any, bool, error) {
	scrubbed := make([]string, 0, len(value))
	for idx, item := range value {
		childPath := fmt.Sprintf("%s[%d]", path, idx)
		updated, _, err := p.scrubStringWithDetectors(childPath, item, report)
		if err != nil {
			return nil, false, err
		}
		scrubbed = append(scrubbed, updated)
	}

	return scrubbed, false, nil
}

func (p *Pipeline) applyAction(path string, value any, action Action, report *reportCollector) (any, bool, error) {
	switch action {
	case ActionDrop:
		report.add(path)
		return nil, true, nil
	case ActionPreserve:
		return deepCopy(value), false, nil
	case ActionMask:
		masked, err := maskValue(value)
		if err != nil {
			return nil, false, fmt.Errorf("mask %s: %w", path, err)
		}
		report.add(path)
		return masked, false, nil
	case ActionHash:
		hashed, err := p.hashValue(value)
		if err != nil {
			return nil, false, fmt.Errorf("hash %s: %w", path, err)
		}
		report.add(path)
		return hashed, false, nil
	default:
		return nil, false, fmt.Errorf("unsupported action %q", action)
	}
}

func (p *Pipeline) applyStringSliceAction(path string, values []string, action Action, report *reportCollector) ([]string, bool, error) {
	switch action {
	case ActionDrop:
		report.add(path)
		return nil, true, nil
	case ActionPreserve:
		return append([]string(nil), values...), false, nil
	case ActionMask:
		masked := make([]string, len(values))
		for idx, value := range values {
			masked[idx] = maskString(value)
		}
		report.add(path)
		return masked, false, nil
	case ActionHash:
		hashed := make([]string, len(values))
		for idx, value := range values {
			replacement, err := p.hashString(value)
			if err != nil {
				return nil, false, fmt.Errorf("hash %s: %w", path, err)
			}
			hashed[idx] = replacement
		}
		report.add(path)
		return hashed, false, nil
	default:
		return nil, false, fmt.Errorf("unsupported string-slice action %q", action)
	}
}

func (p *Pipeline) scrubStringSliceWithDetectors(path string, values []string, report *reportCollector) ([]string, bool, error) {
	if len(values) == 0 {
		return nil, false, nil
	}

	scrubbed := append([]string(nil), values...)
	changed := false
	for idx, value := range values {
		updated, valueChanged, err := p.scrubStringWithDetectors(path, value, report)
		if err != nil {
			return nil, false, err
		}
		if valueChanged {
			changed = true
			scrubbed[idx] = updated
		}
	}

	return scrubbed, changed, nil
}

func (p *Pipeline) scrubStringWithDetectors(path, value string, report *reportCollector) (string, bool, error) {
	if p.detectors == nil || value == "" {
		return value, false, nil
	}

	matches := p.detectors.Scan(value)
	if len(matches) == 0 {
		return value, false, nil
	}

	scrubbed := value
	for idx := len(matches) - 1; idx >= 0; idx-- {
		replacement, err := p.hashString(matches[idx].Value)
		if err != nil {
			return "", false, fmt.Errorf("replace detected %s at %s: %w", matches[idx].Kind, path, err)
		}
		scrubbed = scrubbed[:matches[idx].Start] + replacement + scrubbed[matches[idx].End:]
	}

	report.add(path)
	return scrubbed, true, nil
}

func (p *Pipeline) hashValue(value any) (any, error) {
	switch typed := value.(type) {
	case string:
		return p.hashString(typed)
	case []string:
		hashed := make([]string, len(typed))
		for idx, value := range typed {
			replacement, err := p.hashString(value)
			if err != nil {
				return nil, err
			}
			hashed[idx] = replacement
		}
		return hashed, nil
	case []any:
		hashed := make([]any, len(typed))
		for idx, value := range typed {
			text, ok := stringValue(value)
			if !ok {
				return nil, fmt.Errorf("hash only supports scalar or string-like list values, got %T", value)
			}
			replacement, err := p.hashString(text)
			if err != nil {
				return nil, err
			}
			hashed[idx] = replacement
		}
		return hashed, nil
	default:
		text, ok := stringValue(value)
		if !ok {
			return nil, fmt.Errorf("hash only supports scalar values, got %T", value)
		}
		return p.hashString(text)
	}
}

func maskValue(value any) (any, error) {
	switch typed := value.(type) {
	case string:
		return maskString(typed), nil
	case []string:
		masked := make([]string, len(typed))
		for idx, value := range typed {
			masked[idx] = maskString(value)
		}
		return masked, nil
	case []any:
		masked := make([]any, len(typed))
		for idx, value := range typed {
			text, ok := stringValue(value)
			if !ok {
				return nil, fmt.Errorf("mask only supports scalar or string-like list values, got %T", value)
			}
			masked[idx] = maskString(text)
		}
		return masked, nil
	default:
		text, ok := stringValue(value)
		if !ok {
			return nil, fmt.Errorf("mask only supports scalar values, got %T", value)
		}
		return maskString(text), nil
	}
}

func (p *Pipeline) hashString(value string) (string, error) {
	return session_salt.Replacement(p.hashSalt, value)
}

func maskString(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return ""
	}
	if len(runes) <= 4 {
		return strings.Repeat("*", len(runes))
	}

	return strings.Repeat("*", len(runes)-4) + string(runes[len(runes)-4:])
}

func stringValue(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case fmt.Stringer:
		return typed.String(), true
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprint(typed), true
	default:
		return "", false
	}
}

func deepCopy(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		copied := make(map[string]any, len(typed))
		for key, child := range typed {
			copied[key] = deepCopy(child)
		}
		return copied
	case []any:
		copied := make([]any, len(typed))
		for idx, child := range typed {
			copied[idx] = deepCopy(child)
		}
		return copied
	case []string:
		return append([]string(nil), typed...)
	default:
		return typed
	}
}

func (p *Pipeline) ruleForPath(path string) (compiledRule, bool) {
	var selected compiledRule
	found := false

	for _, rule := range p.rules {
		if !rule.re.MatchString(path) {
			continue
		}

		if !found || rule.specificity > selected.specificity || (rule.specificity == selected.specificity && rule.order > selected.order) {
			selected = rule
			found = true
		}
	}

	return selected, found
}

func isValidAction(action Action) bool {
	switch action {
	case ActionDrop, ActionMask, ActionHash, ActionPreserve:
		return true
	default:
		return false
	}
}

func compilePattern(pattern string) (*regexp.Regexp, error) {
	escaped := regexp.QuoteMeta(pattern)
	escaped = strings.ReplaceAll(escaped, `\[\*\]`, `\[\d+\]`)
	escaped = strings.ReplaceAll(escaped, `\*`, `[^.\[\]]+`)
	return regexp.Compile("^" + escaped + "$")
}

func patternSpecificity(pattern string) int {
	return len(strings.ReplaceAll(pattern, "*", ""))
}

func joinObjectPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

type reportCollector struct {
	seen         map[string]struct{}
	pathsInOrder []string
}

func newReportCollector() *reportCollector {
	return &reportCollector{
		seen: make(map[string]struct{}),
	}
}

func (r *reportCollector) add(path string) {
	if _, ok := r.seen[path]; ok {
		return
	}
	r.seen[path] = struct{}{}
	r.pathsInOrder = append(r.pathsInOrder, path)
}

func (r *reportCollector) paths() []string {
	return append([]string(nil), r.pathsInOrder...)
}
