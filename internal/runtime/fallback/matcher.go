package fallback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"

	"stagehand/internal/config"
	"stagehand/internal/recorder"
)

const minimumNearestScore = 0.35

var ErrNoMatch = errors.New("fallback match not found")

type MatchResult struct {
	Tier           recorder.FallbackTier
	Interaction    recorder.Interaction
	Score          float64
	Reason         string
	CandidateCount int
}

type SynthesisInput struct {
	Request    recorder.Interaction
	Candidates []recorder.Interaction
}

type Synthesizer interface {
	Synthesize(ctx context.Context, input SynthesisInput) (recorder.Interaction, error)
}

type SynthesizeFunc func(ctx context.Context, input SynthesisInput) (recorder.Interaction, error)

func (f SynthesizeFunc) Synthesize(ctx context.Context, input SynthesisInput) (recorder.Interaction, error) {
	return f(ctx, input)
}

type Matcher struct {
	recorded    []recorder.Interaction
	allowed     map[recorder.FallbackTier]bool
	synthesizer Synthesizer
}

type Option func(*Matcher) error

func WithAllowedTiers(tiers []recorder.FallbackTier) Option {
	return func(m *Matcher) error {
		allowed, err := allowedTierSet(tiers)
		if err != nil {
			return err
		}
		m.allowed = allowed
		return nil
	}
}

func WithConfig(cfg config.Config) Option {
	tiers := make([]recorder.FallbackTier, 0, len(cfg.Fallback.AllowedTiers))
	for _, tier := range cfg.Fallback.AllowedTiers {
		tiers = append(tiers, recorder.FallbackTier(tier))
	}
	return WithAllowedTiers(tiers)
}

func WithStateSynthesizer(synthesizer Synthesizer) Option {
	return func(m *Matcher) error {
		m.synthesizer = synthesizer
		return nil
	}
}

func NewMatcher(recorded []recorder.Interaction, opts ...Option) (*Matcher, error) {
	matcher := &Matcher{
		recorded: append([]recorder.Interaction(nil), recorded...),
		allowed: map[recorder.FallbackTier]bool{
			recorder.FallbackTierExact:           true,
			recorder.FallbackTierNearestNeighbor: true,
			recorder.FallbackTierStateSynthesis:  true,
		},
	}

	for _, opt := range opts {
		if err := opt(matcher); err != nil {
			return nil, err
		}
	}

	return matcher, nil
}

func (m *Matcher) Match(ctx context.Context, request recorder.Interaction) (MatchResult, error) {
	candidates := compatibleCandidates(m.recorded, request)

	if m.allowed[recorder.FallbackTierExact] {
		if interaction, ok := exactMatch(candidates, request); ok {
			interaction.FallbackTier = recorder.FallbackTierExact
			return MatchResult{
				Tier:           recorder.FallbackTierExact,
				Interaction:    interaction,
				Score:          1,
				Reason:         "exact request match",
				CandidateCount: len(candidates),
			}, nil
		}
	}

	if m.allowed[recorder.FallbackTierNearestNeighbor] {
		if interaction, score, ok := nearestMatch(candidates, request); ok {
			interaction.FallbackTier = recorder.FallbackTierNearestNeighbor
			return MatchResult{
				Tier:           recorder.FallbackTierNearestNeighbor,
				Interaction:    interaction,
				Score:          score,
				Reason:         "nearest request match",
				CandidateCount: len(candidates),
			}, nil
		}
	}

	if m.allowed[recorder.FallbackTierStateSynthesis] && m.synthesizer != nil {
		interaction, err := m.synthesizer.Synthesize(ctx, SynthesisInput{
			Request:    request,
			Candidates: append([]recorder.Interaction(nil), candidates...),
		})
		if err != nil {
			return MatchResult{}, fmt.Errorf("state synthesis fallback: %w", err)
		}
		interaction.FallbackTier = recorder.FallbackTierStateSynthesis
		return MatchResult{
			Tier:           recorder.FallbackTierStateSynthesis,
			Interaction:    interaction,
			Score:          1,
			Reason:         "state synthesis hook",
			CandidateCount: len(candidates),
		}, nil
	}

	return MatchResult{}, fmt.Errorf(
		"%w for %s %s %s",
		ErrNoMatch,
		request.Service,
		request.Request.Method,
		request.Request.URL,
	)
}

func allowedTierSet(tiers []recorder.FallbackTier) (map[recorder.FallbackTier]bool, error) {
	if len(tiers) == 0 {
		return nil, fmt.Errorf("allowed fallback tiers must not be empty")
	}

	allowed := map[recorder.FallbackTier]bool{}
	for _, tier := range tiers {
		switch tier {
		case recorder.FallbackTierExact, recorder.FallbackTierNearestNeighbor, recorder.FallbackTierStateSynthesis:
			allowed[tier] = true
		case recorder.FallbackTierLLMSynthesis:
			return nil, fmt.Errorf("llm_synthesis fallback is not implemented in runtime tiers 0-2")
		default:
			return nil, fmt.Errorf("unsupported fallback tier %q", tier)
		}
	}

	return allowed, nil
}

func compatibleCandidates(recorded []recorder.Interaction, request recorder.Interaction) []recorder.Interaction {
	candidates := make([]recorder.Interaction, 0, len(recorded))
	for _, candidate := range recorded {
		if candidate.Service != request.Service {
			continue
		}
		if candidate.Operation != request.Operation {
			continue
		}
		if candidate.Protocol != request.Protocol {
			continue
		}
		if !strings.EqualFold(candidate.Request.Method, request.Request.Method) {
			continue
		}
		if requestPath(candidate.Request.URL) != requestPath(request.Request.URL) {
			continue
		}
		candidates = append(candidates, candidate)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Sequence == candidates[j].Sequence {
			return candidates[i].InteractionID < candidates[j].InteractionID
		}
		return candidates[i].Sequence < candidates[j].Sequence
	})

	return candidates
}

func exactMatch(candidates []recorder.Interaction, request recorder.Interaction) (recorder.Interaction, bool) {
	want := exactRequestKey(request)
	for _, candidate := range candidates {
		if exactRequestKey(candidate) == want {
			return candidate, true
		}
	}

	return recorder.Interaction{}, false
}

func nearestMatch(candidates []recorder.Interaction, request recorder.Interaction) (recorder.Interaction, float64, bool) {
	bestScore := math.Inf(-1)
	var best recorder.Interaction
	for _, candidate := range candidates {
		score := nearestScore(candidate, request)
		if score > bestScore {
			bestScore = score
			best = candidate
		}
	}

	if bestScore < minimumNearestScore {
		return recorder.Interaction{}, 0, false
	}

	return best, bestScore, true
}

func exactRequestKey(interaction recorder.Interaction) string {
	parts := []string{
		interaction.Service,
		interaction.Operation,
		string(interaction.Protocol),
		strings.ToUpper(interaction.Request.Method),
		normalizedURL(interaction.Request.URL),
		canonicalJSON(interaction.Request.Body),
	}
	return strings.Join(parts, "\x00")
}

func nearestScore(candidate recorder.Interaction, request recorder.Interaction) float64 {
	queryScore := setSimilarity(queryParts(candidate.Request.URL), queryParts(request.Request.URL))
	bodyScore := setSimilarity(flattenedValueSet(candidate.Request.Body), flattenedValueSet(request.Request.Body))
	headerScore := setSimilarity(headerParts(candidate.Request.Headers), headerParts(request.Request.Headers))

	weightedScore := 0.50*bodyScore + 0.35*queryScore + 0.15*headerScore
	if len(flattenedValueSet(candidate.Request.Body)) == 0 && len(flattenedValueSet(request.Request.Body)) == 0 {
		weightedScore += 0.25
	}
	if len(queryParts(candidate.Request.URL)) == 0 && len(queryParts(request.Request.URL)) == 0 {
		weightedScore += 0.15
	}

	if weightedScore > 1 {
		return 1
	}
	return weightedScore
}

func normalizedURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return strings.TrimSpace(raw)
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.RawQuery = sortedQuery(parsed.Query())
	parsed.Fragment = ""
	return parsed.String()
}

func requestPath(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return strings.TrimSpace(raw)
	}

	return strings.ToLower(parsed.Host) + parsed.EscapedPath()
}

func sortedQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
		sort.Strings(values[key])
	}
	sort.Strings(keys)

	ordered := url.Values{}
	for _, key := range keys {
		for _, value := range values[key] {
			ordered.Add(key, value)
		}
	}

	return ordered.Encode()
}

func queryParts(raw string) []string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil
	}

	values := parsed.Query()
	parts := make([]string, 0)
	for key, entries := range values {
		for _, value := range entries {
			parts = append(parts, key+"="+value)
		}
	}
	sort.Strings(parts)
	return parts
}

func headerParts(headers map[string][]string) []string {
	if len(headers) == 0 {
		return nil
	}

	parts := make([]string, 0)
	for key, entries := range headers {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "authorization" || normalizedKey == "cookie" || normalizedKey == "set-cookie" {
			continue
		}
		for _, value := range entries {
			parts = append(parts, normalizedKey+"="+strings.TrimSpace(value))
		}
	}
	sort.Strings(parts)
	return parts
}

func flattenedValueSet(value any) []string {
	if value == nil {
		return nil
	}

	normalized := normalizeJSONValue(value)
	parts := make([]string, 0)
	flattenValue("", normalized, &parts)
	sort.Strings(parts)
	return parts
}

func flattenValue(path string, value any, parts *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			flattenValue(joinPath(path, key), typed[key], parts)
		}
	case []any:
		for idx, item := range typed {
			flattenValue(fmt.Sprintf("%s[%d]", path, idx), item, parts)
		}
	default:
		*parts = append(*parts, path+"="+canonicalJSON(typed))
	}
}

func joinPath(prefix string, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func normalizeJSONValue(value any) any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}

	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return value
	}

	return decoded
}

func canonicalJSON(value any) string {
	if value == nil {
		return "null"
	}

	encoded, err := json.Marshal(normalizeJSONValue(value))
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(encoded)
}

func setSimilarity(left []string, right []string) float64 {
	if len(left) == 0 && len(right) == 0 {
		return 1
	}
	if len(left) == 0 || len(right) == 0 {
		return 0
	}

	leftSet := map[string]bool{}
	for _, value := range left {
		leftSet[value] = true
	}

	rightSet := map[string]bool{}
	for _, value := range right {
		rightSet[value] = true
	}

	intersection := 0
	for value := range leftSet {
		if rightSet[value] {
			intersection++
		}
	}

	union := len(leftSet)
	for value := range rightSet {
		if !leftSet[value] {
			union++
		}
	}

	if union == 0 {
		return 1
	}
	return float64(intersection) / float64(union)
}
