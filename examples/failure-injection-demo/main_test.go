package main

import (
	"context"
	"path/filepath"
	"testing"

	"stagehand/internal/runtime/injection"
)

func TestFailureInjectionDemoPasses(t *testing.T) {
	t.Parallel()

	summary, err := RunFailureInjectionDemo(context.Background(), filepath.Join(t.TempDir(), "stagehand.db"))
	if err != nil {
		t.Fatalf("RunFailureInjectionDemo() error = %v", err)
	}

	if summary.SessionName != demoSessionName {
		t.Fatalf("SessionName = %q, want %q", summary.SessionName, demoSessionName)
	}
	if err := validateDemoAttempts(summary.Attempts); err != nil {
		t.Fatalf("validateDemoAttempts() error = %v", err)
	}
	if len(summary.Provenance) != 1 {
		t.Fatalf("Provenance len = %d, want 1", len(summary.Provenance))
	}
	provenance := summary.Provenance[0]
	if provenance.Service != "stripe" || provenance.Operation != "payment_intents.create" || provenance.CallNumber != 3 {
		t.Fatalf("Provenance[0] = %#v, want stripe payment_intents.create third-call failure", provenance)
	}

	metadataSection, ok := summary.PersistedMetadata[injection.MetadataKey].(map[string]any)
	if !ok {
		t.Fatalf("PersistedMetadata[%q] = %#v, want map", injection.MetadataKey, summary.PersistedMetadata[injection.MetadataKey])
	}
	applied, ok := metadataSection["applied"].([]any)
	if !ok || len(applied) != 1 {
		t.Fatalf("PersistedMetadata applied = %#v, want one applied provenance entry", metadataSection["applied"])
	}
}
