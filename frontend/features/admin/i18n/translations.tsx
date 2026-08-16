import { enTranslations } from "./en";
import { jaTranslations } from "./ja";
import { modelGovernanceTranslations } from "./model-governance";
import { playgroundTranslations } from "./playground";
import { providerConnectionTranslations } from "./provider-connection";
import { providerMonitoringTranslations } from "./provider-monitoring";
import { routingTranslations } from "./routing";
import { codexImageTranslations } from "./codex-image";
import { scopedRoutingPolicyTranslations } from "./scoped-routing-policy";
import { securityTranslations } from "./security";
import { usageTranslations } from "./usage";
import syntheticDNSTranslations from "./synthetic-dns";

export const translations: Record<"en" | "ja", Record<string, string>> = {
  en: { ...enTranslations, ...routingTranslations.en, ...codexImageTranslations.en, ...scopedRoutingPolicyTranslations.en, ...modelGovernanceTranslations.en, ...providerConnectionTranslations.en, ...providerMonitoringTranslations.en, ...usageTranslations.en, ...playgroundTranslations.en, ...securityTranslations.en, ...syntheticDNSTranslations.en },
  ja: { ...jaTranslations, ...routingTranslations.ja, ...codexImageTranslations.ja, ...scopedRoutingPolicyTranslations.ja, ...modelGovernanceTranslations.ja, ...providerConnectionTranslations.ja, ...providerMonitoringTranslations.ja, ...usageTranslations.ja, ...playgroundTranslations.ja, ...securityTranslations.ja, ...syntheticDNSTranslations.ja },
};
