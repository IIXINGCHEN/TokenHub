import { enTranslations } from "./en";
import { jaTranslations } from "./ja";
import { modelGovernanceTranslations } from "./model-governance";
import { providerConnectionTranslations } from "./provider-connection";
import { routingTranslations } from "./routing";

export const translations: Record<"en" | "ja", Record<string, string>> = {
  en: { ...enTranslations, ...routingTranslations.en, ...modelGovernanceTranslations.en, ...providerConnectionTranslations.en },
  ja: { ...jaTranslations, ...routingTranslations.ja, ...modelGovernanceTranslations.ja, ...providerConnectionTranslations.ja },
};
