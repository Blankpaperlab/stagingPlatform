package main

import (
	"context"
	"testing"

	"stagehand/internal/recorder"
)

func TestCustomAPIRegressionDemoPasses(t *testing.T) {
	t.Parallel()

	summary, err := RunCustomAPIDemo(context.Background())
	if err != nil {
		t.Fatalf("RunCustomAPIDemo() error = %v", err)
	}

	if summary.SessionName != demoSessionName {
		t.Fatalf("SessionName = %q, want %q", summary.SessionName, demoSessionName)
	}
	if summary.CRM.Service != "internal-crm" || summary.CRM.StatusCode != 202 || summary.CRM.ReplayTier != "exact" {
		t.Fatalf("CRM summary = %#v, want exact internal CRM replay", summary.CRM)
	}
	if summary.CRM.ResponseBody["customers"] == nil {
		t.Fatalf("CRM response body = %#v, want customers", summary.CRM.ResponseBody)
	}
	if summary.Billing.Service != "internal-billing" || summary.Billing.StatusCode != 200 || summary.Billing.ReplayTier != "exact" {
		t.Fatalf("Billing summary = %#v, want exact internal billing replay", summary.Billing)
	}
	if summary.Billing.ResponseBody["refund_id"] != "rf_internal_123" {
		t.Fatalf("Billing response body = %#v, want refund_id rf_internal_123", summary.Billing.ResponseBody)
	}
	if summary.Regression.TotalChanges != 1 || summary.Regression.FailingChanges != 1 {
		t.Fatalf("Regression summary = %#v, want one failing change", summary.Regression)
	}
	if summary.Regression.FirstType != "modified" || summary.Regression.FirstService != "internal-billing" {
		t.Fatalf("Regression first change = %#v, want modified internal-billing", summary.Regression)
	}
}

func TestCustomAPIRegressionDemoIgnoresDynamicOnlyChanges(t *testing.T) {
	t.Parallel()

	regression, err := compareCustomAPIRegression(baselineRun(), dynamicOnlyCandidateRun())
	if err != nil {
		t.Fatalf("compareCustomAPIRegression() error = %v", err)
	}

	if regression.TotalChanges != 0 || regression.FailingChanges != 0 {
		t.Fatalf("Regression summary = %#v, want no changes for dynamic-only variance", regression)
	}
}

func dynamicOnlyCandidateRun() recorder.Run {
	run := baselineRun()
	run.RunID = "run_custom_api_dynamic_only"
	run.Mode = recorder.RunModeReplay
	run.Interactions = []recorder.Interaction{
		customAPIInteraction(
			run.RunID,
			"int_crm_dynamic_only",
			1,
			"internal-crm",
			"POST /v1/customers/search",
			"https://crm.internal.test/v1/customers/search?cursor=dynamic-only",
			map[string]any{
				"filters":    map[string]any{"email": "jane.doe@example.com", "active": true},
				"limit":      float64(25),
				"request_id": "req_dynamic_only_crm",
			},
			202,
			map[string]any{
				"customers":    []any{map[string]any{"id": "cus_internal_123", "email": "jane.doe@example.com"}},
				"next_cursor":  nil,
				"generated_at": "2026-05-08T09:00:00Z",
				"trace_id":     "trace_dynamic_only_crm",
			},
		),
		customAPIInteraction(
			run.RunID,
			"int_billing_dynamic_only",
			2,
			"internal-billing",
			"POST /api/refunds",
			"https://billing.internal.test/api/refunds",
			map[string]any{
				"payment_id":      "pay_123",
				"amount":          float64(1500),
				"idempotency_key": "idem_dynamic_only_refund",
			},
			200,
			map[string]any{
				"refund_id": "rf_internal_123",
				"status":    "approved",
				"trace_id":  "trace_dynamic_only_billing",
			},
		),
	}
	return run
}
