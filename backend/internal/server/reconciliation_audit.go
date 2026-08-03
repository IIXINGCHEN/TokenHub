package server

func reconciliationRuleAuditSnapshot(rule ReconciliationRule) map[string]any {
	mappings := map[string]map[string]string{}
	resourceMappingCount := 0
	for dimension, entries := range rule.DimensionMappings {
		if dimension == "resource_account" {
			resourceMappingCount = len(entries)
			continue
		}
		copied := make(map[string]string, len(entries))
		for source, canonical := range entries {
			copied[source] = canonical
		}
		mappings[dimension] = copied
	}
	return map[string]any{
		"id": rule.ID, "name": rule.Name, "connector_id": rule.ConnectorID,
		"status": rule.Status, "granularity": rule.Granularity, "match_dimensions": rule.MatchDimensions,
		"dimension_mappings": mappings, "resource_account_mapping_count": resourceMappingCount,
		"amount_tolerance": rule.AmountTolerance, "ratio_tolerance": rule.RatioTolerance,
		"usd_exchange_rate": rule.USDExchangeRate, "time_window_minutes": rule.TimeWindowMinutes,
		"billing_delay_minutes": rule.BillingDelayMinutes, "schedule_interval_minutes": rule.ScheduleIntervalMinutes,
		"timezone": rule.Timezone, "currency": rule.Currency, "version": rule.Version, "rule_hash": rule.RuleHash,
		"created_by": rule.CreatedBy, "updated_by": rule.UpdatedBy,
	}
}

func reconciliationAuditSnapshot(run ReconciliationRun) map[string]any {
	return map[string]any{
		"id": run.ID, "rule_id": run.RuleID, "connector_id": run.ConnectorID,
		"status": run.Status, "trigger": run.Trigger, "rule_version": run.RuleVersion, "rule_hash": run.RuleHash,
		"input_hash": run.InputHash, "period_start": run.PeriodStart, "period_end": run.PeriodEnd,
		"matched_count": run.MatchedCount, "provider_only_count": run.ProviderOnlyCount,
		"tokenhub_only_count": run.TokenHubOnlyCount, "amount_mismatch_count": run.AmountMismatchCount,
		"error_code": run.ErrorCode, "started_at": run.StartedAt, "finished_at": run.FinishedAt,
		"locked_at": run.LockedAt,
	}
}
