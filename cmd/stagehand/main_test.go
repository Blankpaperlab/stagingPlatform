package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	analysisdiff "stagehand/internal/analysis/diff"
	"stagehand/internal/config"
	"stagehand/internal/recorder"
	"stagehand/internal/store"
	sqlitestore "stagehand/internal/store/sqlite"
	"stagehand/internal/version"
)

func TestRunWritesRootHelpWithNoArgs(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := run(nil, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "stagehand "+version.CLIVersion) {
		t.Fatalf("stdout = %q, want CLI version banner", stdout.String())
	}
	if !strings.Contains(stdout.String(), "record") {
		t.Fatalf("stdout = %q, want record command in help", stdout.String())
	}
	if !strings.Contains(stdout.String(), "replay") {
		t.Fatalf("stdout = %q, want replay command in help", stdout.String())
	}
	if !strings.Contains(stdout.String(), "inspect") {
		t.Fatalf("stdout = %q, want inspect command in help", stdout.String())
	}
	if !strings.Contains(stdout.String(), "diff") {
		t.Fatalf("stdout = %q, want diff command in help", stdout.String())
	}
	if !strings.Contains(stdout.String(), "assert") {
		t.Fatalf("stdout = %q, want assert command in help", stdout.String())
	}
	if !strings.Contains(stdout.String(), "conformance") {
		t.Fatalf("stdout = %q, want conformance command in help", stdout.String())
	}
	if !strings.Contains(stdout.String(), "baseline") {
		t.Fatalf("stdout = %q, want baseline command in help", stdout.String())
	}
	if !strings.Contains(stdout.String(), "init") {
		t.Fatalf("stdout = %q, want init command in help", stdout.String())
	}
	if !strings.Contains(stdout.String(), "doctor") {
		t.Fatalf("stdout = %q, want doctor command in help", stdout.String())
	}
	if !strings.Contains(stdout.String(), "record-baseline") {
		t.Fatalf("stdout = %q, want record-baseline command in help", stdout.String())
	}
	if !strings.Contains(stdout.String(), "ci") {
		t.Fatalf("stdout = %q, want ci command in help", stdout.String())
	}
}

func TestRunInitCreatesScaffoldDetectsProjectAndPrintsNextCommand(t *testing.T) {
	workdir := t.TempDir()
	t.Chdir(workdir)
	writeFile(t, filepath.Join(workdir, "package.json"), `{
  "scripts": {
    "agent": "tsx src/agent.ts",
    "test": "node --test"
  },
  "dependencies": {
    "openai": "^1.0.0",
    "undici": "^6.0.0"
  }
}
`)
	writeFile(t, filepath.Join(workdir, "pyproject.toml"), "[project]\ndependencies = [\"httpx\", \"stripe\"]\n")
	writeFile(t, filepath.Join(workdir, "agent.py"), "import httpx\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"init"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(init) error = %v\nstderr=%s", err, stderr.String())
	}

	for _, path := range []string{
		"stagehand.yml",
		filepath.Join(".stagehand", "runs"),
		filepath.Join(".stagehand", "reports"),
		filepath.Join(".stagehand", "generated"),
		"assertions.yml",
		"error-injection.yml",
	} {
		if _, err := os.Stat(filepath.Join(workdir, path)); err != nil {
			t.Fatalf("expected scaffold path %q: %v", path, err)
		}
	}
	if _, err := config.Load(filepath.Join(workdir, "stagehand.yml")); err != nil {
		t.Fatalf("generated stagehand.yml did not validate: %v", err)
	}
	configText, err := os.ReadFile(filepath.Join(workdir, "stagehand.yml"))
	if err != nil {
		t.Fatalf("read generated stagehand.yml: %v", err)
	}
	assertionsText, err := os.ReadFile(filepath.Join(workdir, "assertions.yml"))
	if err != nil {
		t.Fatalf("read generated assertions.yml: %v", err)
	}
	injectionText, err := os.ReadFile(filepath.Join(workdir, "error-injection.yml"))
	if err != nil {
		t.Fatalf("read generated error-injection.yml: %v", err)
	}
	for _, want := range []string{
		"name: internal-crm",
		"body.request_id",
		"body.generated_at",
	} {
		if !strings.Contains(string(configText), want) {
			t.Fatalf("stagehand.yml = %q, want %q", string(configText), want)
		}
	}
	for _, want := range []string{
		"type: count",
		"type: ordering",
		"type: forbidden-operation",
		"type: payload-field",
		"type: fallback-prohibition",
		"examples that do not match your workflow",
	} {
		if !strings.Contains(string(assertionsText), want) {
			t.Fatalf("assertions.yml = %q, want %q", string(assertionsText), want)
		}
	}
	for _, want := range []string{
		"error: timeout",
		"status: 500",
		"tool: lookup_customer",
		"Set enabled: true",
	} {
		if !strings.Contains(string(injectionText), want) {
			t.Fatalf("error-injection.yml = %q, want %q", string(injectionText), want)
		}
	}

	output := stdout.String()
	for _, want := range []string{
		"Stagehand initialized",
		"project: typescript, node, python",
		"integrations:",
		"openai",
		"undici",
		"httpx",
		"stripe",
		"candidate commands: npm run agent",
		"stagehand record-baseline -- npm run agent",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunInitRefusesExistingConfigUnlessForce(t *testing.T) {
	workdir := t.TempDir()
	t.Chdir(workdir)
	writeFile(t, filepath.Join(workdir, "stagehand.yml"), "existing: true\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"init"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(init) expected overwrite error")
	}
	if !strings.Contains(err.Error(), "refusing to overwrite existing config") {
		t.Fatalf("run(init) error = %v", err)
	}
	data, readErr := os.ReadFile(filepath.Join(workdir, "stagehand.yml"))
	if readErr != nil {
		t.Fatalf("read stagehand.yml: %v", readErr)
	}
	if string(data) != "existing: true\n" {
		t.Fatalf("stagehand.yml = %q, want original content", string(data))
	}

	stdout.Reset()
	if err := run([]string{"init", "--force", "--starter-files=false"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(init --force) error = %v", err)
	}
	if _, err := config.Load(filepath.Join(workdir, "stagehand.yml")); err != nil {
		t.Fatalf("forced stagehand.yml did not validate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workdir, "assertions.yml")); !os.IsNotExist(err) {
		t.Fatalf("assertions.yml exists after --starter-files=false, err=%v", err)
	}
}

func TestRunInitDetectsPythonEntrypointWhenNoNodeScriptExists(t *testing.T) {
	workdir := t.TempDir()
	t.Chdir(workdir)
	writeFile(t, filepath.Join(workdir, "pyproject.toml"), `[project]
dependencies = ["openai", "httpx"]

[project.scripts]
support-agent = "support.agent:main"
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"init", "--starter-files=false"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(init) error = %v\nstderr=%s", err, stderr.String())
	}

	output := stdout.String()
	for _, want := range []string{
		"project: python",
		"integrations: openai, httpx",
		"candidate commands: support-agent",
		"stagehand record-baseline -- support-agent",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
	if _, err := os.Stat(filepath.Join(workdir, "assertions.yml")); !os.IsNotExist(err) {
		t.Fatalf("assertions.yml exists after --starter-files=false, err=%v", err)
	}
}

func TestRunDoctorPassesForValidConfigOnlyProject(t *testing.T) {
	workdir := t.TempDir()
	t.Chdir(workdir)
	writeFile(t, filepath.Join(workdir, "stagehand.yml"), stagehandInitConfigYAML())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"doctor"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(doctor) error = %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	output := stdout.String()
	for _, want := range []string{
		"Stagehand doctor: pass",
		"PASS CLI binary",
		"PASS stagehand.yml",
		"PASS Unsupported network libraries",
		"Summary:",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunDoctorReportsMissingConfigAsJSONFailure(t *testing.T) {
	workdir := t.TempDir()
	t.Chdir(workdir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"doctor", "--json"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(doctor --json) expected failed check error")
	}
	if !strings.Contains(err.Error(), "doctor found 1 failed check") {
		t.Fatalf("run(doctor --json) error = %v", err)
	}

	report := decodeJSONOutput[doctorReport](t, stdout.Bytes())
	if report.Status != doctorStatusFail {
		t.Fatalf("report.Status = %q, want fail", report.Status)
	}
	if report.Summary.Failed != 1 {
		t.Fatalf("report.Summary.Failed = %d, want 1", report.Summary.Failed)
	}
	configCheck := findDoctorCheck(t, report, "config-valid")
	if configCheck.Status != doctorStatusFail || !strings.Contains(configCheck.Repair, "stagehand init") {
		t.Fatalf("config check = %#v, want failed repair with stagehand init", configCheck)
	}
}

func TestRunDoctorWarnsForUnsupportedNetworkLibraries(t *testing.T) {
	workdir := t.TempDir()
	t.Chdir(workdir)
	writeFile(t, filepath.Join(workdir, "stagehand.yml"), stagehandInitConfigYAML())
	writeFile(t, filepath.Join(workdir, "package.json"), `{
  "dependencies": {
    "@stagehand/sdk": "0.1.0-alpha.0",
    "axios": "^1.0.0"
  }
}
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"doctor", "--json"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(doctor --json) error = %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	report := decodeJSONOutput[doctorReport](t, stdout.Bytes())
	if report.Status != doctorStatusWarn {
		t.Fatalf("report.Status = %q, want warn", report.Status)
	}
	unsupported := findDoctorCheck(t, report, "unsupported-network-libraries")
	if unsupported.Status != doctorStatusWarn || !strings.Contains(unsupported.Message, "axios") {
		t.Fatalf("unsupported check = %#v, want axios warning", unsupported)
	}
	build := findDoctorCheck(t, report, "typescript-build")
	if build.Status != doctorStatusWarn || !strings.Contains(build.Repair, "tsc --noEmit") {
		t.Fatalf("typescript build check = %#v, want no build script warning", build)
	}
}

func TestRunCISetupDryRunPrintsLocalWorkflow(t *testing.T) {
	workdir := t.TempDir()
	t.Chdir(workdir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"ci", "setup",
		"--dry-run",
		"--command", "python agent.py",
		"--session", "onboarding-flow",
		"--artifact-name", "stagehand-onboarding",
		"--comment-id", "onboarding-main",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run(ci setup --dry-run) error = %v\nstderr=%s", err, stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"stagehand record-baseline --session onboarding-flow -- python agent.py",
		"uses: ./",
		"go build -o bin/stagehand ./cmd/stagehand",
		"command: python agent.py",
		"sessions: onboarding-flow",
		"upload-artifact: 'true'",
		"artifact-name: stagehand-onboarding",
		"post-pr-comment: 'true'",
		"comment-id: onboarding-main",
		"OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}",
		"STRIPE_API_KEY: ${{ secrets.STRIPE_API_KEY }}",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
	if _, err := os.Stat(filepath.Join(workdir, ".github", "workflows", "stagehand.yml")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote workflow unexpectedly, stat err=%v", err)
	}
}

func TestRunCISetupWritesWorkflowAndRefusesOverwrite(t *testing.T) {
	workdir := t.TempDir()
	t.Chdir(workdir)
	writeFile(t, filepath.Join(workdir, "package.json"), `{"scripts":{"agent":"tsx agent.ts"}}`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"ci", "setup"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(ci setup) error = %v\nstderr=%s", err, stderr.String())
	}
	workflowPath := filepath.Join(workdir, ".github", "workflows", "stagehand.yml")
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", workflowPath, err)
	}
	workflow := string(data)
	if !strings.Contains(workflow, "command: npm run agent") || !strings.Contains(workflow, "stagehand record-baseline --session "+defaultRecordBaselineSession()+" -- npm run agent") {
		t.Fatalf("workflow = %q, want detected npm command and baseline guidance", workflow)
	}
	if !strings.Contains(stdout.String(), "Before enabling PR enforcement") {
		t.Fatalf("stdout = %q, want baseline promotion guidance", stdout.String())
	}

	stdout.Reset()
	err = run([]string{"ci", "setup"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(ci setup) expected overwrite refusal")
	}
	if !strings.Contains(err.Error(), "refusing to overwrite existing workflow") {
		t.Fatalf("run(ci setup) error = %v", err)
	}
}

func TestRunCISetupPublishedActionReference(t *testing.T) {
	workdir := t.TempDir()
	t.Chdir(workdir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{
		"ci", "setup",
		"--dry-run",
		"--published",
		"--command", "npm run agent",
		"--session", "agent-flow",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("run(ci setup --published) error = %v\nstderr=%s", err, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "uses: stagehand/stagehand-action@v0") {
		t.Fatalf("stdout = %q, want published action reference", output)
	}
	if strings.Contains(output, "go build -o bin/stagehand") {
		t.Fatalf("stdout = %q, published workflow should not build local CLI", output)
	}
}

func TestRunRecordRequiresSession(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run([]string{"record"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(record) expected --session validation error")
	}
	if !strings.Contains(err.Error(), "record requires --session") {
		t.Fatalf("run(record) error = %v", err)
	}
}

func TestRunRecordBaselineRecordsPromotesAndEmitsJSON(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	args := []string{
		"record-baseline",
		"--session", "onboarding-flow",
		"--baseline-id", "base_onboarding_001",
		"--config", configPath,
		"--json",
		"--",
	}
	args = append(args, helperCommand("record-write-capture")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run(record-baseline) error = %v\nstderr=%s", err, stderr.String())
	}

	result := decodeJSONOutput[recordBaselineResult](t, stdout.Bytes())
	if result.Mode != "record_baseline" {
		t.Fatalf("result.Mode = %q, want record_baseline", result.Mode)
	}
	if result.SessionName != "onboarding-flow" || result.BaselineID != "base_onboarding_001" {
		t.Fatalf("result = %#v, want requested session and baseline", result)
	}
	if result.InteractionCount != 1 {
		t.Fatalf("result.InteractionCount = %d, want 1", result.InteractionCount)
	}
	if result.FirstRunReport == "" {
		t.Fatal("result.FirstRunReport is empty")
	}
	if result.NextCommand != "stagehand test --session onboarding-flow -- "+strings.Join(helperCommand("record-write-capture"), " ") {
		t.Fatalf("result.NextCommand = %q", result.NextCommand)
	}
	if len(result.CaptureSummary) != 1 || result.CaptureSummary[0].Service != "openai" || result.CaptureSummary[0].Operation != "chat.completions.create" || result.CaptureSummary[0].Count != 1 {
		t.Fatalf("result.CaptureSummary = %#v, want one openai chat completion", result.CaptureSummary)
	}

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()
	baseline, err := sqliteStore.GetBaseline(context.Background(), "base_onboarding_001")
	if err != nil {
		t.Fatalf("GetBaseline() error = %v", err)
	}
	if baseline.SourceRunID != result.RunID || baseline.SessionName != "onboarding-flow" {
		t.Fatalf("baseline = %#v, want promoted record run", baseline)
	}
	reportPath := result.FirstRunReport
	if !filepath.IsAbs(reportPath) {
		reportPath = filepath.Join(workdir, reportPath)
	}
	reportData, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", reportPath, err)
	}
	report := string(reportData)
	for _, want := range []string{
		"# Stagehand First Run",
		"stagehand inspect --run-id " + result.RunID + " --show-bodies",
		result.NextCommand,
		"stagehand ci setup --session onboarding-flow",
		"`openai` `chat.completions.create`: 1",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("first-run report = %q, want %q", report, want)
		}
	}
}

func TestRunRecordBaselineFailsOnEmptyCapture(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	args := []string{
		"record-baseline",
		"--session", "empty-flow",
		"--config", configPath,
		"--",
	}
	args = append(args, helperCommand("record-write-empty-capture")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(args, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(record-baseline empty) expected error")
	}
	if !strings.Contains(err.Error(), "captured zero interactions") || !strings.Contains(err.Error(), "stagehand.tool") {
		t.Fatalf("run(record-baseline empty) error = %v, want actionable empty capture message", err)
	}
}

func TestRunRecordRequiresCommand(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(filepath.Join(workdir, ".stagehand", "runs"))+"\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"record", "--session", "onboarding-flow", "--config", configPath}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(record) expected managed command validation error")
	}
	if !strings.Contains(err.Error(), "record requires a command after --") {
		t.Fatalf("run(record) error = %v", err)
	}
}

func TestRunRecordRunsManagedCommandPersistsInteractionsAndEmitsJSON(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	args := []string{
		"record",
		"--session", "onboarding-flow",
		"--config", configPath,
		"--",
	}
	args = append(args, helperCommand("record-write-capture")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run(record) error = %v\nstderr=%s", err, stderr.String())
	}

	result := decodeJSONOutput[recordResult](t, stdout.Bytes())
	if result.Mode != "record" {
		t.Fatalf("result.Mode = %q, want record", result.Mode)
	}
	if result.SessionName != "onboarding-flow" {
		t.Fatalf("result.SessionName = %q, want onboarding-flow", result.SessionName)
	}
	if result.Status != "complete" {
		t.Fatalf("result.Status = %q, want complete", result.Status)
	}
	if result.InteractionCount != 1 {
		t.Fatalf("result.InteractionCount = %d, want 1", result.InteractionCount)
	}
	if result.EmptyRun {
		t.Fatal("result.EmptyRun = true, want false for recorded interaction")
	}

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	gotRun, err := sqliteStore.GetRun(context.Background(), result.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Mode != recorder.RunModeRecord {
		t.Fatalf("GetRun().Mode = %q, want %q", gotRun.Mode, recorder.RunModeRecord)
	}
	if gotRun.Status != recorder.RunStatusComplete {
		t.Fatalf("GetRun().Status = %q, want %q", gotRun.Status, recorder.RunStatusComplete)
	}
	if gotRun.Metadata["mode"] != "record" || gotRun.Metadata["session"] != "onboarding-flow" {
		t.Fatalf("GetRun().Metadata = %#v, want captured bundle metadata", gotRun.Metadata)
	}
	if len(gotRun.Interactions) != 1 {
		t.Fatalf("len(GetRun().Interactions) = %d, want 1", len(gotRun.Interactions))
	}

	interaction := gotRun.Interactions[0]
	if interaction.RunID != result.RunID {
		t.Fatalf("Interaction.RunID = %q, want %q", interaction.RunID, result.RunID)
	}
	if _, ok := interaction.Request.Headers["authorization"]; ok {
		t.Fatal("authorization header should have been scrubbed before persistence")
	}
	if interaction.ScrubReport.ScrubPolicyVersion != "v1" {
		t.Fatalf(
			"Interaction.ScrubReport.ScrubPolicyVersion = %q, want v1",
			interaction.ScrubReport.ScrubPolicyVersion,
		)
	}
}

func TestRunRecordPassesErrorInjectionToManagedCommandAndPersistsProvenance(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	injectionPath := filepath.Join(workdir, "error-injection.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")
	writeFile(t, injectionPath, `schema_version: v1alpha1
error_injection:
  rules:
    - name: refund fails first
      match:
        service: stripe
        operation: POST /v1/refunds
        nth_call: 1
      inject:
        library: stripe.card_declined
`)

	args := []string{
		"record",
		"--session", "inject-flow",
		"--config", configPath,
		"--error-injection", injectionPath,
		"--",
	}
	args = append(args, helperCommand("record-write-injection-capture")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run(record --error-injection) error = %v\nstderr=%s", err, stderr.String())
	}

	result := decodeJSONOutput[recordResult](t, stdout.Bytes())
	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	gotRun, err := sqliteStore.GetRun(context.Background(), result.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	applied := injectedMetadataEntries(t, gotRun.Metadata)
	if len(applied) != 1 {
		t.Fatalf("applied injection metadata len = %d, want 1: %#v", len(applied), applied)
	}
	entry, ok := applied[0].(map[string]any)
	if !ok {
		t.Fatalf("applied injection metadata entry = %#v, want object", applied[0])
	}
	if entry["name"] != "refund fails first" || entry["status"].(float64) != 402 {
		t.Fatalf("applied injection metadata = %#v, want named 402 override", entry)
	}
	if len(gotRun.Interactions) != 1 {
		t.Fatalf("len(interactions) = %d, want 1", len(gotRun.Interactions))
	}
	if gotRun.Interactions[0].Service != "stripe" || gotRun.Interactions[0].Operation != "POST /v1/refunds" {
		t.Fatalf("interaction route = %s %s, want stripe POST /v1/refunds", gotRun.Interactions[0].Service, gotRun.Interactions[0].Operation)
	}
}

func TestLoadSDKErrorInjectionBundleAcceptsGenericHTTPShortcutShape(t *testing.T) {
	workdir := t.TempDir()
	injectionPath := filepath.Join(workdir, "error-injection.yml")
	writeFile(t, injectionPath, `schema_version: v1alpha1
error_injection:
  - name: CRM timeout on third lookup
    service: internal-crm
    operation: POST /v1/customers/search
    nth_call: 3
    response:
      error: timeout
      latency_ms: 25
  - name: Billing API returns malformed 500
    service: internal-billing
    operation: POST /api/refunds
    probability: 1.0
    response:
      status: 500
      body: "{malformed-json"
`)

	bundle, err := loadSDKErrorInjectionBundle(injectionPath)
	if err != nil {
		t.Fatalf("loadSDKErrorInjectionBundle() error = %v", err)
	}
	if len(bundle.Rules) != 2 {
		t.Fatalf("len(bundle.Rules) = %d, want 2", len(bundle.Rules))
	}
	if bundle.Rules[0].Inject.Error != "timeout" || bundle.Rules[0].Inject.LatencyMS != 25 {
		t.Fatalf("timeout inject = %#v, want timeout latency", bundle.Rules[0].Inject)
	}
	if bundle.Rules[1].Inject.Status != 500 || bundle.Rules[1].Inject.Body != "{malformed-json" {
		t.Fatalf("status inject = %#v, want malformed 500", bundle.Rules[1].Inject)
	}
}

func TestLoadSDKErrorInjectionBundleAcceptsToolShortcutShape(t *testing.T) {
	workdir := t.TempDir()
	injectionPath := filepath.Join(workdir, "error-injection.yml")
	writeFile(t, injectionPath, `schema_version: v1alpha1
error_injection:
  - name: lookup failure on second call
    tool: lookup_customer
    nth_call: 2
    probability: 1.0
    error:
      type: not_found
      message: customer missing
      class_name: CustomerNotFoundError
`)

	bundle, err := loadSDKErrorInjectionBundle(injectionPath)
	if err != nil {
		t.Fatalf("loadSDKErrorInjectionBundle() error = %v", err)
	}
	if len(bundle.Rules) != 1 {
		t.Fatalf("len(bundle.Rules) = %d, want 1", len(bundle.Rules))
	}
	rule := bundle.Rules[0]
	if rule.Match.Tool != "lookup_customer" || rule.Match.NthCall != 2 {
		t.Fatalf("tool match = %#v, want lookup_customer nth 2", rule.Match)
	}
	body, ok := rule.Inject.Body.(map[string]any)
	if !ok {
		t.Fatalf("inject body = %#v, want map", rule.Inject.Body)
	}
	if rule.Inject.Error != "not_found" || body["message"] != "customer missing" || body["error_class"] != "CustomerNotFoundError" {
		t.Fatalf("tool inject = %#v body=%#v, want typed not_found", rule.Inject, body)
	}
}

func TestRunRecordTreatsEmptyCaptureBundleAsCompleteEmptyRun(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	args := []string{
		"record",
		"--session", "empty-flow",
		"--config", configPath,
		"--",
	}
	args = append(args, helperCommand("record-write-empty-capture")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run(record empty) error = %v\nstderr=%s", err, stderr.String())
	}

	result := decodeJSONOutput[recordResult](t, stdout.Bytes())
	if result.Status != "complete" {
		t.Fatalf("result.Status = %q, want complete", result.Status)
	}
	if result.InteractionCount != 0 {
		t.Fatalf("result.InteractionCount = %d, want 0", result.InteractionCount)
	}
	if !result.EmptyRun {
		t.Fatal("result.EmptyRun = false, want true")
	}

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	gotRun, err := sqliteStore.GetRun(context.Background(), result.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Status != recorder.RunStatusComplete {
		t.Fatalf("GetRun().Status = %q, want %q", gotRun.Status, recorder.RunStatusComplete)
	}
	if len(gotRun.Interactions) != 0 {
		t.Fatalf("len(GetRun().Interactions) = %d, want 0", len(gotRun.Interactions))
	}
}

func TestRunRecordSurfacesCaptureBundleWriteFailure(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	args := []string{
		"record",
		"--session", "failing-flow",
		"--config", configPath,
		"--",
	}
	args = append(args, helperCommand("record-fail-write-capture")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(args, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(record failing helper) expected error")
	}
	if !strings.Contains(err.Error(), `run managed command "`) {
		t.Fatalf("run(record failing helper) error = %v", err)
	}
	if !strings.Contains(stderr.String(), "simulated capture bundle write failure:") {
		t.Fatalf("stderr missing concrete capture bundle failure\nstderr=%s", stderr.String())
	}

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	runRecord, err := sqliteStore.GetLatestRunRecord(context.Background(), "failing-flow")
	if err != nil {
		t.Fatalf("GetLatestRunRecord() error = %v", err)
	}
	if runRecord.Status != store.RunLifecycleStatusIncomplete {
		t.Fatalf("run status = %q, want %q", runRecord.Status, store.RunLifecycleStatusIncomplete)
	}
	if len(runRecord.IntegrityIssues) != 1 || runRecord.IntegrityIssues[0].Code != recorder.IntegrityIssueRecorderShutdown {
		t.Fatalf("integrity issues = %#v, want recorder_shutdown", runRecord.IntegrityIssues)
	}
}

func TestRunRecordRejectsInvalidImportBeforePersistingAnyInteractions(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	args := []string{
		"record",
		"--session", "invalid-import-flow",
		"--config", configPath,
		"--",
	}
	args = append(args, helperCommand("record-write-invalid-capture")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(args, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(record invalid import) expected error")
	}
	if !strings.Contains(err.Error(), "validate imported interaction") {
		t.Fatalf("run(record invalid import) error = %v, want validation context", err)
	}

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	runRecord, err := sqliteStore.GetLatestRunRecord(context.Background(), "invalid-import-flow")
	if err != nil {
		t.Fatalf("GetLatestRunRecord() error = %v", err)
	}
	if runRecord.Status != store.RunLifecycleStatusCorrupted {
		t.Fatalf("run status = %q, want %q", runRecord.Status, store.RunLifecycleStatusCorrupted)
	}
	if len(runRecord.IntegrityIssues) != 1 || runRecord.IntegrityIssues[0].Code != recorder.IntegrityIssueSchemaValidation {
		t.Fatalf("integrity issues = %#v, want schema_validation_failed", runRecord.IntegrityIssues)
	}

	interactions, err := sqliteStore.ListInteractions(context.Background(), runRecord.RunID)
	if err != nil {
		t.Fatalf("ListInteractions() error = %v", err)
	}
	if len(interactions) != 0 {
		t.Fatalf("len(ListInteractions()) = %d, want 0 after preflight import rejection", len(interactions))
	}
}

func TestFinalizeRunUsesExplicitRuntimeFailureClassification(t *testing.T) {
	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(t.TempDir(), "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	run, err := newRunRecordForMode("classified-flow", minimalConfig(), recorder.RunModeRecord)
	if err != nil {
		t.Fatalf("newRunRecordForMode() error = %v", err)
	}
	if err := sqliteStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	runErr := schemaValidationFailure("validate imported bundle", fmt.Errorf("bad interaction"))
	if err := finalizeRun(context.Background(), sqliteStore, run, runErr); err != nil {
		t.Fatalf("finalizeRun() error = %v", err)
	}

	gotRecord, err := sqliteStore.GetRunRecord(context.Background(), run.RunID)
	if err != nil {
		t.Fatalf("GetRunRecord() error = %v", err)
	}
	if gotRecord.Status != store.RunLifecycleStatusCorrupted {
		t.Fatalf("Status = %q, want %q", gotRecord.Status, store.RunLifecycleStatusCorrupted)
	}
	if len(gotRecord.IntegrityIssues) != 1 || gotRecord.IntegrityIssues[0].Code != recorder.IntegrityIssueSchemaValidation {
		t.Fatalf("IntegrityIssues = %#v, want schema validation issue", gotRecord.IntegrityIssues)
	}
}

func TestSQLiteDatabasePathUsesFileWhenAlreadyFileLike(t *testing.T) {
	got := sqliteDatabasePath(filepath.Join("tmp", "custom.sqlite"))
	want := filepath.Join("tmp", "custom.sqlite")
	if got != want {
		t.Fatalf("sqliteDatabasePath() = %q, want %q", got, want)
	}
}

func TestRunReplayRequiresSingleSelector(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run([]string{"replay"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(replay) expected selector validation error")
	}
	if !strings.Contains(err.Error(), "exactly one of --run-id or --session") {
		t.Fatalf("run(replay) error = %v", err)
	}
}

func TestRunReplayRequiresCommand(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(filepath.Join(workdir, ".stagehand", "runs"))+"\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"replay", "--run-id", "run_demo", "--config", configPath}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(replay) expected managed command validation error")
	}
	if !strings.Contains(err.Error(), "replay requires a command after --") {
		t.Fatalf("run(replay) error = %v", err)
	}
}

func TestRunReplayRunsManagedCommandStoresReplayRunAndEmitsJSON(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	sourceRunID := seedReplayRun(t, sqliteStore, "onboarding-flow")

	args := []string{
		"replay",
		"--run-id", sourceRunID,
		"--config", configPath,
		"--",
	}
	args = append(args, helperCommand("replay-consume-seed-and-write-capture")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run(replay) error = %v\nstderr=%s", err, stderr.String())
	}

	result := decodeJSONOutput[replayCommandResult](t, stdout.Bytes())
	if result.Mode != "replay" {
		t.Fatalf("result.Mode = %q, want replay", result.Mode)
	}
	if result.SourceRunID != sourceRunID {
		t.Fatalf("result.SourceRunID = %q, want %q", result.SourceRunID, sourceRunID)
	}
	if result.ReplayRunID == "" {
		t.Fatal("result.ReplayRunID is empty")
	}
	if result.Status != "complete" {
		t.Fatalf("result.Status = %q, want complete", result.Status)
	}
	if result.ReplayInteractionCount != 1 {
		t.Fatalf("result.ReplayInteractionCount = %d, want 1", result.ReplayInteractionCount)
	}

	replayRun, err := sqliteStore.GetRun(context.Background(), result.ReplayRunID)
	if err != nil {
		t.Fatalf("GetRun(replay) error = %v", err)
	}
	if replayRun.Mode != recorder.RunModeReplay {
		t.Fatalf("replayRun.Mode = %q, want %q", replayRun.Mode, recorder.RunModeReplay)
	}
	if len(replayRun.Interactions) != 1 {
		t.Fatalf("len(replayRun.Interactions) = %d, want 1", len(replayRun.Interactions))
	}
}

func TestRunTestReplaysBaselineWritesReportsAndPasses(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	t.Chdir(workdir)
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	sourceRunID := seedReplayRun(t, sqliteStore, "onboarding-flow")
	if err := sqliteStore.PutBaseline(context.Background(), store.Baseline{
		BaselineID:  "base_onboarding_latest",
		SessionName: "onboarding-flow",
		SourceRunID: sourceRunID,
		GitSHA:      "abc123",
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("PutBaseline() error = %v", err)
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	args := []string{"test", "--session", "onboarding-flow", "--config", configPath, "--"}
	args = append(args, helperCommand("replay-consume-seed-and-write-capture")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run(test) error = %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"Stagehand test: passed",
		"Diff: 0 failing, 0 informational, 0 total",
		"Reports:",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
	reportDir := filepath.Join(workdir, ".stagehand", "reports")
	entries, err := os.ReadDir(reportDir)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", reportDir, err)
	}
	var jsonReports, markdownReports int
	for _, entry := range entries {
		switch filepath.Ext(entry.Name()) {
		case ".json":
			jsonReports++
		case ".md":
			markdownReports++
		}
	}
	if jsonReports != 1 || markdownReports != 1 {
		t.Fatalf("report files json=%d markdown=%d entries=%v, want one each", jsonReports, markdownReports, entries)
	}
}

func TestRunTestReturnsReplayFailureExitCode(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	t.Chdir(workdir)
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	sourceRunID := seedReplayFailureRun(t, sqliteStore, "failure-flow")
	if err := sqliteStore.PutBaseline(context.Background(), store.Baseline{
		BaselineID:  "base_failure",
		SessionName: "failure-flow",
		SourceRunID: sourceRunID,
		GitSHA:      "abc123",
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("PutBaseline() error = %v", err)
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	args := []string{"test", "--session", "failure-flow", "--config", configPath, "--"}
	args = append(args, helperCommand("replay-fail-on-terminal-error")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err = run(args, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(test replay failure) expected error")
	}
	if exitCodeForError(err) != exitCodeReplayFailure {
		t.Fatalf("exitCodeForError() = %d, want %d; err=%v", exitCodeForError(err), exitCodeReplayFailure, err)
	}
}

func TestRunTestReturnsBehaviorDiffExitCodeAndAppliesErrorInjection(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	t.Chdir(workdir)
	configPath := filepath.Join(workdir, "stagehand.yml")
	injectionPath := filepath.Join(workdir, "error-injection.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")
	writeFile(t, injectionPath, `schema_version: v1alpha1
error_injection:
  rules:
    - name: refund fails
      match:
        service: stripe
        operation: POST /v1/refunds
        nth_call: 3
      inject:
        status: 402
        body:
          error:
            type: card_error
`)

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	sourceRunID := seedReplayRun(t, sqliteStore, "diff-flow")
	if err := sqliteStore.PutBaseline(context.Background(), store.Baseline{
		BaselineID:  "base_diff",
		SessionName: "diff-flow",
		SourceRunID: sourceRunID,
		GitSHA:      "abc123",
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("PutBaseline() error = %v", err)
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	args := []string{"test", "--session", "diff-flow", "--config", configPath, "--error-injection", injectionPath, "--"}
	args = append(args, helperCommand("replay-write-injection-capture")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err = run(args, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(test behavior diff) expected error")
	}
	if exitCodeForError(err) != exitCodeBehaviorDiff {
		t.Fatalf("exitCodeForError() = %d, want %d; err=%v", exitCodeForError(err), exitCodeBehaviorDiff, err)
	}
	if !strings.Contains(stdout.String(), "Stagehand test: failed") || !strings.Contains(stdout.String(), "Diff:") {
		t.Fatalf("stdout = %q, want failed diff report", stdout.String())
	}
	reportDir := filepath.Join(workdir, ".stagehand", "reports")
	entries, readErr := os.ReadDir(reportDir)
	if readErr != nil || len(entries) == 0 {
		t.Fatalf("reports not written entries=%v err=%v", entries, readErr)
	}
}

func TestRunTestReturnsAssertionFailureExitCode(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	t.Chdir(workdir)
	configPath := filepath.Join(workdir, "stagehand.yml")
	assertionsPath := filepath.Join(workdir, "assertions.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")
	writeFile(t, assertionsPath, `schema_version: v1alpha1
assertions:
  - id: no-openai
    type: forbidden-operation
    match:
      service: openai
      operation: chat.completions.create
`)

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	sourceRunID := seedReplayRun(t, sqliteStore, "assert-flow")
	if err := sqliteStore.PutBaseline(context.Background(), store.Baseline{
		BaselineID:  "base_assert",
		SessionName: "assert-flow",
		SourceRunID: sourceRunID,
		GitSHA:      "abc123",
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("PutBaseline() error = %v", err)
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	args := []string{"test", "--session", "assert-flow", "--config", configPath, "--assertions", assertionsPath, "--"}
	args = append(args, helperCommand("replay-consume-seed-and-write-capture")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err = run(args, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(test assertion failure) expected error")
	}
	if exitCodeForError(err) != exitCodeAssertionFailure {
		t.Fatalf("exitCodeForError() = %d, want %d; err=%v", exitCodeForError(err), exitCodeAssertionFailure, err)
	}
	if !strings.Contains(stdout.String(), "Assertions: 0 passed, 1 failed") {
		t.Fatalf("stdout = %q, want assertion failure summary", stdout.String())
	}
}

func TestRunReplayPassesErrorInjectionToManagedCommandAndPersistsProvenance(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	injectionPath := filepath.Join(workdir, "error-injection.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")
	writeFile(t, injectionPath, `schema_version: v1alpha1
error_injection:
  rules:
    - name: third refund fails
      match:
        service: stripe
        operation: POST /v1/refunds
        nth_call: 3
      inject:
        status: 402
        body:
          error:
            type: card_error
            code: card_declined
            message: Your card was declined.
`)

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	sourceRunID := seedReplayRun(t, sqliteStore, "inject-replay-flow")
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	args := []string{
		"replay",
		"--run-id", sourceRunID,
		"--config", configPath,
		"--error-injection", injectionPath,
		"--",
	}
	args = append(args, helperCommand("replay-write-injection-capture")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run(replay --error-injection) error = %v\nstderr=%s", err, stderr.String())
	}

	result := decodeJSONOutput[replayCommandResult](t, stdout.Bytes())
	sqliteStore, err = sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	gotRun, err := sqliteStore.GetRun(context.Background(), result.ReplayRunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	applied := injectedMetadataEntries(t, gotRun.Metadata)
	if len(applied) != 1 {
		t.Fatalf("applied injection metadata len = %d, want 1: %#v", len(applied), applied)
	}
	entry, ok := applied[0].(map[string]any)
	if !ok {
		t.Fatalf("applied injection metadata entry = %#v, want object", applied[0])
	}
	if entry["call_number"].(float64) != 3 {
		t.Fatalf("applied call_number = %#v, want 3", entry["call_number"])
	}
	if result.ReplayInteractionCount != 1 {
		t.Fatalf("ReplayInteractionCount = %d, want 1", result.ReplayInteractionCount)
	}
}

func TestRunReplayLoadsLatestCompleteRunBySession(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	baseStartedAt := time.Date(2026, time.April, 22, 10, 0, 0, 0, time.UTC)
	completeRunID := seedReplayRunAt(t, sqliteStore, "onboarding-flow", baseStartedAt)
	seedCorruptedReplayCandidate(t, sqliteStore, "onboarding-flow", baseStartedAt.Add(10*time.Minute))

	args := []string{
		"replay",
		"--session", "onboarding-flow",
		"--config", configPath,
		"--",
	}
	args = append(args, helperCommand("replay-consume-seed-and-write-capture")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run(replay by session) error = %v\nstderr=%s", err, stderr.String())
	}

	result := decodeJSONOutput[replayCommandResult](t, stdout.Bytes())
	if result.SourceRunID != completeRunID {
		t.Fatalf("result.SourceRunID = %q, want latest complete run %q", result.SourceRunID, completeRunID)
	}
}

func TestRunReplaySurfacesControlledFailureForStoredFailureInteraction(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	sourceRunID := seedReplayFailureRun(t, sqliteStore, "failure-flow")

	args := []string{
		"replay",
		"--run-id", sourceRunID,
		"--config", configPath,
		"--",
	}
	args = append(args, helperCommand("replay-fail-on-terminal-error")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err = run(args, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(replay failing source) expected error")
	}
	if !strings.Contains(err.Error(), `run managed command "`) {
		t.Fatalf("run(replay failing source) error = %v", err)
	}
	if !strings.Contains(stderr.String(), "controlled replay failure: matched source interaction ended with error") {
		t.Fatalf("stderr missing controlled replay failure\nstderr=%s", stderr.String())
	}
}

func TestRunBaselinePromoteCreatesLatestBaseline(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	sourceRunID := seedReplayRun(t, sqliteStore, "baseline-flow")
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	args := []string{
		"baseline",
		"promote",
		"--run-id", sourceRunID,
		"--baseline-id", "base_cli_001",
		"--git-sha", "abc123",
		"--config", configPath,
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run(baseline promote) error = %v\nstderr=%s", err, stderr.String())
	}

	result := decodeJSONOutput[baselinePromoteResult](t, stdout.Bytes())
	if result.Mode != "baseline_promote" {
		t.Fatalf("result.Mode = %q, want baseline_promote", result.Mode)
	}
	if result.BaselineID != "base_cli_001" {
		t.Fatalf("result.BaselineID = %q, want base_cli_001", result.BaselineID)
	}
	if result.SourceRunID != sourceRunID {
		t.Fatalf("result.SourceRunID = %q, want %q", result.SourceRunID, sourceRunID)
	}
	if result.SessionName != "baseline-flow" {
		t.Fatalf("result.SessionName = %q, want baseline-flow", result.SessionName)
	}
	if result.GitSHA != "abc123" {
		t.Fatalf("result.GitSHA = %q, want abc123", result.GitSHA)
	}

	sqliteStore, err = sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore(reopen) error = %v", err)
	}
	defer sqliteStore.Close()

	latest, err := sqliteStore.GetLatestBaseline(context.Background(), "baseline-flow")
	if err != nil {
		t.Fatalf("GetLatestBaseline() error = %v", err)
	}
	if latest.BaselineID != "base_cli_001" || latest.SourceRunID != sourceRunID || latest.GitSHA != "abc123" {
		t.Fatalf("latest baseline = %#v, want promoted source", latest)
	}
}

func TestRunBaselinePromoteRejectsIncompleteRuns(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	runRecord, err := newRunRecordForMode("baseline-incomplete", minimalConfig(), recorder.RunModeRecord)
	if err != nil {
		t.Fatalf("newRunRecordForMode() error = %v", err)
	}
	if err := sqliteStore.CreateRun(context.Background(), runRecord); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	args := []string{
		"baseline",
		"promote",
		"--run-id", runRecord.RunID,
		"--baseline-id", "base_incomplete",
		"--git-sha", "abc123",
		"--config", configPath,
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err = run(args, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(baseline promote incomplete) expected error")
	}
	if !strings.Contains(err.Error(), "must be \"complete\"") {
		t.Fatalf("run(baseline promote incomplete) error = %v, want complete-run eligibility", err)
	}
}

func TestRunBaselineShowResolvesLatestSessionBaseline(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	sourceRunID := seedReplayRun(t, sqliteStore, "baseline-show-flow")
	if err := sqliteStore.PutBaseline(context.Background(), store.Baseline{
		BaselineID:  "base_show_001",
		SessionName: "baseline-show-flow",
		SourceRunID: sourceRunID,
		GitSHA:      "abc123",
		CreatedAt:   time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("PutBaseline() error = %v", err)
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"baseline", "show", "--session", "baseline-show-flow", "--config", configPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run(baseline show) error = %v\nstderr=%s", err, stderr.String())
	}

	result := decodeJSONOutput[baselineShowResult](t, stdout.Bytes())
	if result.Mode != "baseline_show" {
		t.Fatalf("result.Mode = %q, want baseline_show", result.Mode)
	}
	if result.BaselineID != "base_show_001" || result.SourceRunID != sourceRunID {
		t.Fatalf("baseline show result = %#v, want latest baseline source", result)
	}
}

func TestRunDiffRendersJSONAgainstLatestBaseline(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	baseRunID := seedDiffRun(t, sqliteStore, "diff-flow", "hello", recorder.FallbackTierExact, time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC))
	candidateRunID := seedDiffRun(t, sqliteStore, "diff-flow", "goodbye", recorder.FallbackTierExact, time.Date(2026, time.April, 25, 12, 5, 0, 0, time.UTC))
	if err := sqliteStore.PutBaseline(context.Background(), store.Baseline{
		BaselineID:  "base_diff_001",
		SessionName: "diff-flow",
		SourceRunID: baseRunID,
		GitSHA:      "abc123",
		CreatedAt:   time.Date(2026, time.April, 25, 12, 10, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("PutBaseline() error = %v", err)
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"diff", "--session", "diff-flow", "--candidate-run-id", candidateRunID, "--format", "json", "--config", configPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run(diff) error = %v\nstderr=%s", err, stderr.String())
	}

	report := decodeJSONOutput[analysisdiff.JSONReport](t, stdout.Bytes())
	if report.BaseRunID != baseRunID || report.CandidateRunID != candidateRunID {
		t.Fatalf("diff report base/candidate = %q/%q, want %q/%q", report.BaseRunID, report.CandidateRunID, baseRunID, candidateRunID)
	}
	if report.Summary.FailingChanges != 1 || len(report.FailingChanges) != 1 {
		t.Fatalf("diff failing changes = %#v, want one failing modification", report.FailingChanges)
	}
	if report.FailingChanges[0].Type != analysisdiff.ChangeModified {
		t.Fatalf("failing change type = %q, want %q", report.FailingChanges[0].Type, analysisdiff.ChangeModified)
	}
}

func TestServiceIgnoredFieldsFromConfigSkipsBlankNamesAndEmptyIgnores(t *testing.T) {
	got := serviceIgnoredFieldsFromConfig([]config.ServiceConfig{
		{Name: "internal-billing", Ignore: config.ServiceIgnoreConfig{
			RequestPaths:  []string{"headers.x-request-id", "body.request_id"},
			ResponsePaths: []string{"body.generated_at"},
		}},
		{Name: "billing-api"},
		{Name: "  ", Ignore: config.ServiceIgnoreConfig{
			RequestPaths: []string{"body.skip"},
		}},
	})

	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1 (blank-name and ignore-less services should be skipped): %#v", len(got), got)
	}
	billing, ok := got["internal-billing"]
	if !ok {
		t.Fatalf("got = %#v, want internal-billing entry", got)
	}
	if len(billing.RequestPaths) != 2 || billing.RequestPaths[0] != "headers.x-request-id" || billing.RequestPaths[1] != "body.request_id" {
		t.Fatalf("RequestPaths = %#v, want [headers.x-request-id body.request_id]", billing.RequestPaths)
	}
	if len(billing.ResponsePaths) != 1 || billing.ResponsePaths[0] != "body.generated_at" {
		t.Fatalf("ResponsePaths = %#v, want [body.generated_at]", billing.ResponsePaths)
	}
}

func TestServiceIgnoredFieldsFromConfigCopiesSlicesToBlockMutation(t *testing.T) {
	requestPaths := []string{"body.request_id"}
	services := []config.ServiceConfig{
		{Name: "internal-billing", Ignore: config.ServiceIgnoreConfig{
			RequestPaths: requestPaths,
		}},
	}

	got := serviceIgnoredFieldsFromConfig(services)
	requestPaths[0] = "body.MUTATED"

	if got["internal-billing"].RequestPaths[0] != "body.request_id" {
		t.Fatalf("returned slice aliased the source slice; got %q", got["internal-billing"].RequestPaths[0])
	}
}

func TestRunDiffRequiresExactlyOneBaseSelector(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run([]string{"diff", "--candidate-run-id", "run_candidate"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(diff) expected selector validation error")
	}
	if !strings.Contains(err.Error(), "exactly one of --base-run-id, --baseline-id, or --session") {
		t.Fatalf("run(diff) error = %v", err)
	}
}

func TestRunAssertRendersJSONFailures(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	assertionsPath := filepath.Join(workdir, "stagehand.assertions.yml")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")
	writeFile(t, assertionsPath, `schema_version: v1alpha1
assertions:
  - id: no-openai-calls
    type: forbidden-operation
    match:
      service: openai
      operation: chat.completions.create
`)

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	runID := seedReplayRun(t, sqliteStore, "assert-flow")
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"assert", "--run-id", runID, "--assertions", assertionsPath, "--format", "json", "--config", configPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run(assert) error = %v\nstderr=%s", err, stderr.String())
	}

	report := decodeJSONOutput[assertionReport](t, stdout.Bytes())
	if report.RunID != runID {
		t.Fatalf("assert report RunID = %q, want %q", report.RunID, runID)
	}
	if report.Summary.Failed != 1 || len(report.Results) != 1 {
		t.Fatalf("assert summary/results = %#v/%#v, want one failure", report.Summary, report.Results)
	}
}

func TestRunConformanceRunWritesOutputsForSkippedCases(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	casesPath := filepath.Join(workdir, "conformance.yml")
	outputDir := filepath.Join(workdir, "conformance-results")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")
	writeFile(t, casesPath, `schema_version: v1alpha1
cases:
  - id: openai-smoke
    service: openai
    inputs:
      steps:
        - id: chat
          operation: chat.completions.create
          request:
            model: gpt-5.4-mini
    real_service:
      credentials:
        - env: OPENAI_API_KEY
          required: true
          purpose: OpenAI test key.
    comparison:
      match:
        strategy: operation_sequence
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"conformance", "run", "--cases", casesPath, "--config", configPath, "--output-dir", outputDir, "--format", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(conformance run) error = %v\nstderr=%s", err, stderr.String())
	}

	report := decodeJSONOutput[conformanceReportForTest](t, stdout.Bytes())
	if report.Summary.Skipped != 1 {
		t.Fatalf("Skipped = %d, want 1", report.Summary.Skipped)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "conformance-results.json")); err != nil {
		t.Fatalf("conformance-results.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "conformance-summary.md")); err != nil {
		t.Fatalf("conformance-summary.md missing: %v", err)
	}
}

type conformanceReportForTest struct {
	Summary struct {
		Skipped int `json:"skipped"`
	} `json:"summary"`
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	mode := helperModeFromArgs(os.Args)
	switch mode {
	case "record-write-capture":
		capturePath := mustEnv(t, envStagehandCaptureOut)
		if err := writeInteractionBundle(capturePath, interactionBundle{
			BundleVersion: interactionBundleVersion,
			Metadata: map[string]any{
				"mode":    os.Getenv(envStagehandMode),
				"session": os.Getenv(envStagehandSession),
			},
			Interactions: helperCapturedInteractions(),
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(0)
	case "record-write-empty-capture":
		capturePath := mustEnv(t, envStagehandCaptureOut)
		writeFile(t, capturePath, "{\n  \"bundle_version\": \"v1alpha1\",\n  \"metadata\": {\n    \"mode\": \"record\",\n    \"session\": \"empty-flow\"\n  },\n  \"interactions\": []\n}\n")
		os.Exit(0)
	case "record-fail-write-capture":
		capturePath := mustEnv(t, envStagehandCaptureOut)
		if err := os.Mkdir(capturePath, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(6)
		}
		if err := writeInteractionBundle(capturePath, interactionBundle{
			BundleVersion: interactionBundleVersion,
			Metadata: map[string]any{
				"mode":    os.Getenv(envStagehandMode),
				"session": os.Getenv(envStagehandSession),
			},
			Interactions: helperCapturedInteractions(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "simulated capture bundle write failure: %v\n", err)
			os.Exit(6)
		}
		os.Exit(0)
	case "record-write-invalid-capture":
		capturePath := mustEnv(t, envStagehandCaptureOut)
		interactions := helperCapturedInteractions()
		invalid := interactions[0]
		invalid.InteractionID = "int_helper_invalid"
		invalid.Sequence = 2
		invalid.Events = invalid.Events[:1]
		interactions = append(interactions, invalid)
		if err := writeInteractionBundle(capturePath, interactionBundle{
			BundleVersion: interactionBundleVersion,
			Metadata: map[string]any{
				"mode":    os.Getenv(envStagehandMode),
				"session": os.Getenv(envStagehandSession),
			},
			Interactions: interactions,
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(8)
		}
		os.Exit(0)
	case "record-write-injection-capture":
		capturePath := mustEnv(t, envStagehandCaptureOut)
		injectionPath := mustEnv(t, envStagehandInjectInput)
		bundle := helperAppliedInjectionBundle(t, injectionPath, 1)
		if err := writeInteractionBundle(capturePath, bundle); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(9)
		}
		os.Exit(0)
	case "replay-consume-seed-and-write-capture":
		seedPath := mustEnv(t, envStagehandReplayInput)
		capturePath := mustEnv(t, envStagehandCaptureOut)
		bundle, err := loadInteractionBundle(seedPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		if err := writeInteractionBundle(capturePath, interactionBundle{
			BundleVersion: interactionBundleVersion,
			Metadata: map[string]any{
				"mode":    os.Getenv(envStagehandMode),
				"session": os.Getenv(envStagehandSession),
			},
			Interactions: bundle.Interactions,
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(4)
		}
		os.Exit(0)
	case "replay-write-injection-capture":
		_ = mustEnv(t, envStagehandReplayInput)
		capturePath := mustEnv(t, envStagehandCaptureOut)
		injectionPath := mustEnv(t, envStagehandInjectInput)
		bundle := helperAppliedInjectionBundle(t, injectionPath, 3)
		if err := writeInteractionBundle(capturePath, bundle); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(10)
		}
		os.Exit(0)
	case "replay-fail-on-terminal-error":
		seedPath := mustEnv(t, envStagehandReplayInput)
		if bundle, err := loadInteractionBundle(seedPath); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(7)
		} else {
			for _, interaction := range bundle.Interactions {
				for _, event := range interaction.Events {
					if event.Type == recorder.EventTypeError || event.Type == recorder.EventTypeTimeout {
						fmt.Fprintf(os.Stderr, "controlled replay failure: matched source interaction ended with %s\n", event.Type)
						os.Exit(7)
					}
				}
			}
		}
		fmt.Fprintln(os.Stderr, "controlled replay failure helper expected a terminal failure interaction")
		os.Exit(7)
	default:
		fmt.Fprintf(os.Stderr, "unknown helper mode %q\n", mode)
		os.Exit(5)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func toSlash(path string) string {
	return strings.ReplaceAll(path, `\`, `/`)
}

func seedReplayRun(t *testing.T, artifactStore store.ArtifactStore, session string) string {
	t.Helper()

	return seedReplayRunAt(t, artifactStore, session, time.Now().UTC())
}

func seedReplayRunAt(
	t *testing.T,
	artifactStore store.ArtifactStore,
	session string,
	startedAt time.Time,
) string {
	t.Helper()

	run, err := newRunRecordForMode(session, minimalConfig(), recorder.RunModeRecord)
	if err != nil {
		t.Fatalf("newRunRecordForMode() error = %v", err)
	}
	run.StartedAt = startedAt
	if err := artifactStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	interaction := recorder.Interaction{
		RunID:         run.RunID,
		InteractionID: "int_" + run.RunID,
		Sequence:      1,
		Service:       "openai",
		Operation:     "chat.completions.create",
		Protocol:      recorder.ProtocolHTTPS,
		Request: recorder.Request{
			URL:    "https://api.openai.com/v1/chat/completions",
			Method: "POST",
			Body: map[string]any{
				"model":    "gpt-5.4",
				"messages": []map[string]string{{"role": "user", "content": "say hello"}},
			},
		},
		Events: []recorder.Event{
			{Sequence: 1, TMS: 0, SimTMS: 0, Type: recorder.EventTypeRequestSent},
			{Sequence: 2, TMS: 1, SimTMS: 1, Type: recorder.EventTypeResponseReceived},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "v1",
			SessionSaltID:      "salt_fixture",
		},
	}
	if err := artifactStore.WriteInteraction(context.Background(), interaction); err != nil {
		t.Fatalf("WriteInteraction() error = %v", err)
	}
	if err := finalizeRun(context.Background(), artifactStore, run, nil); err != nil {
		t.Fatalf("finalizeRun() error = %v", err)
	}

	return run.RunID
}

func seedDiffRun(
	t *testing.T,
	artifactStore store.ArtifactStore,
	session string,
	message string,
	fallbackTier recorder.FallbackTier,
	startedAt time.Time,
) string {
	t.Helper()

	run, err := newRunRecordForMode(session, minimalConfig(), recorder.RunModeRecord)
	if err != nil {
		t.Fatalf("newRunRecordForMode() error = %v", err)
	}
	run.StartedAt = startedAt
	if err := artifactStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	interaction := recorder.Interaction{
		RunID:         run.RunID,
		InteractionID: "int_" + run.RunID,
		Sequence:      1,
		Service:       "openai",
		Operation:     "chat.completions.create",
		Protocol:      recorder.ProtocolHTTPS,
		FallbackTier:  fallbackTier,
		Request: recorder.Request{
			URL:    "https://api.openai.com/v1/chat/completions",
			Method: "POST",
			Body: map[string]any{
				"model":    "gpt-5.4",
				"messages": []map[string]string{{"role": "user", "content": message}},
			},
		},
		Events: []recorder.Event{
			{Sequence: 1, TMS: 0, SimTMS: 0, Type: recorder.EventTypeRequestSent},
			{Sequence: 2, TMS: 1, SimTMS: 1, Type: recorder.EventTypeResponseReceived},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "v1",
			SessionSaltID:      "salt_fixture",
		},
	}
	if err := artifactStore.WriteInteraction(context.Background(), interaction); err != nil {
		t.Fatalf("WriteInteraction() error = %v", err)
	}
	if err := finalizeRun(context.Background(), artifactStore, run, nil); err != nil {
		t.Fatalf("finalizeRun() error = %v", err)
	}

	return run.RunID
}

func seedCorruptedReplayCandidate(
	t *testing.T,
	artifactStore store.ArtifactStore,
	session string,
	startedAt time.Time,
) {
	t.Helper()

	run, err := newRunRecordForMode(session, minimalConfig(), recorder.RunModeRecord)
	if err != nil {
		t.Fatalf("newRunRecordForMode() error = %v", err)
	}
	run.RunID = "run_corrupted_latest"
	run.StartedAt = startedAt
	if err := artifactStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun(corrupted) error = %v", err)
	}
	endedAt := startedAt.Add(30 * time.Second)
	run.EndedAt = &endedAt
	run.Status = store.RunLifecycleStatusCorrupted
	run.IntegrityIssues = []recorder.IntegrityIssue{
		{
			Code:    recorder.IntegrityIssueRecorderShutdown,
			Message: "simulated failure",
		},
	}
	if err := artifactStore.UpdateRun(context.Background(), run); err != nil {
		t.Fatalf("UpdateRun(corrupted) error = %v", err)
	}
}

func seedReplayFailureRun(t *testing.T, artifactStore store.ArtifactStore, session string) string {
	t.Helper()

	run, err := newRunRecordForMode(session, minimalConfig(), recorder.RunModeRecord)
	if err != nil {
		t.Fatalf("newRunRecordForMode() error = %v", err)
	}
	if err := artifactStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	interaction := recorder.Interaction{
		RunID:         run.RunID,
		InteractionID: "int_" + run.RunID,
		Sequence:      1,
		Service:       "openai",
		Operation:     "chat.completions.create",
		Protocol:      recorder.ProtocolHTTPS,
		Request: recorder.Request{
			URL:    "https://api.openai.com/v1/chat/completions",
			Method: "POST",
			Body: map[string]any{
				"model":    "gpt-5.4",
				"messages": []map[string]string{{"role": "user", "content": "cause failure"}},
			},
		},
		Events: []recorder.Event{
			{Sequence: 1, TMS: 0, SimTMS: 0, Type: recorder.EventTypeRequestSent},
			{
				Sequence: 2,
				TMS:      3,
				SimTMS:   3,
				Type:     recorder.EventTypeError,
				Data: map[string]any{
					"message": "rate limited",
				},
			},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "v1",
			SessionSaltID:      "salt_fixture",
		},
	}
	if err := artifactStore.WriteInteraction(context.Background(), interaction); err != nil {
		t.Fatalf("WriteInteraction() error = %v", err)
	}
	if err := finalizeRun(context.Background(), artifactStore, run, nil); err != nil {
		t.Fatalf("finalizeRun() error = %v", err)
	}

	return run.RunID
}

func minimalConfig() config.Config {
	cfg := config.DefaultConfig()
	cfg.Scrub.PolicyVersion = "v1"
	return cfg
}

func helperCommand(mode string) []string {
	return []string{os.Args[0], "-test.run=TestHelperProcess", "--", mode}
}

func helperModeFromArgs(args []string) string {
	for idx, arg := range args {
		if arg == "--" && idx+1 < len(args) {
			return args[idx+1]
		}
	}
	return ""
}

func helperCapturedInteractions() []recorder.Interaction {
	return []recorder.Interaction{
		{
			RunID:         "run_helper_source",
			InteractionID: "int_helper_001",
			Sequence:      1,
			Service:       "openai",
			Operation:     "chat.completions.create",
			Protocol:      recorder.ProtocolHTTPS,
			Request: recorder.Request{
				URL:    "https://api.openai.com/v1/chat/completions?email=alice@example.com",
				Method: "POST",
				Headers: map[string][]string{
					"authorization": {"Bearer sk_live_test_secret"},
					"content-type":  {"application/json"},
				},
				Body: map[string]any{
					"messages": []map[string]string{{"role": "user", "content": "contact alice@example.com"}},
					"model":    "gpt-5.4",
				},
			},
			Events: []recorder.Event{
				{Sequence: 1, TMS: 0, SimTMS: 0, Type: recorder.EventTypeRequestSent},
				{
					Sequence: 2,
					TMS:      12,
					SimTMS:   12,
					Type:     recorder.EventTypeResponseReceived,
					Data: map[string]any{
						"status_code": 200,
						"body": map[string]any{
							"id": "resp_123",
						},
					},
				},
			},
			ScrubReport: recorder.ScrubReport{
				ScrubPolicyVersion: "v0-unredacted",
				SessionSaltID:      "salt_pending",
			},
			LatencyMS: 12,
		},
	}
}

func helperAppliedInjectionBundle(t *testing.T, injectionPath string, callNumber int) interactionBundle {
	t.Helper()

	var normalized sdkErrorInjectionBundle
	data, err := os.ReadFile(injectionPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", injectionPath, err)
	}
	if err := json.Unmarshal(data, &normalized); err != nil {
		t.Fatalf("json.Unmarshal(normalized injection) error = %v\npayload=%s", err, string(data))
	}
	if len(normalized.Rules) != 1 {
		t.Fatalf("len(normalized.Rules) = %d, want 1", len(normalized.Rules))
	}
	rule := normalized.Rules[0]
	if rule.Inject.Status != 402 {
		t.Fatalf("normalized injected status = %d, want 402", rule.Inject.Status)
	}

	return interactionBundle{
		BundleVersion: interactionBundleVersion,
		Metadata: map[string]any{
			"mode":    os.Getenv(envStagehandMode),
			"session": os.Getenv(envStagehandSession),
			"error_injection": map[string]any{
				"applied": []any{
					map[string]any{
						"rule_index":  0,
						"name":        rule.Name,
						"service":     rule.Match.Service,
						"operation":   rule.Match.Operation,
						"call_number": callNumber,
						"nth_call":    rule.Match.NthCall,
						"library":     rule.Inject.Library,
						"status":      rule.Inject.Status,
					},
				},
			},
		},
		Interactions: []recorder.Interaction{
			{
				RunID:         "run_helper_injected",
				InteractionID: "int_helper_injected_001",
				Sequence:      1,
				Service:       "stripe",
				Operation:     "POST /v1/refunds",
				Protocol:      recorder.ProtocolHTTPS,
				Request: recorder.Request{
					URL:    "https://api.stripe.com/v1/refunds",
					Method: "POST",
					Headers: map[string][]string{
						"content-type": {"application/x-www-form-urlencoded"},
					},
					Body: map[string]any{
						"payment_intent": "pi_123",
					},
				},
				Events: []recorder.Event{
					{Sequence: 1, TMS: 0, SimTMS: 0, Type: recorder.EventTypeRequestSent},
					{
						Sequence: 2,
						TMS:      1,
						SimTMS:   1,
						Type:     recorder.EventTypeResponseReceived,
						Data: map[string]any{
							"status_code": 402,
							"headers": map[string][]string{
								"content-type":         {"application/json"},
								"x-stagehand-injected": {"true"},
							},
							"body": rule.Inject.Body,
						},
					},
				},
				ScrubReport: recorder.ScrubReport{
					ScrubPolicyVersion: "v1",
					SessionSaltID:      "salt_fixture",
				},
				LatencyMS: 1,
			},
		},
	}
}

func injectedMetadataEntries(t *testing.T, metadata map[string]any) []any {
	t.Helper()
	section, ok := metadata["error_injection"].(map[string]any)
	if !ok {
		t.Fatalf("metadata missing error_injection section: %#v", metadata)
	}
	applied, ok := section["applied"].([]any)
	if !ok {
		t.Fatalf("error_injection.applied missing: %#v", section)
	}
	return applied
}

func mustEnv(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if strings.TrimSpace(value) == "" {
		t.Fatalf("%s is empty", key)
	}
	return value
}

func decodeJSONOutput[T any](t *testing.T, data []byte) T {
	t.Helper()

	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\npayload=%s", err, string(data))
	}
	return value
}

func findDoctorCheck(t *testing.T, report doctorReport, id string) doctorCheck {
	t.Helper()
	for _, check := range report.Checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("doctor check %q not found in %#v", id, report.Checks)
	return doctorCheck{}
}
