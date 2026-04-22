package recorder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"
)

const ArtifactSchemaVersion = "v1alpha1"

type RunMode string

const (
	RunModeRecord      RunMode = "record"
	RunModeReplay      RunMode = "replay"
	RunModeHybrid      RunMode = "hybrid"
	RunModePassthrough RunMode = "passthrough"
)

type RunStatus string

const (
	RunStatusComplete   RunStatus = "complete"
	RunStatusIncomplete RunStatus = "incomplete"
	RunStatusCorrupted  RunStatus = "corrupted"
)

type Protocol string

const (
	ProtocolHTTP      Protocol = "http"
	ProtocolHTTPS     Protocol = "https"
	ProtocolSSE       Protocol = "sse"
	ProtocolWebSocket Protocol = "websocket"
	ProtocolPostgres  Protocol = "postgres"
)

type FallbackTier string

const (
	FallbackTierExact           FallbackTier = "exact"
	FallbackTierNearestNeighbor FallbackTier = "nearest_neighbor"
	FallbackTierStateSynthesis  FallbackTier = "state_synthesis"
	FallbackTierLLMSynthesis    FallbackTier = "llm_synthesis"
)

type EventType string

const (
	EventTypeRequestSent      EventType = "request_sent"
	EventTypeResponseReceived EventType = "response_received"
	EventTypeStreamChunk      EventType = "stream_chunk"
	EventTypeStreamEnd        EventType = "stream_end"
	EventTypeToolCallStart    EventType = "tool_call_start"
	EventTypeToolCallEnd      EventType = "tool_call_end"
	EventTypeError            EventType = "error"
	EventTypeTimeout          EventType = "timeout"
)

type IntegrityIssueCode string

const (
	IntegrityIssueRecorderShutdown IntegrityIssueCode = "recorder_shutdown"
	IntegrityIssueInterruptedWrite IntegrityIssueCode = "interrupted_write"
	IntegrityIssueMissingEndState  IntegrityIssueCode = "missing_end_state"
	IntegrityIssueSchemaValidation IntegrityIssueCode = "schema_validation_failed"
)

var (
	validRunModes = []RunMode{
		RunModeRecord,
		RunModeReplay,
		RunModeHybrid,
		RunModePassthrough,
	}
	validRunStatuses = []RunStatus{
		RunStatusComplete,
		RunStatusIncomplete,
		RunStatusCorrupted,
	}
	validProtocols = []Protocol{
		ProtocolHTTP,
		ProtocolHTTPS,
		ProtocolSSE,
		ProtocolWebSocket,
		ProtocolPostgres,
	}
	validFallbackTiers = []FallbackTier{
		FallbackTierExact,
		FallbackTierNearestNeighbor,
		FallbackTierStateSynthesis,
		FallbackTierLLMSynthesis,
	}
)

type Run struct {
	SchemaVersion      string           `json:"schema_version"`
	SDKVersion         string           `json:"sdk_version"`
	RuntimeVersion     string           `json:"runtime_version"`
	ScrubPolicyVersion string           `json:"scrub_policy_version"`
	RunID              string           `json:"run_id"`
	SessionName        string           `json:"session_name"`
	Mode               RunMode          `json:"mode"`
	Status             RunStatus        `json:"status"`
	BaseSnapshotID     string           `json:"base_snapshot_id,omitempty"`
	AgentVersion       string           `json:"agent_version,omitempty"`
	GitSHA             string           `json:"git_sha,omitempty"`
	StartedAt          time.Time        `json:"started_at"`
	EndedAt            *time.Time       `json:"ended_at,omitempty"`
	Interactions       []Interaction    `json:"interactions"`
	IntegrityIssues    []IntegrityIssue `json:"integrity_issues,omitempty"`
}

type Interaction struct {
	RunID               string            `json:"run_id"`
	InteractionID       string            `json:"interaction_id"`
	ParentInteractionID string            `json:"parent_interaction_id,omitempty"`
	Sequence            int               `json:"sequence"`
	Service             string            `json:"service"`
	Operation           string            `json:"operation"`
	Protocol            Protocol          `json:"protocol"`
	Streaming           bool              `json:"streaming"`
	FallbackTier        FallbackTier      `json:"fallback_tier,omitempty"`
	Request             Request           `json:"request"`
	Events              []Event           `json:"events"`
	ExtractedEntities   []ExtractedEntity `json:"extracted_entities,omitempty"`
	ScrubReport         ScrubReport       `json:"scrub_report"`
	LatencyMS           int64             `json:"latency_ms,omitempty"`
}

type Request struct {
	URL     string              `json:"url"`
	Method  string              `json:"method"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    any                 `json:"body,omitempty"`
}

type Event struct {
	Sequence            int            `json:"sequence"`
	TMS                 int64          `json:"t_ms"`
	SimTMS              int64          `json:"sim_t_ms"`
	Type                EventType      `json:"type"`
	Data                map[string]any `json:"data,omitempty"`
	NestedInteractionID string         `json:"nested_interaction_id,omitempty"`
}

type ExtractedEntity struct {
	Type  string         `json:"type"`
	ID    string         `json:"id"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

type ScrubReport struct {
	RedactedPaths      []string `json:"redacted_paths,omitempty"`
	ScrubPolicyVersion string   `json:"scrub_policy_version"`
	SessionSaltID      string   `json:"session_salt_id"`
}

type IntegrityIssue struct {
	Code          IntegrityIssueCode `json:"code"`
	Message       string             `json:"message"`
	InteractionID string             `json:"interaction_id,omitempty"`
	EventSequence int                `json:"event_sequence,omitempty"`
}

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	if len(e.Problems) == 0 {
		return "invalid artifact"
	}

	return fmt.Sprintf("invalid artifact: %s", strings.Join(e.Problems, "; "))
}

func (e *ValidationError) add(format string, args ...any) {
	e.Problems = append(e.Problems, fmt.Sprintf(format, args...))
}

func (e *ValidationError) addNested(prefix string, err error) {
	if err == nil {
		return
	}

	child, ok := err.(*ValidationError)
	if !ok {
		e.Problems = append(e.Problems, fmt.Sprintf("%s: %v", prefix, err))
		return
	}

	for _, problem := range child.Problems {
		e.Problems = append(e.Problems, fmt.Sprintf("%s.%s", prefix, problem))
	}
}

func (e *ValidationError) err() error {
	if len(e.Problems) == 0 {
		return nil
	}

	return e
}

func Load(path string) (Run, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Run{}, fmt.Errorf("read artifact %q: %w", path, err)
	}

	run, err := Decode(data)
	if err != nil {
		return Run{}, fmt.Errorf("load artifact %q: %w", path, err)
	}

	return run, nil
}

func Decode(data []byte) (Run, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var run Run
	if err := decoder.Decode(&run); err != nil {
		return Run{}, fmt.Errorf("decode artifact: %w", err)
	}

	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return Run{}, fmt.Errorf("decode artifact: trailing data is not allowed")
		}

		return Run{}, fmt.Errorf("decode artifact: %w", err)
	}

	if err := run.Validate(); err != nil {
		return Run{}, err
	}

	return run, nil
}

func (r Run) Validate() error {
	verr := &ValidationError{}

	if r.SchemaVersion != ArtifactSchemaVersion {
		verr.add("schema_version must be %q", ArtifactSchemaVersion)
	}

	if strings.TrimSpace(r.SDKVersion) == "" {
		verr.add("sdk_version is required")
	}

	if strings.TrimSpace(r.RuntimeVersion) == "" {
		verr.add("runtime_version is required")
	}

	if strings.TrimSpace(r.ScrubPolicyVersion) == "" {
		verr.add("scrub_policy_version is required")
	}

	if strings.TrimSpace(r.RunID) == "" {
		verr.add("run_id is required")
	}

	if strings.TrimSpace(r.SessionName) == "" {
		verr.add("session_name is required")
	}

	if !slices.Contains(validRunModes, r.Mode) {
		verr.add("mode must be one of %q, %q, %q, %q", RunModeRecord, RunModeReplay, RunModeHybrid, RunModePassthrough)
	}

	if !slices.Contains(validRunStatuses, r.Status) {
		verr.add("status must be one of %q, %q, %q", RunStatusComplete, RunStatusIncomplete, RunStatusCorrupted)
	}

	if r.StartedAt.IsZero() {
		verr.add("started_at is required")
	}

	if r.EndedAt != nil && !r.StartedAt.IsZero() && r.EndedAt.Before(r.StartedAt) {
		verr.add("ended_at cannot be before started_at")
	}

	switch r.Status {
	case RunStatusComplete:
		if r.EndedAt == nil || r.EndedAt.IsZero() {
			verr.add("ended_at is required when status is %q", RunStatusComplete)
		}
		if len(r.IntegrityIssues) > 0 {
			verr.add("integrity_issues must be empty when status is %q", RunStatusComplete)
		}
	case RunStatusIncomplete, RunStatusCorrupted:
		if len(r.IntegrityIssues) == 0 {
			verr.add("integrity_issues must contain at least one issue when status is %q", r.Status)
		}
	}

	seenInteractionIDs := map[string]bool{}
	lastSequence := 0
	for idx, interaction := range r.Interactions {
		if seenInteractionIDs[interaction.InteractionID] {
			verr.add("interactions[%d].interaction_id %q is duplicated", idx, interaction.InteractionID)
		}
		seenInteractionIDs[interaction.InteractionID] = true

		if interaction.Sequence <= lastSequence {
			verr.add("interactions[%d].sequence must be strictly increasing", idx)
		}
		lastSequence = interaction.Sequence

		verr.addNested(fmt.Sprintf("interactions[%d]", idx), interaction.validate(r.RunID, r.ScrubPolicyVersion))
	}

	for idx, issue := range r.IntegrityIssues {
		verr.addNested(fmt.Sprintf("integrity_issues[%d]", idx), issue.validate())
	}

	return verr.err()
}

func (r Run) ReplayEligible() bool {
	return r.Status == RunStatusComplete
}

func (i Interaction) Validate(expectedRunID, expectedScrubPolicyVersion string) error {
	return i.validate(expectedRunID, expectedScrubPolicyVersion)
}

func (i Interaction) validate(expectedRunID, expectedScrubPolicyVersion string) error {
	verr := &ValidationError{}

	if strings.TrimSpace(i.RunID) == "" {
		verr.add("run_id is required")
	}

	if expectedRunID != "" && i.RunID != expectedRunID {
		verr.add("run_id must match parent run_id %q", expectedRunID)
	}

	if strings.TrimSpace(i.InteractionID) == "" {
		verr.add("interaction_id is required")
	}

	if i.ParentInteractionID != "" && i.ParentInteractionID == i.InteractionID {
		verr.add("parent_interaction_id cannot equal interaction_id")
	}

	if i.Sequence <= 0 {
		verr.add("sequence must be greater than 0")
	}

	if strings.TrimSpace(i.Service) == "" {
		verr.add("service is required")
	}

	if strings.TrimSpace(i.Operation) == "" {
		verr.add("operation is required")
	}

	if !slices.Contains(validProtocols, i.Protocol) {
		verr.add("protocol must be one of %q, %q, %q, %q, %q", ProtocolHTTP, ProtocolHTTPS, ProtocolSSE, ProtocolWebSocket, ProtocolPostgres)
	}

	if i.FallbackTier != "" && !slices.Contains(validFallbackTiers, i.FallbackTier) {
		verr.add("fallback_tier must be a valid fallback tier when set")
	}

	if i.LatencyMS < 0 {
		verr.add("latency_ms cannot be negative")
	}

	verr.addNested("request", i.Request.validate())

	if len(i.Events) == 0 {
		verr.add("events must contain at least one event")
	}

	lastSequence := 0
	lastTMS := int64(-1)
	lastSimTMS := int64(-1)
	seenEventSequences := map[int]bool{}
	for idx, event := range i.Events {
		if seenEventSequences[event.Sequence] {
			verr.add("events[%d].sequence %d is duplicated", idx, event.Sequence)
		}
		seenEventSequences[event.Sequence] = true

		if event.Sequence <= lastSequence {
			verr.add("events[%d].sequence must be strictly increasing", idx)
		}
		lastSequence = event.Sequence

		if event.TMS < lastTMS {
			verr.add("events[%d].t_ms must be non-decreasing", idx)
		}
		lastTMS = event.TMS

		if event.SimTMS < lastSimTMS {
			verr.add("events[%d].sim_t_ms must be non-decreasing", idx)
		}
		lastSimTMS = event.SimTMS

		verr.addNested(fmt.Sprintf("events[%d]", idx), event.validate())
	}

	for idx, entity := range i.ExtractedEntities {
		verr.addNested(fmt.Sprintf("extracted_entities[%d]", idx), entity.validate())
	}

	verr.addNested("scrub_report", i.ScrubReport.validate(expectedScrubPolicyVersion))

	return verr.err()
}

func (r Request) validate() error {
	verr := &ValidationError{}

	if strings.TrimSpace(r.URL) == "" {
		verr.add("url is required")
	}

	if strings.TrimSpace(r.Method) == "" {
		verr.add("method is required")
	}

	for key := range r.Headers {
		if strings.TrimSpace(key) == "" {
			verr.add("headers cannot contain an empty header name")
			break
		}
	}

	return verr.err()
}

func (e Event) validate() error {
	verr := &ValidationError{}

	if e.Sequence <= 0 {
		verr.add("sequence must be greater than 0")
	}

	if e.TMS < 0 {
		verr.add("t_ms cannot be negative")
	}

	if e.SimTMS < 0 {
		verr.add("sim_t_ms cannot be negative")
	}

	if strings.TrimSpace(string(e.Type)) == "" {
		verr.add("type is required")
	}

	return verr.err()
}

func (e ExtractedEntity) validate() error {
	verr := &ValidationError{}

	if strings.TrimSpace(e.Type) == "" {
		verr.add("type is required")
	}

	if strings.TrimSpace(e.ID) == "" {
		verr.add("id is required")
	}

	return verr.err()
}

func (s ScrubReport) validate(expectedPolicyVersion string) error {
	verr := &ValidationError{}

	if strings.TrimSpace(s.ScrubPolicyVersion) == "" {
		verr.add("scrub_policy_version is required")
	}

	if expectedPolicyVersion != "" && s.ScrubPolicyVersion != expectedPolicyVersion {
		verr.add("scrub_policy_version must match run scrub_policy_version %q", expectedPolicyVersion)
	}

	if strings.TrimSpace(s.SessionSaltID) == "" {
		verr.add("session_salt_id is required")
	}

	for _, path := range s.RedactedPaths {
		if strings.TrimSpace(path) == "" {
			verr.add("redacted_paths cannot contain empty values")
			break
		}
	}

	return verr.err()
}

func (i IntegrityIssue) validate() error {
	verr := &ValidationError{}

	if strings.TrimSpace(string(i.Code)) == "" {
		verr.add("code is required")
	}

	if strings.TrimSpace(i.Message) == "" {
		verr.add("message is required")
	}

	if i.EventSequence < 0 {
		verr.add("event_sequence cannot be negative")
	}

	return verr.err()
}
