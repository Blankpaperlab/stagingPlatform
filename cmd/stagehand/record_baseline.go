package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"stagehand/internal/config"
	"stagehand/internal/recorder"
	"stagehand/internal/store"
	sqlitestore "stagehand/internal/store/sqlite"
)

type recordBaselineResult struct {
	Mode             string                `json:"mode"`
	RunID            string                `json:"run_id"`
	BaselineID       string                `json:"baseline_id"`
	SessionName      string                `json:"session_name"`
	Status           string                `json:"status"`
	InteractionCount int                   `json:"interaction_count"`
	StoragePath      string                `json:"storage_path"`
	FirstRunReport   string                `json:"first_run_report"`
	Command          []string              `json:"command"`
	NextCommand      string                `json:"next_command"`
	CaptureSummary   []captureSummaryEntry `json:"capture_summary"`
	Warnings         []string              `json:"warnings,omitempty"`
}

type captureSummaryEntry struct {
	Service   string `json:"service"`
	Operation string `json:"operation"`
	Count     int    `json:"count"`
}

func runRecordBaseline(args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("record-baseline", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	session := flags.String("session", "", "Session name to record and promote")
	baselineID := flags.String("baseline-id", "", "Optional baseline identifier")
	configPath := flags.String("config", "", "Path to stagehand.yml")
	jsonOutput := flags.Bool("json", false, "Emit machine-readable output")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse record-baseline flags: %w\n\n%s", err, recordBaselineHelpText())
	}
	commandArgs := flags.Args()
	if len(commandArgs) == 0 {
		return fmt.Errorf("record-baseline requires a command after --\n\n%s", recordBaselineHelpText())
	}

	cfgPath := strings.TrimSpace(*configPath)
	if cfgPath == "" {
		cfgPath = defaultRuntimeConfigPath
	}
	resolvedSession := strings.TrimSpace(*session)
	if resolvedSession == "" {
		resolvedSession = defaultRecordBaselineSession()
	}

	var recordStdout bytes.Buffer
	recordArgs := []string{"--session", resolvedSession, "--config", cfgPath, "--"}
	recordArgs = append(recordArgs, commandArgs...)
	if err := runRecord(recordArgs, &recordStdout, stderr); err != nil {
		return actionableRecordBaselineError(err)
	}
	recorded, err := decodeRecordResult(recordStdout.Bytes())
	if err != nil {
		return fmt.Errorf("decode record output: %w", err)
	}
	if recorded.EmptyRun || recorded.InteractionCount == 0 {
		return fmt.Errorf("record-baseline captured zero interactions; make sure your command initializes the Stagehand SDK and uses supported capture paths such as Python httpx, TypeScript fetch/undici, provider SDK hooks, or stagehand.tool wrappers")
	}

	ctx := context.Background()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load runtime config %q after record: %w", cfgPath, err)
	}
	dbPath := sqliteDatabasePath(cfg.Record.StoragePath)
	sqliteStore, err := sqlitestore.OpenStore(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open local store %q: %w", dbPath, err)
	}
	defer sqliteStore.Close()

	run, err := sqliteStore.GetRun(ctx, recorded.RunID)
	if err != nil {
		return fmt.Errorf("load recorded run %q: %w", recorded.RunID, err)
	}

	resolvedBaselineID := strings.TrimSpace(*baselineID)
	if resolvedBaselineID == "" {
		resolvedBaselineID, err = generateBaselineID()
		if err != nil {
			return fmt.Errorf("generate baseline id: %w", err)
		}
	}
	gitSHA := run.GitSHA
	if strings.TrimSpace(gitSHA) == "" {
		if current, gitErr := currentGitSHA(); gitErr == nil {
			gitSHA = current
		} else {
			gitSHA = "unknown"
		}
	}
	baseline := store.Baseline{
		BaselineID:  resolvedBaselineID,
		SessionName: run.SessionName,
		SourceRunID: run.RunID,
		GitSHA:      gitSHA,
		CreatedAt:   time.Now().UTC(),
	}
	if err := sqliteStore.PutBaseline(ctx, baseline); err != nil {
		return fmt.Errorf("promote recorded run %q to baseline: %w", run.RunID, err)
	}

	warnings := []string{}
	if gitSHA == "unknown" {
		warnings = append(warnings, "current git SHA could not be resolved; baseline git_sha was recorded as unknown")
	}
	nextCommand := fmt.Sprintf("stagehand test --session %s -- %s", run.SessionName, strings.Join(commandArgs, " "))
	firstRunReportPath := firstRunReportPath(cfg.Record.StoragePath, run.SessionName, run.RunID)
	result := recordBaselineResult{
		Mode:             "record_baseline",
		RunID:            run.RunID,
		BaselineID:       baseline.BaselineID,
		SessionName:      run.SessionName,
		Status:           string(store.RunLifecycleStatusComplete),
		InteractionCount: len(run.Interactions),
		StoragePath:      dbPath,
		FirstRunReport:   firstRunReportPath,
		Command:          append([]string(nil), commandArgs...),
		NextCommand:      nextCommand,
		CaptureSummary:   summarizeCapturedInteractions(run.Interactions),
		Warnings:         warnings,
	}
	if err := writeFirstRunReport(result); err != nil {
		return err
	}
	if *jsonOutput {
		return emitJSON(stdout, result)
	}
	return renderRecordBaselineTerminal(stdout, result)
}

func decodeRecordResult(data []byte) (recordResult, error) {
	var result recordResult
	if err := json.Unmarshal(data, &result); err != nil {
		return recordResult{}, err
	}
	return result, nil
}

func summarizeCapturedInteractions(interactions []recorder.Interaction) []captureSummaryEntry {
	counts := map[string]int{}
	for _, interaction := range interactions {
		key := interaction.Service + "\x00" + interaction.Operation
		counts[key]++
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	summary := make([]captureSummaryEntry, 0, len(keys))
	for _, key := range keys {
		service, operation, _ := strings.Cut(key, "\x00")
		summary = append(summary, captureSummaryEntry{
			Service:   service,
			Operation: operation,
			Count:     counts[key],
		})
	}
	return summary
}

func renderRecordBaselineTerminal(w io.Writer, result recordBaselineResult) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Stagehand baseline recorded\n\n")
	fmt.Fprintf(&b, "Session: %s\n", result.SessionName)
	fmt.Fprintf(&b, "Run ID: %s\n", result.RunID)
	fmt.Fprintf(&b, "Baseline ID: %s\n", result.BaselineID)
	fmt.Fprintf(&b, "Storage: %s\n", filepath.ToSlash(result.StoragePath))
	fmt.Fprintf(&b, "First-run report: %s\n", filepath.ToSlash(result.FirstRunReport))
	fmt.Fprintf(&b, "Captured: %d interactions\n", result.InteractionCount)
	if len(result.CaptureSummary) > 0 {
		fmt.Fprintf(&b, "\nCapture summary:\n")
		for _, entry := range result.CaptureSummary {
			fmt.Fprintf(&b, "- %s %s: %d\n", entry.Service, entry.Operation, entry.Count)
		}
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(&b, "\nWarning: %s\n", warning)
	}
	fmt.Fprintf(&b, "\nNext command:\n  %s\n", result.NextCommand)
	_, err := io.WriteString(w, b.String())
	return err
}

func firstRunReportPath(storagePath, sessionName, runID string) string {
	cleaned := filepath.Clean(storagePath)
	stagehandDir := filepath.Dir(cleaned)
	if strings.EqualFold(filepath.Ext(cleaned), ".db") || strings.EqualFold(filepath.Ext(cleaned), ".sqlite") || strings.EqualFold(filepath.Ext(cleaned), ".sqlite3") {
		stagehandDir = filepath.Dir(filepath.Dir(cleaned))
	}
	return filepath.Join(stagehandDir, "reports", safeReportSlug(sessionName+"-"+runID+"-first-run")+".md")
}

func writeFirstRunReport(result recordBaselineResult) error {
	if err := os.MkdirAll(filepath.Dir(result.FirstRunReport), 0o755); err != nil {
		return fmt.Errorf("create first-run report directory: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Stagehand First Run\n\n")
	fmt.Fprintf(&b, "Baseline recorded for session `%s`.\n\n", result.SessionName)
	fmt.Fprintf(&b, "## Artifacts\n\n")
	fmt.Fprintf(&b, "- Run ID: `%s`\n", result.RunID)
	fmt.Fprintf(&b, "- Baseline ID: `%s`\n", result.BaselineID)
	fmt.Fprintf(&b, "- Storage: `%s`\n", filepath.ToSlash(result.StoragePath))
	fmt.Fprintf(&b, "- Report: `%s`\n\n", filepath.ToSlash(result.FirstRunReport))
	fmt.Fprintf(&b, "## Capture Summary\n\n")
	if len(result.CaptureSummary) == 0 {
		fmt.Fprintf(&b, "- No interactions captured\n")
	} else {
		for _, entry := range result.CaptureSummary {
			fmt.Fprintf(&b, "- `%s` `%s`: %d\n", entry.Service, entry.Operation, entry.Count)
		}
	}
	fmt.Fprintf(&b, "\n## Useful Commands\n\n")
	fmt.Fprintf(&b, "```bash\n")
	fmt.Fprintf(&b, "stagehand inspect --run-id %s --show-bodies\n", result.RunID)
	fmt.Fprintf(&b, "%s\n", result.NextCommand)
	fmt.Fprintf(&b, "stagehand ci setup --session %s --command \"%s\"\n", result.SessionName, strings.Join(result.Command, " "))
	fmt.Fprintf(&b, "```\n")
	if err := os.WriteFile(result.FirstRunReport, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write first-run report %q: %w", result.FirstRunReport, err)
	}
	return nil
}

func actionableRecordBaselineError(err error) error {
	message := err.Error()
	if strings.Contains(message, "load record capture bundle") || strings.Contains(message, "did not produce") || strings.Contains(message, "capture") {
		return fmt.Errorf("%w\n\nRepair: initialize the Stagehand SDK in the managed command and confirm it writes a capture bundle. Run `stagehand doctor` to check supported capture paths", err)
	}
	return err
}

func defaultRecordBaselineSession() string {
	wd := mustGetwd()
	base := strings.ToLower(filepath.Base(wd))
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	trimmed := strings.Trim(b.String(), "-")
	if trimmed == "" {
		return "stagehand-baseline"
	}
	return trimmed
}

func recordBaselineHelpText() string {
	return `Usage:
  stagehand record-baseline [--session name] [--baseline-id id] [--config path] [--json] -- <command> [args...]

Flags:
  --session string      Session name to record and promote (default: current directory name)
  --baseline-id string  Optional baseline identifier (default: generated)
  --config string       Path to stagehand.yml (default: stagehand.yml)
  --json                Emit machine-readable output
`
}
