package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	analysisdiff "stagehand/internal/analysis/diff"
	"stagehand/internal/recorder"
	"stagehand/internal/store"
	sqlitestore "stagehand/internal/store/sqlite"
	"stagehand/internal/version"
)

const demoSessionName = "custom-api-regression-demo"
const demoStoragePath = ".stagehand/runs"

type DemoSummary struct {
	SessionName string            `json:"session_name"`
	CRM         ReplaySummary     `json:"crm"`
	Billing     ReplaySummary     `json:"billing"`
	Regression  RegressionSummary `json:"regression"`
	Artifacts   *ArtifactSummary  `json:"artifacts,omitempty"`
}

type ReplaySummary struct {
	Service      string         `json:"service"`
	Operation    string         `json:"operation"`
	StatusCode   int            `json:"status_code"`
	ReplayTier   string         `json:"replay_tier"`
	ResponseBody map[string]any `json:"response_body"`
}

type RegressionSummary struct {
	TotalChanges   int    `json:"total_changes"`
	FailingChanges int    `json:"failing_changes"`
	FirstType      string `json:"first_type,omitempty"`
	FirstService   string `json:"first_service,omitempty"`
	FirstOperation string `json:"first_operation,omitempty"`
}

type ArtifactSummary struct {
	Persisted         bool   `json:"persisted"`
	StoragePath       string `json:"storage_path"`
	DatabasePath      string `json:"database_path"`
	BaseRunID         string `json:"base_run_id"`
	CandidateRunID    string `json:"candidate_run_id"`
	InspectCommand    string `json:"inspect_command"`
	DiffCommand       string `json:"diff_command"`
	SessionInspectCmd string `json:"session_inspect_command"`
}

type replayRequest struct {
	Service   string
	Operation string
	Method    string
	URL       string
	Body      map[string]any
}

func main() {
	summary, err := RunCustomAPIDemo(context.Background())
	if err != nil {
		fatal(err)
	}
	artifacts, err := PersistCustomAPIDemoRuns(context.Background(), demoStoragePath)
	if err != nil {
		fatal(err)
	}
	summary.Artifacts = &artifacts

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		fatal(err)
	}
}

func RunCustomAPIDemo(ctx context.Context) (DemoSummary, error) {
	select {
	case <-ctx.Done():
		return DemoSummary{}, ctx.Err()
	default:
	}

	base := baselineRun()
	crmReplay, err := replayExactGenericHTTP(base.Interactions, replayRequest{
		Service:   "internal-crm",
		Operation: "POST /v1/customers/search",
		Method:    "POST",
		URL:       "https://crm.internal.test/v1/customers/search?cursor=candidate",
		Body: map[string]any{
			"filters":    map[string]any{"email": "jane.doe@example.com", "active": true},
			"limit":      float64(25),
			"request_id": "req_candidate_crm",
		},
	})
	if err != nil {
		return DemoSummary{}, err
	}

	billingReplay, err := replayExactGenericHTTP(base.Interactions, replayRequest{
		Service:   "internal-billing",
		Operation: "POST /api/refunds",
		Method:    "POST",
		URL:       "https://billing.internal.test/api/refunds",
		Body: map[string]any{
			"payment_id":      "pay_123",
			"amount":          float64(1500),
			"idempotency_key": "idem_candidate_refund",
		},
	})
	if err != nil {
		return DemoSummary{}, err
	}

	regression, err := compareCustomAPIRegression(base, regressedCandidateRun())
	if err != nil {
		return DemoSummary{}, err
	}

	return DemoSummary{
		SessionName: demoSessionName,
		CRM:         crmReplay,
		Billing:     billingReplay,
		Regression:  regression,
	}, nil
}

func PersistCustomAPIDemoRuns(ctx context.Context, storagePath string) (ArtifactSummary, error) {
	storagePath = strings.TrimSpace(storagePath)
	if storagePath == "" {
		storagePath = demoStoragePath
	}
	dbPath := sqliteDatabasePath(storagePath)

	sqliteStore, err := sqlitestore.OpenStore(ctx, dbPath)
	if err != nil {
		return ArtifactSummary{}, fmt.Errorf("open local store %q: %w", dbPath, err)
	}
	defer sqliteStore.Close()

	if err := sqliteStore.DeleteSession(ctx, demoSessionName); err != nil && !errors.Is(err, store.ErrNotFound) {
		return ArtifactSummary{}, fmt.Errorf("clear existing demo session %q: %w", demoSessionName, err)
	}

	base := baselineRun()
	candidate := regressedCandidateRun()
	for _, run := range []recorder.Run{base, candidate} {
		if err := persistDemoRun(ctx, sqliteStore, run); err != nil {
			return ArtifactSummary{}, err
		}
	}

	return ArtifactSummary{
		Persisted:         true,
		StoragePath:       storagePath,
		DatabasePath:      dbPath,
		BaseRunID:         base.RunID,
		CandidateRunID:    candidate.RunID,
		InspectCommand:    "go run ./cmd/stagehand inspect --run-id " + base.RunID + " --show-bodies",
		DiffCommand:       "go run ./cmd/stagehand diff --candidate-run-id " + candidate.RunID + " --base-run-id " + base.RunID,
		SessionInspectCmd: "go run ./cmd/stagehand inspect --session " + demoSessionName + " --show-bodies",
	}, nil
}

func persistDemoRun(ctx context.Context, artifactStore store.ArtifactStore, run recorder.Run) error {
	runRecord := store.RunRecordFromRun(run)
	if err := artifactStore.CreateRun(ctx, runRecord); err != nil {
		return fmt.Errorf("create demo run %q: %w", run.RunID, err)
	}
	for _, interaction := range run.Interactions {
		if err := artifactStore.WriteInteraction(ctx, interaction); err != nil {
			return fmt.Errorf("write demo interaction %q: %w", interaction.InteractionID, err)
		}
	}
	return nil
}

func replayExactGenericHTTP(recorded []recorder.Interaction, request replayRequest) (ReplaySummary, error) {
	want := genericHTTPReplayKey(request.Service, request.Operation, request.Method, request.URL, request.Body)
	for _, interaction := range recorded {
		key := genericHTTPReplayKey(
			interaction.Service,
			interaction.Operation,
			interaction.Request.Method,
			interaction.Request.URL,
			asObject(interaction.Request.Body),
		)
		if key != want {
			continue
		}
		statusCode, body, err := responseFromInteraction(interaction)
		if err != nil {
			return ReplaySummary{}, err
		}
		return ReplaySummary{
			Service:      interaction.Service,
			Operation:    interaction.Operation,
			StatusCode:   statusCode,
			ReplayTier:   "exact",
			ResponseBody: body,
		}, nil
	}
	return ReplaySummary{}, fmt.Errorf("no exact generic HTTP replay match for %s %s", request.Method, request.URL)
}

func compareCustomAPIRegression(base recorder.Run, candidate recorder.Run) (RegressionSummary, error) {
	result, err := analysisdiff.Compare(base, candidate, analysisdiff.Options{
		ServiceIgnoredFields: map[string]analysisdiff.ServiceIgnoredFields{
			"internal-crm": {
				RequestPaths:  []string{"query.cursor", "body.request_id"},
				ResponsePaths: []string{"body.generated_at", "body.trace_id"},
			},
			"internal-billing": {
				RequestPaths:  []string{"headers.idempotency-key", "body.idempotency_key"},
				ResponsePaths: []string{"body.trace_id"},
			},
		},
	})
	if err != nil {
		return RegressionSummary{}, err
	}

	summary := result.Summary()
	regression := RegressionSummary{
		TotalChanges:   summary.TotalChanges,
		FailingChanges: summary.FailingChanges,
	}
	if len(result.FailingChanges()) > 0 {
		first := result.FailingChanges()[0]
		regression.FirstType = string(first.Type)
		regression.FirstService = first.Service
		regression.FirstOperation = first.Operation
	}
	return regression, nil
}

func baselineRun() recorder.Run {
	return recorder.Run{
		SchemaVersion:      recorder.ArtifactSchemaVersion,
		SDKVersion:         version.ArtifactVersion,
		RuntimeVersion:     version.ArtifactVersion,
		ScrubPolicyVersion: "v1",
		RunID:              "run_custom_api_base",
		SessionName:        demoSessionName,
		Mode:               recorder.RunModeRecord,
		Status:             recorder.RunStatusComplete,
		StartedAt:          time.Unix(1, 0).UTC(),
		EndedAt:            timePtr(time.Unix(2, 0).UTC()),
		Interactions: []recorder.Interaction{
			customAPIInteraction(
				"run_custom_api_base",
				"int_crm_base",
				1,
				"internal-crm",
				"POST /v1/customers/search",
				"https://crm.internal.test/v1/customers/search?cursor=record",
				map[string]any{
					"filters":    map[string]any{"email": "jane.doe@example.com", "active": true},
					"limit":      float64(25),
					"request_id": "req_record_crm",
				},
				202,
				map[string]any{
					"customers":    []any{map[string]any{"id": "cus_internal_123", "email": "jane.doe@example.com"}},
					"next_cursor":  nil,
					"generated_at": "2026-05-07T12:00:00Z",
					"trace_id":     "trace_record_crm",
				},
			),
			customAPIInteraction(
				"run_custom_api_base",
				"int_billing_base",
				2,
				"internal-billing",
				"POST /api/refunds",
				"https://billing.internal.test/api/refunds",
				map[string]any{
					"payment_id":      "pay_123",
					"amount":          float64(1500),
					"idempotency_key": "idem_record_refund",
				},
				200,
				map[string]any{
					"refund_id": "rf_internal_123",
					"status":    "approved",
					"trace_id":  "trace_record_billing",
				},
			),
		},
	}
}

func regressedCandidateRun() recorder.Run {
	run := baselineRun()
	run.RunID = "run_custom_api_candidate"
	run.Mode = recorder.RunModeReplay
	run.StartedAt = time.Unix(3, 0).UTC()
	run.EndedAt = timePtr(time.Unix(4, 0).UTC())
	run.Interactions = []recorder.Interaction{
		customAPIInteraction(
			run.RunID,
			"int_crm_candidate",
			1,
			"internal-crm",
			"POST /v1/customers/search",
			"https://crm.internal.test/v1/customers/search?cursor=candidate",
			map[string]any{
				"filters":    map[string]any{"email": "jane.doe@example.com", "active": true},
				"limit":      float64(25),
				"request_id": "req_candidate_crm",
			},
			202,
			map[string]any{
				"customers":    []any{map[string]any{"id": "cus_internal_123", "email": "jane.doe@example.com"}},
				"next_cursor":  nil,
				"generated_at": "2026-05-07T12:01:00Z",
				"trace_id":     "trace_candidate_crm",
			},
		),
		customAPIInteraction(
			run.RunID,
			"int_billing_candidate",
			2,
			"internal-billing",
			"POST /api/refunds",
			"https://billing.internal.test/api/refunds",
			map[string]any{
				"payment_id":      "pay_123",
				"amount":          float64(1500),
				"idempotency_key": "idem_candidate_refund",
			},
			409,
			map[string]any{
				"error":    "refund_window_closed",
				"trace_id": "trace_candidate_billing",
			},
		),
	}
	return run
}

func customAPIInteraction(runID, interactionID string, sequence int, service, operation, rawURL string, requestBody map[string]any, statusCode int, responseBody map[string]any) recorder.Interaction {
	return recorder.Interaction{
		RunID:         runID,
		InteractionID: interactionID,
		Sequence:      sequence,
		Service:       service,
		Operation:     operation,
		Protocol:      recorder.ProtocolHTTPS,
		Streaming:     false,
		FallbackTier:  recorder.FallbackTierExact,
		Request: recorder.Request{
			URL:    rawURL,
			Method: "POST",
			Headers: map[string][]string{
				"content-type": {"application/json"},
			},
			Body: requestBody,
		},
		Events: []recorder.Event{
			{Sequence: 1, TMS: 0, SimTMS: 0, Type: recorder.EventTypeRequestSent},
			{
				Sequence: 2,
				TMS:      12,
				SimTMS:   12,
				Type:     recorder.EventTypeResponseReceived,
				Data: map[string]any{
					"status_code": statusCode,
					"headers": map[string][]string{
						"content-type": {"application/json"},
					},
					"body": responseBody,
				},
			},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "v1",
			SessionSaltID:      "salt_custom_api_demo",
		},
		LatencyMS: 12,
	}
}

func genericHTTPReplayKey(service, operation, method, rawURL string, body map[string]any) string {
	return strings.Join([]string{
		service,
		operation,
		strings.ToUpper(method),
		normalizedDemoURL(rawURL),
		canonicalJSON(removeMutableDemoFields(body)),
	}, "\x00")
}

func normalizedDemoURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return strings.TrimSpace(raw)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	query := parsed.Query()
	query.Del("cursor")
	query.Del("page_token")
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String()
}

func removeMutableDemoFields(source map[string]any) map[string]any {
	cloned := cloneMap(source)
	delete(cloned, "request_id")
	delete(cloned, "idempotency_key")
	return cloned
}

func responseFromInteraction(interaction recorder.Interaction) (int, map[string]any, error) {
	for _, event := range interaction.Events {
		if event.Type != recorder.EventTypeResponseReceived {
			continue
		}
		status, ok := numberAsInt(event.Data["status_code"])
		if !ok {
			return 0, nil, fmt.Errorf("interaction %s missing status_code", interaction.InteractionID)
		}
		body, ok := event.Data["body"].(map[string]any)
		if !ok {
			return 0, nil, fmt.Errorf("interaction %s response body is not an object", interaction.InteractionID)
		}
		return status, body, nil
	}
	return 0, nil, fmt.Errorf("interaction %s has no response_received event", interaction.InteractionID)
}

func asObject(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

func cloneMap(source map[string]any) map[string]any {
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		switch typed := value.(type) {
		case map[string]any:
			cloned[key] = cloneMap(typed)
		case []any:
			cloned[key] = cloneSlice(typed)
		default:
			cloned[key] = typed
		}
	}
	return cloned
}

func cloneSlice(source []any) []any {
	cloned := make([]any, len(source))
	for idx, value := range source {
		switch typed := value.(type) {
		case map[string]any:
			cloned[idx] = cloneMap(typed)
		case []any:
			cloned[idx] = cloneSlice(typed)
		default:
			cloned[idx] = typed
		}
	}
	return cloned
}

func canonicalJSON(value any) string {
	encoded, err := json.Marshal(normalizeMapOrder(value))
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(encoded)
}

func normalizeMapOrder(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		ordered := make(map[string]any, len(typed))
		for _, key := range keys {
			ordered[key] = normalizeMapOrder(typed[key])
		}
		return ordered
	case []any:
		items := make([]any, len(typed))
		for idx, item := range typed {
			items[idx] = normalizeMapOrder(item)
		}
		return items
	default:
		return typed
	}
}

func numberAsInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func sqliteDatabasePath(storagePath string) string {
	cleaned := filepath.Clean(storagePath)
	if strings.EqualFold(filepath.Ext(cleaned), ".db") || strings.EqualFold(filepath.Ext(cleaned), ".sqlite") || strings.EqualFold(filepath.Ext(cleaned), ".sqlite3") {
		return cleaned
	}

	return filepath.Join(cleaned, "stagehand.db")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
