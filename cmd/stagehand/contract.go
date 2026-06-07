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
	default:
		return fmt.Errorf("unknown contract subcommand %q\n\n%s", args[0], contractHelpText())
	}
}

func runContractGenerate(args []string, stdout io.Writer, _ io.Writer) error {
	flags := flag.NewFlagSet("contract generate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	session := flags.String("session", "", "Session name whose latest baseline should be used")
	output := flags.String("output", "stagehand.contract.yml", "Path to write behavior contract YAML")
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

func contractHelpText() string {
	return `Usage:
  stagehand contract <subcommand> [flags]

Subcommands:
  generate  Generate stagehand.contract.yml from the latest promoted baseline
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
