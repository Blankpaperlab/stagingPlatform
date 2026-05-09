package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"stagehand/internal/config"
	"stagehand/internal/version"
)

const (
	doctorStatusPass = "pass"
	doctorStatusWarn = "warn"
	doctorStatusFail = "fail"
)

type doctorReport struct {
	Mode         string        `json:"mode"`
	Status       string        `json:"status"`
	ConfigPath   string        `json:"config_path"`
	ProjectTypes []string      `json:"project_types,omitempty"`
	Integrations []string      `json:"integrations,omitempty"`
	Commands     []string      `json:"commands,omitempty"`
	Checks       []doctorCheck `json:"checks"`
	Summary      doctorSummary `json:"summary"`
}

type doctorSummary struct {
	Passed int `json:"passed"`
	Warned int `json:"warned"`
	Failed int `json:"failed"`
	Total  int `json:"total"`
}

type doctorCheck struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Repair  string `json:"repair,omitempty"`
}

func runDoctor(args []string, stdout io.Writer, _ io.Writer) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	configPath := flags.String("config", defaultRuntimeConfigPath, "Path to stagehand.yml")
	jsonOutput := flags.Bool("json", false, "Emit machine-readable diagnostics")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse doctor flags: %w\n\n%s", err, doctorHelpText())
	}
	if len(flags.Args()) > 0 {
		return fmt.Errorf("doctor does not accept positional arguments\n\n%s", doctorHelpText())
	}

	cfgPath := strings.TrimSpace(*configPath)
	if cfgPath == "" {
		cfgPath = defaultRuntimeConfigPath
	}
	report := buildDoctorReport(cfgPath)

	var emitErr error
	if *jsonOutput {
		emitErr = emitJSON(stdout, report)
	} else {
		emitErr = renderDoctorTerminal(stdout, report)
	}
	if emitErr != nil {
		return emitErr
	}
	if report.Summary.Failed > 0 {
		return fmt.Errorf("doctor found %d failed check(s)", report.Summary.Failed)
	}
	return nil
}

func buildDoctorReport(configPath string) doctorReport {
	workdir, err := os.Getwd()
	if err != nil {
		workdir = "."
	}
	info := detectInitProject(workdir)
	checks := []doctorCheck{
		{
			ID:      "cli-runnable",
			Name:    "CLI binary",
			Status:  doctorStatusPass,
			Message: fmt.Sprintf("stagehand CLI is runnable (%s)", version.CLIVersion),
			Repair:  "go build -o bin/stagehand ./cmd/stagehand",
		},
	}
	checks = append(checks, doctorConfigCheck(configPath))
	checks = append(checks, doctorPythonChecks(info)...)
	checks = append(checks, doctorTypeScriptChecks(workdir, info)...)
	checks = append(checks, doctorOptionalPackageChecks(workdir, info)...)
	checks = append(checks, doctorUnsupportedNetworkChecks(workdir)...)

	report := doctorReport{
		Mode:         "doctor",
		ConfigPath:   configPath,
		ProjectTypes: info.ProjectTypes,
		Integrations: info.Integrations,
		Commands:     info.Commands,
		Checks:       checks,
	}
	report.Summary = summarizeDoctorChecks(checks)
	report.Status = doctorStatusPass
	if report.Summary.Warned > 0 {
		report.Status = doctorStatusWarn
	}
	if report.Summary.Failed > 0 {
		report.Status = doctorStatusFail
	}
	return report
}

func doctorConfigCheck(configPath string) doctorCheck {
	if _, err := config.Load(configPath); err != nil {
		return doctorCheck{
			ID:      "config-valid",
			Name:    "stagehand.yml",
			Status:  doctorStatusFail,
			Message: fmt.Sprintf("could not load %s: %v", filepath.ToSlash(configPath), err),
			Repair:  "stagehand init",
		}
	}
	return doctorCheck{
		ID:      "config-valid",
		Name:    "stagehand.yml",
		Status:  doctorStatusPass,
		Message: fmt.Sprintf("%s loads and validates", filepath.ToSlash(configPath)),
	}
}

func doctorPythonChecks(info initProjectInfo) []doctorCheck {
	if !containsString(info.ProjectTypes, "python") {
		return nil
	}
	python, ok := findExecutable("python", "python3")
	if !ok {
		return []doctorCheck{{
			ID:      "python-sdk-import",
			Name:    "Python SDK import",
			Status:  doctorStatusFail,
			Message: "Python project detected, but no python executable was found on PATH",
			Repair:  "Install Python 3.10+ and rerun stagehand doctor",
		}}
	}
	output, err := runDoctorCommand(python, []string{"-c", "import stagehand; print(getattr(stagehand, '__version__', 'unknown'))"}, managedCommandEnvironment(nil))
	if err != nil {
		return []doctorCheck{{
			ID:      "python-sdk-import",
			Name:    "Python SDK import",
			Status:  doctorStatusFail,
			Message: fmt.Sprintf("Python could not import stagehand: %s", compactCommandOutput(output, err)),
			Repair:  "python -m pip install stagehand",
		}}
	}
	return []doctorCheck{{
		ID:      "python-sdk-import",
		Name:    "Python SDK import",
		Status:  doctorStatusPass,
		Message: "Python can import stagehand",
	}}
}

func doctorTypeScriptChecks(root string, info initProjectInfo) []doctorCheck {
	if !containsString(info.ProjectTypes, "typescript") && !containsString(info.ProjectTypes, "node") {
		return nil
	}
	checks := []doctorCheck{}
	pkg, ok := readPackageJSON(filepath.Join(root, "package.json"))
	if !ok {
		return []doctorCheck{{
			ID:      "typescript-sdk-declared",
			Name:    "TypeScript SDK",
			Status:  doctorStatusFail,
			Message: "TypeScript project detected, but package.json was not readable",
			Repair:  "npm install @stagehand/sdk",
		}}
	}

	if packageHasDependency(pkg, "@stagehand/sdk") || fileExists(filepath.Join(root, "node_modules", "@stagehand", "sdk", "package.json")) {
		checks = append(checks, doctorCheck{
			ID:      "typescript-sdk-declared",
			Name:    "TypeScript SDK",
			Status:  doctorStatusPass,
			Message: "@stagehand/sdk is declared or installed",
		})
	} else {
		checks = append(checks, doctorCheck{
			ID:      "typescript-sdk-declared",
			Name:    "TypeScript SDK",
			Status:  doctorStatusFail,
			Message: "TypeScript project detected, but @stagehand/sdk is not declared in package.json",
			Repair:  "npm install @stagehand/sdk",
		})
	}

	scriptName := firstExistingScript(pkg, "check", "typecheck", "build")
	if scriptName == "" {
		checks = append(checks, doctorCheck{
			ID:      "typescript-build",
			Name:    "TypeScript build",
			Status:  doctorStatusWarn,
			Message: "No package script named check, typecheck, or build was found",
			Repair:  "Add a package.json script such as \"check\": \"tsc --noEmit\"",
		})
		return checks
	}
	if _, ok := findExecutable("npm"); !ok {
		checks = append(checks, doctorCheck{
			ID:      "typescript-build",
			Name:    "TypeScript build",
			Status:  doctorStatusFail,
			Message: "npm was not found on PATH",
			Repair:  "Install Node.js, then run npm install",
		})
		return checks
	}
	output, err := runDoctorCommand("npm", []string{"run", scriptName}, os.Environ())
	if err != nil {
		checks = append(checks, doctorCheck{
			ID:      "typescript-build",
			Name:    "TypeScript build",
			Status:  doctorStatusFail,
			Message: fmt.Sprintf("npm run %s failed: %s", scriptName, compactCommandOutput(output, err)),
			Repair:  fmt.Sprintf("npm install && npm run %s", scriptName),
		})
		return checks
	}
	checks = append(checks, doctorCheck{
		ID:      "typescript-build",
		Name:    "TypeScript build",
		Status:  doctorStatusPass,
		Message: "npm run " + scriptName + " completed successfully",
	})
	return checks
}

func doctorOptionalPackageChecks(root string, info initProjectInfo) []doctorCheck {
	checks := []doctorCheck{}
	if containsString(info.Integrations, "openai") {
		checks = append(checks, optionalPackageCheck(root, info, "openai"))
	}
	if containsString(info.Integrations, "stripe") {
		checks = append(checks, optionalPackageCheck(root, info, "stripe"))
	}
	return checks
}

func optionalPackageCheck(root string, info initProjectInfo, packageName string) doctorCheck {
	if containsString(info.ProjectTypes, "node") || containsString(info.ProjectTypes, "typescript") {
		if pkg, ok := readPackageJSON(filepath.Join(root, "package.json")); ok && packageHasDependency(pkg, packageName) {
			return doctorCheck{
				ID:      "optional-package-" + packageName,
				Name:    packageName + " package",
				Status:  doctorStatusPass,
				Message: packageName + " is declared in package.json",
			}
		}
	}
	if containsString(info.ProjectTypes, "python") {
		python, ok := findExecutable("python", "python3")
		if ok {
			output, err := runDoctorCommand(python, []string{"-c", fmt.Sprintf("import importlib.util, sys; sys.exit(0 if importlib.util.find_spec(%q) else 1)", packageName)}, managedCommandEnvironment(nil))
			if err == nil {
				return doctorCheck{
					ID:      "optional-package-" + packageName,
					Name:    packageName + " package",
					Status:  doctorStatusPass,
					Message: packageName + " is importable in Python",
				}
			}
			_ = output
		}
	}
	return doctorCheck{
		ID:      "optional-package-" + packageName,
		Name:    packageName + " package",
		Status:  doctorStatusWarn,
		Message: packageName + " is referenced but not confirmed as installed",
		Repair:  fmt.Sprintf("Install it with npm install %s or python -m pip install %s", packageName, packageName),
	}
}

func doctorUnsupportedNetworkChecks(root string) []doctorCheck {
	unsupported := detectUnsupportedNetworkLibraries(root)
	if len(unsupported) == 0 {
		return []doctorCheck{{
			ID:      "unsupported-network-libraries",
			Name:    "Unsupported network libraries",
			Status:  doctorStatusPass,
			Message: "No common unsupported network libraries detected",
		}}
	}
	return []doctorCheck{{
		ID:      "unsupported-network-libraries",
		Name:    "Unsupported network libraries",
		Status:  doctorStatusWarn,
		Message: fmt.Sprintf("Detected %s; Stagehand V1 captures Python httpx and TypeScript fetch/undici paths first", strings.Join(unsupported, ", ")),
		Repair:  "Prefer httpx in Python or fetch/undici in TypeScript for captured calls, or wrap unsupported clients with stagehand.tool",
	}}
}

func detectUnsupportedNetworkLibraries(root string) []string {
	found := orderedSet{}
	if data, err := os.ReadFile(filepath.Join(root, "requirements.txt")); err == nil {
		addUnsupportedMarkers(strings.ToLower(string(data)), &found)
	}
	if data, err := os.ReadFile(filepath.Join(root, "pyproject.toml")); err == nil {
		addUnsupportedMarkers(strings.ToLower(string(data)), &found)
	}
	if pkg, ok := readPackageJSON(filepath.Join(root, "package.json")); ok {
		for name := range pkg.Dependencies {
			addUnsupportedNodePackage(name, &found)
		}
		for name := range pkg.DevDeps {
			addUnsupportedNodePackage(name, &found)
		}
	}
	seen := 0
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || seen >= 200 {
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".stagehand", "node_modules", "dist", "build", ".venv", "venv", "__pycache__":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".py" && ext != ".ts" && ext != ".tsx" && ext != ".js" && ext != ".mjs" && ext != ".cjs" {
			return nil
		}
		seen++
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		addUnsupportedMarkers(strings.ToLower(string(data)), &found)
		return nil
	})
	return found.values()
}

func addUnsupportedNodePackage(name string, found *orderedSet) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "axios", "got", "superagent", "node-fetch":
		found.add(strings.ToLower(strings.TrimSpace(name)))
	}
}

func addUnsupportedMarkers(text string, found *orderedSet) {
	markers := map[string][]string{
		"requests":   {"import requests", "requests.", "\"requests\"", "'requests'"},
		"aiohttp":    {"import aiohttp", "aiohttp.", "\"aiohttp\"", "'aiohttp'"},
		"urllib":     {"urllib.request", "from urllib"},
		"axios":      {"from 'axios'", "from \"axios\"", "require('axios')", "require(\"axios\")", "\"axios\"", "'axios'"},
		"got":        {"from 'got'", "from \"got\"", "require('got')", "require(\"got\")", "\"got\"", "'got'"},
		"superagent": {"superagent", "\"superagent\"", "'superagent'"},
		"node-fetch": {"node-fetch", "\"node-fetch\"", "'node-fetch'"},
	}
	for name, candidates := range markers {
		for _, marker := range candidates {
			if strings.Contains(text, marker) {
				found.add(name)
				break
			}
		}
	}
}

func summarizeDoctorChecks(checks []doctorCheck) doctorSummary {
	summary := doctorSummary{Total: len(checks)}
	for _, check := range checks {
		switch check.Status {
		case doctorStatusPass:
			summary.Passed++
		case doctorStatusWarn:
			summary.Warned++
		case doctorStatusFail:
			summary.Failed++
		}
	}
	return summary
}

func renderDoctorTerminal(w io.Writer, report doctorReport) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Stagehand doctor: %s\n", report.Status)
	if len(report.ProjectTypes) > 0 {
		fmt.Fprintf(&b, "Project: %s\n", strings.Join(report.ProjectTypes, ", "))
	}
	if len(report.Integrations) > 0 {
		fmt.Fprintf(&b, "Integrations: %s\n", strings.Join(report.Integrations, ", "))
	}
	for _, check := range report.Checks {
		fmt.Fprintf(&b, "- %s %s: %s\n", strings.ToUpper(check.Status), check.Name, check.Message)
		if check.Repair != "" && check.Status != doctorStatusPass {
			fmt.Fprintf(&b, "  Repair: %s\n", check.Repair)
		}
	}
	fmt.Fprintf(&b, "Summary: %d passed, %d warned, %d failed, %d total\n", report.Summary.Passed, report.Summary.Warned, report.Summary.Failed, report.Summary.Total)
	_, err := io.WriteString(w, b.String())
	return err
}

func runDoctorCommand(name string, args []string, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	command.Env = env
	output, err := command.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(output), fmt.Errorf("%s timed out", name)
	}
	return string(output), err
}

func compactCommandOutput(output string, err error) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return err.Error()
	}
	if len(trimmed) > 300 {
		trimmed = trimmed[:300] + "..."
	}
	return trimmed
}

func findExecutable(names ...string) (string, bool) {
	for _, name := range names {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, true
		}
	}
	return "", false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func packageHasDependency(pkg packageJSON, name string) bool {
	if _, ok := pkg.Dependencies[name]; ok {
		return true
	}
	if _, ok := pkg.DevDeps[name]; ok {
		return true
	}
	return false
}

func firstExistingScript(pkg packageJSON, names ...string) string {
	for _, name := range names {
		if _, ok := pkg.Scripts[name]; ok {
			return name
		}
	}
	return ""
}

func doctorHelpText() string {
	return `Usage:
  stagehand doctor [--config path] [--json]

Flags:
  --config string  Path to stagehand.yml (default: stagehand.yml)
  --json           Emit machine-readable diagnostics for CI
`
}
