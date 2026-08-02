package server

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

func (s *GormStore) CreateReconciliationRule(rule ReconciliationRule) (ReconciliationRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if rule.ID == "" {
		rule.ID = NewID("recrule")
	}
	if rule.Version <= 0 {
		rule.Version = 1
	}
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = now
	}
	rule.UpdatedAt = now
	rule.NextRunAt = nextReconciliationRunAt(rule, now)
	if err := s.db.Create(&rule).Error; err != nil {
		return ReconciliationRule{}, writeConflict(err, "reconciliation_rule_conflict", "Reconciliation rule already exists")
	}
	return rule, nil
}

func (s *GormStore) ListReconciliationRules() []ReconciliationRule {
	var rules []ReconciliationRule
	_ = s.db.Order("created_at asc").Find(&rules).Error
	return rules
}

func (s *GormStore) GetReconciliationRule(id string) (ReconciliationRule, error) {
	var rule ReconciliationRule
	if err := s.db.First(&rule, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		return ReconciliationRule{}, notFound(err, "reconciliation_rule_not_found", "Reconciliation rule not found")
	}
	return rule, nil
}

func (s *GormStore) UpdateReconciliationRule(rule ReconciliationRule) (ReconciliationRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var existing ReconciliationRule
	if err := s.db.First(&existing, "id = ?", strings.TrimSpace(rule.ID)).Error; err != nil {
		return ReconciliationRule{}, notFound(err, "reconciliation_rule_not_found", "Reconciliation rule not found")
	}
	rule.CreatedAt = existing.CreatedAt
	rule.LastRunAt = existing.LastRunAt
	rule.UpdatedAt = time.Now().UTC()
	if rule.ScheduleIntervalMinutes <= 0 || rule.Status != StatusActive {
		rule.NextRunAt = nil
	} else if existing.ScheduleIntervalMinutes != rule.ScheduleIntervalMinutes || existing.Status != rule.Status || existing.NextRunAt == nil {
		rule.NextRunAt = nextReconciliationRunAt(rule, rule.UpdatedAt)
	} else {
		rule.NextRunAt = existing.NextRunAt
	}
	if err := s.db.Save(&rule).Error; err != nil {
		return ReconciliationRule{}, err
	}
	return rule, nil
}

func (s *GormStore) ListDueReconciliationRules(now time.Time, limit int) []ReconciliationRule {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	var rules []ReconciliationRule
	_ = s.db.Where("status = ? AND schedule_interval_minutes > 0 AND next_run_at IS NOT NULL AND next_run_at <= ?", StatusActive, now.UTC()).
		Order("next_run_at asc").Limit(limit).Find(&rules).Error
	return rules
}

func (s *GormStore) LoadReconciliationInputs(connectorID string, from time.Time, to time.Time, window time.Duration) ([]BillingRecord, []UsageRecord, error) {
	connectorID = strings.TrimSpace(connectorID)
	var records []BillingRecord
	if err := s.db.Where("connector_id = ? AND usage_start_at >= ? AND usage_start_at < ?", connectorID, from.UTC(), to.UTC()).
		Order("usage_start_at asc, id asc").Find(&records).Error; err != nil {
		return nil, nil, err
	}
	usageFrom := from.UTC()
	usageTo := to.UTC()
	if window > 0 {
		usageFrom = usageFrom.Add(-window)
		usageTo = usageTo.Add(window)
	}
	var usages []UsageRecord
	if err := s.db.Where("created_at >= ? AND created_at < ?", usageFrom, usageTo).
		Order("created_at asc, id asc").Find(&usages).Error; err != nil {
		return nil, nil, err
	}
	return records, usages, nil
}

func (s *GormStore) SaveReconciliationRun(run ReconciliationRun, items []ReconciliationItem) (ReconciliationRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&run).Error; err != nil {
			return err
		}
		if len(items) > 0 {
			if err := tx.Create(&items).Error; err != nil {
				return err
			}
		}
		return updateReconciliationRuleAfterRun(tx, run)
	})
	return run, err
}

func (s *GormStore) ReplaceReconciliationRun(run ReconciliationRun, items []ReconciliationItem) (ReconciliationRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var existing ReconciliationRun
		if err := tx.First(&existing, "id = ?", run.ID).Error; err != nil {
			return notFound(err, "reconciliation_run_not_found", "Reconciliation run not found")
		}
		if existing.LockedAt != nil {
			return NewHTTPError(409, "reconciliation_run_locked", "Locked reconciliation runs cannot be recalculated")
		}
		run.LockedAt = nil
		run.LockedBy = ""
		if err := tx.Save(&run).Error; err != nil {
			return err
		}
		if err := tx.Where("run_id = ?", run.ID).Delete(&ReconciliationItem{}).Error; err != nil {
			return err
		}
		if len(items) > 0 {
			return tx.Create(&items).Error
		}
		return nil
	})
	return run, err
}

func (s *GormStore) ListReconciliationRuns(ruleID string, limit int) []ReconciliationRun {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := s.db.Order("started_at desc").Limit(limit)
	if ruleID = strings.TrimSpace(ruleID); ruleID != "" {
		query = query.Where("rule_id = ?", ruleID)
	}
	var runs []ReconciliationRun
	_ = query.Find(&runs).Error
	return runs
}

func (s *GormStore) GetReconciliationRun(id string) (ReconciliationRun, error) {
	var run ReconciliationRun
	if err := s.db.First(&run, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		return ReconciliationRun{}, notFound(err, "reconciliation_run_not_found", "Reconciliation run not found")
	}
	return run, nil
}

func (s *GormStore) ListReconciliationItems(runID string, status string, limit int) []ReconciliationItem {
	if limit == 0 || limit > 5000 {
		limit = 1000
	}
	query := s.db.Where("run_id = ?", strings.TrimSpace(runID)).Order("status asc, bucket_start asc, id asc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if status = strings.TrimSpace(status); status != "" {
		query = query.Where("status = ?", status)
	}
	var items []ReconciliationItem
	_ = query.Find(&items).Error
	for index := range items {
		items[index].ResourceAccountMasked = maskReconciliationIdentifier(items[index].ResourceAccount)
	}
	return items
}

func (s *GormStore) LockReconciliationRun(id string, actor string) (ReconciliationRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var run ReconciliationRun
	if err := s.db.First(&run, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		return ReconciliationRun{}, notFound(err, "reconciliation_run_not_found", "Reconciliation run not found")
	}
	if run.Status != ReconciliationRunSucceeded {
		return ReconciliationRun{}, NewHTTPError(409, "reconciliation_run_not_complete", "Only successful reconciliation runs can be locked")
	}
	if run.LockedAt != nil {
		return run, nil
	}
	now := time.Now().UTC()
	run.LockedAt = &now
	run.LockedBy = strings.TrimSpace(actor)
	return run, s.db.Save(&run).Error
}

func (s *GormStore) RecordScheduledReconciliationAudit(run ReconciliationRun) {
	status := "success"
	if run.Status != ReconciliationRunSucceeded {
		status = "failed"
	}
	s.RecordAuditEvent(AuditEvent{
		ActorUserID:   "system",
		ActorName:     "TokenHub Scheduler",
		ActorRole:     "system",
		Action:        "reconcile",
		ResourceType:  "billing_reconciliation",
		ResourceID:    run.ID,
		Status:        status,
		Message:       run.ErrorCode,
		AfterSnapshot: snapshotJSON(run),
	})
}

func nextReconciliationRunAt(rule ReconciliationRule, now time.Time) *time.Time {
	if rule.Status != StatusActive || rule.ScheduleIntervalMinutes <= 0 {
		return nil
	}
	next := now.UTC().Add(time.Duration(rule.ScheduleIntervalMinutes) * time.Minute)
	return &next
}

func updateReconciliationRuleAfterRun(tx *gorm.DB, run ReconciliationRun) error {
	var rule ReconciliationRule
	if err := tx.First(&rule, "id = ?", run.RuleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	finishedAt := run.StartedAt
	if run.FinishedAt != nil {
		finishedAt = *run.FinishedAt
	}
	rule.LastRunAt = &finishedAt
	if run.Trigger == "scheduled" {
		rule.NextRunAt = nextReconciliationRunAt(rule, finishedAt)
	}
	return tx.Model(&ReconciliationRule{}).Where("id = ?", rule.ID).Updates(map[string]any{
		"last_run_at": rule.LastRunAt,
		"next_run_at": rule.NextRunAt,
	}).Error
}
