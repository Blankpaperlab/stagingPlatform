package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"stagehand/internal/config"
	"stagehand/internal/recorder"
	runtimereplay "stagehand/internal/runtime/replay"
	"stagehand/internal/store"
	sqlitestore "stagehand/internal/store/sqlite"
	"stagehand/internal/version"
)

const defaultRuntimeConfigPath = "stagehand.yml"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		writeRootHelp(stdout)
		return nil
	}

	switch args[0] {
	case "-h", "--help", "help":
		writeRootHelp(stdout)
		return nil
	case "record":
		return runRecord(args[1:], stdout, stderr)
	case "replay":
		return runReplay(args[1:], stdout, stderr)
	case "inspect":
		return runInspect(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], rootHelpText())
	}
}

func runRecord(args []string, stdout io.Writer, stderr io.Writer) (runErr error) {
	flags := flag.NewFlagSet("record", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	session := flags.String("session", "", "Session name to record into")
	configPath := flags.String("config", "", "Path to stagehand.yml")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse record flags: %w\n\n%s", err, recordHelpText())
	}
	if strings.TrimSpace(*session) == "" {
		return fmt.Errorf("record requires --session\n\n%s", recordHelpText())
	}
	commandArgs := flags.Args()
	if len(commandArgs) == 0 {
		return fmt.Errorf("record requires a command after --\n\n%s", recordHelpText())
	}

	cfgPath := strings.TrimSpace(*configPath)
	if cfgPath == "" {
		cfgPath = defaultRuntimeConfigPath
	}

	ctx := context.Background()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load runtime config %q: %w", cfgPath, err)
	}

	dbPath := sqliteDatabasePath(cfg.Record.StoragePath)
	sqliteStore, err := sqlitestore.OpenStore(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open local store %q: %w", dbPath, err)
	}
	defer sqliteStore.Close()

	runRecord, err := newRunRecordForMode(strings.TrimSpace(*session), cfg, recorder.RunModeRecord)
	if err != nil {
		return err
	}

	if err := sqliteStore.CreateRun(ctx, runRecord); err != nil {
		return fmt.Errorf("create run %q: %w", runRecord.RunID, err)
	}

	writer, err := newRecordingWriter(sqliteStore, cfg, dbPath)
	if err != nil {
		return err
	}

	tempDir, err := os.MkdirTemp("", "stagehand-record-*")
	if err != nil {
		return fmt.Errorf("create record temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	capturePath := filepath.Join(tempDir, "capture.json")
	finalized := false
	defer func() {
		if finalized {
			return
		}
		finalizeErr := finalizeRun(ctx, sqliteStore, runRecord, runErr)
		if finalizeErr != nil {
			if runErr == nil {
				runErr = finalizeErr
				return
			}
			runErr = fmt.Errorf("%w; finalize run failed: %v", runErr, finalizeErr)
		}
	}()

	runErr = runManagedCommand(
		ctx,
		commandArgs,
		map[string]string{
			envStagehandSession:    runRecord.SessionName,
			envStagehandMode:       string(recorder.RunModeRecord),
			envStagehandConfigPath: cfgPath,
			envStagehandCaptureOut: capturePath,
		},
		stderr,
	)

	interactionCount := 0
	if bundle, err := loadInteractionBundle(capturePath); err != nil {
		if runErr == nil {
			runErr = fmt.Errorf("load record capture bundle: %w", err)
		}
	} else {
		interactionCount, err = persistImportedInteractions(ctx, writer, runRecord.RunID, bundle.Interactions)
		if err != nil && runErr == nil {
			runErr = fmt.Errorf("persist recorded interactions: %w", err)
		}
	}
	if err := finalizeRun(ctx, sqliteStore, runRecord, runErr); err != nil {
		return err
	}
	finalized = true

	if runErr != nil {
		return runErr
	}

	return emitJSON(stdout, recordResult{
		Mode:             "record",
		RunID:            runRecord.RunID,
		SessionName:      runRecord.SessionName,
		Status:           string(store.RunLifecycleStatusComplete),
		InteractionCount: interactionCount,
		EmptyRun:         interactionCount == 0,
		StoragePath:      dbPath,
		Command:          commandArgs,
	})
}

func runReplay(args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("replay", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	runID := flags.String("run-id", "", "Run identifier to replay")
	session := flags.String("session", "", "Session name to replay latest run from")
	configPath := flags.String("config", "", "Path to stagehand.yml")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse replay flags: %w\n\n%s", err, replayHelpText())
	}

	resolvedRunID := strings.TrimSpace(*runID)
	resolvedSession := strings.TrimSpace(*session)
	if (resolvedRunID == "" && resolvedSession == "") || (resolvedRunID != "" && resolvedSession != "") {
		return fmt.Errorf("replay requires exactly one of --run-id or --session\n\n%s", replayHelpText())
	}
	commandArgs := flags.Args()
	if len(commandArgs) == 0 {
		return fmt.Errorf("replay requires a command after --\n\n%s", replayHelpText())
	}

	cfgPath := strings.TrimSpace(*configPath)
	if cfgPath == "" {
		cfgPath = defaultRuntimeConfigPath
	}

	ctx := context.Background()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load runtime config %q: %w", cfgPath, err)
	}

	dbPath := sqliteDatabasePath(cfg.Record.StoragePath)
	sqliteStore, err := sqlitestore.OpenStore(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open local store %q: %w", dbPath, err)
	}
	defer sqliteStore.Close()

	sourceRun, err := loadReplayRun(ctx, sqliteStore, resolvedRunID, resolvedSession)
	if err != nil {
		return err
	}

	sourceSummary, err := runtimereplay.Exact(sourceRun)
	if err != nil {
		return fmt.Errorf("exact replay failed for run %q: %w", sourceRun.RunID, err)
	}

	replayRun, err := newRunRecordForMode(sourceRun.SessionName, cfg, recorder.RunModeReplay)
	if err != nil {
		return err
	}
	if err := sqliteStore.CreateRun(ctx, replayRun); err != nil {
		return fmt.Errorf("create replay run %q: %w", replayRun.RunID, err)
	}

	writer, err := newRecordingWriter(sqliteStore, cfg, dbPath)
	if err != nil {
		return err
	}

	tempDir, err := os.MkdirTemp("", "stagehand-replay-*")
	if err != nil {
		return fmt.Errorf("create replay temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	seedPath := filepath.Join(tempDir, "seed.json")
	capturePath := filepath.Join(tempDir, "capture.json")
	if err := writeInteractionBundle(seedPath, interactionBundle{
		BundleVersion: interactionBundleVersion,
		Metadata: map[string]any{
			"source_run_id": sourceRun.RunID,
			"session":       sourceRun.SessionName,
			"mode":          "replay",
		},
		Interactions: sourceRun.Interactions,
	}); err != nil {
		return fmt.Errorf("write replay seed bundle: %w", err)
	}

	commandErr := runManagedCommand(
		ctx,
		commandArgs,
		map[string]string{
			envStagehandSession:     sourceRun.SessionName,
			envStagehandMode:        string(recorder.RunModeReplay),
			envStagehandConfigPath:  cfgPath,
			envStagehandCaptureOut:  capturePath,
			envStagehandReplayInput: seedPath,
		},
		stderr,
	)

	interactionCount := 0
	if bundle, err := loadInteractionBundle(capturePath); err != nil {
		if commandErr == nil {
			commandErr = fmt.Errorf("load replay capture bundle: %w", err)
		}
	} else {
		interactionCount, err = persistImportedInteractions(ctx, writer, replayRun.RunID, bundle.Interactions)
		if err != nil && commandErr == nil {
			commandErr = fmt.Errorf("persist replayed interactions: %w", err)
		}
	}
	if commandErr == nil && interactionCount == 0 {
		commandErr = fmt.Errorf("replay command did not produce any interactions")
	}
	if err := finalizeRun(ctx, sqliteStore, replayRun, commandErr); err != nil {
		return err
	}
	if commandErr != nil {
		return commandErr
	}

	return emitJSON(stdout, replayCommandResult{
		Mode:                   "replay",
		SourceRunID:            sourceRun.RunID,
		ReplayRunID:            replayRun.RunID,
		SessionName:            sourceRun.SessionName,
		Status:                 string(store.RunLifecycleStatusComplete),
		SourceInteractionCount: len(sourceRun.Interactions),
		ReplayInteractionCount: interactionCount,
		Services:               sourceSummary.Services,
		FallbackTiersUsed:      sourceSummary.FallbackTiersUsed,
	})
}

func finalizeRun(ctx context.Context, artifactStore store.ArtifactStore, runRecord store.RunRecord, runErr error) error {
	final := runRecord
	endedAt := time.Now().UTC()
	final.EndedAt = &endedAt

	if runErr == nil {
		final.Status = store.RunLifecycleStatusComplete
		final.IntegrityIssues = nil
	} else {
		final.Status = store.RunLifecycleStatusCorrupted
		final.IntegrityIssues = []recorder.IntegrityIssue{
			{
				Code:    recorder.IntegrityIssueRecorderShutdown,
				Message: runErr.Error(),
			},
		}
	}

	if err := artifactStore.UpdateRun(ctx, final); err != nil {
		return fmt.Errorf("finalize run %q: %w", runRecord.RunID, err)
	}

	return nil
}

func newRunRecord(session string, cfg config.Config) (store.RunRecord, error) {
	runID, err := generateRunID()
	if err != nil {
		return store.RunRecord{}, fmt.Errorf("generate run id: %w", err)
	}

	return store.RunRecord{
		SchemaVersion:      recorder.ArtifactSchemaVersion,
		SDKVersion:         version.ArtifactVersion,
		RuntimeVersion:     version.CLIVersion,
		ScrubPolicyVersion: cfg.Scrub.PolicyVersion,
		RunID:              runID,
		SessionName:        session,
		Mode:               recorder.RunModeRecord,
		Status:             store.RunLifecycleStatusRunning,
		StartedAt:          time.Now().UTC(),
	}, nil
}

func generateRunID() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}

	return "run_" + hex.EncodeToString(raw[:]), nil
}

func sqliteDatabasePath(storagePath string) string {
	cleaned := filepath.Clean(storagePath)
	if strings.EqualFold(filepath.Ext(cleaned), ".db") || strings.EqualFold(filepath.Ext(cleaned), ".sqlite") || strings.EqualFold(filepath.Ext(cleaned), ".sqlite3") {
		return cleaned
	}

	return filepath.Join(cleaned, "stagehand.db")
}

func writeRootHelp(w io.Writer) {
	_, _ = io.WriteString(w, rootHelpText())
}

func rootHelpText() string {
	return fmt.Sprintf(`stagehand %s

Usage:
  stagehand <command> [flags]

Commands:
  record   Run a command and persist captured interactions
  replay   Replay a stored run against a command
  inspect  Inspect a stored run in the terminal

Global help:
  stagehand help
  stagehand --help
`, version.CLIVersion)
}

func recordHelpText() string {
	return `Usage:
  stagehand record --session <name> [--config path] -- <command> [args...]

Flags:
  --session string   Session name to record into
  --config string    Path to stagehand.yml (default: stagehand.yml)
`
}

func replayHelpText() string {
	return `Usage:
  stagehand replay (--run-id <id> | --session <name>) [--config path] -- <command> [args...]

Flags:
  --run-id string    Run identifier to replay
  --session string   Session name to replay the latest replayable stored run from
  --config string    Path to stagehand.yml (default: stagehand.yml)
`
}

func inspectHelpText() string {
	return `Usage:
  stagehand inspect (--run-id <id> | --session <name>) [--config path] [--show-bodies]

Flags:
  --run-id string      Run identifier to inspect
  --session string     Session name to inspect the latest stored run from
  --config string      Path to stagehand.yml (default: stagehand.yml)
  --show-bodies        Expand request bodies and event payloads
`
}

func loadReplayRun(ctx context.Context, artifactStore store.ArtifactStore, runID, session string) (recorder.Run, error) {
	if strings.TrimSpace(runID) != "" {
		run, err := artifactStore.GetRun(ctx, runID)
		if err != nil {
			return recorder.Run{}, fmt.Errorf("load run %q: %w", runID, err)
		}
		return run, nil
	}

	run, err := artifactStore.GetLatestRun(ctx, session)
	if err != nil {
		return recorder.Run{}, fmt.Errorf("load latest run for session %q: %w", session, err)
	}
	return run, nil
}
