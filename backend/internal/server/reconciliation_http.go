package server

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) registerReconciliationRoutes() {
	s.mux.HandleFunc("/api/admin/billing/reconciliation-rules", s.handleAdminReconciliationRules)
	s.mux.HandleFunc("/api/admin/billing/reconciliation-rules/", s.handleAdminReconciliationRuleItem)
	s.mux.HandleFunc("/api/admin/billing/reconciliations", s.handleAdminReconciliations)
	s.mux.HandleFunc("/api/admin/billing/reconciliations/", s.handleAdminReconciliationItem)
}

func (s *Server) handleAdminReconciliationRules(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdmin(w, r, "billing", r.Method)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"data": s.store.ListReconciliationRules()})
	case http.MethodPost:
		var request ReconciliationRuleRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, r, NewHTTPError(http.StatusBadRequest, "invalid_reconciliation_rule", "Invalid reconciliation rule payload"))
			return
		}
		rule, err := s.reconciliation.CreateRule(request, user.ID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		s.recordAdminAudit(r, user, "create", "reconciliation_rule", rule.ID, nil, rule)
		writeJSON(w, http.StatusCreated, rule)
	default:
		writeError(w, r, NewHTTPError(http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed"))
	}
}

func (s *Server) handleAdminReconciliationRuleItem(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdmin(w, r, "billing", r.Method)
	if !ok {
		return
	}
	parts := pathPartsAfter(r.URL.Path, "/api/admin/billing/reconciliation-rules/")
	if len(parts) == 0 || len(parts) > 2 {
		writeError(w, r, NewHTTPError(http.StatusNotFound, "reconciliation_rule_not_found", "Reconciliation rule not found"))
		return
	}
	if len(parts) == 2 {
		if parts[1] != "run" || r.Method != http.MethodPost {
			writeError(w, r, NewHTTPError(http.StatusNotFound, "reconciliation_action_not_found", "Reconciliation action not found"))
			return
		}
		var request ReconciliationRunRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, r, NewHTTPError(http.StatusBadRequest, "invalid_reconciliation_run", "Invalid reconciliation run payload"))
			return
		}
		run, err := s.reconciliation.Run(r.Context(), parts[0], request, "manual", user.ID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		s.recordAdminAudit(r, user, "reconcile", "billing_reconciliation", run.ID, nil, reconciliationAuditSnapshot(run))
		writeJSON(w, http.StatusCreated, run)
		return
	}
	switch r.Method {
	case http.MethodGet:
		rule, err := s.store.GetReconciliationRule(parts[0])
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, rule)
	case http.MethodPatch:
		var request ReconciliationRulePatchRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, r, NewHTTPError(http.StatusBadRequest, "invalid_reconciliation_rule", "Invalid reconciliation rule payload"))
			return
		}
		before, updated, err := s.reconciliation.UpdateRule(parts[0], request, user.ID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		s.recordAdminAudit(r, user, "update", "reconciliation_rule", updated.ID, before, updated)
		writeJSON(w, http.StatusOK, updated)
	default:
		writeError(w, r, NewHTTPError(http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed"))
	}
}

func (s *Server) handleAdminReconciliations(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r, "billing", r.Method); !ok {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, r, NewHTTPError(http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": s.store.ListReconciliationRuns(r.URL.Query().Get("rule_id"), reconciliationListLimit(r, 100, 500))})
}

func (s *Server) handleAdminReconciliationItem(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdmin(w, r, "billing", r.Method)
	if !ok {
		return
	}
	parts := pathPartsAfter(r.URL.Path, "/api/admin/billing/reconciliations/")
	if len(parts) == 0 || len(parts) > 2 {
		writeError(w, r, NewHTTPError(http.StatusNotFound, "reconciliation_run_not_found", "Reconciliation run not found"))
		return
	}
	runID := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			writeError(w, r, NewHTTPError(http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed"))
			return
		}
		run, err := s.store.GetReconciliationRun(runID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		limit := -1
		if r.URL.Query().Has("limit") {
			limit = reconciliationListLimit(r, 1000, 5000)
		}
		items := s.store.ListReconciliationItems(runID, r.URL.Query().Get("status"), limit)
		writeJSON(w, http.StatusOK, ReconciliationDetail{Run: run, Items: items})
		return
	}
	switch parts[1] {
	case "lock":
		if r.Method != http.MethodPost {
			writeError(w, r, NewHTTPError(http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed"))
			return
		}
		before, err := s.store.GetReconciliationRun(runID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		run, err := s.store.LockReconciliationRun(runID, user.ID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		s.recordAdminAudit(r, user, "lock", "billing_reconciliation", run.ID, reconciliationAuditSnapshot(before), reconciliationAuditSnapshot(run))
		writeJSON(w, http.StatusOK, run)
	case "recalculate":
		if r.Method != http.MethodPost {
			writeError(w, r, NewHTTPError(http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed"))
			return
		}
		before, err := s.store.GetReconciliationRun(runID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		run, err := s.reconciliation.Recalculate(r.Context(), runID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		s.recordAdminAudit(r, user, "recalculate", "billing_reconciliation", run.ID, reconciliationAuditSnapshot(before), reconciliationAuditSnapshot(run))
		writeJSON(w, http.StatusOK, run)
	case "export":
		if r.Method != http.MethodGet {
			writeError(w, r, NewHTTPError(http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed"))
			return
		}
		run, err := s.store.GetReconciliationRun(runID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		status := strings.TrimSpace(r.URL.Query().Get("status"))
		items := s.store.ListReconciliationItems(runID, status, -1)
		if status == "" {
			differences := items[:0]
			for _, item := range items {
				if item.Status != ReconciliationMatched {
					differences = append(differences, item)
				}
			}
			items = differences
		}
		s.recordAdminAudit(r, user, "export", "billing_reconciliation", run.ID, nil, reconciliationAuditSnapshot(run))
		writeReconciliationCSV(w, run, items)
	default:
		writeError(w, r, NewHTTPError(http.StatusNotFound, "reconciliation_action_not_found", "Reconciliation action not found"))
	}
}

func pathPartsAfter(path string, prefix string) []string {
	trimmed := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func reconciliationListLimit(r *http.Request, fallback int, maximum int) int {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > maximum {
		return fallback
	}
	return limit
}

func reconciliationAuditSnapshot(run ReconciliationRun) map[string]any {
	return map[string]any{
		"id": run.ID, "rule_id": run.RuleID, "connector_id": run.ConnectorID,
		"status": run.Status, "rule_version": run.RuleVersion, "rule_hash": run.RuleHash,
		"input_hash": run.InputHash, "period_start": run.PeriodStart, "period_end": run.PeriodEnd,
		"matched_count": run.MatchedCount, "provider_only_count": run.ProviderOnlyCount,
		"tokenhub_only_count": run.TokenHubOnlyCount, "amount_mismatch_count": run.AmountMismatchCount,
		"locked_at": run.LockedAt,
	}
}

func writeReconciliationCSV(w http.ResponseWriter, run ReconciliationRun, items []ReconciliationItem) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="reconciliation-%s.csv"`, run.ID))
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{
		"status", "bucket_start", "bucket_end", "request_id", "provider", "resource_account", "model", "project", "currency",
		"provider_amount", "tokenhub_amount", "difference_amount", "difference_ratio", "possible_reason", "provider_record_ids", "tokenhub_record_ids",
	})
	for _, item := range items {
		_ = writer.Write([]string{
			safeReconciliationCSVCell(item.Status), item.BucketStart.Format("2006-01-02T15:04:05Z07:00"), item.BucketEnd.Format("2006-01-02T15:04:05Z07:00"),
			safeReconciliationCSVCell(item.RequestID), safeReconciliationCSVCell(item.Provider), safeReconciliationCSVCell(item.ResourceAccountMasked),
			safeReconciliationCSVCell(item.Model), safeReconciliationCSVCell(item.Project), safeReconciliationCSVCell(item.Currency),
			item.ProviderAmount, item.TokenHubAmount, item.DifferenceAmount, item.DifferenceRatio, safeReconciliationCSVCell(item.PossibleReason),
			safeReconciliationCSVCell(strings.Join(item.ProviderRecordIDs, "|")), safeReconciliationCSVCell(strings.Join(item.TokenHubRecordIDs, "|")),
		})
	}
	writer.Flush()
}

func safeReconciliationCSVCell(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}
