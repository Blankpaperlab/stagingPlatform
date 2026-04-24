package recording_test

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"stagehand/internal/config"
	"stagehand/internal/recorder"
	"stagehand/internal/recording"
	sqlitestore "stagehand/internal/store/sqlite"
)

func TestWriterMatchesSharedScrubParityFixture(t *testing.T) {
	t.Parallel()

	fixture := loadScrubParityFixture(t)
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "stagehand.db")
	sqliteStore, err := sqlitestore.OpenStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	run := validRunningRunRecord()
	if err := sqliteStore.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Scrub.CustomRules = append(cfg.Scrub.CustomRules, config.ScrubRule{
		Name:    "response-token",
		Pattern: "response.headers.X-Response-Token",
		Action:  "hash",
	})

	saltManager := newSaltManager(t, sqliteStore)
	writer, err := recording.NewWriter(recording.WriterOptions{
		Store:       sqliteStore,
		SaltManager: saltManager,
		ScrubConfig: cfg.Scrub,
	})
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}

	raw := fixture.toInteraction(run.RunID)
	scrubbed, err := writer.PersistInteraction(ctx, raw)
	if err != nil {
		t.Fatalf("PersistInteraction() error = %v", err)
	}

	if !reflect.DeepEqual(scrubbed.ScrubReport.RedactedPaths, fixture.ExpectedScrub.RedactedPaths) {
		t.Fatalf("ScrubReport.RedactedPaths = %#v, want %#v", scrubbed.ScrubReport.RedactedPaths, fixture.ExpectedScrub.RedactedPaths)
	}

	for _, header := range fixture.ExpectedScrub.DroppedRequestHeaders {
		if _, ok := scrubbed.Request.Headers[strings.ToLower(header)]; ok {
			t.Fatalf("scrubbed request headers still contain dropped header %q", header)
		}
	}

	hashedEmail := scrubbed.Request.Headers["x-customer-email"][0]
	if hashedEmail == "alice@example.com" {
		t.Fatal("x-customer-email was not scrubbed")
	}

	scrubbedURL, err := url.Parse(scrubbed.Request.URL)
	if err != nil {
		t.Fatalf("url.Parse(scrubbed.Request.URL) error = %v", err)
	}
	if got := scrubbedURL.Query().Get("email"); got != hashedEmail {
		t.Fatalf("scrubbed query email = %q, want %q", got, hashedEmail)
	}

	notes, _ := scrubbed.Request.Body.(map[string]any)["notes"].(string)
	if !strings.Contains(notes, hashedEmail) {
		t.Fatalf("scrubbed request notes = %q, want hashed email %q", notes, hashedEmail)
	}

	responseEvent := scrubbed.Events[1]
	responseHeaderValues := responseHeaderValues(t, responseEvent.Data["headers"], fixture.ExpectedScrub.ResponseHeaderPath)
	if len(responseHeaderValues) != 1 {
		t.Fatalf("scrubbed response header %q = %#v, want single value", fixture.ExpectedScrub.ResponseHeaderPath, responseHeaderValues)
	}
	if got := responseHeaderValues[0]; got == fixture.Response.Headers["x-response-token"] {
		t.Fatalf("response header %q was not scrubbed", fixture.ExpectedScrub.ResponseHeaderPath)
	}

	responseBody, _ := responseEvent.Data["body"].(map[string]any)
	if got := responseBody["assistant_email"]; got != hashedEmail {
		t.Fatalf("scrubbed response body assistant_email = %#v, want %q", got, hashedEmail)
	}

	requestJSON, eventJSON := rawStoredJSON(t, dbPath, raw.InteractionID)
	persisted := requestJSON + "\n" + eventJSON
	for _, leak := range []string{
		fixture.Request.Headers["authorization"],
		fixture.Request.Headers["x-customer-email"],
		fixture.Response.Headers["x-response-token"],
		"alice@example.com",
	} {
		if strings.Contains(persisted, leak) {
			t.Fatalf("persisted SQLite JSON still contains plaintext %q:\n%s", leak, persisted)
		}
	}
}

type scrubParityFixture struct {
	Request struct {
		Method  string            `json:"method"`
		Path    string            `json:"path"`
		Headers map[string]string `json:"headers"`
		Body    map[string]any    `json:"body"`
	} `json:"request"`
	Response struct {
		StatusCode int               `json:"status_code"`
		Headers    map[string]string `json:"headers"`
		Body       map[string]any    `json:"body"`
	} `json:"response"`
	ExpectedScrub struct {
		RedactedPaths         []string `json:"redacted_paths"`
		DroppedRequestHeaders []string `json:"dropped_request_headers"`
		ResponseHeaderPath    string   `json:"response_header_path"`
	} `json:"expected_scrub"`
}

func loadScrubParityFixture(t *testing.T) scrubParityFixture {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}

	path := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "sdk-parity", "http-post-scrub-fixture.json")
	var fixture scrubParityFixture
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", path, err)
	}
	return fixture
}

func (f scrubParityFixture) toInteraction(runID string) recorder.Interaction {
	requestHeaders := make(map[string][]string, len(f.Request.Headers))
	for name, value := range f.Request.Headers {
		requestHeaders[strings.ToLower(name)] = []string{value}
	}

	responseHeaders := make(map[string][]string, len(f.Response.Headers))
	for name, value := range f.Response.Headers {
		responseHeaders[strings.ToLower(name)] = []string{value}
	}

	return recorder.Interaction{
		RunID:         runID,
		InteractionID: "int_parity_scrub_001",
		Sequence:      1,
		Service:       "127.0.0.1",
		Operation:     "POST /parity/scrub",
		Protocol:      recorder.ProtocolHTTP,
		Request: recorder.Request{
			URL:     "http://127.0.0.1" + f.Request.Path,
			Method:  f.Request.Method,
			Headers: requestHeaders,
			Body:    f.Request.Body,
		},
		Events: []recorder.Event{
			{
				Sequence: 1,
				TMS:      0,
				SimTMS:   0,
				Type:     recorder.EventTypeRequestSent,
			},
			{
				Sequence: 2,
				TMS:      25,
				SimTMS:   25,
				Type:     recorder.EventTypeResponseReceived,
				Data: map[string]any{
					"status_code": f.Response.StatusCode,
					"headers":     responseHeaders,
					"body":        f.Response.Body,
				},
			},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "placeholder",
			SessionSaltID:      "placeholder",
		},
		LatencyMS: 25,
	}
}

func responseHeaderValues(t *testing.T, raw any, name string) []string {
	t.Helper()

	switch headers := raw.(type) {
	case map[string][]string:
		return headers[name]
	case map[string]any:
		switch values := headers[name].(type) {
		case []string:
			return values
		case []any:
			result := make([]string, len(values))
			for idx, value := range values {
				result[idx] = value.(string)
			}
			return result
		}
	}

	return nil
}
