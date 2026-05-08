package injection

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"

	"stagehand/internal/config"
)

const MetadataKey = "error_injection"

type Request struct {
	Service   string
	Operation string
	Tool      string
}

type Rule struct {
	Match  Match
	Inject Inject
}

type Match struct {
	Service     string
	Operation   string
	Tool        string
	NthCall     int
	AnyCall     bool
	Probability *float64
}

type Inject struct {
	Library   string
	Status    int
	Error     string
	LatencyMS int
	Body      any
}

type ResponseOverride struct {
	Status    int            `json:"status,omitempty"`
	Error     string         `json:"error,omitempty"`
	LatencyMS int            `json:"latency_ms,omitempty"`
	Body      map[string]any `json:"body,omitempty"`
}

type Provenance struct {
	RuleIndex   int      `json:"rule_index"`
	Tool        string   `json:"tool,omitempty"`
	Service     string   `json:"service"`
	Operation   string   `json:"operation"`
	CallNumber  int      `json:"call_number"`
	NthCall     int      `json:"nth_call,omitempty"`
	AnyCall     bool     `json:"any_call,omitempty"`
	Probability *float64 `json:"probability,omitempty"`
	Library     string   `json:"library,omitempty"`
	Status      int      `json:"status,omitempty"`
	Error       string   `json:"error,omitempty"`
	LatencyMS   int      `json:"latency_ms,omitempty"`
}

type Decision struct {
	Matched    bool
	Override   ResponseOverride
	Provenance Provenance
}

type Engine struct {
	rules  []Rule
	counts map[string]int
	rng    *rand.Rand
	mu     sync.Mutex
}

type Option func(*Engine)

func WithRandomSource(source rand.Source) Option {
	return func(e *Engine) {
		if source != nil {
			e.rng = rand.New(source)
		}
	}
}

func NewEngine(rules []Rule, opts ...Option) (*Engine, error) {
	engine := &Engine{
		rules:  append([]Rule(nil), rules...),
		counts: map[string]int{},
		rng:    rand.New(rand.NewSource(1)),
	}

	for _, opt := range opts {
		opt(engine)
	}

	for idx, rule := range engine.rules {
		tool := strings.TrimSpace(rule.Match.Tool)
		if tool != "" && (strings.TrimSpace(rule.Match.Service) != "" || strings.TrimSpace(rule.Match.Operation) != "") {
			return nil, fmt.Errorf("error injection rule %d match tool cannot be combined with service or operation", idx)
		}
		if tool == "" && strings.TrimSpace(rule.Match.Service) == "" {
			return nil, fmt.Errorf("error injection rule %d match service is required", idx)
		}
		if tool == "" && strings.TrimSpace(rule.Match.Operation) == "" {
			return nil, fmt.Errorf("error injection rule %d match operation is required", idx)
		}
		if rule.Match.NthCall < 0 {
			return nil, fmt.Errorf("error injection rule %d nth_call must be greater than or equal to 0", idx)
		}
		if rule.Match.AnyCall && rule.Match.NthCall > 0 {
			return nil, fmt.Errorf("error injection rule %d cannot set both nth_call and any_call", idx)
		}
		if rule.Match.Probability != nil && (*rule.Match.Probability < 0 || *rule.Match.Probability > 1) {
			return nil, fmt.Errorf("error injection rule %d probability must be between 0 and 1", idx)
		}
		if _, err := resolveInject(rule.Inject, tool != ""); err != nil {
			return nil, fmt.Errorf("error injection rule %d: %w", idx, err)
		}
	}

	return engine, nil
}

func NewEngineFromConfig(cfg config.ErrorInjectionConfig, opts ...Option) (*Engine, error) {
	if !cfg.Enabled {
		return NewEngine(nil, opts...)
	}

	rules := make([]Rule, 0, len(cfg.Rules))
	for _, rule := range cfg.Rules {
		rules = append(rules, Rule{
			Match: Match{
				Service:     rule.Match.Service,
				Operation:   rule.Match.Operation,
				Tool:        rule.Match.Tool,
				NthCall:     rule.Match.NthCall,
				AnyCall:     rule.Match.AnyCall,
				Probability: rule.Match.Probability,
			},
			Inject: Inject{
				Library:   rule.Inject.Library,
				Status:    rule.Inject.Status,
				Error:     rule.Inject.Error,
				LatencyMS: rule.Inject.LatencyMS,
				Body:      rule.Inject.Body,
			},
		})
	}

	return NewEngine(rules, opts...)
}

func (e *Engine) Evaluate(request Request) (Decision, error) {
	if e == nil {
		return Decision{}, fmt.Errorf("error injection engine is nil")
	}

	service := strings.TrimSpace(request.Service)
	operation := strings.TrimSpace(request.Operation)
	tool := strings.TrimSpace(request.Tool)
	if tool != "" {
		service = "stagehand.tool"
		operation = tool
	}
	if service == "" || operation == "" {
		return Decision{}, fmt.Errorf("error injection request service and operation are required")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	key := service + "\x00" + operation
	e.counts[key]++
	callNumber := e.counts[key]

	for idx, rule := range e.rules {
		if !rule.matches(service, operation, callNumber, e.rng) {
			continue
		}

		override, err := resolveInject(rule.Inject, strings.TrimSpace(rule.Match.Tool) != "")
		if err != nil {
			return Decision{}, fmt.Errorf("resolve error injection rule %d: %w", idx, err)
		}

		return Decision{
			Matched:  true,
			Override: override,
			Provenance: Provenance{
				RuleIndex:   idx,
				Tool:        strings.TrimSpace(rule.Match.Tool),
				Service:     service,
				Operation:   operation,
				CallNumber:  callNumber,
				NthCall:     rule.Match.NthCall,
				AnyCall:     rule.Match.AnyCall,
				Probability: cloneProbability(rule.Match.Probability),
				Library:     strings.TrimSpace(rule.Inject.Library),
				Status:      override.Status,
				Error:       override.Error,
				LatencyMS:   override.LatencyMS,
			},
		}, nil
	}

	return Decision{}, nil
}

func AppendProvenance(metadata map[string]any, provenance Provenance) map[string]any {
	if metadata == nil {
		metadata = map[string]any{}
	}

	section, _ := metadata[MetadataKey].(map[string]any)
	if section == nil {
		section = map[string]any{}
	}

	applied, _ := section["applied"].([]any)
	section["applied"] = append(applied, provenance)
	metadata[MetadataKey] = section
	return metadata
}

func (r Rule) matches(service string, operation string, callNumber int, rng *rand.Rand) bool {
	matchService := strings.TrimSpace(r.Match.Service)
	matchOperation := strings.TrimSpace(r.Match.Operation)
	if tool := strings.TrimSpace(r.Match.Tool); tool != "" {
		matchService = "stagehand.tool"
		matchOperation = tool
	}
	if matchService != service || matchOperation != operation {
		return false
	}
	if r.Match.NthCall > 0 && callNumber != r.Match.NthCall {
		return false
	}
	probability := 1.0
	if r.Match.Probability != nil {
		probability = *r.Match.Probability
	}
	if probability <= 0 {
		return false
	}
	if probability >= 1 {
		return true
	}
	return rng.Float64() < probability
}

func resolveInject(inject Inject, allowToolError bool) (ResponseOverride, error) {
	library := strings.TrimSpace(inject.Library)
	if library != "" {
		override, ok := NamedLibrary(library)
		if !ok {
			return ResponseOverride{}, fmt.Errorf("unknown named error library entry %q", library)
		}
		return override, nil
	}

	errorKind := strings.ToLower(strings.TrimSpace(inject.Error))
	if errorKind != "" && errorKind != "timeout" && !allowToolError {
		return ResponseOverride{}, fmt.Errorf("inject error must be timeout when set")
	}

	if inject.LatencyMS < 0 {
		return ResponseOverride{}, fmt.Errorf("inject latency_ms must be greater than or equal to 0")
	}

	if errorKind == "timeout" {
		return ResponseOverride{
			Error:     "timeout",
			LatencyMS: inject.LatencyMS,
		}, nil
	}

	if allowToolError {
		if errorKind == "" {
			return ResponseOverride{}, fmt.Errorf("inject error is required for tool error injection")
		}
		body, _ := inject.Body.(map[string]any)
		return ResponseOverride{
			Error:     errorKind,
			LatencyMS: inject.LatencyMS,
			Body:      cloneAnyMap(body),
		}, nil
	}

	if inject.Status < 100 || inject.Status > 599 {
		return ResponseOverride{}, fmt.Errorf("inject status must be between 100 and 599 unless error is timeout")
	}
	body, _ := inject.Body.(map[string]any)
	body = cloneAnyMap(body)
	if body == nil {
		body = map[string]any{
			"error": map[string]any{
				"type":    "invalid_request_error",
				"message": "Injected Stagehand error.",
			},
		}
	}
	return ResponseOverride{
		Status:    inject.Status,
		LatencyMS: inject.LatencyMS,
		Body:      body,
	}, nil
}

func cloneProbability(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneAnyMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = cloneAnyValue(value)
	}
	return cloned
}

func cloneAnyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for idx, item := range typed {
			cloned[idx] = cloneAnyValue(item)
		}
		return cloned
	default:
		return typed
	}
}
