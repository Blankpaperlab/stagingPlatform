package main

import (
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

	"stagehand/internal/config"
	"stagehand/internal/recorder"
	"stagehand/internal/recording"
	"stagehand/internal/scrub/session_salt"
	"stagehand/internal/store"
)

const (
	interactionBundleVersion = "v1alpha1"
	localMasterKeyFilename   = "stagehand.master.key"
	envStagehandSession      = "STAGEHAND_SESSION"
	envStagehandMode         = "STAGEHAND_MODE"
	envStagehandConfigPath   = "STAGEHAND_CONFIG_PATH"
	envStagehandCaptureOut   = "STAGEHAND_CAPTURE_OUTPUT"
	envStagehandReplayInput  = "STAGEHAND_REPLAY_INPUT"
	envStagehandMasterKey    = "STAGEHAND_MASTER_KEY"
)

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

func persistImportedInteractions(
	ctx context.Context,
	writer *recording.Writer,
	runID string,
	source []recorder.Interaction,
) (int, error) {
	normalized := normalizeImportedInteractions(runID, source)
	for _, interaction := range normalized {
		if _, err := writer.PersistInteraction(ctx, interaction); err != nil {
			return 0, err
		}
	}

	return len(normalized), nil
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
