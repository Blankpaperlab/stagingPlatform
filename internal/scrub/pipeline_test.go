package scrub

import (
	"net/url"
	"slices"
	"strings"
	"testing"

	"stagehand/internal/recorder"
	"stagehand/internal/scrub/detectors"
)

func TestScrubInteractionAppliesStructuralRules(t *testing.T) {
	pipeline := mustNewPipeline(t, Options{
		PolicyVersion: "v1",
		SessionSaltID: "salt_test",
		HashSalt:      []byte("session-salt"),
		Rules: append(DefaultRules(),
			Rule{
				Name:    "email-mask",
				Pattern: "request.body.customer.email",
				Action:  ActionMask,
			},
			Rule{
				Name:    "message-content-mask",
				Pattern: "request.body.messages[*].content",
				Action:  ActionMask,
			},
			Rule{
				Name:    "query-token-hash",
				Pattern: "request.query.token",
				Action:  ActionHash,
			},
			Rule{
				Name:    "response-email-mask",
				Pattern: "events[*].data.body.customer.email",
				Action:  ActionMask,
			},
		),
	})

	interaction := baseInteraction()
	scrubbed, err := pipeline.ScrubInteraction(interaction)
	if err != nil {
		t.Fatalf("ScrubInteraction() error = %v", err)
	}

	if _, ok := scrubbed.Request.Headers["authorization"]; ok {
		t.Fatalf("authorization header was not dropped")
	}
	if _, ok := scrubbed.Request.Headers["cookie"]; ok {
		t.Fatalf("cookie header was not dropped")
	}

	body, ok := scrubbed.Request.Body.(map[string]any)
	if !ok {
		t.Fatalf("Request.Body type = %T, want map[string]any", scrubbed.Request.Body)
	}
	customer := body["customer"].(map[string]any)
	if customer["email"] == "alice@example.com" {
		t.Fatalf("request.body.customer.email was not masked")
	}
	messages := body["messages"].([]any)
	if got := messages[0].(map[string]any)["content"].(string); !strings.HasSuffix(got, "orld") {
		t.Fatalf("masked message content = %q, want masked suffix", got)
	}

	if strings.Contains(scrubbed.Request.URL, "tok_live_secret") {
		t.Fatalf("query token was not hashed: %q", scrubbed.Request.URL)
	}
	if !strings.Contains(scrubbed.Request.URL, "token=hash_") {
		t.Fatalf("query token hash missing from scrubbed URL: %q", scrubbed.Request.URL)
	}

	eventEmail := scrubbed.Events[0].Data["body"].(map[string]any)["customer"].(map[string]any)["email"].(string)
	if eventEmail == "alice@example.com" {
		t.Fatalf("events[0].data.body.customer.email was not masked")
	}

	wantPaths := []string{
		"request.headers.authorization",
		"request.headers.cookie",
		"request.query.token",
		"request.body.customer.email",
		"request.body.messages[0].content",
		"request.body.messages[1].content",
		"events[0].data.body.customer.email",
	}
	if !slices.Equal(scrubbed.ScrubReport.RedactedPaths, wantPaths) {
		t.Fatalf("ScrubReport.RedactedPaths = %#v, want %#v", scrubbed.ScrubReport.RedactedPaths, wantPaths)
	}
	if scrubbed.ScrubReport.ScrubPolicyVersion != "v1" {
		t.Fatalf("ScrubPolicyVersion = %q, want %q", scrubbed.ScrubReport.ScrubPolicyVersion, "v1")
	}
	if scrubbed.ScrubReport.SessionSaltID != "salt_test" {
		t.Fatalf("SessionSaltID = %q, want %q", scrubbed.ScrubReport.SessionSaltID, "salt_test")
	}
}

func TestScrubInteractionPreserveRuleOverridesBroaderWildcard(t *testing.T) {
	pipeline := mustNewPipeline(t, Options{
		PolicyVersion: "v1",
		SessionSaltID: "salt_test",
		HashSalt:      []byte("session-salt"),
		Rules: []Rule{
			{
				Name:    "mask-all-message-content",
				Pattern: "request.body.messages[*].content",
				Action:  ActionMask,
			},
			{
				Name:    "preserve-first-message",
				Pattern: "request.body.messages[0].content",
				Action:  ActionPreserve,
			},
		},
	})

	interaction := baseInteraction()
	scrubbed, err := pipeline.ScrubInteraction(interaction)
	if err != nil {
		t.Fatalf("ScrubInteraction() error = %v", err)
	}

	messages := scrubbed.Request.Body.(map[string]any)["messages"].([]any)
	if got := messages[0].(map[string]any)["content"].(string); got != "hello world" {
		t.Fatalf("messages[0].content = %q, want preserved original", got)
	}
	if got := messages[1].(map[string]any)["content"].(string); got == "follow-up details" {
		t.Fatalf("messages[1].content = %q, want masked value", got)
	}

	wantPaths := []string{"request.body.messages[1].content"}
	if !slices.Equal(scrubbed.ScrubReport.RedactedPaths, wantPaths) {
		t.Fatalf("ScrubReport.RedactedPaths = %#v, want %#v", scrubbed.ScrubReport.RedactedPaths, wantPaths)
	}
}

func TestScrubInteractionHashIsDeterministicWithinPipeline(t *testing.T) {
	pipeline := mustNewPipeline(t, Options{
		PolicyVersion: "v1",
		SessionSaltID: "salt_test",
		HashSalt:      []byte("session-salt"),
		Rules: []Rule{
			{
				Name:    "hash-email",
				Pattern: "request.body.customer.email",
				Action:  ActionHash,
			},
		},
	})

	first, err := pipeline.ScrubInteraction(baseInteraction())
	if err != nil {
		t.Fatalf("first ScrubInteraction() error = %v", err)
	}
	second, err := pipeline.ScrubInteraction(baseInteraction())
	if err != nil {
		t.Fatalf("second ScrubInteraction() error = %v", err)
	}

	firstEmail := first.Request.Body.(map[string]any)["customer"].(map[string]any)["email"]
	secondEmail := second.Request.Body.(map[string]any)["customer"].(map[string]any)["email"]
	if firstEmail != secondEmail {
		t.Fatalf("hashed emails differ: %v vs %v", firstEmail, secondEmail)
	}
	if !strings.HasSuffix(firstEmail.(string), "@scrub.local") {
		t.Fatalf("hashed email = %q, want email-shaped replacement", firstEmail)
	}
}

func TestScrubInteractionHashDiffersAcrossSessions(t *testing.T) {
	firstPipeline := mustNewPipeline(t, Options{
		PolicyVersion: "v1",
		SessionSaltID: "salt_session_a",
		HashSalt:      []byte("session-salt-a"),
		Rules: []Rule{
			{
				Name:    "hash-email",
				Pattern: "request.body.customer.email",
				Action:  ActionHash,
			},
		},
	})

	secondPipeline := mustNewPipeline(t, Options{
		PolicyVersion: "v1",
		SessionSaltID: "salt_session_b",
		HashSalt:      []byte("session-salt-b"),
		Rules: []Rule{
			{
				Name:    "hash-email",
				Pattern: "request.body.customer.email",
				Action:  ActionHash,
			},
		},
	})

	first, err := firstPipeline.ScrubInteraction(baseInteraction())
	if err != nil {
		t.Fatalf("first ScrubInteraction() error = %v", err)
	}
	second, err := secondPipeline.ScrubInteraction(baseInteraction())
	if err != nil {
		t.Fatalf("second ScrubInteraction() error = %v", err)
	}

	firstEmail := first.Request.Body.(map[string]any)["customer"].(map[string]any)["email"]
	secondEmail := second.Request.Body.(map[string]any)["customer"].(map[string]any)["email"]
	if firstEmail == secondEmail {
		t.Fatalf("hashed emails should differ across sessions: %v vs %v", firstEmail, secondEmail)
	}
}

func TestScrubInteractionReplayIdentifierParityWithinSession(t *testing.T) {
	pipeline := mustNewPipeline(t, Options{
		PolicyVersion: "v1",
		SessionSaltID: "salt_test",
		HashSalt:      []byte("session-salt"),
		Rules: []Rule{
			{
				Name:    "hash-body-email",
				Pattern: "request.body.customer.email",
				Action:  ActionHash,
			},
			{
				Name:    "hash-query-email",
				Pattern: "request.query.email",
				Action:  ActionHash,
			},
		},
	})

	create := baseInteraction()
	create.Request.URL = "https://api.example.com/customers"

	lookup := baseInteraction()
	lookup.Request.URL = "https://api.example.com/customers?email=alice@example.com"
	lookup.Request.Body = nil

	scrubbedCreate, err := pipeline.ScrubInteraction(create)
	if err != nil {
		t.Fatalf("create ScrubInteraction() error = %v", err)
	}
	scrubbedLookup, err := pipeline.ScrubInteraction(lookup)
	if err != nil {
		t.Fatalf("lookup ScrubInteraction() error = %v", err)
	}

	bodyEmail := scrubbedCreate.Request.Body.(map[string]any)["customer"].(map[string]any)["email"].(string)
	if !strings.Contains(scrubbedLookup.Request.URL, urlEncoded("email", bodyEmail)) {
		t.Fatalf("lookup URL = %q, want hashed body email %q to be reused for replay parity", scrubbedLookup.Request.URL, bodyEmail)
	}
}

func TestScrubInteractionAppliesDetectorsWithoutStructuralRules(t *testing.T) {
	pipeline := mustNewPipeline(t, Options{
		PolicyVersion:   "v1",
		SessionSaltID:   "salt_test",
		HashSalt:        []byte("session-salt"),
		Rules:           DefaultRules(),
		DetectorLibrary: detectors.DefaultLibrary(),
	})

	interaction := baseInteraction()
	interaction.Request.Headers["x-customer-email"] = []string{"alice@example.com"}
	interaction.Request.URL = "https://api.openai.com/v1/chat/completions?email=alice@example.com&token=" + url.QueryEscape(fakeOpenAIProjectKey())
	interaction.Request.Body = map[string]any{
		"notes": "Email alice@example.com or call +1 (415) 555-2671 with SSN 123-45-6789 and card 4242 4242 4242 4242 using " + fakeStripeKey(),
	}
	interaction.Events[0].Data = map[string]any{
		"body": map[string]any{
			"message": "JWT " + fakeJWT() + " for alice@example.com",
		},
	}

	scrubbed, err := pipeline.ScrubInteraction(interaction)
	if err != nil {
		t.Fatalf("ScrubInteraction() error = %v", err)
	}

	if got := scrubbed.Request.Headers["x-customer-email"][0]; got == "alice@example.com" {
		t.Fatal("x-customer-email header was not detector-scrubbed")
	}

	if strings.Contains(scrubbed.Request.URL, "alice@example.com") || strings.Contains(scrubbed.Request.URL, fakeOpenAIProjectKey()) {
		t.Fatalf("request URL still contains detector-matched plaintext: %q", scrubbed.Request.URL)
	}

	body := scrubbed.Request.Body.(map[string]any)
	notes := body["notes"].(string)
	for _, secret := range []string{
		"alice@example.com",
		"+1 (415) 555-2671",
		"123-45-6789",
		"4242 4242 4242 4242",
		fakeStripeKey(),
	} {
		if strings.Contains(notes, secret) {
			t.Fatalf("detector-scrubbed body still contains %q: %q", secret, notes)
		}
	}

	eventMessage := scrubbed.Events[0].Data["body"].(map[string]any)["message"].(string)
	if strings.Contains(eventMessage, "alice@example.com") || strings.Contains(eventMessage, fakeJWTHeaderPrefix()) {
		t.Fatalf("detector-scrubbed event data still contains plaintext secrets: %q", eventMessage)
	}

	wantPaths := []string{
		"request.headers.authorization",
		"request.headers.cookie",
		"request.headers.x-customer-email",
		"request.query.email",
		"request.query.token",
		"request.body.notes",
		"events[0].data.body.message",
	}
	if !slices.Equal(scrubbed.ScrubReport.RedactedPaths, wantPaths) {
		t.Fatalf("ScrubReport.RedactedPaths = %#v, want %#v", scrubbed.ScrubReport.RedactedPaths, wantPaths)
	}
}

func TestNewPipelineValidatesHashSaltRequirement(t *testing.T) {
	_, err := NewPipeline(Options{
		PolicyVersion: "v1",
		SessionSaltID: "salt_test",
		Rules: []Rule{
			{
				Pattern: "request.query.token",
				Action:  ActionHash,
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "hash_salt is empty") {
		t.Fatalf("NewPipeline() error = %v, want hash_salt validation error", err)
	}
}

func TestNewPipelineValidatesHashSaltForDetectorScrubbing(t *testing.T) {
	_, err := NewPipeline(Options{
		PolicyVersion:   "v1",
		SessionSaltID:   "salt_test",
		Rules:           DefaultRules(),
		DetectorLibrary: detectors.DefaultLibrary(),
	})
	if err == nil || !strings.Contains(err.Error(), "detector-based scrubbing requires hash_salt") {
		t.Fatalf("NewPipeline() error = %v, want detector hash_salt validation error", err)
	}
}

func TestScrubInteractionRejectsMaskingComplexObject(t *testing.T) {
	pipeline := mustNewPipeline(t, Options{
		PolicyVersion: "v1",
		SessionSaltID: "salt_test",
		Rules: []Rule{
			{
				Pattern: "request.body.customer",
				Action:  ActionMask,
			},
		},
	})

	_, err := pipeline.ScrubInteraction(baseInteraction())
	if err == nil || !strings.Contains(err.Error(), "mask request.body.customer") {
		t.Fatalf("ScrubInteraction() error = %v, want complex-object mask failure", err)
	}
}

func mustNewPipeline(t *testing.T, opts Options) *Pipeline {
	t.Helper()

	pipeline, err := NewPipeline(opts)
	if err != nil {
		t.Fatalf("NewPipeline() error = %v", err)
	}

	return pipeline
}

func baseInteraction() recorder.Interaction {
	return recorder.Interaction{
		RunID:         "run_123",
		InteractionID: "int_123",
		Sequence:      1,
		Service:       "openai",
		Operation:     "chat.completions.create",
		Protocol:      recorder.ProtocolHTTPS,
		Request: recorder.Request{
			URL:    "https://api.openai.com/v1/chat/completions?token=tok_live_secret&keep=true",
			Method: "POST",
			Headers: map[string][]string{
				"authorization": {"Bearer sk-live-secret"},
				"cookie":        {"session=secret-cookie"},
				"content-type":  {"application/json"},
			},
			Body: map[string]any{
				"customer": map[string]any{
					"email": "alice@example.com",
				},
				"messages": []any{
					map[string]any{
						"content": "hello world",
						"role":    "user",
					},
					map[string]any{
						"content": "follow-up details",
						"role":    "user",
					},
				},
			},
		},
		Events: []recorder.Event{
			{
				Sequence: 1,
				TMS:      0,
				SimTMS:   0,
				Type:     recorder.EventTypeResponseReceived,
				Data: map[string]any{
					"body": map[string]any{
						"customer": map[string]any{
							"email": "alice@example.com",
						},
					},
					"status_code": 200,
				},
			},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "placeholder",
			SessionSaltID:      "placeholder",
		},
	}
}

func urlEncoded(key, value string) string {
	return key + "=" + url.QueryEscape(value)
}

func fakeStripeKey() string {
	return "sk" + "_live_" + "1234567890abcdef1234567890abcdef"
}

func fakeOpenAIProjectKey() string {
	return "sk" + "-proj-" + "abcdefghijklmnopQRST_uvwx"
}

func fakeJWT() string {
	return fakeJWTHeaderPrefix() + ".eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkFsaWNlIn0.c2lnbmF0dXJl"
}

func fakeJWTHeaderPrefix() string {
	return "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"
}
