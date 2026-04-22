package config

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"

	"stagehand/internal/scrub"

	"gopkg.in/yaml.v3"
)

const SchemaVersion = "v1alpha1"

type Mode string

const (
	ModeRecord      Mode = "record"
	ModeReplay      Mode = "replay"
	ModePassthrough Mode = "passthrough"
)

type ReplayMode string

const (
	ReplayModeExact        ReplayMode = "exact"
	ReplayModePromptReplay ReplayMode = "prompt_replay"
)

type ClockMode string

const (
	ClockModeWall ClockMode = "wall"
)

type FallbackTier string

const (
	FallbackTierExact           FallbackTier = "exact"
	FallbackTierNearestNeighbor FallbackTier = "nearest_neighbor"
	FallbackTierStateSynthesis  FallbackTier = "state_synthesis"
	FallbackTierLLMSynthesis    FallbackTier = "llm_synthesis"
)

type AuthMode string

const (
	AuthModePermissive     AuthMode = "permissive"
	AuthModeStrictRecorded AuthMode = "strict-recorded"
	AuthModeStrictMatch    AuthMode = "strict-match"
)

type ReportFormat string

const (
	ReportFormatTerminal       ReportFormat = "terminal"
	ReportFormatJSON           ReportFormat = "json"
	ReportFormatGitHubMarkdown ReportFormat = "github-markdown"
)

type FailCondition string

const (
	FailConditionBehaviorDiff       FailCondition = "behavior_diff"
	FailConditionAssertionFailure   FailCondition = "assertion_failure"
	FailConditionFallbackRegression FailCondition = "fallback_regression"
)

var (
	validModes         = []Mode{ModeRecord, ModeReplay, ModePassthrough}
	validReplayModes   = []ReplayMode{ReplayModeExact, ReplayModePromptReplay}
	validClockModes    = []ClockMode{ClockModeWall}
	validFallbackTiers = []FallbackTier{
		FallbackTierExact,
		FallbackTierNearestNeighbor,
		FallbackTierStateSynthesis,
		FallbackTierLLMSynthesis,
	}
	validAuthModes      = []AuthMode{AuthModePermissive, AuthModeStrictRecorded, AuthModeStrictMatch}
	validReportFormats  = []ReportFormat{ReportFormatTerminal, ReportFormatJSON, ReportFormatGitHubMarkdown}
	validFailConditions = []FailCondition{
		FailConditionBehaviorDiff,
		FailConditionAssertionFailure,
		FailConditionFallbackRegression,
	}
)

type Config struct {
	SchemaVersion string         `yaml:"schema_version"`
	Record        RecordConfig   `yaml:"record"`
	Replay        ReplayConfig   `yaml:"replay"`
	Scrub         ScrubConfig    `yaml:"scrub"`
	Fallback      FallbackConfig `yaml:"fallback"`
	Auth          AuthConfig     `yaml:"auth"`
}

type RecordConfig struct {
	DefaultMode Mode          `yaml:"default_mode"`
	StoragePath string        `yaml:"storage_path"`
	Capture     CaptureConfig `yaml:"capture"`
}

type CaptureConfig struct {
	MaxBodyBytes        int      `yaml:"max_body_bytes"`
	IncludeHeaders      []string `yaml:"include_headers"`
	RedactBeforePersist bool     `yaml:"redact_before_persist"`
}

type ReplayConfig struct {
	Mode             ReplayMode `yaml:"mode"`
	ClockMode        ClockMode  `yaml:"clock_mode"`
	AllowLiveNetwork bool       `yaml:"allow_live_network"`
}

type ScrubConfig struct {
	Enabled          bool           `yaml:"enabled"`
	PolicyVersion    string         `yaml:"policy_version"`
	CustomRulesFiles []string       `yaml:"custom_rules_files"`
	CustomRules      []ScrubRule    `yaml:"custom_rules"`
	Detectors        DetectorConfig `yaml:"detectors"`
}

type ScrubRule struct {
	Name    string `yaml:"name"`
	Pattern string `yaml:"pattern"`
	Action  string `yaml:"action"`
}

type DetectorConfig struct {
	Email      bool `yaml:"email"`
	Phone      bool `yaml:"phone"`
	SSN        bool `yaml:"ssn"`
	CreditCard bool `yaml:"credit_card"`
	JWT        bool `yaml:"jwt"`
	APIKey     bool `yaml:"api_key"`
}

type FallbackConfig struct {
	AllowedTiers []FallbackTier     `yaml:"allowed_tiers"`
	LLMSynthesis LLMSynthesisConfig `yaml:"llm_synthesis"`
}

type LLMSynthesisConfig struct {
	Enabled     bool   `yaml:"enabled"`
	ProviderEnv string `yaml:"provider_env"`
	Model       string `yaml:"model"`
}

type AuthConfig struct {
	DefaultMode  AuthMode            `yaml:"default_mode"`
	ServiceModes map[string]AuthMode `yaml:"service_modes"`
}

type TestConfig struct {
	SchemaVersion  string               `yaml:"schema_version"`
	Session        string               `yaml:"session"`
	Baseline       BaselineConfig       `yaml:"baseline"`
	Replay         TestReplayConfig     `yaml:"replay"`
	Fallback       TestFallbackConfig   `yaml:"fallback"`
	ErrorInjection ErrorInjectionConfig `yaml:"error_injection"`
	CI             CIConfig             `yaml:"ci"`
}

type BaselineConfig struct {
	Branch          string `yaml:"branch"`
	RequireExisting bool   `yaml:"require_existing"`
}

type TestReplayConfig struct {
	Mode              ReplayMode `yaml:"mode"`
	ForbidLiveNetwork bool       `yaml:"forbid_live_network"`
}

type TestFallbackConfig struct {
	DisallowedTiers []FallbackTier `yaml:"disallowed_tiers"`
}

type ErrorInjectionConfig struct {
	Enabled bool                 `yaml:"enabled"`
	Rules   []ErrorInjectionRule `yaml:"rules"`
}

type ErrorInjectionRule struct {
	Match  ErrorMatch  `yaml:"match"`
	Inject ErrorInject `yaml:"inject"`
}

type ErrorMatch struct {
	Service     string  `yaml:"service"`
	Operation   string  `yaml:"operation"`
	NthCall     int     `yaml:"nth_call"`
	AnyCall     bool    `yaml:"any_call"`
	Probability float64 `yaml:"probability"`
}

type ErrorInject struct {
	Library string         `yaml:"library"`
	Status  int            `yaml:"status"`
	Body    map[string]any `yaml:"body"`
}

type CIConfig struct {
	FailOn          []FailCondition `yaml:"fail_on"`
	ReportFormat    ReportFormat    `yaml:"report_format"`
	ArtifactName    string          `yaml:"artifact_name"`
	PostPRComment   bool            `yaml:"post_pr_comment"`
	MaxFallbackTier FallbackTier    `yaml:"max_fallback_tier"`
}

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	if len(e.Problems) == 0 {
		return "invalid config"
	}

	return fmt.Sprintf("invalid config: %s", strings.Join(e.Problems, "; "))
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

func DefaultConfig() Config {
	return Config{
		SchemaVersion: SchemaVersion,
		Record: RecordConfig{
			DefaultMode: ModeRecord,
			StoragePath: ".stagehand/runs",
			Capture: CaptureConfig{
				MaxBodyBytes:        1 << 20,
				IncludeHeaders:      []string{"content-type", "accept"},
				RedactBeforePersist: true,
			},
		},
		Replay: ReplayConfig{
			Mode:             ReplayModeExact,
			ClockMode:        ClockModeWall,
			AllowLiveNetwork: false,
		},
		Scrub: ScrubConfig{
			Enabled:       true,
			PolicyVersion: "v1",
			CustomRules:   []ScrubRule{},
			Detectors: DetectorConfig{
				Email:      true,
				Phone:      true,
				SSN:        true,
				CreditCard: true,
				JWT:        true,
				APIKey:     true,
			},
		},
		Fallback: FallbackConfig{
			AllowedTiers: []FallbackTier{
				FallbackTierExact,
				FallbackTierNearestNeighbor,
				FallbackTierStateSynthesis,
			},
			LLMSynthesis: LLMSynthesisConfig{
				Enabled: false,
			},
		},
		Auth: AuthConfig{
			DefaultMode:  AuthModePermissive,
			ServiceModes: map[string]AuthMode{},
		},
	}
}

func DefaultTestConfig() TestConfig {
	return TestConfig{
		SchemaVersion: SchemaVersion,
		Baseline: BaselineConfig{
			Branch:          "main",
			RequireExisting: true,
		},
		Replay: TestReplayConfig{
			Mode:              ReplayModeExact,
			ForbidLiveNetwork: true,
		},
		Fallback: TestFallbackConfig{
			DisallowedTiers: []FallbackTier{FallbackTierLLMSynthesis},
		},
		ErrorInjection: ErrorInjectionConfig{
			Enabled: false,
			Rules:   []ErrorInjectionRule{},
		},
		CI: CIConfig{
			FailOn: []FailCondition{
				FailConditionBehaviorDiff,
				FailConditionAssertionFailure,
			},
			ReportFormat:    ReportFormatGitHubMarkdown,
			ArtifactName:    "stagehand-run",
			PostPRComment:   true,
			MaxFallbackTier: FallbackTierNearestNeighbor,
		},
	}
}

func Load(path string) (Config, error) {
	cfg := DefaultConfig()
	if err := loadYAML(path, &cfg); err != nil {
		return Config{}, err
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func LoadTest(path string) (TestConfig, error) {
	cfg := DefaultTestConfig()
	if err := loadYAML(path, &cfg); err != nil {
		return TestConfig{}, err
	}

	if err := cfg.Validate(); err != nil {
		return TestConfig{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	verr := &ValidationError{}

	if c.SchemaVersion != SchemaVersion {
		verr.add("schema_version must be %q", SchemaVersion)
	}

	if !slices.Contains(validModes, c.Record.DefaultMode) {
		verr.add("record.default_mode must be one of %q, %q, %q", ModeRecord, ModeReplay, ModePassthrough)
	}

	if strings.TrimSpace(c.Record.StoragePath) == "" {
		verr.add("record.storage_path is required")
	}

	if c.Record.Capture.MaxBodyBytes <= 0 {
		verr.add("record.capture.max_body_bytes must be greater than 0")
	}

	if !c.Record.Capture.RedactBeforePersist {
		verr.add("record.capture.redact_before_persist must be true in v1")
	}

	for _, header := range c.Record.Capture.IncludeHeaders {
		if strings.TrimSpace(header) == "" {
			verr.add("record.capture.include_headers cannot contain empty values")
			break
		}
	}

	if !slices.Contains(validReplayModes, c.Replay.Mode) {
		verr.add("replay.mode must be one of %q or %q", ReplayModeExact, ReplayModePromptReplay)
	}

	if !slices.Contains(validClockModes, c.Replay.ClockMode) {
		verr.add("replay.clock_mode must be %q in v1", ClockModeWall)
	}

	if strings.TrimSpace(c.Scrub.PolicyVersion) == "" {
		verr.add("scrub.policy_version is required")
	}

	if !c.Scrub.Enabled {
		verr.add("scrub.enabled must be true in v1")
	}

	if !c.Scrub.Detectors.Email {
		verr.add("scrub.detectors.email must be true in v1")
	}
	if !c.Scrub.Detectors.Phone {
		verr.add("scrub.detectors.phone must be true in v1")
	}
	if !c.Scrub.Detectors.SSN {
		verr.add("scrub.detectors.ssn must be true in v1")
	}
	if !c.Scrub.Detectors.CreditCard {
		verr.add("scrub.detectors.credit_card must be true in v1")
	}
	if !c.Scrub.Detectors.JWT {
		verr.add("scrub.detectors.jwt must be true in v1")
	}
	if !c.Scrub.Detectors.APIKey {
		verr.add("scrub.detectors.api_key must be true in v1")
	}

	for _, file := range c.Scrub.CustomRulesFiles {
		if strings.TrimSpace(file) == "" {
			verr.add("scrub.custom_rules_files cannot contain empty values")
			break
		}
	}
	if len(c.Scrub.CustomRulesFiles) > 0 {
		verr.add("scrub.custom_rules_files is reserved and must be empty in v1; use scrub.custom_rules")
	}

	for idx, rule := range c.Scrub.CustomRules {
		if strings.TrimSpace(rule.Name) == "" {
			verr.add("scrub.custom_rules[%d].name is required", idx)
		}
		if strings.TrimSpace(rule.Pattern) == "" {
			verr.add("scrub.custom_rules[%d].pattern is required", idx)
		}
		if strings.TrimSpace(rule.Action) == "" {
			verr.add("scrub.custom_rules[%d].action is required", idx)
		}
	}

	if _, err := c.Scrub.Rules(); err != nil {
		verr.add("%v", err)
	}

	validateTierSequence("fallback.allowed_tiers", c.Fallback.AllowedTiers, verr)
	if c.Fallback.LLMSynthesis.Enabled {
		if !slices.Contains(c.Fallback.AllowedTiers, FallbackTierLLMSynthesis) {
			verr.add("fallback.allowed_tiers must include %q when fallback.llm_synthesis.enabled is true", FallbackTierLLMSynthesis)
		}

		if strings.TrimSpace(c.Fallback.LLMSynthesis.ProviderEnv) == "" {
			verr.add("fallback.llm_synthesis.provider_env is required when llm synthesis is enabled")
		}

		if strings.TrimSpace(c.Fallback.LLMSynthesis.Model) == "" {
			verr.add("fallback.llm_synthesis.model is required when llm synthesis is enabled")
		}
	}

	if !slices.Contains(validAuthModes, c.Auth.DefaultMode) {
		verr.add("auth.default_mode must be one of %q, %q, %q", AuthModePermissive, AuthModeStrictRecorded, AuthModeStrictMatch)
	}

	for service, mode := range c.Auth.ServiceModes {
		if strings.TrimSpace(service) == "" {
			verr.add("auth.service_modes cannot contain an empty service name")
			continue
		}

		if !slices.Contains(validAuthModes, mode) {
			verr.add("auth.service_modes[%q] has invalid mode %q", service, mode)
		}
	}

	return verr.err()
}

func (c ScrubConfig) Rules() ([]scrub.Rule, error) {
	custom := make([]scrub.Rule, len(c.CustomRules))
	for idx, rule := range c.CustomRules {
		custom[idx] = scrub.Rule{
			Name:    rule.Name,
			Pattern: rule.Pattern,
			Action:  scrub.Action(rule.Action),
		}
	}

	return scrub.MergeRules(scrub.DefaultRules(), custom)
}

func (c TestConfig) Validate() error {
	verr := &ValidationError{}

	if c.SchemaVersion != SchemaVersion {
		verr.add("schema_version must be %q", SchemaVersion)
	}

	if strings.TrimSpace(c.Session) == "" {
		verr.add("session is required")
	}

	if strings.TrimSpace(c.Baseline.Branch) == "" {
		verr.add("baseline.branch is required")
	}

	if !slices.Contains(validReplayModes, c.Replay.Mode) {
		verr.add("replay.mode must be one of %q or %q", ReplayModeExact, ReplayModePromptReplay)
	}

	if c.Replay.Mode == ReplayModePromptReplay && c.Replay.ForbidLiveNetwork {
		verr.add("replay.forbid_live_network cannot be true when replay.mode is %q", ReplayModePromptReplay)
	}

	validateTierSet("fallback.disallowed_tiers", c.Fallback.DisallowedTiers, verr)

	for idx, rule := range c.ErrorInjection.Rules {
		validateErrorRule(idx, rule, verr)
	}

	if len(c.CI.FailOn) == 0 {
		verr.add("ci.fail_on must contain at least one failure condition")
	}

	for _, condition := range c.CI.FailOn {
		if !slices.Contains(validFailConditions, condition) {
			verr.add("ci.fail_on contains invalid value %q", condition)
		}
	}

	if !slices.Contains(validReportFormats, c.CI.ReportFormat) {
		verr.add("ci.report_format must be one of %q, %q, %q", ReportFormatTerminal, ReportFormatJSON, ReportFormatGitHubMarkdown)
	}

	if strings.TrimSpace(c.CI.ArtifactName) == "" {
		verr.add("ci.artifact_name is required")
	}

	if !slices.Contains(validFallbackTiers, c.CI.MaxFallbackTier) {
		verr.add("ci.max_fallback_tier must be a valid fallback tier")
	}

	return verr.err()
}

func loadYAML(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %q: %w", path, err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode config %q: %w", path, err)
	}

	return nil
}

func validateTierSequence(field string, tiers []FallbackTier, verr *ValidationError) {
	if len(tiers) == 0 {
		verr.add("%s must contain at least one fallback tier", field)
		return
	}

	lastIndex := -1
	seen := map[FallbackTier]bool{}

	for _, tier := range tiers {
		index := slices.Index(validFallbackTiers, tier)
		if index == -1 {
			verr.add("%s contains invalid tier %q", field, tier)
			continue
		}

		if seen[tier] {
			verr.add("%s contains duplicate tier %q", field, tier)
			continue
		}

		if index < lastIndex {
			verr.add("%s must keep tiers in canonical order", field)
			return
		}

		seen[tier] = true
		lastIndex = index
	}
}

func validateTierSet(field string, tiers []FallbackTier, verr *ValidationError) {
	seen := map[FallbackTier]bool{}

	for _, tier := range tiers {
		if !slices.Contains(validFallbackTiers, tier) {
			verr.add("%s contains invalid tier %q", field, tier)
			continue
		}

		if seen[tier] {
			verr.add("%s contains duplicate tier %q", field, tier)
			continue
		}

		seen[tier] = true
	}
}

func validateErrorRule(index int, rule ErrorInjectionRule, verr *ValidationError) {
	prefix := fmt.Sprintf("error_injection.rules[%d]", index)

	if strings.TrimSpace(rule.Match.Service) == "" {
		verr.add("%s.match.service is required", prefix)
	}

	if strings.TrimSpace(rule.Match.Operation) == "" {
		verr.add("%s.match.operation is required", prefix)
	}

	if rule.Match.NthCall < 0 {
		verr.add("%s.match.nth_call must be greater than or equal to 0", prefix)
	}

	if rule.Match.AnyCall && rule.Match.NthCall > 0 {
		verr.add("%s.match cannot set both nth_call and any_call", prefix)
	}

	if rule.Match.Probability < 0 || rule.Match.Probability > 1 {
		verr.add("%s.match.probability must be between 0 and 1", prefix)
	}

	if strings.TrimSpace(rule.Inject.Library) != "" && (rule.Inject.Status != 0 || len(rule.Inject.Body) > 0) {
		verr.add("%s.inject must use either library or status/body, not both", prefix)
	}

	if strings.TrimSpace(rule.Inject.Library) == "" {
		if rule.Inject.Status < 100 || rule.Inject.Status > 599 {
			verr.add("%s.inject.status must be between 100 and 599 when library is not used", prefix)
		}
	}
}
