package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"

	"stagehand/internal/config"
)

type preflightResult struct {
	Mode        string   `json:"mode"`
	Status      string   `json:"status"`
	ConfigPath  string   `json:"config_path"`
	Command     []string `json:"command"`
	StdoutJSON  bool     `json:"stdout_json"`
	ResultKeys  []string `json:"result_keys,omitempty"`
	StderrBytes int      `json:"stderr_bytes"`
	TaskEnv     string   `json:"task_env,omitempty"`
}

func runPreflight(args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("preflight", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	configPath := flags.String("config", "", "Path to stagehand.yml")
	taskText := flags.String("task", "", "Task text to pass through STAGEHAND_TASK_TEXT")
	jsonOutput := flags.Bool("json", false, "Emit machine-readable output")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse preflight flags: %w\n\n%s", err, preflightHelpText())
	}
	commandArgs := flags.Args()
	if len(commandArgs) == 0 {
		return fmt.Errorf("preflight requires a command after --\n\n%s", preflightHelpText())
	}

	cfgPath := strings.TrimSpace(*configPath)
	if cfgPath == "" {
		cfgPath = defaultRuntimeConfigPath
	}
	if _, err := configLoadForPreflight(cfgPath); err != nil {
		return err
	}

	extraEnv := map[string]string{
		envStagehandPreflight:  "1",
		envStagehandConfigPath: cfgPath,
	}
	if strings.TrimSpace(*taskText) != "" {
		extraEnv[envStagehandTaskText] = *taskText
	}

	var commandStdout bytes.Buffer
	var commandStderr bytes.Buffer
	command := exec.CommandContext(context.Background(), commandArgs[0], commandArgs[1:]...)
	command.Stdout = &commandStdout
	command.Stderr = &commandStderr
	command.Env = managedCommandEnvironment(extraEnv)
	if err := command.Run(); err != nil {
		if commandStderr.Len() > 0 {
			_, _ = stderr.Write(commandStderr.Bytes())
		}
		return fmt.Errorf("preflight command failed: %w", err)
	}

	keys, err := validatePreflightStdout(commandStdout.Bytes())
	if err != nil {
		if commandStderr.Len() > 0 {
			_, _ = stderr.Write(commandStderr.Bytes())
		}
		return err
	}

	result := preflightResult{
		Mode:        "preflight",
		Status:      "passed",
		ConfigPath:  cfgPath,
		Command:     append([]string(nil), commandArgs...),
		StdoutJSON:  true,
		ResultKeys:  keys,
		StderrBytes: commandStderr.Len(),
	}
	if strings.TrimSpace(*taskText) != "" {
		result.TaskEnv = envStagehandTaskText
	}
	if *jsonOutput {
		return emitJSON(stdout, result)
	}
	return renderPreflightTerminal(stdout, result)
}

func configLoadForPreflight(path string) (bool, error) {
	if _, err := config.Load(path); err != nil {
		return false, fmt.Errorf("load runtime config %q: %w", path, err)
	}
	return true, nil
}

func validatePreflightStdout(data []byte) ([]string, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("preflight stdout must be a JSON object; got empty stdout. Keep logs on stderr and print one result JSON object to stdout")
	}

	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil {
		return nil, fmt.Errorf("preflight stdout must be a JSON object: %w. Keep logs on stderr and print one result JSON object to stdout", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("preflight stdout must contain one JSON object")
	}
	if len(object) == 0 {
		return nil, fmt.Errorf("preflight stdout JSON object must include at least one result field")
	}

	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

func renderPreflightTerminal(w io.Writer, result preflightResult) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Stagehand preflight passed\n\n")
	fmt.Fprintf(&b, "Config: %s\n", result.ConfigPath)
	fmt.Fprintf(&b, "Command: %s\n", strings.Join(result.Command, " "))
	fmt.Fprintf(&b, "Stdout contract: JSON object")
	if len(result.ResultKeys) > 0 {
		fmt.Fprintf(&b, " with keys %s", strings.Join(result.ResultKeys, ", "))
	}
	fmt.Fprintf(&b, "\n")
	if result.TaskEnv != "" {
		fmt.Fprintf(&b, "Task text env: %s\n", result.TaskEnv)
	}
	if result.StderrBytes > 0 {
		fmt.Fprintf(&b, "Logs: %d bytes on stderr\n", result.StderrBytes)
	}
	fmt.Fprintf(&b, "\nNext command:\n  stagehand record-baseline -- %s\n", strings.Join(result.Command, " "))
	_, err := io.WriteString(w, b.String())
	return err
}

func preflightHelpText() string {
	return `Usage:
  stagehand preflight [--config path] [--task text] [--json] -- <command> [args...]

Flags:
  --config string  Path to stagehand.yml (default: stagehand.yml)
  --task string    Task text to pass through STAGEHAND_TASK_TEXT
  --json           Emit machine-readable output
`
}
