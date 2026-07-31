import { enTranslations } from "./en";
import { jaTranslations } from "./ja";
import { modelGovernanceTranslations } from "./model-governance";
import { routingTranslations } from "./routing";

export const translations: Record<"en" | "ja", Record<string, string>> = {
  en: { ...enTranslations, ...routingTranslations.en, ...modelGovernanceTranslations.en },
  ja: { ...jaTranslations, ...routingTranslations.ja, ...modelGovernanceTranslations.ja },
};
