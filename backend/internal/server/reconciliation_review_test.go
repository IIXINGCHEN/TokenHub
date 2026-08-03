package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestReconciliationScopesUsageToBillingConnectorProvider(t *testing.T) {
	store := NewMemoryStore()
	connector := createReconciliationTestConnector(t, store, "bcon_reconciliation_scope")
	connector.Config = map[string]string{"provider_id": "provider-a"}
	if err := store.db.Save(&connector).Error; err != nil {
		t.Fatal(err)
	}
	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	createReconciliationBillingRecords(t, store, []BillingRecord{{
		ID: "bill_provider_a", ConnectorID: connector.ID, ExternalID: "external-provider-a", SourceType: BillingConnectorOneAPI,
		Model: "model-scope", Currency: "USD", NetAmount: "1", UsageStartAt: periodStart.Add(time.Minute),
		Metadata: map[string]string{"provider_id": "provider-a"},
	}})
	createReconciliationUsageRecords(t, store, []UsageRecord{
		{ID: "use_provider_a", ProviderID: "provider-a", ModelName: "model-scope", ProviderCostUSD: 1, CreatedAt: periodStart.Add(time.Minute)},
		{ID: "use_provider_b", ProviderID: "provider-b", ModelName: "model-scope", ProviderCostUSD: 5, CreatedAt: periodStart.Add(time.Minute)},
	})

	app := New(store).Handler()
	rule := createReconciliationReviewRule(t, app, connector.ID, ReconciliationGranularityDay, []string{"model", "currency"})
	run := runReconciliationTestRule(t, app, rule.ID, periodStart, periodStart.Add(24*time.Hour))
	assertReconciliationCounts(t, run, 1, 0, 0, 0)
	if run.TokenHubRecordCount != 1 || run.ProviderAmount != "1" || run.TokenHubAmount != "1" {
		t.Fatalf("usage from another provider contaminated reconciliation: %#v", run)
	}
}

func TestReconciliationAccumulatesSubMicroAmountsBeforeRounding(t *testing.T) {
	store := NewMemoryStore()
	connector := createReconciliationTestConnector(t, store, "bcon_reconciliation_precision")
	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	createReconciliationBillingRecords(t, store, []BillingRecord{{
		ID: "bill_sub_micro", ConnectorID: connector.ID, ExternalID: "external-sub-micro", SourceType: BillingConnectorOneAPI,
		Model: "model-small", Currency: "USD", NetAmount: "0.0004", UsageStartAt: periodStart.Add(time.Minute),
	}})
	usages := make([]UsageRecord, 1000)
	for index := range usages {
		usages[index] = UsageRecord{
			ID: fmt.Sprintf("use_sub_micro_%04d", index), ProviderID: BillingConnectorOneAPI, ModelName: "model-small",
			ProviderCostUSD: 0.0000004, CreatedAt: periodStart.Add(time.Minute),
		}
	}
	createReconciliationUsageRecords(t, store, usages)

	app := New(store).Handler()
	rule := createReconciliationReviewRule(t, app, connector.ID, ReconciliationGranularityDay, []string{"model", "currency"})
	run := runReconciliationTestRule(t, app, rule.ID, periodStart, periodStart.Add(24*time.Hour))
	assertReconciliationCounts(t, run, 1, 0, 0, 0)
	if run.TokenHubRecordCount != 1000 || run.ProviderAmount != "0.0004" || run.TokenHubAmount != "0.0004" {
		t.Fatalf("sub-micro usage was rounded before aggregation: %#v", run)
	}
}

func TestNewAPIBillingConnectorRejectsDetailReconciliation(t *testing.T) {
	store := NewMemoryStore()
	connector := createReconciliationTestConnector(t, store, "bcon_reconciliation_newapi")
	connector.Type = BillingConnectorNewAPI
	if err := store.db.Save(&connector).Error; err != nil {
		t.Fatal(err)
	}
	response := doJSON(t, New(store).Handler(), http.MethodPost, "/api/admin/billing/reconciliation-rules", map[string]any{
		"name": "Unsupported detail", "connector_id": connector.ID, "granularity": ReconciliationGranularityDetail,
		"match_dimensions": []string{"request_id", "currency"}, "timezone": "UTC",
	}, "")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body, "reconciliation_detail_unsupported") {
		t.Fatalf("NewAPI detail reconciliation was accepted: %d %s", response.Code, response.Body)
	}
}

func TestDetailReconciliationMaximizesMatchesBeforeDistance(t *testing.T) {
	base := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	providers := []reconciliationDetailEntry{{id: "provider-zero", occurredAt: base}, {id: "provider-five", occurredAt: base.Add(5 * time.Minute)}}
	usages := []reconciliationDetailEntry{{id: "usage-minus-five", occurredAt: base.Add(-5 * time.Minute)}, {id: "usage-four", occurredAt: base.Add(4 * time.Minute)}}
	matches := matchReconciliationDetailEntries(providers, usages, 5*time.Minute)
	if len(matches) != 2 || matches[0] != 0 || matches[1] != 1 {
		t.Fatalf("matching did not maximize cardinality before time distance: %#v", matches)
	}
}

func TestFailedRecalculationPreservesSuccessfulRunAndAuditsFailure(t *testing.T) {
	store := NewMemoryStore()
	connector := createReconciliationTestConnector(t, store, "bcon_reconciliation_recalculate_failure")
	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	createReconciliationBillingRecords(t, store, []BillingRecord{{
		ID: "bill_recalculate", ConnectorID: connector.ID, ExternalID: "external-recalculate", SourceType: BillingConnectorOneAPI,
		Model: "model-recalculate", Currency: "USD", NetAmount: "1", UsageStartAt: periodStart.Add(time.Minute),
	}})
	createReconciliationUsageRecords(t, store, []UsageRecord{{
		ID: "use_recalculate", ProviderID: BillingConnectorOneAPI, ModelName: "model-recalculate", ProviderCostUSD: 1, CreatedAt: periodStart.Add(time.Minute),
	}})
	app := New(store).Handler()
	rule := createReconciliationReviewRule(t, app, connector.ID, ReconciliationGranularityDay, []string{"model", "currency"})
	run := runReconciliationTestRule(t, app, rule.ID, periodStart, periodStart.Add(24*time.Hour))
	before := getReconciliationTestDetail(t, app, run.ID)
	if err := store.db.Model(&BillingRecord{}).Where("id = ?", "bill_recalculate").Update("net_amount", "not-a-decimal").Error; err != nil {
		t.Fatal(err)
	}

	response := doJSON(t, app, http.MethodPost, "/api/admin/billing/reconciliations/"+run.ID+"/recalculate", map[string]any{}, "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected failed recalculation, got %d %s", response.Code, response.Body)
	}
	stored, err := store.GetReconciliationRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	after := getReconciliationTestDetail(t, app, run.ID)
	if stored.Status != ReconciliationRunSucceeded || stored.InputHash != run.InputHash || len(after.Items) != len(before.Items) || after.Items[0].ID != before.Items[0].ID {
		t.Fatalf("failed recalculation replaced the last successful result: before=%#v after=%#v", before, after)
	}
	assertReconciliationAuditEvent(t, store, "recalculate", "failed", "internal_error")
}

func TestReconciliationFailureAuditPaginationAndRedaction(t *testing.T) {
	store := NewMemoryStore()
	connector := createReconciliationTestConnector(t, store, "bcon_reconciliation_audit")
	app := New(store).Handler()
	sensitiveSource := "sensitive-billing-account"
	sensitiveTarget := "sensitive-tokenhub-resource"
	created := doJSON(t, app, http.MethodPost, "/api/admin/billing/reconciliation-rules", map[string]any{
		"name": "Audited reconciliation", "connector_id": connector.ID, "granularity": ReconciliationGranularityDay,
		"match_dimensions": []string{"resource_account", "currency"}, "timezone": "UTC",
		"dimension_mappings": map[string]map[string]string{"resource_account": {sensitiveSource: sensitiveTarget}},
	}, "")
	if created.Code != http.StatusCreated {
		t.Fatalf("create audited reconciliation rule: %d %s", created.Code, created.Body)
	}
	var rule ReconciliationRule
	if err := json.Unmarshal([]byte(created.Body), &rule); err != nil {
		t.Fatal(err)
	}
	failed := doJSON(t, app, http.MethodPost, "/api/admin/billing/reconciliation-rules/"+rule.ID+"/run", map[string]any{
		"period_start": "2026-07-02T00:00:00Z", "period_end": "2026-07-01T00:00:00Z",
	}, "")
	if failed.Code != http.StatusBadRequest {
		t.Fatalf("invalid manual run was accepted: %d %s", failed.Code, failed.Body)
	}
	assertReconciliationAuditEvent(t, store, "reconcile", "failed", "invalid_reconciliation_period")
	for _, event := range store.ListAuditEvents() {
		if strings.Contains(event.BeforeSnapshot+event.AfterSnapshot, sensitiveSource) || strings.Contains(event.BeforeSnapshot+event.AfterSnapshot, sensitiveTarget) {
			t.Fatalf("resource-account mapping leaked into audit event: %#v", event)
		}
	}
	if !canAdmin("security_admin", "admin_audit", http.MethodGet) || canAdmin("security_admin", "billing", http.MethodGet) {
		t.Fatal("security_admin reconciliation audit RBAC boundary changed")
	}

	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	run := runReconciliationTestRule(t, app, rule.ID, periodStart, periodStart.Add(24*time.Hour))
	items := make([]ReconciliationItem, 5)
	for index := range items {
		items[index] = ReconciliationItem{
			ID: fmt.Sprintf("recitem_page_%02d", index), RunID: run.ID, MatchKey: fmt.Sprintf("page-%02d", index), Status: ReconciliationProviderOnly,
			BucketStart: periodStart.Add(time.Duration(index) * time.Minute), BucketEnd: periodStart.Add(time.Duration(index) * time.Minute), Currency: "USD",
			ProviderAmount: "1", DifferenceAmount: "1", DifferenceRatio: "1", CreatedAt: time.Now().UTC(),
		}
	}
	if err := store.db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	pageResponse := doJSON(t, app, http.MethodGet, "/api/admin/billing/reconciliations/"+run.ID+"?limit=2&offset=1", nil, "")
	if pageResponse.Code != http.StatusOK {
		t.Fatalf("list reconciliation page: %d %s", pageResponse.Code, pageResponse.Body)
	}
	var page ReconciliationDetail
	if err := json.Unmarshal([]byte(pageResponse.Body), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 5 || page.Limit != 2 || page.Offset != 1 || len(page.Items) != 2 {
		t.Fatalf("reconciliation detail was not paginated: %#v", page)
	}
}

func TestNonUSDProviderOnlyUsesMissingUsageReason(t *testing.T) {
	run := ReconciliationRun{ID: "recon_reason", Currency: "CNY"}
	bucket := reconciliationBucket{
		key: "provider-only", dimensions: map[string]string{"currency": "CNY"}, providerAmount: 1 * reconciliationMoney(reconciliationScale),
		providerRecordIDs: []string{"bill-cny"},
	}
	item := reconciliationItemFromBucket(run, bucket, 0, 0, "")
	if item.Status != ReconciliationProviderOnly || item.PossibleReason != "missing_tokenhub_usage_or_late_data" {
		t.Fatalf("non-USD provider-only result was misclassified: %#v", item)
	}
}

func createReconciliationReviewRule(t *testing.T, app http.Handler, connectorID string, granularity string, dimensions []string) ReconciliationRule {
	t.Helper()
	response := doJSON(t, app, http.MethodPost, "/api/admin/billing/reconciliation-rules", map[string]any{
		"name": "Review regression", "connector_id": connectorID, "granularity": granularity,
		"match_dimensions": dimensions, "timezone": "UTC", "amount_tolerance": "0", "ratio_tolerance": "0",
	}, "")
	if response.Code != http.StatusCreated {
		t.Fatalf("create reconciliation rule: %d %s", response.Code, response.Body)
	}
	var rule ReconciliationRule
	if err := json.Unmarshal([]byte(response.Body), &rule); err != nil {
		t.Fatal(err)
	}
	return rule
}

func assertReconciliationAuditEvent(t *testing.T, store *GormStore, action string, status string, message string) {
	t.Helper()
	for _, event := range store.ListAuditEvents() {
		if event.Action == action && event.ResourceType == "billing_reconciliation" && event.Status == status && event.Message == message {
			return
		}
	}
	t.Fatalf("missing reconciliation audit event action=%s status=%s message=%s: %#v", action, status, message, store.ListAuditEvents())
}
