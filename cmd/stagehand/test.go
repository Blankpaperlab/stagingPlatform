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
	"strings"
	"time"

	analysisassertions "stagehand/internal/analysis/assertions"
	analysiscontracts "stagehand/internal/analysis/contracts"
	analysisdiff "stagehand/internal/analysis/diff"
	analysisgates "stagehand/internal/analysis/gates"
	"stagehand/internal/config"
	"stagehand/internal/recorder"
	sqlitestore "stagehand/internal/store/sqlite"
)

const defaultGatesPath = "stagehand.gates.yml"

type testCommandReport struct {
	Mode               string                        `json:"mode"`
	Status             string                        `json:"status"`
	SessionName        string                        `json:"session_name"`
	BaselineID         string                        `json:"baseline_id"`
	BaselineRunID      string                        `json:"baseline_run_id"`
	ReplayRunID        string                        `json:"replay_run_id"`
	StoragePath        string                        `json:"storage_path"`
	Command            []string                      `json:"command"`
	DiffSummary        analysisdiff.Summary          `json:"diff_summary"`
	AssertionSummary   assertionSummary              `json:"assertion_summary"`
	ContractSummary    contractSummary               `json:"contract_summary"`
	GateSummary        assertionSummary              `json:"gate_summary"`
	RiskLevel          analysiscontracts.RiskLevel   `json:"risk_level"`
	RiskReasons        []string                      `json:"risk_reasons,omitempty"`
	ContractPath       string                        `json:"contract_path,omitempty"`
	ContractViolations []analysiscontracts.Violation `json:"contract_violations,omitempty"`
	GatesPath          string                        `json:"gates_path,omitempty"`
	GateResults        []analysisgates.Result        `json:"gate_results,omitempty"`
	AssertionsPath     string                        `json:"assertions_path,omitempty"`
	ErrorInjection     string                        `json:"error_injection,omitempty"`
	ReportJSONPath     string                        `json:"report_json_path"`
	ReportMarkdown     string                        `json:"report_markdown_path"`
	Warnings           []string                      `json:"warnings,omitempty"`
}

type contractSummary struct {
	Status     string `json:"status"`
	Total      int    `json:"total"`
	Forbidden  int    `json:"forbidden"`
	Restricted int    `json:"restricted_without_approval"`
	Unapproved int    `json:"new_unapproved"`
}

func runTest(args []string, stdout io.Writer, stderr io.Writer) error {
	return runTestOrReview("test", args, stdout, stderr)
}

func runReview(args []string, stdout io.Writer, stderr io.Writer) error {
	return runTestOrReview("review", args, stdout, stderr)
}

func runTestOrReview(mode string, args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet(mode, flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	session := flags.String("session", "", "Session name to resolve the latest baseline for")
	configPath := flags.String("config", "", "Path to stagehand.yml")
	assertionsPath := flags.String("assertions", "assertions.yml", "Path to assertions YAML")
	contractPath := flags.String("contract", "", "Path to behavior contract YAML")
	gatesPath := flags.String("gates", "", "Path to release gates YAML")
	errorInjectionPath := flags.String("error-injection", "", "Path to deterministic error injection rules")
	contractExplicit := flagProvided(args, "contract")
	gatesExplicit := flagProvided(args, "gates")
	if err := flags.Parse(args); err != nil {
		return newCommandError(exitCodeConfiguration, fmt.Errorf("parse %s flags: %w\n\n%s", mode, err, testReviewHelpText(mode)))
	}
	commandArgs := flags.Args()
	if len(commandArgs) == 0 {
		return newCommandError(exitCodeConfiguration, fmt.Errorf("%s requires a command after --\n\n%s", mode, testReviewHelpText(mode)))
	}

	cfgPath := strings.TrimSpace(*configPath)
	if cfgPath == "" {
		cfgPath = defaultRuntimeConfigPath
	}
	resolvedSession := strings.TrimSpace(*session)
	if resolvedSession == "" {
		resolvedSession = defaultRecordBaselineSession()
	}
	resolvedContractPath := strings.TrimSpace(*contractPath)
	if resolvedContractPath == "" && fileExists(defaultContractPath) {
		resolvedContractPath = defaultContractPath
	}
	var contractFile *analysiscontracts.File
	if resolvedContractPath != "" {
		if !fileExists(resolvedContractPath) {
			if contractExplicit {
				return newCommandError(exitCodeConfiguration, fmt.Errorf("load contract %q: file does not exist", resolvedContractPath))
			}
		} else {
			file, loadErr := analysiscontracts.Load(resolvedContractPath)
			if loadErr != nil {
				return newCommandError(exitCodeConfiguration, fmt.Errorf("load contract %q: %w", resolvedContractPath, loadErr))
			}
			contractFile = &file
		}
	}
	resolvedGatesPath := strings.TrimSpace(*gatesPath)
	if resolvedGatesPath == "" && fileExists(defaultGatesPath) {
		resolvedGatesPath = defaultGatesPath
	}
	var gateFile *analysisgates.File
	if resolvedGatesPath != "" {
		if !fileExists(resolvedGatesPath) {
			if gatesExplicit {
				return newCommandError(exitCodeConfiguration, fmt.Errorf("load gates %q: file does not exist", resolvedGatesPath))
			}
		} else {
			file, loadErr := analysisgates.Load(resolvedGatesPath)
			if loadErr != nil {
				return newCommandError(exitCodeConfiguration, fmt.Errorf("load gates %q: %w", resolvedGatesPath, loadErr))
			}
			gateFile = &file
		}
	}

	ctx := context.Background()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return newCommandError(exitCodeConfiguration, fmt.Errorf("load runtime config %q: %w", cfgPath, err))
	}
	dbPath := sqliteDatabasePath(cfg.Record.StoragePath)
	sqliteStore, err := sqlitestore.OpenStore(ctx, dbPath)
	if err != nil {
		return newCommandError(exitCodeConfiguration, fmt.Errorf("open local store %q: %w", dbPath, err))
	}
	defer sqliteStore.Close()

	baseline, err := loadBaseline(ctx, sqliteStore, "", resolvedSession)
	if err != nil {
		return newCommandError(exitCodeConfiguration, fmt.Errorf("resolve latest baseline for session %q: %w", resolvedSession, err))
	}

	var replayStdout bytes.Buffer
	replayArgs := []string{"--run-id", baseline.SourceRunID, "--config", cfgPath}
	if strings.TrimSpace(*errorInjectionPath) != "" {
		replayArgs = append(replayArgs, "--error-injection", strings.TrimSpace(*errorInjectionPath))
	}
	replayArgs = append(replayArgs, "--")
	replayArgs = append(replayArgs, commandArgs...)
	if err := runReplay(replayArgs, &replayStdout, stderr); err != nil {
		return newCommandError(exitCodeReplayFailure, fmt.Errorf("stagehand test replay failed: %w", err))
	}
	replayed, err := decodeReplayCommandResult(replayStdout.Bytes())
	if err != nil {
		return newCommandError(exitCodeReplayFailure, fmt.Errorf("decode replay output: %w", err))
	}

	baseRun, err := sqliteStore.GetRun(ctx, baseline.SourceRunID)
	if err != nil {
		return newCommandError(exitCodeConfiguration, fmt.Errorf("load baseline source run %q: %w", baseline.SourceRunID, err))
	}
	replayRun, err := sqliteStore.GetRun(ctx, replayed.ReplayRunID)
	if err != nil {
		return newCommandError(exitCodeReplayFailure, fmt.Errorf("load replay run %q: %w", replayed.ReplayRunID, err))
	}

	diffResult, err := analysisdiff.Compare(baseRun, replayRun, analysisdiff.Options{
		ServiceIgnoredFields: serviceIgnoredFieldsFromConfig(cfg.Services),
	})
	if err != nil {
		return newCommandError(exitCodeConfiguration, fmt.Errorf("compare baseline and replay: %w", err))
	}

	assertionReport := assertionReport{
		RunID:       replayRun.RunID,
		SessionName: replayRun.SessionName,
	}
	warnings := []string{}
	resolvedAssertionsPath := strings.TrimSpace(*assertionsPath)
	if resolvedAssertionsPath != "" && fileExists(resolvedAssertionsPath) {
		file, loadErr := analysisassertions.Load(resolvedAssertionsPath)
		if loadErr != nil {
			if strings.Contains(loadErr.Error(), "assertions must contain at least one assertion") {
				warnings = append(warnings, fmt.Sprintf("skipped %s because it does not contain active assertions", resolvedAssertionsPath))
			} else {
				return newCommandError(exitCodeConfiguration, fmt.Errorf("load assertions %q: %w", resolvedAssertionsPath, loadErr))
			}
		} else {
			results, evalErr := analysisassertions.Evaluate(replayRun, file)
			if evalErr != nil {
				return newCommandError(exitCodeConfiguration, fmt.Errorf("evaluate assertions %q: %w", resolvedAssertionsPath, evalErr))
			}
			assertionReport = newAssertionReport(replayRun, results)
		}
	} else if resolvedAssertionsPath != "" {
		warnings = append(warnings, fmt.Sprintf("no assertions file found at %s; skipped assertions", resolvedAssertionsPath))
	}

	contractReport := contractSummary{Status: "skipped"}
	var contractViolations []analysiscontracts.Violation
	var evaluationResult analysiscontracts.EvaluationResult
	if contractFile != nil {
		result, evalErr := analysiscontracts.Evaluate(replayRun, *contractFile)
		if evalErr != nil {
			return newCommandError(exitCodeConfiguration, fmt.Errorf("evaluate contract %q: %w", resolvedContractPath, evalErr))
		}
		evaluationResult = result
		contractReport = contractSummary{
			Status:     string(result.Status),
			Total:      result.Summary.Total,
			Forbidden:  result.Summary.Forbidden,
			Restricted: result.Summary.Restricted,
			Unapproved: result.Summary.Unapproved,
		}
		contractViolations = append(contractViolations, result.Violations...)
	}

	contractDiff, err := analysiscontracts.DiffRuns(baseRun, replayRun)
	if err != nil {
		return newCommandError(exitCodeConfiguration, fmt.Errorf("compare contract behavior between baseline and replay: %w", err))
	}
	risk := analysiscontracts.ScoreRisk(contractDiff, evaluationResult)

	gateReport := gateReport{
		RunID:       replayRun.RunID,
		SessionName: replayRun.SessionName,
	}
	if gateFile != nil {
		result, evalErr := analysisgates.Evaluate(replayRun, *gateFile, analysisgates.EvaluationOptions{BaseRun: &baseRun})
		if evalErr != nil {
			return newCommandError(exitCodeConfiguration, fmt.Errorf("evaluate gates %q: %w", resolvedGatesPath, evalErr))
		}
		gateReport = newGateReport(replayRun, result)
	}

	status := "passed"
	if diffResult.Summary().FailingChanges > 0 || assertionReport.Summary.Failed > 0 || contractReport.Total > 0 || gateReport.Summary.Failed > 0 || gateReport.Summary.Unsupported > 0 {
		status = "failed"
	}

	report := testCommandReport{
		Mode:               mode,
		Status:             status,
		SessionName:        replayRun.SessionName,
		BaselineID:         baseline.BaselineID,
		BaselineRunID:      baseline.SourceRunID,
		ReplayRunID:        replayRun.RunID,
		StoragePath:        dbPath,
		Command:            append([]string(nil), commandArgs...),
		DiffSummary:        diffResult.Summary(),
		AssertionSummary:   assertionReport.Summary,
		ContractSummary:    contractReport,
		GateSummary:        gateReport.Summary,
		RiskLevel:          risk.Level,
		RiskReasons:        risk.Reasons,
		ContractViolations: contractViolations,
		GateResults:        gateReport.Results,
		Warnings:           warnings,
	}
	if resolvedAssertionsPath != "" && fileExists(resolvedAssertionsPath) {
		report.AssertionsPath = resolvedAssertionsPath
	}
	if contractFile != nil {
		report.ContractPath = resolvedContractPath
	}
	if gateFile != nil {
		report.GatesPath = resolvedGatesPath
	}
	if strings.TrimSpace(*errorInjectionPath) != "" {
		report.ErrorInjection = strings.TrimSpace(*errorInjectionPath)
	}
	report.ReportJSONPath, report.ReportMarkdown = reportPaths(replayRun.SessionName, replayRun.RunID)
	if err := writeTestReports(report, diffResult, assertionReport, gateReport); err != nil {
		return newCommandError(exitCodeConfiguration, err)
	}

	if err := renderTestTerminal(stdout, report, diffResult, assertionReport, gateReport); err != nil {
		return err
	}
	if contractReport.Total > 0 {
		return newCommandError(exitCodeContractViolation, fmt.Errorf("stagehand %s failed: %d contract violation(s)", mode, contractReport.Total))
	}
	if diffResult.Summary().FailingChanges > 0 {
		return newCommandError(exitCodeBehaviorDiff, fmt.Errorf("stagehand %s failed: %d behavior diff(s)", mode, diffResult.Summary().FailingChanges))
	}
	if gateReport.Summary.Failed > 0 || gateReport.Summary.Unsupported > 0 {
		return newCommandError(exitCodeAssertionFailure, fmt.Errorf("stagehand %s failed: %d release gate failure(s)", mode, gateReport.Summary.Failed+gateReport.Summary.Unsupported))
	}
	if assertionReport.Summary.Failed > 0 {
		return newCommandError(exitCodeAssertionFailure, fmt.Errorf("stagehand %s failed: %d assertion failure(s)", mode, assertionReport.Summary.Failed))
	}
	return nil
}

func decodeReplayCommandResult(data []byte) (replayCommandResult, error) {
	var result replayCommandResult
	if err := json.Unmarshal(data, &result); err != nil {
		return replayCommandResult{}, err
	}
	return result, nil
}

func reportPaths(sessionName, runID string) (string, string) {
	slug := safeReportSlug(sessionName + "-" + runID)
	return filepath.Join(".stagehand", "reports", slug+".json"), filepath.Join(".stagehand", "reports", slug+".md")
}

func safeReportSlug(value string) string {
	lower := strings.ToLower(value)
	var b strings.Builder
	lastDash := false
	for _, r := range lower {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "stagehand-test"
	}
	return slug
}

func writeTestReports(report testCommandReport, diffResult analysisdiff.Result, assertionReport assertionReport, gateReport gateReport) error {
	if err := os.MkdirAll(filepath.Dir(report.ReportJSONPath), 0o755); err != nil {
		return fmt.Errorf("create test report directory: %w", err)
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode test JSON report: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(report.ReportJSONPath, encoded, 0o644); err != nil {
		return fmt.Errorf("write test JSON report %q: %w", report.ReportJSONPath, err)
	}
	markdown := renderTestMarkdown(report, diffResult, assertionReport, gateReport)
	if err := os.WriteFile(report.ReportMarkdown, []byte(markdown), 0o644); err != nil {
		return fmt.Errorf("write test markdown report %q: %w", report.ReportMarkdown, err)
	}
	return nil
}

func renderTestTerminal(w io.Writer, report testCommandReport, diffResult analysisdiff.Result, assertionReport assertionReport, gateReport gateReport) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Stagehand %s: %s\n", report.Mode, report.Status)
	fmt.Fprintf(&b, "Session: %s\n", report.SessionName)
	fmt.Fprintf(&b, "Baseline: %s (%s)\n", report.BaselineID, report.BaselineRunID)
	fmt.Fprintf(&b, "Replay: %s\n", report.ReplayRunID)
	fmt.Fprintf(&b, "Diff: %d failing, %d informational, %d total\n", report.DiffSummary.FailingChanges, report.DiffSummary.InformationalChanges, report.DiffSummary.TotalChanges)
	fmt.Fprintf(&b, "Assertions: %d passed, %d failed, %d unsupported, %d total\n", report.AssertionSummary.Passed, report.AssertionSummary.Failed, report.AssertionSummary.Unsupported, report.AssertionSummary.Total)
	fmt.Fprintf(&b, "Contract: %s, %d violation(s)\n", report.ContractSummary.Status, report.ContractSummary.Total)
	fmt.Fprintf(&b, "Release gates: %d passed, %d failed, %d unsupported, %d total\n", report.GateSummary.Passed, report.GateSummary.Failed, report.GateSummary.Unsupported, report.GateSummary.Total)
	fmt.Fprintf(&b, "Risk: %s\n", strings.ToUpper(string(report.RiskLevel)))
	for _, reason := range report.RiskReasons {
		fmt.Fprintf(&b, "  - %s\n", reason)
	}
	for _, warning := range report.Warnings {
		fmt.Fprintf(&b, "Warning: %s\n", warning)
	}
	if report.DiffSummary.TotalChanges > 0 {
		fmt.Fprintf(&b, "\n%s", analysisdiff.RenderTerminal(diffResult))
	}
	if assertionReport.Summary.Total > 0 {
		fmt.Fprintf(&b, "\n%s", renderAssertionTerminal(assertionReport))
	}
	if gateReport.Summary.Total > 0 {
		fmt.Fprintf(&b, "\n%s", renderGateTerminal(gateReport))
	}
	if len(report.ContractViolations) > 0 {
		fmt.Fprintf(&b, "\n%s", renderContractTerminal(report.ContractViolations))
	}
	fmt.Fprintf(&b, "\nReports:\n- %s\n- %s\n", filepath.ToSlash(report.ReportJSONPath), filepath.ToSlash(report.ReportMarkdown))
	_, err := io.WriteString(w, b.String())
	return err
}

func renderTestMarkdown(report testCommandReport, diffResult analysisdiff.Result, assertionReport assertionReport, gateReport gateReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Stagehand %s Report\n\n", reportModeTitle(report.Mode))
	fmt.Fprintf(&b, "- Status: **%s**\n", report.Status)
	fmt.Fprintf(&b, "- Session: `%s`\n", report.SessionName)
	fmt.Fprintf(&b, "- Baseline: `%s` (`%s`)\n", report.BaselineID, report.BaselineRunID)
	fmt.Fprintf(&b, "- Replay: `%s`\n", report.ReplayRunID)
	fmt.Fprintf(&b, "- Risk: **%s**\n", strings.ToUpper(string(report.RiskLevel)))
	fmt.Fprintf(&b, "- Generated: `%s`\n\n", time.Now().UTC().Format(time.RFC3339))
	if len(report.RiskReasons) > 0 {
		fmt.Fprintf(&b, "## Risk\n\n")
		for _, reason := range report.RiskReasons {
			fmt.Fprintf(&b, "- %s\n", reason)
		}
		fmt.Fprintf(&b, "\n")
	}
	if len(report.Warnings) > 0 {
		fmt.Fprintf(&b, "## Warnings\n\n")
		for _, warning := range report.Warnings {
			fmt.Fprintf(&b, "- %s\n", warning)
		}
		fmt.Fprintf(&b, "\n")
	}
	fmt.Fprintf(&b, "## Diff\n\n")
	fmt.Fprintf(&b, "%s\n", analysisdiff.RenderGitHubMarkdown(diffResult))
	if assertionReport.Summary.Total > 0 {
		fmt.Fprintf(&b, "\n## Assertions\n\n")
		fmt.Fprintf(&b, "Results: %d passed, %d failed, %d unsupported, %d total\n\n", assertionReport.Summary.Passed, assertionReport.Summary.Failed, assertionReport.Summary.Unsupported, assertionReport.Summary.Total)
		for _, result := range assertionReport.Results {
			fmt.Fprintf(&b, "- `%s` `%s` `%s`: %s\n", result.Status, result.Type, result.AssertionID, result.Message)
		}
	}
	if gateReport.Summary.Total > 0 {
		fmt.Fprintf(&b, "\n## Release Gates\n\n")
		fmt.Fprintf(&b, "Results: %d passed, %d failed, %d unsupported, %d total\n\n", gateReport.Summary.Passed, gateReport.Summary.Failed, gateReport.Summary.Unsupported, gateReport.Summary.Total)
		for _, result := range failedGateResultsFirst(gateReport.Results) {
			fmt.Fprintf(&b, "- `%s` `%s`: %s\n", result.Status, result.GateName, result.Message)
			for _, evidence := range conciseGateEvidence(result) {
				fmt.Fprintf(&b, "  - %s\n", evidence)
			}
		}
	}
	if len(report.ContractViolations) > 0 {
		fmt.Fprintf(&b, "\n## Contract Violations\n\n")
		for _, violation := range report.ContractViolations {
			fmt.Fprintf(&b, "- `%s` `%s`: %s\n", violation.Type, contractViolationLabel(violation), violation.Reason)
			for _, evidence := range violation.Evidence {
				fmt.Fprintf(&b, "  - interaction `%s` sequence `%d` `%s %s`\n", evidence.InteractionID, evidence.Sequence, evidence.RequestMethod, evidence.RequestURL)
			}
		}
	}
	return b.String()
}

func reportModeTitle(mode string) string {
	switch mode {
	case "review":
		return "Review"
	default:
		return "Test"
	}
}

func testHelpText() string {
	return testReviewHelpText("test")
}

func reviewHelpText() string {
	return testReviewHelpText("review")
}

func testReviewHelpText(mode string) string {
	return `Usage:
  stagehand ` + mode + ` [--session name] [--config path] [--assertions path] [--contract path] [--error-injection path] -- <command> [args...]

Flags:
  --session string          Session name to resolve the latest baseline for (default: current directory name)
  --config string           Path to stagehand.yml (default: stagehand.yml)
  --assertions string       Path to assertions YAML (default: assertions.yml)
  --contract string         Path to behavior contract YAML (default: stagehand.contract.yml when present)
  --gates string            Path to release gates YAML (default: stagehand.gates.yml when present)
  --error-injection string  Path to deterministic error injection rules
`
}

type gateReport struct {
	RunID       string                 `json:"run_id"`
	SessionName string                 `json:"session_name"`
	Summary     assertionSummary       `json:"summary"`
	Results     []analysisgates.Result `json:"results"`
}

func newGateReport(run recorder.Run, result analysisgates.EvaluationResult) gateReport {
	return gateReport{
		RunID:       run.RunID,
		SessionName: run.SessionName,
		Summary: assertionSummary{
			Total:       result.Summary.Total,
			Passed:      result.Summary.Passed,
			Failed:      result.Summary.Failed,
			Unsupported: result.Summary.Unsupported,
		},
		Results: append([]analysisgates.Result(nil), result.Results...),
	}
}

func renderGateTerminal(report gateReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Stagehand release gates: %s\n", report.RunID)
	fmt.Fprintf(&b, "Session: %s\n", report.SessionName)
	fmt.Fprintf(&b, "Results: %d passed, %d failed, %d unsupported, %d total\n", report.Summary.Passed, report.Summary.Failed, report.Summary.Unsupported, report.Summary.Total)
	for _, result := range failedGateResultsFirst(report.Results) {
		fmt.Fprintf(&b, "- %s %s: %s\n", result.Status, result.GateName, result.Message)
		for _, evidence := range conciseGateEvidence(result) {
			fmt.Fprintf(&b, "  evidence: %s\n", evidence)
		}
	}
	return b.String()
}

func failedGateResultsFirst(results []analysisgates.Result) []analysisgates.Result {
	ordered := make([]analysisgates.Result, 0, len(results))
	for _, result := range results {
		if result.Status != analysisassertions.ResultStatusPassed {
			ordered = append(ordered, result)
		}
	}
	for _, result := range results {
		if result.Status == analysisassertions.ResultStatusPassed {
			ordered = append(ordered, result)
		}
	}
	return ordered
}

func conciseGateEvidence(result analysisgates.Result) []string {
	evidence := result.Evidence.Violations
	if len(evidence) == 0 {
		evidence = result.Evidence.MatchedInteractions
	}
	if len(evidence) == 0 && result.Evidence.Before != nil && result.Evidence.After != nil {
		return []string{fmt.Sprintf("before interaction=%s sequence=%d, after interaction=%s sequence=%d", result.Evidence.Before.InteractionID, result.Evidence.Before.Sequence, result.Evidence.After.InteractionID, result.Evidence.After.Sequence)}
	}
	lines := make([]string, 0, len(evidence))
	for _, item := range evidence {
		label := strings.TrimSpace(item.Service + " " + item.Operation)
		if label == "" {
			label = "interaction"
		}
		lines = append(lines, fmt.Sprintf("interaction=%s sequence=%d %s", item.InteractionID, item.Sequence, label))
	}
	return lines
}

func renderContractTerminal(violations []analysiscontracts.Violation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Stagehand contract violations:\n")
	for _, violation := range violations {
		fmt.Fprintf(&b, "- %s %s: %s\n", violation.Type, contractViolationLabel(violation), violation.Reason)
		for _, evidence := range violation.Evidence {
			fmt.Fprintf(&b, "  evidence: interaction=%s sequence=%d request=%s %s\n", evidence.InteractionID, evidence.Sequence, evidence.RequestMethod, evidence.RequestURL)
		}
	}
	return b.String()
}

func contractViolationLabel(violation analysiscontracts.Violation) string {
	if strings.TrimSpace(violation.Tool) != "" {
		return "tool:" + violation.Tool
	}
	return strings.TrimSpace(violation.Service + " " + violation.Operation)
}

func flagProvided(args []string, name string) bool {
	long := "--" + name
	for _, arg := range args {
		if arg == long || strings.HasPrefix(arg, long+"=") {
			return true
		}
	}
	return false
}
