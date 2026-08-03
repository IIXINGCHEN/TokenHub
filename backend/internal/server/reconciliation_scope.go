package server

import (
	"strings"
)

func scopeReconciliationUsages(run ReconciliationRun, connector BillingConnector, bills []BillingRecord, usages []UsageRecord) []UsageRecord {
	providerIDs := map[string]bool{}
	resourceIDs := map[string]bool{}
	addReconciliationScopeValues(providerIDs, connector.Config["provider_id"])
	addReconciliationScopeValues(resourceIDs, connector.Config["provider_resource_id"])
	if len(providerIDs) == 0 && len(resourceIDs) == 0 {
		for _, record := range bills {
			if record.UsageStartAt.Before(run.PeriodStart) || !record.UsageStartAt.Before(run.PeriodEnd) ||
				(run.Currency != "" && !strings.EqualFold(record.Currency, run.Currency)) {
				continue
			}
			dimensions := providerReconciliationDimensions(run, record)
			if dimensions["provider"] != "" {
				providerIDs[dimensions["provider"]] = true
			}
			if dimensions["resource_account"] != "" {
				resourceIDs[dimensions["resource_account"]] = true
			}
		}
	}

	result := make([]UsageRecord, 0, len(usages))
	for _, usage := range usages {
		if len(resourceIDs) > 0 {
			if resourceIDs[strings.TrimSpace(usage.ProviderResourceID)] {
				result = append(result, usage)
			}
			continue
		}
		if providerIDs[strings.TrimSpace(usage.ProviderID)] {
			result = append(result, usage)
		}
	}
	return result
}

func addReconciliationScopeValues(target map[string]bool, raw string) {
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			target[value] = true
		}
	}
}
