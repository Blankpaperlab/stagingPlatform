package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	analysisassertions "stagehand/internal/analysis/assertions"
	analysisconformance "stagehand/internal/analysis/conformance"
	analysisdiff "stagehand/internal/analysis/diff"
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
	case "diff":
		return runDiff(args[1:], stdout, stderr)
	case "assert":
		return runAssert(args[1:], stdout, stderr)
	case "conformance":
		return runConformance(args[1:], stdout, stderr)
	case "baseline":
		return runBaseline(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], rootHelpText())
	}
}

func runRecord(args []string, stdout io.Writer, stderr io.Writer) (runErr error) {
	flags := flag.NewFlagSet("record", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	session := flags.String("session", "", "Session name to record into")
	configPath := flags.String("config", "", "Path to stagehand.yml")
	errorInjectionPath := flags.String("error-injection", "", "Path to error injection rules")
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

	errorInjectionBundle, err := loadSDKErrorInjectionBundle(*errorInjectionPath)
	if err != nil {
		return err
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
	resolvedErrorInjectionPath := ""
	if len(errorInjectionBundle.Rules) > 0 {
		resolvedErrorInjectionPath = filepath.Join(tempDir, "error-injection.json")
		if err := writeSDKErrorInjectionBundle(resolvedErrorInjectionPath, errorInjectionBundle); err != nil {
			return err
		}
	}
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

	extraEnv := map[string]string{
		envStagehandSession:    runRecord.SessionName,
		envStagehandMode:       string(recorder.RunModeRecord),
		envStagehandConfigPath: cfgPath,
		envStagehandCaptureOut: capturePath,
	}
	if resolvedErrorInjectionPath != "" {
		extraEnv[envStagehandInjectInput] = resolvedErrorInjectionPath
	}

	if err := runManagedCommand(
		ctx,
		commandArgs,
		extraEnv,
		stderr,
	); err != nil {
		runErr = incompleteRunFailure("managed record command failed", err)
	}

	interactionCount := 0
	if bundle, err := loadInteractionBundle(capturePath); err != nil {
		if runErr == nil {
			runErr = missingEndStateFailure("load record capture bundle", err)
		}
	} else {
		runRecord.Metadata = mergeRunMetadata(runRecord.Metadata, bundle.Metadata)
		interactionCount, err = persistImportedInteractions(ctx, writer, runRecord.RunID, bundle.Interactions)
		if err != nil && runErr == nil {
			runErr = importFailure("persist recorded interactions", err)
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
	errorInjectionPath := flags.String("error-injection", "", "Path to error injection rules")
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

	errorInjectionBundle, err := loadSDKErrorInjectionBundle(*errorInjectionPath)
	if err != nil {
		return err
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

	sourceSummary, err := runtimereplay.SummarizeExactSource(sourceRun)
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
	resolvedErrorInjectionPath := ""
	if len(errorInjectionBundle.Rules) > 0 {
		resolvedErrorInjectionPath = filepath.Join(tempDir, "error-injection.json")
		if err := writeSDKErrorInjectionBundle(resolvedErrorInjectionPath, errorInjectionBundle); err != nil {
			return err
		}
	}
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

	commandErr := error(nil)
	extraEnv := map[string]string{
		envStagehandSession:     sourceRun.SessionName,
		envStagehandMode:        string(recorder.RunModeReplay),
		envStagehandConfigPath:  cfgPath,
		envStagehandCaptureOut:  capturePath,
		envStagehandReplayInput: seedPath,
	}
	if resolvedErrorInjectionPath != "" {
		extraEnv[envStagehandInjectInput] = resolvedErrorInjectionPath
	}

	if err := runManagedCommand(
		ctx,
		commandArgs,
		extraEnv,
		stderr,
	); err != nil {
		commandErr = incompleteRunFailure("managed replay command failed", err)
	}

	interactionCount := 0
	if bundle, err := loadInteractionBundle(capturePath); err != nil {
		if commandErr == nil {
			commandErr = missingEndStateFailure("load replay capture bundle", err)
		}
	} else {
		replayRun.Metadata = mergeRunMetadata(replayRun.Metadata, bundle.Metadata)
		interactionCount, err = persistImportedInteractions(ctx, writer, replayRun.RunID, bundle.Interactions)
		if err != nil && commandErr == nil {
			commandErr = importFailure("persist replayed interactions", err)
		}
	}
	if commandErr == nil && interactionCount == 0 {
		commandErr = missingEndStateFailure("validate replay capture bundle", fmt.Errorf("replay command did not produce any interactions"))
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
		status := store.RunLifecycleStatusCorrupted
		code := recorder.IntegrityIssueRecorderShutdown
		var classified *runFailure
		if errors.As(runErr, &classified) {
			status = classified.status
			code = classified.code
		}

		final.Status = status
		final.IntegrityIssues = []recorder.IntegrityIssue{
			{
				Code:    code,
				Message: runErr.Error(),
			},
		}
	}

	if err := artifactStore.UpdateRun(ctx, final); err != nil {
		return fmt.Errorf("finalize run %q: %w", runRecord.RunID, err)
	}

	return nil
}

func runBaseline(args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("baseline requires a subcommand\n\n%s", baselineHelpText())
	}

	switch args[0] {
	case "-h", "--help", "help":
		_, _ = io.WriteString(stdout, baselineHelpText())
		return nil
	case "promote":
		return runBaselinePromote(args[1:], stdout, stderr)
	case "show":
		return runBaselineShow(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown baseline subcommand %q\n\n%s", args[0], baselineHelpText())
	}
}

func runBaselinePromote(args []string, stdout io.Writer, _ io.Writer) error {
	flags := flag.NewFlagSet("baseline promote", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	runID := flags.String("run-id", "", "Complete run identifier to promote")
	baselineID := flags.String("baseline-id", "", "Optional baseline identifier")
	gitSHA := flags.String("git-sha", "", "Git SHA to attach to the baseline")
	configPath := flags.String("config", "", "Path to stagehand.yml")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse baseline promote flags: %w\n\n%s", err, baselinePromoteHelpText())
	}

	resolvedRunID := strings.TrimSpace(*runID)
	if resolvedRunID == "" {
		return fmt.Errorf("baseline promote requires --run-id\n\n%s", baselinePromoteHelpText())
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

	runRecord, err := sqliteStore.GetRunRecord(ctx, resolvedRunID)
	if err != nil {
		return fmt.Errorf("load baseline source run %q: %w", resolvedRunID, err)
	}

	resolvedGitSHA, err := resolveBaselineGitSHA(*gitSHA, runRecord)
	if err != nil {
		return err
	}

	resolvedBaselineID := strings.TrimSpace(*baselineID)
	if resolvedBaselineID == "" {
		resolvedBaselineID, err = generateBaselineID()
		if err != nil {
			return fmt.Errorf("generate baseline id: %w", err)
		}
	}

	baseline := store.Baseline{
		BaselineID:  resolvedBaselineID,
		SessionName: runRecord.SessionName,
		SourceRunID: runRecord.RunID,
		GitSHA:      resolvedGitSHA,
		CreatedAt:   time.Now().UTC(),
	}
	if err := sqliteStore.PutBaseline(ctx, baseline); err != nil {
		return fmt.Errorf("promote run %q to baseline: %w", runRecord.RunID, err)
	}

	return emitJSON(stdout, baselinePromoteResult{
		Mode:        "baseline_promote",
		BaselineID:  baseline.BaselineID,
		SessionName: baseline.SessionName,
		SourceRunID: baseline.SourceRunID,
		GitSHA:      baseline.GitSHA,
		StoragePath: dbPath,
	})
}

func runBaselineShow(args []string, stdout io.Writer, _ io.Writer) error {
	flags := flag.NewFlagSet("baseline show", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	baselineID := flags.String("baseline-id", "", "Baseline identifier to show")
	session := flags.String("session", "", "Session name to resolve the latest baseline for")
	configPath := flags.String("config", "", "Path to stagehand.yml")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse baseline show flags: %w\n\n%s", err, baselineShowHelpText())
	}

	resolvedBaselineID := strings.TrimSpace(*baselineID)
	resolvedSession := strings.TrimSpace(*session)
	if (resolvedBaselineID == "" && resolvedSession == "") || (resolvedBaselineID != "" && resolvedSession != "") {
		return fmt.Errorf("baseline show requires exactly one of --baseline-id or --session\n\n%s", baselineShowHelpText())
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

	baseline, err := loadBaseline(ctx, sqliteStore, resolvedBaselineID, resolvedSession)
	if err != nil {
		return err
	}

	return emitJSON(stdout, baselineShowResult{
		Mode:        "baseline_show",
		BaselineID:  baseline.BaselineID,
		SessionName: baseline.SessionName,
		SourceRunID: baseline.SourceRunID,
		GitSHA:      baseline.GitSHA,
		CreatedAt:   baseline.CreatedAt.Format(time.RFC3339Nano),
		StoragePath: dbPath,
	})
}

type repeatedStringFlag []string

func (f *repeatedStringFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *repeatedStringFlag) Set(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed != "" {
		*f = append(*f, trimmed)
	}
	return nil
}

func runDiff(args []string, stdout io.Writer, _ io.Writer) error {
	flags := flag.NewFlagSet("diff", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	baseRunID := flags.String("base-run-id", "", "Base run identifier")
	baselineID := flags.String("baseline-id", "", "Baseline identifier to use as base")
	session := flags.String("session", "", "Session name to resolve latest baseline as base")
	candidateRunID := flags.String("candidate-run-id", "", "Candidate run identifier")
	format := flags.String("format", string(config.ReportFormatTerminal), "Output format: terminal, json, or github-markdown")
	configPath := flags.String("config", "", "Path to stagehand.yml")
	var ignoredFields repeatedStringFlag
	flags.Var(&ignoredFields, "ignore-field", "Additional interaction field path to ignore; may be repeated")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse diff flags: %w\n\n%s", err, diffHelpText())
	}

	resolvedCandidateRunID := strings.TrimSpace(*candidateRunID)
	if resolvedCandidateRunID == "" {
		return fmt.Errorf("diff requires --candidate-run-id\n\n%s", diffHelpText())
	}

	selectors := 0
	for _, selector := range []string{*baseRunID, *baselineID, *session} {
		if strings.TrimSpace(selector) != "" {
			selectors++
		}
	}
	if selectors != 1 {
		return fmt.Errorf("diff requires exactly one of --base-run-id, --baseline-id, or --session\n\n%s", diffHelpText())
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

	resolvedBaseRunID := strings.TrimSpace(*baseRunID)
	if resolvedBaseRunID == "" {
		baseline, err := loadBaseline(ctx, sqliteStore, strings.TrimSpace(*baselineID), strings.TrimSpace(*session))
		if err != nil {
			return err
		}
		resolvedBaseRunID = baseline.SourceRunID
	}

	baseRun, err := sqliteStore.GetRun(ctx, resolvedBaseRunID)
	if err != nil {
		return fmt.Errorf("load base run %q: %w", resolvedBaseRunID, err)
	}
	candidateRun, err := sqliteStore.GetRun(ctx, resolvedCandidateRunID)
	if err != nil {
		return fmt.Errorf("load candidate run %q: %w", resolvedCandidateRunID, err)
	}

	result, err := analysisdiff.Compare(baseRun, candidateRun, analysisdiff.Options{IgnoredFields: ignoredFields})
	if err != nil {
		return fmt.Errorf("compare runs: %w", err)
	}

	switch config.ReportFormat(strings.TrimSpace(*format)) {
	case config.ReportFormatTerminal, "":
		_, err = io.WriteString(stdout, analysisdiff.RenderTerminal(result))
		return err
	case config.ReportFormatJSON:
		report, err := analysisdiff.RenderJSON(result)
		if err != nil {
			return err
		}
		_, err = stdout.Write(report)
		return err
	case config.ReportFormatGitHubMarkdown:
		_, err = io.WriteString(stdout, analysisdiff.RenderGitHubMarkdown(result))
		return err
	default:
		return fmt.Errorf("diff --format must be one of %q, %q, or %q\n\n%s", config.ReportFormatTerminal, config.ReportFormatJSON, config.ReportFormatGitHubMarkdown, diffHelpText())
	}
}

func runAssert(args []string, stdout io.Writer, _ io.Writer) error {
	flags := flag.NewFlagSet("assert", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	runID := flags.String("run-id", "", "Run identifier to evaluate")
	assertionsPath := flags.String("assertions", "", "Path to stagehand assertions YAML")
	format := flags.String("format", string(config.ReportFormatTerminal), "Output format: terminal or json")
	configPath := flags.String("config", "", "Path to stagehand.yml")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse assert flags: %w\n\n%s", err, assertHelpText())
	}

	resolvedRunID := strings.TrimSpace(*runID)
	if resolvedRunID == "" {
		return fmt.Errorf("assert requires --run-id\n\n%s", assertHelpText())
	}
	resolvedAssertionsPath := strings.TrimSpace(*assertionsPath)
	if resolvedAssertionsPath == "" {
		return fmt.Errorf("assert requires --assertions\n\n%s", assertHelpText())
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

	run, err := sqliteStore.GetRun(ctx, resolvedRunID)
	if err != nil {
		return fmt.Errorf("load run %q: %w", resolvedRunID, err)
	}
	assertionFile, err := analysisassertions.Load(resolvedAssertionsPath)
	if err != nil {
		return err
	}
	results, err := analysisassertions.Evaluate(run, assertionFile)
	if err != nil {
		return err
	}
	report := newAssertionReport(run, results)

	switch config.ReportFormat(strings.TrimSpace(*format)) {
	case config.ReportFormatTerminal, "":
		_, err = io.WriteString(stdout, renderAssertionTerminal(report))
		return err
	case config.ReportFormatJSON:
		return emitJSON(stdout, report)
	default:
		return fmt.Errorf("assert --format must be one of %q or %q\n\n%s", config.ReportFormatTerminal, config.ReportFormatJSON, assertHelpText())
	}
}

func newAssertionReport(run recorder.Run, results []analysisassertions.Result) assertionReport {
	report := assertionReport{
		RunID:       run.RunID,
		SessionName: run.SessionName,
		Results:     append([]analysisassertions.Result(nil), results...),
	}
	report.Summary.Total = len(results)
	for _, result := range results {
		switch result.Status {
		case analysisassertions.ResultStatusPassed:
			report.Summary.Passed++
		case analysisassertions.ResultStatusFailed:
			report.Summary.Failed++
		case analysisassertions.ResultStatusUnsupported:
			report.Summary.Unsupported++
		}
	}
	return report
}

func renderAssertionTerminal(report assertionReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Stagehand assertions: %s\n", report.RunID)
	fmt.Fprintf(&b, "Session: %s\n", report.SessionName)
	fmt.Fprintf(&b, "Results: %d passed, %d failed, %d unsupported, %d total\n", report.Summary.Passed, report.Summary.Failed, report.Summary.Unsupported, report.Summary.Total)
	for _, result := range report.Results {
		fmt.Fprintf(&b, "- %s %s %s: %s\n", result.Status, result.Type, result.AssertionID, result.Message)
	}
	return b.String()
}

func runConformance(args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("conformance requires a subcommand\n\n%s", conformanceHelpText())
	}
	switch args[0] {
	case "-h", "--help", "help":
		_, _ = io.WriteString(stdout, conformanceHelpText())
		return nil
	case "run":
		return runConformanceRun(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown conformance subcommand %q\n\n%s", args[0], conformanceHelpText())
	}
}

func runConformanceRun(args []string, stdout io.Writer, _ io.Writer) error {
	flags := flag.NewFlagSet("conformance run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	casesPath := flags.String("cases", "", "Path to conformance case YAML")
	configPath := flags.String("config", "", "Path to stagehand.yml")
	outputDir := flags.String("output-dir", "", "Directory for conformance-results.json and conformance-summary.md")
	format := flags.String("format", string(config.ReportFormatJSON), "Output format: json or github-markdown")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse conformance run flags: %w\n\n%s", err, conformanceRunHelpText())
	}

	resolvedCasesPath := strings.TrimSpace(*casesPath)
	if resolvedCasesPath == "" {
		return fmt.Errorf("conformance run requires --cases\n\n%s", conformanceRunHelpText())
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

	caseFile, err := analysisconformance.Load(resolvedCasesPath)
	if err != nil {
		return err
	}

	dbPath := sqliteDatabasePath(cfg.Record.StoragePath)
	sqliteStore, err := sqlitestore.OpenStore(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open local store %q: %w", dbPath, err)
	}
	defer sqliteStore.Close()

	runner := analysisconformance.NewRunner(analysisconformance.WithSessionStore(sqliteStore))
	results := make([]analysisconformance.Result, 0, len(caseFile.Cases))
	for _, testCase := range caseFile.Cases {
		results = append(results, runner.RunCase(ctx, testCase))
	}
	report := analysisconformance.NewReport(time.Now().UTC(), results)

	if dir := strings.TrimSpace(*outputDir); dir != "" {
		if err := writeConformanceOutputs(dir, report); err != nil {
			return err
		}
	}

	switch config.ReportFormat(strings.TrimSpace(*format)) {
	case config.ReportFormatJSON, "":
		encoded, err := analysisconformance.RenderJSON(report)
		if err != nil {
			return err
		}
		if _, err := stdout.Write(encoded); err != nil {
			return err
		}
	case config.ReportFormatGitHubMarkdown, config.ReportFormatTerminal:
		if _, err := io.WriteString(stdout, analysisconformance.RenderMarkdown(report)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("conformance run --format must be one of %q or %q\n\n%s", config.ReportFormatJSON, config.ReportFormatGitHubMarkdown, conformanceRunHelpText())
	}

	if report.HasDriftOrErrors() {
		return fmt.Errorf("conformance drift detected: %d failed, %d errors", report.Summary.Failed, report.Summary.Errors)
	}
	return nil
}

func writeConformanceOutputs(outputDir string, report analysisconformance.Report) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create conformance output directory %q: %w", outputDir, err)
	}
	encoded, err := analysisconformance.RenderJSON(report)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "conformance-results.json"), encoded, 0o644); err != nil {
		return fmt.Errorf("write conformance JSON: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "conformance-summary.md"), []byte(analysisconformance.RenderMarkdown(report)), 0o644); err != nil {
		return fmt.Errorf("write conformance markdown: %w", err)
	}
	return nil
}

func resolveBaselineGitSHA(explicit string, runRecord store.RunRecord) (string, error) {
	if trimmed := strings.TrimSpace(explicit); trimmed != "" {
		return trimmed, nil
	}
	if trimmed := strings.TrimSpace(runRecord.GitSHA); trimmed != "" {
		return trimmed, nil
	}

	sha, err := currentGitSHA()
	if err != nil {
		return "", fmt.Errorf("baseline promote requires --git-sha because source run %q has no git_sha and current git SHA could not be resolved: %w", runRecord.RunID, err)
	}
	return sha, nil
}

func currentGitSHA() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "git", "rev-parse", "HEAD").CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("git rev-parse HEAD timed out")
	}
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	sha := strings.TrimSpace(string(out))
	if sha == "" {
		return "", fmt.Errorf("git rev-parse HEAD returned empty output")
	}
	return sha, nil
}

func importFailure(operation string, err error) error {
	if strings.Contains(err.Error(), "validate imported interaction") {
		return schemaValidationFailure(operation, err)
	}
	return interruptedWriteFailure(operation, err)
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

func generateBaselineID() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}

	return "base_" + hex.EncodeToString(raw[:]), nil
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
  diff     Compare a candidate run against a base run or baseline
  assert   Evaluate assertions against a stored run
  conformance Run simulator conformance cases
  baseline Promote and inspect baseline selections

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
  --error-injection string
                    Path to deterministic error injection rules
`
}

func replayHelpText() string {
	return `Usage:
  stagehand replay (--run-id <id> | --session <name>) [--config path] -- <command> [args...]

Flags:
  --run-id string    Run identifier to replay
  --session string   Session name to replay the latest replayable stored run from
  --config string    Path to stagehand.yml (default: stagehand.yml)
  --error-injection string
                    Path to deterministic error injection rules
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

func baselineHelpText() string {
	return `Usage:
  stagehand baseline <subcommand> [flags]

Subcommands:
  promote  Promote a complete run to a session baseline
  show     Show a baseline by id or latest session selection
`
}

func baselinePromoteHelpText() string {
	return `Usage:
  stagehand baseline promote --run-id <id> [--baseline-id id] [--git-sha sha] [--config path]

Flags:
  --run-id string       Complete run identifier to promote
  --baseline-id string  Optional baseline identifier (default: generated)
  --git-sha string      Git SHA to attach when the source run does not have one
  --config string       Path to stagehand.yml (default: stagehand.yml)
`
}

func baselineShowHelpText() string {
	return `Usage:
  stagehand baseline show (--baseline-id id | --session name) [--config path]

Flags:
  --baseline-id string  Baseline identifier to show
  --session string      Session name to resolve the latest baseline for
  --config string       Path to stagehand.yml (default: stagehand.yml)
`
}

func diffHelpText() string {
	return `Usage:
  stagehand diff --candidate-run-id <id> (--base-run-id <id> | --baseline-id <id> | --session <name>) [flags]

Flags:
  --candidate-run-id string  Candidate run identifier
  --base-run-id string       Base run identifier
  --baseline-id string       Baseline identifier to use as base
  --session string           Session name to resolve the latest baseline as base
  --format string            Output format: terminal, json, or github-markdown (default: terminal)
  --ignore-field string      Additional interaction field path to ignore; may be repeated
  --config string            Path to stagehand.yml (default: stagehand.yml)
`
}

func assertHelpText() string {
	return `Usage:
  stagehand assert --run-id <id> --assertions <path> [--format terminal|json] [--config path]

Flags:
  --run-id string      Run identifier to evaluate
  --assertions string  Path to stagehand assertions YAML
  --format string      Output format: terminal or json (default: terminal)
  --config string      Path to stagehand.yml (default: stagehand.yml)
`
}

func conformanceHelpText() string {
	return `Usage:
  stagehand conformance <subcommand> [flags]

Subcommands:
  run  Run real-vs-simulator conformance cases
`
}

func conformanceRunHelpText() string {
	return `Usage:
  stagehand conformance run --cases <path> [--config path] [--output-dir dir] [--format json|github-markdown]

Flags:
  --cases string       Path to conformance case YAML
  --config string      Path to stagehand.yml (default: stagehand.yml)
  --output-dir string  Directory for conformance-results.json and conformance-summary.md
  --format string      Output format: json or github-markdown (default: json)
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

func loadBaseline(ctx context.Context, artifactStore store.ArtifactStore, baselineID, session string) (store.Baseline, error) {
	if strings.TrimSpace(baselineID) != "" {
		baseline, err := artifactStore.GetBaseline(ctx, baselineID)
		if err != nil {
			return store.Baseline{}, fmt.Errorf("load baseline %q: %w", baselineID, err)
		}
		return baseline, nil
	}

	baseline, err := artifactStore.GetLatestBaseline(ctx, session)
	if err != nil {
		return store.Baseline{}, fmt.Errorf("load latest baseline for session %q: %w", session, err)
	}
	return baseline, nil
}
