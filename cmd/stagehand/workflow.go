package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	analysisassertions "stagehand/internal/analysis/assertions"
	"stagehand/internal/config"
	"stagehand/internal/recorder"
	"stagehand/internal/recording"
	"stagehand/internal/runtime/injection"
	"stagehand/internal/scrub/session_salt"
	"stagehand/internal/store"

	"gopkg.in/yaml.v3"
)

const (
	interactionBundleVersion = "v1alpha1"
	localMasterKeyFilename   = "stagehand.master.key"
	envStagehandSession      = "STAGEHAND_SESSION"
	envStagehandMode         = "STAGEHAND_MODE"
	envStagehandConfigPath   = "STAGEHAND_CONFIG_PATH"
	envStagehandCaptureOut   = "STAGEHAND_CAPTURE_OUTPUT"
	envStagehandReplayInput  = "STAGEHAND_REPLAY_INPUT"
	envStagehandInjectInput  = "STAGEHAND_ERROR_INJECTION_INPUT"
	envStagehandMasterKey    = "STAGEHAND_MASTER_KEY"
)

type errorInjectionFile struct {
	SchemaVersion  string                `yaml:"schema_version"`
	ErrorInjection errorInjectionSection `yaml:"error_injection"`
}

type errorInjectionSection struct {
	Enabled *bool                     `yaml:"enabled"`
	Rules   []namedErrorInjectionRule `yaml:"rules"`
}

type namedErrorInjectionRule struct {
	Name   string             `yaml:"name"`
	Match  config.ErrorMatch  `yaml:"match"`
	Inject config.ErrorInject `yaml:"inject"`
}

type sdkErrorInjectionBundle struct {
	SchemaVersion string                  `json:"schema_version"`
	Rules         []sdkErrorInjectionRule `json:"rules"`
}

type sdkErrorInjectionRule struct {
	Name   string         `json:"name,omitempty"`
	Match  sdkErrorMatch  `json:"match"`
	Inject sdkErrorInject `json:"inject"`
}

type sdkErrorMatch struct {
	Service     string   `json:"service"`
	Operation   string   `json:"operation"`
	NthCall     int      `json:"nth_call,omitempty"`
	AnyCall     bool     `json:"any_call,omitempty"`
	Probability *float64 `json:"probability,omitempty"`
}

type sdkErrorInject struct {
	Library string         `json:"library,omitempty"`
	Status  int            `json:"status"`
	Body    map[string]any `json:"body,omitempty"`
}

type interactionBundle struct {
	BundleVersion string                 `json:"bundle_version,omitempty"`
	Metadata      map[string]any         `json:"metadata,omitempty"`
	Interactions  []recorder.Interaction `json:"interactions"`
}

type recordResult struct {
	Mode             string   `json:"mode"`
	RunID            string   `json:"run_id"`
	SessionName      string   `json:"session_name"`
	Status           string   `json:"status"`
	InteractionCount int      `json:"interaction_count"`
	EmptyRun         bool     `json:"empty_run"`
	StoragePath      string   `json:"storage_path"`
	Command          []string `json:"command"`
}

type replayCommandResult struct {
	Mode                   string   `json:"mode"`
	SourceRunID            string   `json:"source_run_id"`
	ReplayRunID            string   `json:"replay_run_id"`
	SessionName            string   `json:"session_name"`
	Status                 string   `json:"status"`
	SourceInteractionCount int      `json:"source_interaction_count"`
	ReplayInteractionCount int      `json:"replay_interaction_count"`
	Services               []string `json:"services"`
	FallbackTiersUsed      []string `json:"fallback_tiers_used"`
}

type baselinePromoteResult struct {
	Mode        string `json:"mode"`
	BaselineID  string `json:"baseline_id"`
	SessionName string `json:"session_name"`
	SourceRunID string `json:"source_run_id"`
	GitSHA      string `json:"git_sha"`
	StoragePath string `json:"storage_path"`
}

type baselineShowResult struct {
	Mode        string `json:"mode"`
	BaselineID  string `json:"baseline_id"`
	SessionName string `json:"session_name"`
	SourceRunID string `json:"source_run_id"`
	GitSHA      string `json:"git_sha"`
	CreatedAt   string `json:"created_at"`
	StoragePath string `json:"storage_path"`
}

type assertionSummary struct {
	Total       int `json:"total"`
	Passed      int `json:"passed"`
	Failed      int `json:"failed"`
	Unsupported int `json:"unsupported"`
}

type assertionReport struct {
	RunID       string                      `json:"run_id"`
	SessionName string                      `json:"session_name"`
	Summary     assertionSummary            `json:"summary"`
	Results     []analysisassertions.Result `json:"results"`
}

type runFailure struct {
	status store.RunLifecycleStatus
	code   recorder.IntegrityIssueCode
	err    error
}

func (e *runFailure) Error() string {
	return e.err.Error()
}

func (e *runFailure) Unwrap() error {
	return e.err
}

func newRunRecordForMode(session string, cfg config.Config, mode recorder.RunMode) (store.RunRecord, error) {
	run, err := newRunRecord(session, cfg)
	if err != nil {
		return store.RunRecord{}, err
	}
	run.Mode = mode
	return run, nil
}

func newRecordingWriter(artifactStore store.ArtifactStore, cfg config.Config, dbPath string) (*recording.Writer, error) {
	masterKey, err := loadOrCreateMasterKey(filepath.Dir(dbPath))
	if err != nil {
		return nil, err
	}

	saltManager, err := session_salt.NewManager(artifactStore, masterKey)
	if err != nil {
		return nil, fmt.Errorf("create session salt manager: %w", err)
	}

	writer, err := recording.NewWriter(recording.WriterOptions{
		Store:       artifactStore,
		SaltManager: saltManager,
		ScrubConfig: cfg.Scrub,
	})
	if err != nil {
		return nil, fmt.Errorf("create recording writer: %w", err)
	}

	return writer, nil
}

func loadOrCreateMasterKey(storageDir string) ([]byte, error) {
	if raw := strings.TrimSpace(os.Getenv(envStagehandMasterKey)); raw != "" {
		key, err := decodeMasterKey(raw)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", envStagehandMasterKey, err)
		}
		return key, nil
	}

	keyPath := filepath.Join(storageDir, localMasterKeyFilename)
	if data, err := os.ReadFile(keyPath); err == nil {
		key, err := decodeMasterKey(string(data))
		if err != nil {
			return nil, fmt.Errorf("decode local master key %q: %w", keyPath, err)
		}
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read local master key %q: %w", keyPath, err)
	}

	key := make([]byte, session_salt.MasterKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate local master key: %w", err)
	}

	if err := os.MkdirAll(storageDir, 0o755); err != nil {
		return nil, fmt.Errorf("create storage directory %q: %w", storageDir, err)
	}
	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("write local master key %q: %w", keyPath, err)
	}

	return key, nil
}

func decodeMasterKey(value string) ([]byte, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, fmt.Errorf("master key is empty")
	}

	if decoded, err := hex.DecodeString(trimmed); err == nil {
		if len(decoded) != session_salt.MasterKeySize {
			return nil, fmt.Errorf("hex-decoded master key must be %d bytes", session_salt.MasterKeySize)
		}
		return decoded, nil
	}

	if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil {
		if len(decoded) != session_salt.MasterKeySize {
			return nil, fmt.Errorf("base64-decoded master key must be %d bytes", session_salt.MasterKeySize)
		}
		return decoded, nil
	}

	raw := []byte(trimmed)
	if len(raw) != session_salt.MasterKeySize {
		return nil, fmt.Errorf("master key must be %d bytes", session_salt.MasterKeySize)
	}
	return raw, nil
}

func runManagedCommand(
	ctx context.Context,
	commandArgs []string,
	extraEnv map[string]string,
	logOutput io.Writer,
) error {
	if len(commandArgs) == 0 {
		return fmt.Errorf("managed command is required")
	}

	command := exec.CommandContext(ctx, commandArgs[0], commandArgs[1:]...)
	command.Stdout = logOutput
	command.Stderr = logOutput
	command.Env = managedCommandEnvironment(extraEnv)

	if err := command.Run(); err != nil {
		return fmt.Errorf("run managed command %q: %w", strings.Join(commandArgs, " "), err)
	}

	return nil
}

func managedCommandEnvironment(extraEnv map[string]string) []string {
	envMap := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		envMap[key] = value
	}

	for key, value := range extraEnv {
		envMap[key] = value
	}

	repoPythonPath := filepath.Join(mustGetwd(), "sdk", "python")
	if info, err := os.Stat(repoPythonPath); err == nil && info.IsDir() {
		existing := envMap["PYTHONPATH"]
		if existing == "" {
			envMap["PYTHONPATH"] = repoPythonPath
		} else {
			envMap["PYTHONPATH"] = repoPythonPath + string(os.PathListSeparator) + existing
		}
	}

	env := make([]string, 0, len(envMap))
	for key, value := range envMap {
		env = append(env, key+"="+value)
	}
	sort.Strings(env)
	return env
}

func loadInteractionBundle(path string) (interactionBundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return interactionBundle{}, fmt.Errorf("read interaction bundle %q: %w", path, err)
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return interactionBundle{}, fmt.Errorf("interaction bundle %q is empty", path)
	}

	if strings.HasPrefix(trimmed, "[") {
		var interactions []recorder.Interaction
		if err := json.Unmarshal(data, &interactions); err != nil {
			return interactionBundle{}, fmt.Errorf("decode interaction bundle %q: %w", path, err)
		}
		return interactionBundle{
			BundleVersion: interactionBundleVersion,
			Interactions:  interactions,
		}, nil
	}

	var bundle interactionBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return interactionBundle{}, fmt.Errorf("decode interaction bundle %q: %w", path, err)
	}
	return bundle, nil
}

func writeInteractionBundle(path string, bundle interactionBundle) error {
	if bundle.BundleVersion == "" {
		bundle.BundleVersion = interactionBundleVersion
	}
	if len(bundle.Interactions) == 0 {
		return fmt.Errorf("interaction bundle must contain at least one interaction")
	}

	target := filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create interaction bundle directory for %q: %w", target, err)
	}

	encoded, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return fmt.Errorf("encode interaction bundle %q: %w", target, err)
	}
	encoded = append(encoded, '\n')

	tempFile, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp bundle for %q: %w", target, err)
	}
	tempPath := tempFile.Name()
	if _, err := tempFile.Write(encoded); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("write temp bundle for %q: %w", target, err)
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close temp bundle for %q: %w", target, err)
	}
	if err := os.Rename(tempPath, target); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("replace bundle %q: %w", target, err)
	}

	return nil
}

func loadSDKErrorInjectionBundle(path string) (sdkErrorInjectionBundle, error) {
	resolvedPath := strings.TrimSpace(path)
	if resolvedPath == "" {
		return sdkErrorInjectionBundle{}, nil
	}

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return sdkErrorInjectionBundle{}, fmt.Errorf("read error injection file %q: %w", resolvedPath, err)
	}

	var file errorInjectionFile
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		return sdkErrorInjectionBundle{}, fmt.Errorf("decode error injection file %q: %w", resolvedPath, err)
	}

	rules := make([]config.ErrorInjectionRule, 0, len(file.ErrorInjection.Rules))
	for _, rule := range file.ErrorInjection.Rules {
		rules = append(rules, config.ErrorInjectionRule{
			Match:  rule.Match,
			Inject: rule.Inject,
		})
	}

	enabled := len(rules) > 0
	if file.ErrorInjection.Enabled != nil {
		enabled = *file.ErrorInjection.Enabled
	}
	cfg := config.ErrorInjectionConfig{
		Enabled: enabled,
		Rules:   rules,
	}
	if _, err := injection.NewEngineFromConfig(cfg); err != nil {
		return sdkErrorInjectionBundle{}, fmt.Errorf("validate error injection file %q: %w", resolvedPath, err)
	}
	if !enabled || len(file.ErrorInjection.Rules) == 0 {
		return sdkErrorInjectionBundle{SchemaVersion: interactionBundleVersion}, nil
	}

	sdkRules := make([]sdkErrorInjectionRule, 0, len(file.ErrorInjection.Rules))
	for _, rule := range file.ErrorInjection.Rules {
		inject := sdkErrorInject{
			Library: strings.TrimSpace(rule.Inject.Library),
			Status:  rule.Inject.Status,
			Body:    cloneAnyMap(rule.Inject.Body),
		}
		if inject.Library != "" {
			override, ok := injection.NamedLibrary(inject.Library)
			if !ok {
				return sdkErrorInjectionBundle{}, fmt.Errorf("validate error injection file %q: unknown named error library entry %q", resolvedPath, inject.Library)
			}
			inject.Status = override.Status
			inject.Body = cloneAnyMap(override.Body)
		}

		sdkRules = append(sdkRules, sdkErrorInjectionRule{
			Name: strings.TrimSpace(rule.Name),
			Match: sdkErrorMatch{
				Service:     strings.TrimSpace(rule.Match.Service),
				Operation:   strings.TrimSpace(rule.Match.Operation),
				NthCall:     rule.Match.NthCall,
				AnyCall:     rule.Match.AnyCall,
				Probability: cloneFloat64(rule.Match.Probability),
			},
			Inject: inject,
		})
	}

	return sdkErrorInjectionBundle{
		SchemaVersion: firstNonEmpty(file.SchemaVersion, interactionBundleVersion),
		Rules:         sdkRules,
	}, nil
}

func writeSDKErrorInjectionBundle(path string, bundle sdkErrorInjectionBundle) error {
	if len(bundle.Rules) == 0 {
		return nil
	}
	encoded, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return fmt.Errorf("encode error injection bundle: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return fmt.Errorf("write error injection bundle %q: %w", path, err)
	}
	return nil
}

func normalizeImportedInteractions(runID string, source []recorder.Interaction) []recorder.Interaction {
	ordered := append([]recorder.Interaction(nil), source...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Sequence == ordered[j].Sequence {
			return ordered[i].InteractionID < ordered[j].InteractionID
		}
		return ordered[i].Sequence < ordered[j].Sequence
	})

	idMap := make(map[string]string, len(ordered))
	for idx, interaction := range ordered {
		oldID := strings.TrimSpace(interaction.InteractionID)
		if oldID == "" {
			oldID = fmt.Sprintf("interaction_%03d", idx+1)
		}
		idMap[interaction.InteractionID] = fmt.Sprintf("%s__%s", runID, oldID)
	}

	normalized := make([]recorder.Interaction, len(ordered))
	for idx, interaction := range ordered {
		copied := interaction
		copied.RunID = runID
		copied.Sequence = idx + 1
		copied.InteractionID = idMap[interaction.InteractionID]
		if copied.InteractionID == "" {
			copied.InteractionID = fmt.Sprintf("%s__interaction_%03d", runID, idx+1)
		}
		if interaction.ParentInteractionID != "" {
			if mappedParent, ok := idMap[interaction.ParentInteractionID]; ok {
				copied.ParentInteractionID = mappedParent
			}
		}
		normalized[idx] = copied
	}

	return normalized
}

func prepareImportedInteractions(runID string, source []recorder.Interaction) ([]recorder.Interaction, error) {
	normalized := normalizeImportedInteractions(runID, source)
	for idx, interaction := range normalized {
		if err := interaction.Validate(runID, ""); err != nil {
			return nil, fmt.Errorf(
				"validate imported interaction %d (%q): %w",
				idx,
				interaction.InteractionID,
				err,
			)
		}
	}

	return normalized, nil
}

func persistImportedInteractions(
	ctx context.Context,
	writer *recording.Writer,
	runID string,
	source []recorder.Interaction,
) (int, error) {
	normalized, err := prepareImportedInteractions(runID, source)
	if err != nil {
		return 0, err
	}

	for _, interaction := range normalized {
		if _, err := writer.PersistInteraction(ctx, interaction); err != nil {
			return 0, err
		}
	}

	return len(normalized), nil
}

func mergeRunMetadata(base map[string]any, overlay map[string]any) map[string]any {
	if len(overlay) == 0 {
		return base
	}
	merged := map[string]any{}
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range overlay {
		merged[key] = value
	}
	return merged
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

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func incompleteRunFailure(operation string, err error) error {
	return classifiedRunFailure(store.RunLifecycleStatusIncomplete, recorder.IntegrityIssueRecorderShutdown, operation, err)
}

func corruptedRunFailure(code recorder.IntegrityIssueCode, operation string, err error) error {
	return classifiedRunFailure(store.RunLifecycleStatusCorrupted, code, operation, err)
}

func interruptedWriteFailure(operation string, err error) error {
	return corruptedRunFailure(recorder.IntegrityIssueInterruptedWrite, operation, err)
}

func schemaValidationFailure(operation string, err error) error {
	return corruptedRunFailure(recorder.IntegrityIssueSchemaValidation, operation, err)
}

func missingEndStateFailure(operation string, err error) error {
	return corruptedRunFailure(recorder.IntegrityIssueMissingEndState, operation, err)
}

func classifiedRunFailure(
	status store.RunLifecycleStatus,
	code recorder.IntegrityIssueCode,
	operation string,
	err error,
) error {
	if err == nil {
		return nil
	}

	return &runFailure{
		status: status,
		code:   code,
		err:    fmt.Errorf("%s: %w", operation, err),
	}
}

func emitJSON(w io.Writer, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "%s\n", encoded)
	return err
}

func mustGetwd() string {
	workingDir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return workingDir
}
