package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type initProjectInfo struct {
	ProjectTypes []string
	Commands     []string
	Integrations []string
}

type packageJSON struct {
	Scripts      map[string]string `json:"scripts"`
	Dependencies map[string]string `json:"dependencies"`
	DevDeps      map[string]string `json:"devDependencies"`
}

func runInit(args []string, stdout io.Writer, _ io.Writer) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	configPath := flags.String("config", defaultRuntimeConfigPath, "Path to write stagehand.yml")
	force := flags.Bool("force", false, "Overwrite existing Stagehand scaffold files")
	starterFiles := flags.Bool("starter-files", true, "Write starter assertions.yml, stagehand.gates.yml, and error-injection.yml")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse init flags: %w\n\n%s", err, initHelpText())
	}
	if len(flags.Args()) > 0 {
		return fmt.Errorf("init does not accept positional arguments\n\n%s", initHelpText())
	}

	cfgPath := strings.TrimSpace(*configPath)
	if cfgPath == "" {
		cfgPath = defaultRuntimeConfigPath
	}

	if fileExists(cfgPath) && !*force {
		return fmt.Errorf("refusing to overwrite existing config %q; rerun with --force to replace it", cfgPath)
	}

	workdir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	info := detectInitProject(workdir)
	recommended := "python agent.py"
	if len(info.Commands) > 0 {
		recommended = info.Commands[0]
	}

	if err := writeInitFile(cfgPath, stagehandInitConfigYAML(), *force); err != nil {
		return err
	}
	for _, dir := range []string{
		filepath.Join(".stagehand", "runs"),
		filepath.Join(".stagehand", "reports"),
		filepath.Join(".stagehand", "generated"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create directory %q: %w", dir, err)
		}
	}
	if *starterFiles {
		if err := writeInitFile("assertions.yml", starterAssertionsYAML(), *force); err != nil {
			return err
		}
		if err := writeInitFile(defaultGatesPath, starterGatesYAML(), *force); err != nil {
			return err
		}
		if err := writeInitFile("error-injection.yml", starterErrorInjectionYAML(), *force); err != nil {
			return err
		}
	}

	return renderInitSummary(stdout, cfgPath, recommended, info, *starterFiles)
}

func detectInitProject(root string) initProjectInfo {
	types := orderedSet{}
	commands := orderedSet{}
	integrations := orderedSet{}

	if pkg, ok := readPackageJSON(filepath.Join(root, "package.json")); ok {
		types.add("typescript")
		types.add("node")
		addPackageCommands(pkg, &commands)
		for name := range pkg.Dependencies {
			addNodeIntegration(name, &integrations)
		}
		for name := range pkg.DevDeps {
			addNodeIntegration(name, &integrations)
		}
	}

	if fileExists(filepath.Join(root, "tsconfig.json")) {
		types.add("typescript")
	}
	for _, marker := range []string{"pyproject.toml", "requirements.txt", "setup.py", "setup.cfg"} {
		if fileExists(filepath.Join(root, marker)) {
			types.add("python")
			break
		}
	}
	addPyprojectCommands(filepath.Join(root, "pyproject.toml"), &commands)
	addPythonCommands(root, &commands)
	addPythonIntegrations(root, &integrations)
	addSourceIntegrations(root, &integrations)

	if commands.hasPrefix("python ") {
		types.add("python")
	}

	return initProjectInfo{
		ProjectTypes: types.values(),
		Commands:     commands.values(),
		Integrations: integrations.values(),
	}
}

func addPackageCommands(pkg packageJSON, commands *orderedSet) {
	preferred := []string{"agent", "demo", "start", "dev", "test"}
	for _, name := range preferred {
		if _, ok := pkg.Scripts[name]; ok {
			commands.add("npm run " + name)
		}
	}

	names := make([]string, 0, len(pkg.Scripts))
	for name := range pkg.Scripts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "agent") || strings.Contains(lower, "demo") || strings.Contains(lower, "eval") {
			commands.add("npm run " + name)
		}
	}
}

func addPythonCommands(root string, commands *orderedSet) {
	for _, candidate := range []string{"agent.py", "main.py", "app.py"} {
		if fileExists(filepath.Join(root, candidate)) {
			commands.add("python " + candidate)
		}
	}
	for _, candidate := range []string{
		filepath.Join("examples", "agent.py"),
		filepath.Join("examples", "main.py"),
		filepath.Join("demo", "agent.py"),
	} {
		if fileExists(filepath.Join(root, candidate)) {
			commands.add("python " + filepath.ToSlash(candidate))
		}
	}
	for _, marker := range []string{"pytest.ini", "tox.ini"} {
		if fileExists(filepath.Join(root, marker)) {
			commands.add("python -m pytest")
			return
		}
	}
	if dirExists(filepath.Join(root, "tests")) {
		commands.add("python -m pytest")
	}
}

func addPyprojectCommands(path string, commands *orderedSet) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	inScripts := false
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inScripts = line == "[project.scripts]" || line == "[tool.poetry.scripts]"
			continue
		}
		if !inScripts {
			continue
		}
		name, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		command := strings.Trim(strings.TrimSpace(name), `"'`)
		lower := strings.ToLower(command)
		if strings.Contains(lower, "agent") || strings.Contains(lower, "demo") || strings.Contains(lower, "eval") {
			commands.add(command)
		}
	}
}

func addPythonIntegrations(root string, integrations *orderedSet) {
	for _, path := range []string{filepath.Join(root, "requirements.txt"), filepath.Join(root, "pyproject.toml")} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lower := strings.ToLower(string(data))
		for _, name := range []string{"openai", "stripe", "httpx", "requests", "aiohttp"} {
			if strings.Contains(lower, name) {
				integrations.add(name)
			}
		}
	}
}

func addSourceIntegrations(root string, integrations *orderedSet) {
	seen := 0
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || seen >= 200 {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			switch name {
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
		text := strings.ToLower(string(data))
		for _, marker := range []string{"openai", "stripe", "httpx", "requests", "aiohttp", "undici"} {
			if strings.Contains(text, marker) {
				integrations.add(marker)
			}
		}
		if strings.Contains(text, "fetch(") {
			integrations.add("fetch")
		}
		return nil
	})
}

func addNodeIntegration(name string, integrations *orderedSet) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "openai", "stripe", "undici":
		integrations.add(normalized)
	case "node-fetch", "cross-fetch":
		integrations.add("fetch")
	}
}

func readPackageJSON(path string) (packageJSON, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return packageJSON{}, false
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return packageJSON{}, false
	}
	return pkg, true
}

func writeInitFile(path, content string, force bool) error {
	if fileExists(path) && !force {
		return nil
	}
	cleaned := filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(cleaned), 0o755); err != nil {
		return fmt.Errorf("create parent directory for %q: %w", cleaned, err)
	}
	if err := os.WriteFile(cleaned, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %q: %w", cleaned, err)
	}
	return nil
}

func renderInitSummary(w io.Writer, configPath, recommended string, info initProjectInfo, starterFiles bool) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Stagehand initialized\n\n")
	fmt.Fprintf(&b, "Created:\n")
	fmt.Fprintf(&b, "- %s\n", filepath.ToSlash(configPath))
	fmt.Fprintf(&b, "- .stagehand/runs\n")
	fmt.Fprintf(&b, "- .stagehand/reports\n")
	fmt.Fprintf(&b, "- .stagehand/generated\n")
	if starterFiles {
		fmt.Fprintf(&b, "- assertions.yml\n")
		fmt.Fprintf(&b, "- %s\n", defaultGatesPath)
		fmt.Fprintf(&b, "- error-injection.yml\n")
	}
	if len(info.ProjectTypes) > 0 || len(info.Integrations) > 0 || len(info.Commands) > 0 {
		fmt.Fprintf(&b, "\nDetected:\n")
		if len(info.ProjectTypes) > 0 {
			fmt.Fprintf(&b, "- project: %s\n", strings.Join(info.ProjectTypes, ", "))
		}
		if len(info.Integrations) > 0 {
			fmt.Fprintf(&b, "- integrations: %s\n", strings.Join(info.Integrations, ", "))
		}
		if len(info.Commands) > 0 {
			fmt.Fprintf(&b, "- candidate commands: %s\n", strings.Join(info.Commands, "; "))
		}
	}
	fmt.Fprintf(&b, "\nNext command:\n")
	fmt.Fprintf(&b, "  stagehand record-baseline -- %s\n", recommended)
	_, err := io.WriteString(w, b.String())
	return err
}

func stagehandInitConfigYAML() string {
	return `schema_version: v1alpha1
record:
  storage_path: .stagehand/runs
  capture:
    max_body_bytes: 1048576
    include_headers:
      - content-type
      - accept
    redact_before_persist: true
replay:
  mode: exact
  clock_mode: wall
  allow_live_network: false
scrub:
  enabled: true
  policy_version: v1
  detectors:
    email: true
    phone: true
    ssn: true
    credit_card: true
    jwt: true
    api_key: true
    password: true
fallback:
  allowed_tiers:
    - exact
    - nearest_neighbor
auth:
  default_mode: permissive
services:
  # Delete this example if you only call prebuilt providers.
  # Use service mappings for internal APIs so diffs say "internal-crm"
  # instead of raw host names. Add ignore paths for request IDs, cursors,
  # timestamps, and other fields that change on every call.
  # - name: internal-crm
  #   type: api
  #   match:
  #     host: crm.internal.example.com
  #     path_prefix: /v1
  #   replay:
  #     mode: exact
  #     allowed_tiers: [0]
  #   ignore:
  #     request_paths:
  #       - body.request_id
  #       - headers.x-request-id
  #     response_paths:
  #       - body.generated_at
  #       - body.trace_id
  []
`
}

func starterAssertionsYAML() string {
	return `schema_version: v1alpha1
assertions:
  # These examples are intentionally commented out. Uncomment one at a time
  # after your first baseline, edit service/operation/path values, and delete
  # the examples that do not match your workflow.
  #
  # - id: model-called-once
  #   type: count
  #   match:
  #     service: openai
  #   min: 1
  #
  # - id: lookup-before-refund
  #   type: ordering
  #   before:
  #     tool: lookup_customer
  #   after:
  #     service: stripe
  #     operation: POST /v1/refunds
  #
  # - id: no-unsafe-refund
  #   type: forbidden-operation
  #   match:
  #     service: stripe
  #     operation: POST /v1/refunds
  #
  # - id: refund-status-approved
  #   type: payload-field
  #   match:
  #     service: internal-billing
  #   path: events[-1].data.body.status
  #   equals: approved
  #
  # - id: no-llm-synthesis
  #   type: fallback-prohibition
  #   disallowed_tiers:
  #     - llm_synthesis
`
}

func starterErrorInjectionYAML() string {
	return `schema_version: v1alpha1
error_injection:
  enabled: false
  rules:
    # Set enabled: true, uncomment one rule, edit the target, and run
    # stagehand test --error-injection error-injection.yml -- <command>
    #
    # - name: CRM timeout on first lookup
    #   match:
    #     service: internal-crm
    #     operation: POST /v1/customers/search
    #     nth_call: 1
    #   inject:
    #     error: timeout
    #
    # - name: Billing returns 500
    #   match:
    #     service: internal-billing
    #     operation: POST /api/refunds
    #     nth_call: 1
    #   inject:
    #     status: 500
    #     body:
    #       error: internal_server_error
    #
    # - name: lookup tool not found
    #   match:
    #     tool: lookup_customer
    #     nth_call: 1
    #   inject:
    #     error: not_found
    #     body:
    #       message: customer missing
    #       error_class: CustomerNotFoundError
`
}

func starterGatesYAML() string {
	return `schema_version: v1alpha1

release_gates:
  # Edit service if your database is not recorded as postgres.
  - name: Block destructive database actions
    block_if:
      service: postgres
      side_effect: destructive

  # External customer messages must carry approval evidence.
  - name: External messages require approval
    require_approval:
      side_effect: external_message

  # Amounts are minor currency units, so 5000 means $50.00 for USD.
  - name: High-value financial actions require approval
    require_approval:
      side_effect: financial
      amount_gt: 5000

  # Review unknown-risk actions before approving a release.
  - name: Review unknown-risk actions
    forbid_unknown_risk: {}
`
}

func initHelpText() string {
	return `Usage:
  stagehand init [--config path] [--force] [--starter-files=false]

Flags:
  --config string          Path to write stagehand.yml (default: stagehand.yml)
  --force                  Overwrite existing Stagehand scaffold files
  --starter-files bool     Write starter assertions.yml, stagehand.gates.yml, and error-injection.yml (default: true)
`
}

type orderedSet struct {
	items []string
	seen  map[string]bool
}

func (s *orderedSet) add(value string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	if s.seen[trimmed] {
		return
	}
	s.seen[trimmed] = true
	s.items = append(s.items, trimmed)
}

func (s *orderedSet) values() []string {
	return append([]string(nil), s.items...)
}

func (s *orderedSet) hasPrefix(prefix string) bool {
	for _, item := range s.items {
		if strings.HasPrefix(item, prefix) {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
