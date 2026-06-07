package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	analysiscontracts "stagehand/internal/analysis/contracts"
	"stagehand/internal/config"
	sqlitestore "stagehand/internal/store/sqlite"
)

const defaultContractPath = "stagehand.contract.yml"

func runContract(args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("contract requires a subcommand\n\n%s", contractHelpText())
	}

	switch args[0] {
	case "-h", "--help", "help":
		_, _ = io.WriteString(stdout, contractHelpText())
		return nil
	case "generate":
		return runContractGenerate(args[1:], stdout, stderr)
	case "diff":
		return runContractDiff(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown contract subcommand %q\n\n%s", args[0], contractHelpText())
	}
}

func runContractGenerate(args []string, stdout io.Writer, _ io.Writer) error {
	flags := flag.NewFlagSet("contract generate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	session := flags.String("session", "", "Session name whose latest baseline should be used")
	output := flags.String("output", defaultContractPath, "Path to write behavior contract YAML")
	configPath := flags.String("config", "", "Path to stagehand.yml")
	force := flags.Bool("force", false, "Overwrite an existing behavior contract")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse contract generate flags: %w\n\n%s", err, contractGenerateHelpText())
	}

	resolvedSession := strings.TrimSpace(*session)
	if resolvedSession == "" {
		return fmt.Errorf("contract generate requires --session\n\n%s", contractGenerateHelpText())
	}
	resolvedOutput := strings.TrimSpace(*output)
	if resolvedOutput == "" {
		return fmt.Errorf("contract generate requires --output when overriding the default path\n\n%s", contractGenerateHelpText())
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

	baseline, err := loadBaseline(ctx, sqliteStore, "", resolvedSession)
	if err != nil {
		return err
	}
	sourceRun, err := sqliteStore.GetRun(ctx, baseline.SourceRunID)
	if err != nil {
		return fmt.Errorf("load baseline source run %q: %w", baseline.SourceRunID, err)
	}

	contract, summary := analysiscontracts.GenerateFromRun(sourceRun, analysiscontracts.GenerateOptions{
		AgentName: resolvedSession,
	})
	rendered := analysiscontracts.RenderYAML(contract, sourceRun.RunID, baseline.BaselineID)
	if _, err := analysiscontracts.Parse(rendered); err != nil {
		return fmt.Errorf("generated behavior contract did not validate: %w", err)
	}

	if _, err := os.Stat(resolvedOutput); err == nil && !*force {
		return fmt.Errorf("contract generate refuses to overwrite %q; pass --force to replace it", resolvedOutput)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect output path %q: %w", resolvedOutput, err)
	}

	outputDir := filepath.Dir(resolvedOutput)
	if outputDir != "." {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return fmt.Errorf("create contract output directory %q: %w", outputDir, err)
		}
	}
	if err := os.WriteFile(resolvedOutput, rendered, 0o644); err != nil {
		return fmt.Errorf("write behavior contract %q: %w", resolvedOutput, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Stagehand behavior contract generated\n\n")
	fmt.Fprintf(&b, "Session: %s\n", resolvedSession)
	fmt.Fprintf(&b, "Baseline ID: %s\n", baseline.BaselineID)
	fmt.Fprintf(&b, "Source run: %s\n", sourceRun.RunID)
	fmt.Fprintf(&b, "Output: %s\n", filepath.ToSlash(resolvedOutput))
	fmt.Fprintf(&b, "Allowed actions: %d\n", summary.AllowedActions)
	fmt.Fprintf(&b, "Restricted actions: %d\n", summary.RestrictedActions)
	fmt.Fprintf(&b, "Forbidden actions: %d\n", summary.ForbiddenActions)
	if len(summary.Models) > 0 {
		fmt.Fprintf(&b, "Models: %s\n", strings.Join(summary.Models, ", "))
	}
	if summary.UnknownRisk > 0 {
		fmt.Fprintf(&b, "Unknown-risk actions: %d\n", summary.UnknownRisk)
	}
	fmt.Fprintf(&b, "\nReview %s before committing it.\n", filepath.ToSlash(resolvedOutput))
	_, err = io.WriteString(stdout, b.String())
	return err
}

func runContractDiff(args []string, stdout io.Writer, _ io.Writer) error {
	flags := flag.NewFlagSet("contract diff", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	baseRunID := flags.String("base-run-id", "", "Base run identifier")
	baselineID := flags.String("baseline-id", "", "Baseline identifier to use as base")
	session := flags.String("session", "", "Session name to resolve latest baseline as base")
	candidateRunID := flags.String("candidate-run-id", "", "Candidate run identifier")
	format := flags.String("format", string(config.ReportFormatTerminal), "Output format: terminal, json, or github-markdown")
	configPath := flags.String("config", "", "Path to stagehand.yml")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse contract diff flags: %w\n\n%s", err, contractDiffHelpText())
	}

	resolvedCandidateRunID := strings.TrimSpace(*candidateRunID)
	if resolvedCandidateRunID == "" {
		return fmt.Errorf("contract diff requires --candidate-run-id\n\n%s", contractDiffHelpText())
	}

	selectors := 0
	for _, selector := range []string{*baseRunID, *baselineID, *session} {
		if strings.TrimSpace(selector) != "" {
			selectors++
		}
	}
	if selectors != 1 {
		return fmt.Errorf("contract diff requires exactly one of --base-run-id, --baseline-id, or --session\n\n%s", contractDiffHelpText())
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

	result, err := analysiscontracts.DiffRuns(baseRun, candidateRun)
	if err != nil {
		return fmt.Errorf("contract diff runs: %w", err)
	}

	switch config.ReportFormat(strings.TrimSpace(*format)) {
	case config.ReportFormatTerminal, "":
		_, err = io.WriteString(stdout, analysiscontracts.RenderDiffTerminal(result))
		return err
	case config.ReportFormatJSON:
		report, err := analysiscontracts.RenderDiffJSON(result)
		if err != nil {
			return err
		}
		_, err = stdout.Write(report)
		return err
	case config.ReportFormatGitHubMarkdown:
		_, err = io.WriteString(stdout, analysiscontracts.RenderDiffGitHubMarkdown(result))
		return err
	default:
		return fmt.Errorf("contract diff --format must be one of %q, %q, or %q\n\n%s", config.ReportFormatTerminal, config.ReportFormatJSON, config.ReportFormatGitHubMarkdown, contractDiffHelpText())
	}
}

func contractHelpText() string {
	return `Usage:
  stagehand contract <subcommand> [flags]

Subcommands:
  generate  Generate stagehand.contract.yml from the latest promoted baseline
  diff      Compare contract-level behavior between a base run and candidate run
`
}

func contractGenerateHelpText() string {
	return `Usage:
  stagehand contract generate --session <name> [--config path] [--output path] [--force]

Flags:
  --session string  Session name whose latest promoted baseline should be used
  --config string   Path to stagehand.yml (default: stagehand.yml)
  --output string   Path to write behavior contract YAML (default: stagehand.contract.yml)
  --force           Overwrite an existing behavior contract
`
}

func contractDiffHelpText() string {
	return `Usage:
  stagehand contract diff --candidate-run-id <id> (--base-run-id <id> | --baseline-id <id> | --session <name>) [flags]

Flags:
  --candidate-run-id string  Candidate run identifier
  --base-run-id string       Base run identifier
  --baseline-id string       Baseline identifier to use as base
  --session string           Session name to resolve latest baseline as base
  --format string            Output format: terminal, json, or github-markdown (default: terminal)
  --config string            Path to stagehand.yml (default: stagehand.yml)
`
}
