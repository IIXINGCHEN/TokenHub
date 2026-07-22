import { AlertCircle, Boxes, Check, Copy, KeyRound, Plus, Search, Send, Server, Settings, UserRoundCheck } from "lucide-react";
import { type FormEvent, useEffect, useMemo, useRef, useState } from "react";
import { clearPendingProviderAccountOAuthSession, consumePendingProviderAccountOAuthResult, hasPendingProviderAccountOAuthResult, parseProviderAccountOAuthResult, providerAccountOAuthCallbackURL, type ProviderAccountOAuthGenerateResponse, type ProviderAccountOAuthResult, readPendingProviderAccountOAuthSession, savePendingProviderAccountOAuthSession } from "../core/session";
import { type ApiContext, type FieldConfig, type Model, type ModelRoute, type Provider, type ProviderCatalogEntry, type ProviderCredentialMode, type ProviderResource } from "../core/types";
import { buildCustomProviderCatalogEntry, canonicalModelNameForUI, catalogModelCategoryOptions, modelCategory, modelCategoryForCatalog, modelCategoryLabel, providerEntryCategoryCount, providerEntrySupportsCategory } from "../domain/catalog";
import { compactNumber, formatModelPrice, modelCapabilities } from "../domain/formatting";
import { enumOptionLabel, providerTypeLabel } from "../domain/labels";
import { clearCustomValidity, countWithLabel, countWithUnit, handleRequiredFieldInvalid, tx } from "../i18n/runtime";
import { adminFetch, isAuthExpiredError, providerPayload, providerResourcePayload, providerUpdatePayload, readAdminError } from "../resources/payloads";
import { assertProviderAccountResourceReady, defaultProviderResourceName, providerAccountTokenSummary, providerCreateAccountManualTokenFields, providerCreateAccountRuntimeFields, providerResourceDraftDefaults } from "../resources/provider-model-config";
import { ReviewItem } from "../shared/modals";
import { providerTypeOptions } from "../shared/ui";

const openAIAccountOAuthRedirectURI = "http://localhost:1455/auth/callback";

type OpenAIQuotaWindow = {
  used_percent: number;
  limit_window_seconds: number;
  reset_after_seconds: number;
  reset_at: number;
};

type OpenAIAccountQuota = {
  account_id?: string;
  email?: string;
  plan_type?: string;
  rate_limit?: {
    allowed: boolean;
    limit_reached: boolean;
    primary_window?: OpenAIQuotaWindow;
    secondary_window?: OpenAIQuotaWindow;
  };
  additional_rate_limits?: Array<{
    limit_name: string;
    metered_feature: string;
    rate_limit?: {
      allowed: boolean;
      limit_reached: boolean;
      primary_window?: OpenAIQuotaWindow;
      secondary_window?: OpenAIQuotaWindow;
    };
  }>;
  rate_limit_reset_credits?: { available_count: number };
  fetched_at: number;
};

type CodexSubscriptionTestResult = {
  resource_id: string;
  model: string;
  reasoning_effort: string;
  speed: "standard" | "fast";
  upstream_service_tier?: string;
  output_text: string;
  latency_ms: number;
  usage: {
    prompt_tokens: number;
    completion_tokens: number;
    total_tokens: number;
  };
};

const codexSubscriptionModels = [
  ["gpt-5.6-sol", "5.6 Sol"],
  ["gpt-5.6-terra", "5.6 Terra"],
  ["gpt-5.6-luna", "5.6 Luna"],
  ["gpt-5.5", "5.5"],
  ["gpt-5.4", "5.4"],
  ["gpt-5.4-mini", "5.4 Mini"],
  ["gpt-5.3-codex-spark", "5.3 Codex Spark"],
] as const;

const codexReasoningEfforts = ["none", "minimal", "low", "medium", "high", "xhigh", "max"] as const;

export function ProviderUpsertModal({
  mode,
  provider,
  api,
  catalog,
  standardModels,
  routes = [],
  resources = [],
  loading,
  onClose,
  onSaved,
  setLoading,
  setError,
  setNotice,
}: {
  mode: "create" | "edit";
  provider?: Provider;
  api: ApiContext;
  catalog: ProviderCatalogEntry[];
  standardModels: Model[];
  routes?: ModelRoute[];
  resources?: ProviderResource[];
  loading: boolean;
  onClose: () => void;
  onSaved: () => Promise<void>;
  setLoading: (value: boolean) => void;
  setError: (value: string) => void;
  setNotice: (value: string) => void;
}) {
  const availableCategories = useMemo(() => catalogModelCategoryOptions(catalog), [catalog]);
  const initialCategory = availableCategories.find((item) => item.key !== "all")?.key ?? "custom";
  const initialEntry = catalog.find((entry) => providerEntrySupportsCategory(entry, initialCategory)) ?? catalog.find((entry) => entry.id === "custom") ?? catalog[0];
  const [modelCategory, setModelCategory] = useState(initialCategory);
  const [catalogID, setCatalogID] = useState(initialEntry?.id ?? "custom");
  const [detail, setDetail] = useState<ProviderCatalogEntry | null>(null);
  const [catalogQuery, setCatalogQuery] = useState("");
  const [modelQuery, setModelQuery] = useState("");
  const [modelLoading, setModelLoading] = useState(false);
  const [modelError, setModelError] = useState("");
  const [catalogReloadKey, setCatalogReloadKey] = useState(0);
  const [selectedModels, setSelectedModels] = useState<Record<string, boolean>>({});
  const [values, setValues] = useState<Record<string, string>>(() => ({
    id: mode === "edit" ? provider?.id ?? "" : "",
    name: mode === "edit" ? provider?.name ?? "" : initialEntry?.display_name ?? "",
    type: mode === "edit" ? provider?.type ?? "openai_compatible" : initialEntry?.type ?? "openai_compatible",
    base_url: mode === "edit" ? provider?.base_url ?? "" : initialEntry?.base_url ?? "",
    api_key: "",
    priority: String(provider?.priority ?? 10),
    status: provider?.status ?? "active",
    healthy: String(provider?.healthy ?? true),
    create_routes: mode === "create" ? "true" : "false",
  }));
  const [credentialMode, setCredentialMode] = useState<ProviderCredentialMode>("provider_api_key");
  const [accountValues, setAccountValues] = useState<Record<string, string>>(() =>
    providerResourceDraftDefaults({
      provider_id: "",
      name: mode === "edit" ? provider?.name ?? "" : initialEntry?.display_name || initialEntry?.name || "",
      base_url: mode === "edit" ? provider?.base_url ?? "" : initialEntry?.base_url ?? "",
    }),
  );
  const [accountOAuthCallback, setAccountOAuthCallback] = useState("");
  const [accountOAuthStatus, setAccountOAuthStatus] = useState("");
  const [accountOAuthBusy, setAccountOAuthBusy] = useState(false);
  const [accountQuotaBusyID, setAccountQuotaBusyID] = useState("");
  const [accountQuotaError, setAccountQuotaError] = useState("");
  const [accountQuotas, setAccountQuotas] = useState<Record<string, OpenAIAccountQuota>>({});
  const [codexTestValues, setCodexTestValues] = useState({ model: "gpt-5.6-luna", reasoning_effort: "medium", speed: "standard", prompt: "" });
  const [codexTestBusyID, setCodexTestBusyID] = useState("");
  const [codexTestError, setCodexTestError] = useState("");
  const [codexTestResults, setCodexTestResults] = useState<Record<string, CodexSubscriptionTestResult>>({});
  const [createStep, setCreateStep] = useState(0);
  const createSteps = useMemo(() => providerCreateWizardSteps(), []);
  const lastCreateStep = createSteps.length - 1;
  const accountCallbackURL = useMemo(() => providerAccountOAuthCallbackURL(), []);
  const modalRef = useRef<HTMLFormElement | null>(null);
  const existingRouteModels = useMemo(
    () => new Set(routes.filter((route) => provider && route.provider_id === provider.id).map((route) => route.model_name)),
    [provider, routes],
  );
  const subscriptionResources = useMemo(
    () => resources.filter((resource) => resource.resource_type === "openai_subscription"),
    [resources],
  );

  const categoryCatalog = useMemo(
    () => catalog.filter((entry) => providerEntrySupportsCategory(entry, modelCategory)),
    [catalog, modelCategory],
  );
  const customCatalogEntry = useMemo(() => buildCustomProviderCatalogEntry(modelCategory, standardModels), [modelCategory, standardModels]);

  useEffect(() => {
    if (categoryCatalog.length === 0) return;
    if (!categoryCatalog.some((entry) => entry.id === catalogID)) {
      selectCatalog(categoryCatalog[0]);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [modelCategory, categoryCatalog.length]);

  useEffect(() => {
    const entry = catalogID === "custom" ? customCatalogEntry : catalog.find((item) => item.id === catalogID);
    setModelQuery("");
    setModelError("");
    if (entry && mode === "create") {
      setValues((current) => ({
        ...current,
        name: catalogID === "custom" ? (current.name === initialEntry?.display_name ? "" : current.name) : entry.display_name || entry.name || current.name,
        type: entry.type || current.type || "openai_compatible",
        base_url: catalogID === "custom" ? current.base_url : entry.base_url ?? "",
      }));
    }
    let cancelled = false;
    setDetail(null);
    setSelectedModels({});
    if (catalogID === "custom") {
      setDetail(customCatalogEntry);
      setModelLoading(false);
      return () => {
        cancelled = true;
      };
    }
    setModelLoading(true);
    adminFetch(api, `/api/admin/provider-catalog/${encodeURIComponent(catalogID)}`)
      .then(async (resp) => {
        if (!resp.ok) throw new Error(`provider catalog ${resp.status}`);
        return (await resp.json()) as { data: ProviderCatalogEntry };
      })
      .then((payload) => {
        if (cancelled) return;
        setDetail(payload.data);
        setModelError("");
        if (mode === "create") {
          setValues((current) => ({
            ...current,
            name: payload.data.display_name || payload.data.name || current.name,
            type: payload.data.type || current.type || "openai_compatible",
            base_url: payload.data.base_url ?? "",
          }));
        }
      })
      .catch((err) => {
        if (!cancelled) {
          if (isAuthExpiredError(err)) return;
          const message = err instanceof Error ? err.message : tx("Provider 模板加载失败");
          setModelError(message);
          setError(message);
        }
      })
      .finally(() => {
        if (!cancelled) setModelLoading(false);
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [catalogID, catalogReloadKey, customCatalogEntry, initialEntry?.display_name, mode]);

  useEffect(() => {
    if (mode === "create") {
      modalRef.current?.scrollTo({ top: 0 });
    }
  }, [createStep, mode]);

  const models = useMemo(
    () => (detail?.models ?? []).filter((model) => {
      if (modelCategory !== "all" && modelCategoryForCatalog(model) !== modelCategory) return false;
      const canonical = model.canonical_name || canonicalModelNameForUI(model.id, model.display_name);
      return standardModels.some((standard) => canonicalModelNameForUI(standard.name, standard.name) === canonicalModelNameForUI(canonical, canonical));
    }),
    [detail, modelCategory, standardModels],
  );
  useEffect(() => {
    if (mode !== "create" || !detail || values.create_routes !== "true") return;
    const nextSelected: Record<string, boolean> = {};
    for (const model of models) {
      nextSelected[model.id] = true;
    }
    setSelectedModels(nextSelected);
  }, [detail, mode, models, values.create_routes]);
  const filteredCatalog = useMemo(() => {
    const normalized = catalogQuery.trim().toLowerCase();
    const entries = categoryCatalog;
    if (!normalized) return entries;
    return entries.filter((entry) =>
      [
        entry.id,
        entry.name,
        entry.display_name,
      ].filter(Boolean).join(" ").toLowerCase().includes(normalized),
    );
  }, [categoryCatalog, catalogQuery]);
  const filteredModels = useMemo(() => {
    const normalized = modelQuery.trim().toLowerCase();
    if (!normalized) return models.slice(0, 80);
    return models
      .filter((model) => JSON.stringify(model).toLowerCase().includes(normalized))
      .slice(0, 80);
  }, [models, modelQuery]);
  const selectedModelIDs = Object.entries(selectedModels)
    .filter(([, selected]) => selected)
    .map(([id]) => id);
  const autoRouteEnabled = values.create_routes === "true";
  const selectedRouteCount = autoRouteEnabled ? selectedModelIDs.length : 0;
  const selectedEntry = detail ?? (catalogID === "custom" ? customCatalogEntry : catalog.find((entry) => entry.id === catalogID));
  const showProviderCatalog = mode === "edit" || (mode === "create" && createStep === 1 && credentialMode !== "account_integration");
  const providerBodyClassName = mode === "create" && !showProviderCatalog ? "provider-modal-body provider-wizard-single" : "provider-modal-body";
  const accountRuntimeFields = useMemo(() => providerCreateAccountRuntimeFields(), []);
  const accountManualTokenFields = useMemo(() => providerCreateAccountManualTokenFields(), []);
  const accountTokenSummary = useMemo(() => providerAccountTokenSummary(accountValues), [accountValues]);

  useEffect(() => {
    if (mode !== "create" || !hasPendingProviderAccountOAuthResult()) return;
    selectCredentialMode("account_integration");
    setCreateStep(2);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode]);

  useEffect(() => {
    if (mode !== "create" || credentialMode !== "account_integration") return;
    const pending = consumePendingProviderAccountOAuthResult();
    if (!pending) return;
    void applyProviderAccountOAuthResult(pending, tx("已从回调 URL 自动回填账号 Token。"));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode, credentialMode]);

  useEffect(() => {
    if (mode !== "create" || credentialMode !== "account_integration" || catalog.length === 0 || catalogID !== "custom") return;
    const recommended = recommendedAccountProviderEntry(catalog);
    if (!recommended) return;
    setModelCategory("openai");
    selectCatalog(recommended);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [catalog.length, credentialMode, mode, catalogID]);

  function update(key: string, value: string) {
    const previousProviderName = values.name;
    const previousBaseURL = values.base_url;
    setValues((current) => ({ ...current, [key]: value }));
    if (mode !== "create") return;
    if (key === "name") {
      setAccountValues((current) => {
        if (current.name && current.name !== defaultProviderResourceName(previousProviderName)) return current;
        return { ...current, name: defaultProviderResourceName(value) };
      });
    }
    if (key === "base_url") {
      setAccountValues((current) => {
        if (current.base_url && current.base_url !== previousBaseURL) return current;
        return { ...current, base_url: value || "https://api.openai.com/v1" };
      });
    }
  }

  function updateAccountValue(key: string, value: string) {
    setAccountValues((current) => ({ ...current, [key]: value }));
  }

  async function applyProviderAccountOAuthResult(result: ProviderAccountOAuthResult, message: string) {
    if (result.error) {
      const errorMessage = `${tx("账号授权失败")}：${result.error}`;
      setAccountOAuthStatus(errorMessage);
      setError(errorMessage);
      clearPendingProviderAccountOAuthSession();
      return;
    }
    if (result.authorization_code && !result.access_token && !result.refresh_token && !result.id_token) {
      await exchangeProviderAccountAuthorizationCode(result, message);
      return;
    }
    if (!result.access_token && !result.refresh_token && !result.id_token) {
      setAccountOAuthStatus(tx("未在回调结果中识别到 Token。"));
      setError(tx("未在回调结果中识别到 Token。"));
      return;
    }
    setAccountValues((current) => ({
      ...current,
      resource_type: "openai_subscription",
      auth_type: "oauth",
      access_token: result.access_token || current.access_token || "",
      refresh_token: result.refresh_token || current.refresh_token || "",
      id_token: result.id_token || current.id_token || "",
      account_email: result.account_email || current.account_email || "",
      account_id: result.account_id || current.account_id || "",
      organization_id: result.organization_id || current.organization_id || "",
      plan_type: result.plan_type || current.plan_type || "",
      token_type: result.token_type || current.token_type || "",
      expires_at: result.expires_at || current.expires_at || "",
      scopes: result.scopes || current.scopes || "",
    }));
    setAccountOAuthStatus(message);
    setError("");
  }

  function parseAccountOAuthCallback(raw: string) {
    setAccountOAuthCallback(raw);
    const result = parseProviderAccountOAuthResult(raw, true);
    if (!result) return;
    void applyProviderAccountOAuthResult(result, tx("已从粘贴的回调结果回填账号 Token。"));
  }

  function parseAccountOAuthCallbackNow() {
    const result = parseProviderAccountOAuthResult(accountOAuthCallback, true);
    if (!result) {
      setAccountOAuthStatus(tx("未在回调结果中识别到 Token。"));
      setError(tx("未在回调结果中识别到 Token。"));
      return;
    }
    void applyProviderAccountOAuthResult(result, tx("已从粘贴的回调结果回填账号 Token。"));
  }

  async function exchangeProviderAccountAuthorizationCode(result: ProviderAccountOAuthResult, message: string) {
    const pendingSession = readPendingProviderAccountOAuthSession();
    const sessionID = result.session_id || pendingSession?.session_id || "";
    const state = result.state || pendingSession?.state || "";
    if (!sessionID || !state || !result.authorization_code) {
      setAccountOAuthStatus(tx("授权回调缺少会话信息，请重新打开授权。"));
      setError(tx("授权回调缺少会话信息，请重新打开授权。"));
      return;
    }
    setAccountOAuthBusy(true);
    setAccountOAuthStatus(tx("正在换取账号 Token..."));
    try {
      const resp = await adminFetch(api, "/api/admin/provider-account-oauth/openai/exchange-code", {
        method: "POST",
        body: JSON.stringify({
          session_id: sessionID,
          state,
          code: result.authorization_code,
        }),
      });
      if (!resp.ok) throw new Error(await readAdminError(resp, tx("账号授权换取 Token")));
      const tokenInfo = (await resp.json()) as ProviderAccountOAuthResult;
      clearPendingProviderAccountOAuthSession();
      await applyProviderAccountOAuthResult(tokenInfo, message);
    } catch (err) {
      if (isAuthExpiredError(err)) return;
      const errorMessage = err instanceof Error ? err.message : tx("账号授权换取 Token 失败");
      setAccountOAuthStatus(errorMessage);
      setError(errorMessage);
    } finally {
      setAccountOAuthBusy(false);
    }
  }

  async function openProviderAccountAuthorization() {
    try {
      setAccountOAuthBusy(true);
      const resp = await adminFetch(api, "/api/admin/provider-account-oauth/openai/generate-auth-url", {
        method: "POST",
        body: JSON.stringify({ return_url: accountCallbackURL }),
      });
      if (!resp.ok) throw new Error(await readAdminError(resp, tx("生成账号授权地址")));
      const generated = (await resp.json()) as ProviderAccountOAuthGenerateResponse;
      savePendingProviderAccountOAuthSession({ session_id: generated.session_id, state: generated.state });
      window.open(generated.auth_url, "_blank", "noopener,noreferrer");
      setAccountOAuthStatus(tx("已打开 OpenAI/Codex 授权页。授权后请复制浏览器地址栏中的完整 localhost callback URL，并粘贴到回调结果。"));
      setError("");
    } catch (err) {
      if (isAuthExpiredError(err)) return;
      const errorMessage = err instanceof Error ? err.message : tx("生成账号授权地址失败");
      setAccountOAuthStatus(errorMessage);
      setError(errorMessage);
    } finally {
      setAccountOAuthBusy(false);
    }
  }

  async function copyProviderAccountCallbackURL() {
    try {
      await navigator.clipboard.writeText(openAIAccountOAuthRedirectURI);
      setAccountOAuthStatus(tx("已复制 OpenAI 固定回调地址。"));
    } catch {
      setAccountOAuthCallback(openAIAccountOAuthRedirectURI);
      setAccountOAuthStatus(openAIAccountOAuthRedirectURI);
    }
  }

  async function queryAccountQuota(resource: ProviderResource) {
    setAccountQuotaBusyID(resource.id);
    setAccountQuotaError("");
    try {
      const resp = await adminFetch(api, `/api/admin/provider-resources/${resource.id}/quota`);
      if (!resp.ok) throw new Error(await readAdminError(resp, tx("查询订阅额度")));
      const quota = (await resp.json()) as OpenAIAccountQuota;
      setAccountQuotas((current) => ({ ...current, [resource.id]: quota }));
    } catch (err) {
      if (isAuthExpiredError(err)) return;
      setAccountQuotaError(err instanceof Error ? err.message : tx("查询订阅额度失败"));
    } finally {
      setAccountQuotaBusyID("");
    }
  }

  async function testCodexSubscription(resource: ProviderResource) {
    setCodexTestBusyID(resource.id);
    setCodexTestError("");
    try {
      const resp = await adminFetch(api, `/api/admin/provider-resources/${resource.id}/test`, {
        method: "POST",
        body: JSON.stringify(codexTestValues),
      });
      if (!resp.ok) throw new Error(await readAdminError(resp, tx("测试 Codex Subscription")));
      const result = (await resp.json()) as CodexSubscriptionTestResult;
      setCodexTestResults((current) => ({ ...current, [resource.id]: result }));
    } catch (err) {
      if (isAuthExpiredError(err)) return;
      setCodexTestError(err instanceof Error ? err.message : tx("Codex Subscription 测试失败"));
    } finally {
      setCodexTestBusyID("");
    }
  }

  function selectCredentialMode(nextMode: ProviderCredentialMode) {
    setCredentialMode(nextMode);
    if (nextMode === "account_integration") {
      setAccountValues((current) => ({
        ...current,
        resource_type: "openai_subscription",
        auth_type: "oauth",
      }));
      const recommended = recommendedAccountProviderEntry(catalog);
      if (recommended) {
        setModelCategory("openai");
        setCatalogQuery("");
        setModelQuery("");
        setSelectedModels({});
        selectCatalog(recommended);
      }
    }
  }

  function syncAccountDefaults(providerName: string, baseURL?: string) {
    if (mode !== "create") return;
    setAccountValues((current) => ({
      ...current,
      name: defaultProviderResourceName(providerName),
      base_url: baseURL || "https://api.openai.com/v1",
    }));
  }

  function selectCategory(category: string) {
    setModelCategory(category);
    setCatalogQuery("");
    setModelQuery("");
    setSelectedModels({});
    const nextEntry = catalog.find((entry) => providerEntrySupportsCategory(entry, category));
    if (nextEntry) selectCatalog(nextEntry);
  }

  function selectCatalog(entry: ProviderCatalogEntry) {
    const nextName = entry.display_name || entry.name || values.name;
    setCatalogID(entry.id);
    setCatalogReloadKey((current) => current + 1);
    setDetail(null);
    setSelectedModels({});
    setModelQuery("");
    setModelError("");
    setValues((current) => ({
      ...current,
      name: mode === "create" ? entry.display_name || entry.name || current.name : current.name,
      type: entry.type || current.type || "openai_compatible",
      base_url: mode === "create" ? entry.base_url ?? "" : current.base_url,
    }));
    syncAccountDefaults(nextName, entry.base_url);
  }

  function selectCustomCatalog() {
    setCatalogID("custom");
    setCatalogReloadKey((current) => current + 1);
    setDetail(customCatalogEntry);
    setSelectedModels({});
    setModelQuery("");
    setModelError("");
    setValues((current) => ({
      ...current,
      id: mode === "create" ? "" : current.id,
      name: mode === "create" ? "" : current.name,
      type: current.type || "openai_compatible",
      base_url: mode === "create" ? "" : current.base_url,
    }));
    syncAccountDefaults(values.name || "Provider", "");
  }

  function canContinueCreateStep(targetStep = createStep) {
    if (mode !== "create") return true;
    if (targetStep === 0) return Boolean(credentialMode);
    if (targetStep === 1) {
      return Boolean(selectedEntry && values.name?.trim());
    }
    if (targetStep === 2 && credentialMode === "account_integration") return providerAccountResourceReady(accountValues);
    return true;
  }

  function validateCreateStep(targetStep = createStep) {
    if (mode !== "create") return true;
    if (targetStep === 0 && !credentialMode) {
      setError(tx("请先选择一种接入方式。"));
      return false;
    }
    if (targetStep === 1 && !selectedEntry) {
      setError(tx("请先选择一个渠道商。"));
      return false;
    }
    if (targetStep === 1 && !values.name?.trim()) {
      setError(tx(credentialMode === "account_integration" ? "请填写通道名称。" : "请填写渠道名称。"));
      return false;
    }
    if (targetStep === 2 && credentialMode === "account_integration") {
      try {
        assertProviderAccountResourceReady(accountValues);
      } catch (err) {
        setError(err instanceof Error ? err.message : tx("账号资源配置不完整"));
        return false;
      }
    }
    setError("");
    return true;
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (mode === "create" && createStep < lastCreateStep) {
      if (!validateCreateStep(createStep)) return;
      setCreateStep((current) => Math.min(current + 1, lastCreateStep));
      return;
    }
    setLoading(true);
    setError("");
    setNotice("");
    try {
      if (mode === "create" && credentialMode === "account_integration") {
        assertProviderAccountResourceReady(accountValues);
      }
      const payload = (mode === "edit" ? providerUpdatePayload : providerPayload)({
        ...values,
        api_key: mode === "create" && credentialMode !== "provider_api_key" ? "" : values.api_key,
        create_routes: autoRouteEnabled && selectedModelIDs.length > 0 ? "true" : "false",
        catalog_id: catalogID,
        model_category: modelCategory,
        selected_models: selectedModelIDs.length > 0 ? selectedModelIDs.join(",") : "",
      });
      const resp = await adminFetch(api, mode === "edit" && provider ? `/api/admin/providers/${provider.id}` : "/api/admin/providers", {
        method: mode === "edit" ? "PATCH" : "POST",
        body: JSON.stringify(payload),
      });
      if (!resp.ok) throw new Error(await readAdminError(resp, `${mode === "edit" ? tx("更新") : tx("创建")} ${tx("Provider 渠道")}`));
      const result = (await resp.json()) as { created_routes?: number; provider?: Provider };
      let accountResourceCreated = false;
      if (mode === "create" && credentialMode === "account_integration") {
        const payloadProviderID = typeof payload.id === "string" ? payload.id : "";
        const providerID = result.provider?.id || payloadProviderID || values.id;
        if (!providerID) throw new Error(tx("Provider 已创建，但无法确认账号资源所属 Provider。"));
        const resourceValues = {
          ...accountValues,
          provider_id: providerID,
          name: accountValues.name?.trim() || defaultProviderResourceName(result.provider?.name || values.name || providerID),
          base_url: accountValues.base_url?.trim() || values.base_url || "https://api.openai.com/v1",
        };
        const resourceResp = await adminFetch(api, "/api/admin/provider-resources", {
          method: "POST",
          body: JSON.stringify(providerResourcePayload(resourceValues)),
        });
        if (!resourceResp.ok) throw new Error(await readAdminError(resourceResp, tx("创建账号资源")));
        accountResourceCreated = true;
      }
      const routed = result.created_routes ?? 0;
      setNotice(`${tx("Provider 已")}${tx(mode === "edit" ? "更新" : "新增")}${accountResourceCreated ? `，${tx("已创建账号资源")}` : ""}${routed ? `，${tx("创建")} ${countWithUnit(routed, `条${modelCategoryLabel(modelCategory)}路由`, `${modelCategoryLabel(modelCategory)} route`, `${modelCategoryLabel(modelCategory)} ルート`)}` : ""}`);
      await onSaved();
    } catch (err) {
      if (isAuthExpiredError(err)) return;
      setError(err instanceof Error ? err.message : tx("保存失败"));
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="modal-backdrop" role="presentation">
      <form className={mode === "create" ? "modal provider-modal provider-wizard-modal" : "modal provider-modal"} ref={modalRef} onSubmit={submit}>
        <div className="modal-header">
          <div>
            <p className="eyebrow">{tx(mode === "edit" ? "编辑" : "新增")}</p>
            <h2>{tx("Provider 渠道")}</h2>
          </div>
          <button className="icon-button" onClick={onClose} type="button" title={tx("关闭")}>×</button>
        </div>
        {mode === "create" ? (
          <div className="wizard-stepper provider-wizard-stepper" aria-label={tx("创建 Provider 步骤")}>
            {createSteps.map((item, index) => {
              const Icon = item.icon;
              const title = providerCreateWizardStepTitle(item.title, credentialMode);
              return (
                <button
                  aria-current={createStep === index ? "step" : undefined}
                  className={createStep === index ? "wizard-step active" : index < createStep ? "wizard-step done" : "wizard-step"}
                  disabled={index > createStep || loading}
                  key={item.title}
                  onClick={() => setCreateStep(index)}
                  type="button"
                >
                  <span><Icon size={14} /></span>
                  <strong>{tx(title)}</strong>
                </button>
              );
            })}
          </div>
        ) : null}
        <div className={providerBodyClassName}>
          {showProviderCatalog ? (
            <section className="provider-catalog-pane">
            <div className="provider-catalog-head">
              <strong>{tx("模型类型")}</strong>
              <span>{countWithLabel(availableCategories.length, "类")}</span>
            </div>
            <div className="provider-category-list">
              {availableCategories.map((category) => (
                <button
                  className={category.key === modelCategory ? "provider-category-item active" : "provider-category-item"}
                  key={category.key}
                  onClick={() => selectCategory(category.key)}
                  type="button"
                >
                  <strong>{tx(category.label)}</strong>
                  <span>{countWithLabel(category.count, "个模型")}</span>
                </button>
              ))}
            </div>

            <div className="provider-catalog-head provider-catalog-subhead">
              <strong>{tx("渠道商")}</strong>
              <span>{filteredCatalog.length}/{categoryCatalog.length}</span>
            </div>
            <button
              className={catalogID === "custom" ? "custom-provider-button active" : "custom-provider-button"}
              onClick={selectCustomCatalog}
              type="button"
            >
              <Plus size={14} />
              <span>{tx("自定义渠道商")}</span>
              <em>{modelCategoryLabel(modelCategory)} · {countWithLabel(customCatalogEntry.models_count, "个标准模型")}</em>
            </button>
            <div className="provider-template-search">
              <Search size={14} />
              <input
                value={catalogQuery}
                onChange={(event) => setCatalogQuery(event.target.value)}
                placeholder={tx("搜索渠道商名称或 ID")}
              />
            </div>
            <div className="provider-catalog-list compact">
              {filteredCatalog.length === 0 ? (
                <div className="empty compact-empty">
                  <span>{tx("没有匹配的渠道商")}</span>
                  <button className="secondary-button" onClick={selectCustomCatalog} type="button">{tx("使用自定义渠道商")}</button>
                </div>
              ) : filteredCatalog.map((entry) => (
                <button
                  className={entry.id === catalogID ? "catalog-item active" : "catalog-item"}
                  key={entry.id}
                  onClick={() => selectCatalog(entry)}
                  type="button"
                >
                  <strong>{entry.display_name || entry.name}</strong>
                  <span>{providerTypeLabel(entry.type)} · {countWithLabel(providerEntryCategoryCount(entry, modelCategory), "个模型")}</span>
                </button>
              ))}
            </div>
          </section>
          ) : null}

          <section className="provider-config-pane">
            {mode === "edit" || createStep > 0 ? (
              <div className="provider-selected-summary">
                <strong>{modelCategoryLabel(modelCategory)}</strong>
                <span>{selectedEntry?.display_name || selectedEntry?.name || tx("请选择渠道商")}</span>
                <em>{providerTypeLabel(selectedEntry?.type || values.type || "openai_compatible")}</em>
              </div>
            ) : null}
            {mode === "create" && createStep === 0 ? (
              <section className="provider-wizard-panel provider-access-panel">
                <div className="wizard-panel-head">
                  <h3>{tx("选择接入方式")}</h3>
                  <p>{tx("先告诉 TokenHub 你手里有什么：上游 API Key、OpenAI 账号资源，或者只是先占位建路由。")}</p>
                </div>
                <div className="provider-access-options" role="radiogroup" aria-label={tx("选择接入方式")}>
                  {providerCredentialOptions().map((option) => {
                    const Icon = option.icon;
                    const active = credentialMode === option.key;
                    return (
                      <button
                        aria-checked={active}
                        className={active ? "provider-access-card active" : "provider-access-card"}
                        key={option.key}
                        onClick={() => selectCredentialMode(option.key)}
                        role="radio"
                        type="button"
                      >
                        <span><Icon size={18} /></span>
                        <strong>{tx(option.label)}</strong>
                        <em>{tx(option.description)}</em>
                        {option.key === "account_integration" ? <small>{tx("账号资源池会自动推荐 OpenAI 兼容通道，下一步只需确认 Base URL 和账号凭据。")}</small> : null}
                      </button>
                    );
                  })}
                </div>
              </section>
            ) : null}
            {mode === "create" && createStep === 1 ? (
              <section className="provider-wizard-panel">
                <div className="wizard-panel-head">
                  <h3>{tx(credentialMode === "account_integration" ? "确认账号通道和基础信息" : "选择渠道和基础信息")}</h3>
                  <p>{tx(credentialMode === "account_integration" ? "账号资源池已为你选好默认通道。这里通常只确认 Base URL；账号走企业代理时再修改。" : "选择上游渠道商模板，TokenHub 会带出类型、Base URL 和可映射模型。")}</p>
                </div>
                {credentialMode === "account_integration" ? (
                  <div className="provider-account-channel-note">
                    <strong>{tx("推荐通道")}</strong>
                    <span>{tx("默认通道只负责协议与 Base URL，真实账号 Token 会在下一步保存为账号资源。")}</span>
                  </div>
                ) : null}
                {!showProviderCatalog ? (
                  <div className="wizard-review-grid provider-create-review">
                    <ReviewItem label={credentialMode === "account_integration" ? "模型协议" : "模型类型"} value={modelCategoryLabel(modelCategory)} />
                    <ReviewItem label={credentialMode === "account_integration" ? "默认通道" : "渠道商"} value={selectedEntry?.display_name || selectedEntry?.name || "-"} />
                    <ReviewItem label={credentialMode === "account_integration" ? "兼容协议" : "渠道商类型"} value={providerTypeLabel(selectedEntry?.type || values.type || "openai_compatible")} />
                    <ReviewItem label="可映射模型" value={detail ? `${models.length}/${detail.models_count}` : tx("加载中")} />
                  </div>
                ) : null}
              </section>
            ) : null}
            {mode === "create" && createStep === 2 ? (
              <section className="provider-wizard-panel">
                <div className="wizard-panel-head">
                  <h3>{tx("配置账号与凭据")}</h3>
                  <p>{tx(credentialMode === "account_integration" ? "先完成账号授权回填；TokenHub 会把回填的 Token 保存为账号资源。" : "选择是直接保存 API Key、接入账号资源池，还是稍后补齐凭据。")}</p>
                </div>
              </section>
            ) : null}
            {mode === "create" && createStep === 3 ? (
              <section className="provider-wizard-panel">
                <div className="wizard-panel-head">
                  <h3>{tx("确认路由策略")}</h3>
                  <p>{tx("选择是否自动创建默认路由，并确认要映射到标准模型目录的上游模型。")}</p>
                </div>
                <div className="wizard-review-grid provider-create-review">
                  <ReviewItem label="渠道商" value={values.name || selectedEntry?.display_name || selectedEntry?.name || "-"} />
                  <ReviewItem label="凭据方式" value={providerCredentialModeLabel(credentialMode)} />
                  <ReviewItem label="自动路由" value={autoRouteEnabled ? tx("开启") : tx("关闭开关")} />
                  <ReviewItem label="已选模型" value={selectedRouteCount ? String(selectedRouteCount) : tx("无")} />
                </div>
              </section>
            ) : null}
            {mode === "create" && createStep === 2 ? (
              <section className="provider-credential-panel">
                <div className="provider-credential-head">
                  <div>
                    <strong>{tx("认证与账号来源")}</strong>
                    <span>{tx("选择 Provider 使用哪一种上游凭据。账号集成会把账号作为资源池管理，适合 OpenAI subscription 或多个账号轮询。")}</span>
                  </div>
                </div>
                <div className="provider-credential-options" role="radiogroup" aria-label={tx("认证与账号来源")}>
                  {providerCredentialOptions().map((option) => {
                    const Icon = option.icon;
                    const active = credentialMode === option.key;
                    return (
                      <button
                        aria-checked={active}
                        className={active ? "provider-credential-option active" : "provider-credential-option"}
                        key={option.key}
                        onClick={() => selectCredentialMode(option.key)}
                        role="radio"
                        type="button"
                      >
                        <Icon size={16} />
                        <span>
                          <strong>{tx(option.label)}</strong>
                          <em>{tx(option.description)}</em>
                        </span>
                      </button>
                    );
                  })}
                </div>
                {credentialMode === "provider_api_key" ? (
                  <div className="provider-direct-key-fields">
                    <label className="field">
                      <span>API Key</span>
                      <input value={values.api_key ?? ""} type="password" onChange={(event) => update("api_key", event.target.value)} />
                    </label>
                  </div>
                ) : credentialMode === "account_integration" ? (
                  <div className="provider-account-inline">
                    <div className="provider-account-inline-head">
                      <strong>{tx("账号授权")}</strong>
                      <span>{tx("使用 OpenAI/Codex OAuth 授权账号；TokenHub 会在后端换取并保存账号 Token。")}</span>
                    </div>
                    <div className="provider-account-auth-grid">
                      <label className="field">
                        <span>{tx("账号资源名称")}</span>
                        <input value={accountValues.name ?? ""} onChange={(event) => updateAccountValue("name", event.target.value)} required />
                      </label>
                      <label className="field">
                        <span>{tx("账号地址/邮箱")}</span>
                        <input value={accountValues.account_email ?? ""} onChange={(event) => updateAccountValue("account_email", event.target.value)} placeholder="name@example.com" />
                        <small>{tx("用于区分账号资源，可填写邮箱或账号系统里的唯一地址。")}</small>
                      </label>
                      <label className="field provider-account-auth-wide">
                        <span>{tx("OpenAI/Codex 授权")}</span>
                        <div className="field-action-row">
                          <input readOnly value={openAIAccountOAuthRedirectURI} />
                          <button className="secondary-button" onClick={openProviderAccountAuthorization} type="button" disabled={accountOAuthBusy}>
                            <Send size={14} />
                            {tx(accountOAuthBusy ? "授权中" : "打开授权")}
                          </button>
                        </div>
                        <small>{tx("点击后由后端生成授权地址。OpenAI 固定回调到 localhost:1455；无需该端口实际启动服务。")}</small>
                      </label>
                      <label className="field provider-account-auth-wide">
                        <span>{tx("回调结果")}</span>
                        <textarea
                          value={accountOAuthCallback}
                          onChange={(event) => parseAccountOAuthCallback(event.target.value)}
                          placeholder="http://localhost:1455/auth/callback?code=...&state=..."
                        />
                        <small>{tx("授权完成后，即使 localhost 页面显示无法访问，也请复制地址栏中的完整 callback URL 粘贴到这里。")}</small>
                      </label>
                      <div className="provider-account-auth-actions">
                        <button className="secondary-button" onClick={parseAccountOAuthCallbackNow} type="button">
                          <Check size={14} />
                          {tx("解析回填")}
                        </button>
                        <button className="secondary-button" onClick={copyProviderAccountCallbackURL} type="button">
                          <Copy size={14} />
                          {tx("复制固定回调地址")}
                        </button>
                        <div className={accountTokenSummary.ready ? "provider-account-token-status ready" : "provider-account-token-status"}>
                          {accountTokenSummary.ready ? <Check size={15} /> : <AlertCircle size={15} />}
                          <span>{tx(accountTokenSummary.ready ? "已回填账号 Token" : "等待授权回填")}</span>
                          {accountTokenSummary.items.map((item) => <em key={item}>{tx(item)}</em>)}
                        </div>
                      </div>
                    </div>
                    {accountOAuthStatus ? <p className="provider-credential-note">{accountOAuthStatus}</p> : null}
                    <div className="provider-account-runtime">
                      <div className="provider-account-inline-head">
                        <strong>{tx("资源调度")}</strong>
                        <span>{tx("这些配置决定账号资源参与路由时的权重、并发和限流。")}</span>
                      </div>
                      <div className="provider-account-fields compact">
                        {accountRuntimeFields.filter((field) => field.visible?.(accountValues) ?? true).map((field) => (
                          <ProviderInlineField
                            key={field.key}
                            field={field}
                            value={accountValues[field.key] ?? ""}
                            values={accountValues}
                            onChange={(value) => updateAccountValue(field.key, value)}
                          />
                        ))}
                      </div>
                    </div>
                    <details className="provider-account-advanced">
                      <summary>{tx("高级：手动粘贴 Token")}</summary>
                      <p>{tx("只有在授权回填不可用时使用；保存后 Token 不会再次显示。")}</p>
                      <div className="provider-account-fields">
                        {accountManualTokenFields.filter((field) => field.visible?.(accountValues) ?? true).map((field) => (
                          <ProviderInlineField
                            key={field.key}
                            field={field}
                            value={accountValues[field.key] ?? ""}
                            values={accountValues}
                            onChange={(value) => updateAccountValue(field.key, value)}
                          />
                        ))}
                      </div>
                    </details>
                  </div>
                ) : (
                  <p className="provider-credential-note">
                    {tx("保存后不会写入上游凭据，可稍后通过编辑 Provider 或账号集成补齐。")}
                  </p>
                )}
              </section>
            ) : null}
            {mode === "edit" && subscriptionResources.length > 0 ? (
              <section className="provider-quota-panel">
                <div className="wizard-panel-head">
                  <h3>{tx("订阅额度")}</h3>
                  <p>{tx("实时查询 ChatGPT/Codex 套餐用量和重置时间；不会显示虚构的美元余额。")}</p>
                </div>
                <div className="provider-quota-list">
                  {subscriptionResources.map((resource) => {
                    const quota = accountQuotas[resource.id];
                    const primary = quota?.rate_limit?.primary_window;
                    const secondary = quota?.rate_limit?.secondary_window;
                    const accountLabel = resource.credential_summary?.account_email || resource.credential_summary?.account_id || resource.name;
                    return (
                      <article className="provider-quota-card" key={resource.id}>
                        <div className="provider-quota-card-head">
                          <div>
                            <strong>{resource.name}</strong>
                            <span>{accountLabel}</span>
                          </div>
                          <button className="secondary-button" disabled={accountQuotaBusyID === resource.id} onClick={() => void queryAccountQuota(resource)} type="button">
                            {tx(accountQuotaBusyID === resource.id ? "查询中" : quota ? "刷新额度" : "查询额度")}
                          </button>
                        </div>
                        {quota ? (
                          <div className="provider-quota-grid">
                            <QuotaMetric label="套餐" value={quota.plan_type || resource.credential_summary?.plan_type || "-"} />
                            <QuotaMetric label="状态" value={quota.rate_limit?.limit_reached ? "已达上限" : quota.rate_limit?.allowed ? "可用" : "不可用"} />
                            <QuotaMetric label="主窗口使用率" value={primary ? `${formatQuotaPercent(primary.used_percent)}%` : "-"} />
                            <QuotaMetric label="主窗口重置" value={quotaWindowResetLabel(primary)} />
                            {secondary ? <QuotaMetric label="次窗口使用率" value={`${formatQuotaPercent(secondary.used_percent)}%`} /> : null}
                            {secondary ? <QuotaMetric label="次窗口重置" value={quotaWindowResetLabel(secondary)} /> : null}
                            {quota.rate_limit_reset_credits ? <QuotaMetric label="重置额度" value={String(quota.rate_limit_reset_credits.available_count)} /> : null}
                            <QuotaMetric label="查询时间" value={new Date(quota.fetched_at * 1000).toLocaleString()} />
                          </div>
                        ) : (
                          <p className="provider-credential-note">{tx("点击查询后显示该真实账号当前的套餐额度。")}</p>
                        )}
                      </article>
                    );
                  })}
                </div>
                {accountQuotaError ? <p className="provider-quota-error">{accountQuotaError}</p> : null}
              </section>
            ) : null}
            {mode === "edit" && subscriptionResources.length > 0 ? (
              <section className="provider-quota-panel provider-codex-test-panel">
                <div className="wizard-panel-head">
                  <h3>{tx("Codex 真实请求测试")}</h3>
                  <p>{tx("选择模型、推理强度和速度后，直接使用该订阅账号调用 Codex Responses；该操作会消耗真实套餐额度。")}</p>
                </div>
                <div className="provider-codex-test-controls">
                  <label className="field">
                    <span>{tx("模型")}</span>
                    <select
                      disabled={codexTestValues.speed === "fast"}
                      value={codexTestValues.model}
                      onChange={(event) => setCodexTestValues((current) => ({ ...current, model: event.target.value }))}
                    >
                      {codexSubscriptionModels.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
                    </select>
                    {codexTestValues.speed === "fast" ? <small>{tx("快速模式测试已锁定 5.6 家族中成本最低的 Luna。")}</small> : null}
                  </label>
                  <label className="field">
                    <span>{tx("推理强度")}</span>
                    <select value={codexTestValues.reasoning_effort} onChange={(event) => setCodexTestValues((current) => ({ ...current, reasoning_effort: event.target.value }))}>
                      {codexReasoningEfforts.map((value) => <option key={value} value={value}>{value}</option>)}
                    </select>
                  </label>
                  <label className="field">
                    <span>{tx("速度")}</span>
                    <select
                      value={codexTestValues.speed}
                      onChange={(event) => {
                        const speed = event.target.value;
                        setCodexTestValues((current) => ({ ...current, speed, model: speed === "fast" ? "gpt-5.6-luna" : current.model }));
                      }}
                    >
                      <option value="standard">{tx("标准")}</option>
                      <option value="fast">{tx("快速（仅 5.6 Luna 测试）")}</option>
                    </select>
                  </label>
                  <label className="field provider-codex-test-prompt">
                    <span>{tx("真实提示词")}</span>
                    <textarea
                      rows={3}
                      value={codexTestValues.prompt}
                      onChange={(event) => setCodexTestValues((current) => ({ ...current, prompt: event.target.value }))}
                      placeholder={tx("输入要真实发送给 Codex 的内容")}
                    />
                  </label>
                </div>
                <div className="provider-quota-list">
                  {subscriptionResources.map((resource) => {
                    const result = codexTestResults[resource.id];
                    return (
                      <article className="provider-quota-card" key={resource.id}>
                        <div className="provider-quota-card-head">
                          <div>
                            <strong>{resource.name}</strong>
                            <span>{resource.credential_summary?.account_email || resource.credential_summary?.account_id || resource.id}</span>
                          </div>
                          <button
                            className="primary-button"
                            disabled={codexTestBusyID === resource.id || !codexTestValues.prompt.trim()}
                            onClick={() => void testCodexSubscription(resource)}
                            type="button"
                          >
                            <Send size={14} />
                            {tx(codexTestBusyID === resource.id ? "正在真实调用" : "发送真实测试")}
                          </button>
                        </div>
                        {result ? (
                          <div className="provider-codex-test-result">
                            <div className="provider-quota-grid">
                              <QuotaMetric label="模型" value={result.model} />
                              <QuotaMetric label="推理强度" value={result.reasoning_effort} />
                              <QuotaMetric label="请求速度" value={result.speed === "fast" ? "快速" : "标准"} />
                              <QuotaMetric label="上游 Service Tier" value={result.upstream_service_tier || "未返回"} />
                              <QuotaMetric label="耗时" value={`${result.latency_ms} ms`} />
                              <QuotaMetric label="输入 Token" value={String(result.usage.prompt_tokens)} />
                              <QuotaMetric label="输出 Token" value={String(result.usage.completion_tokens)} />
                              <QuotaMetric label="总 Token" value={String(result.usage.total_tokens)} />
                            </div>
                            <pre>{result.output_text}</pre>
                          </div>
                        ) : (
                          <p className="provider-credential-note">{tx("填写提示词后发送；返回内容来自真实 Codex 上游，不使用 Mock。")}</p>
                        )}
                      </article>
                    );
                  })}
                </div>
                {codexTestError ? <p className="provider-quota-error">{codexTestError}</p> : null}
              </section>
            ) : null}
            {mode === "edit" || createStep === 1 ? (
              <div className="provider-form-grid">
              <label className="field">
                <span>Provider ID</span>
                <input value={values.id ?? ""} onChange={(event) => update("id", event.target.value)} placeholder={catalogID === "custom" ? tx("例如 prv_company_proxy") : tx("留空自动生成")} readOnly={mode === "edit"} />
              </label>
              <label className="field">
                <span>{tx(credentialMode === "account_integration" ? "通道名称" : "渠道名称")}</span>
                <input value={values.name ?? ""} onChange={(event) => update("name", event.target.value)} required />
              </label>
              <label className="field">
                <span>{tx(credentialMode === "account_integration" ? "兼容协议" : "渠道商类型")}</span>
                <select value={values.type ?? ""} onChange={(event) => update("type", event.target.value)} required>
                  {providerTypeOptions.map((option) => <option key={option} value={option}>{providerTypeLabel(option)}</option>)}
                </select>
              </label>
              <label className="field">
                <span>Base URL</span>
                <input value={values.base_url ?? ""} onChange={(event) => update("base_url", event.target.value)} />
              </label>
              {mode === "edit" ? (
                <label className="field">
                  <span>API Key</span>
                  <input value={values.api_key ?? ""} type="password" onChange={(event) => update("api_key", event.target.value)} />
                  {mode === "edit" ? <small>{tx("留空表示不修改现有 Key；填写新值才会覆盖。")}</small> : null}
                </label>
              ) : null}
              <label className="field">
                <span>{tx("优先级")}</span>
                <input value={values.priority ?? "10"} type="number" onChange={(event) => update("priority", event.target.value)} />
              </label>
            </div>
            ) : null}

            {mode === "edit" || createStep === 3 ? (
              <>
            <div className="provider-import-options">
              <div>
                <strong>{tx("自动路由")}</strong>
                <span>{mode === "edit" ? tx("开启后会为下方勾选模型补齐缺失线路，不覆盖已有策略。") : tx("保存渠道时会自动创建下方勾选模型的默认路由。")}</span>
              </div>
              <div className="boolean-toggle provider-route-toggle" role="radiogroup" aria-label={tx("自动路由")}>
                <button
                  aria-checked={autoRouteEnabled}
                  className={autoRouteEnabled ? "active" : ""}
                  onClick={() => update("create_routes", "true")}
                  role="radio"
                  type="button"
                >
                  {tx("开启")}
                </button>
                <button
                  aria-checked={!autoRouteEnabled}
                  className={!autoRouteEnabled ? "active" : ""}
                  onClick={() => update("create_routes", "false")}
                  role="radio"
                  type="button"
                >
                  {tx("关闭开关")}
                </button>
              </div>
            </div>

            <div className="provider-model-head">
              <div>
                <strong>{tx("上游模型映射")}</strong>
                <span>{detail ? `${models.length}/${detail.models_count} ${tx("个可映射模型")}` : tx("加载中")}</span>
              </div>
              <div className="provider-model-tools">
                <input value={modelQuery} onChange={(event) => setModelQuery(event.target.value)} placeholder={tx("搜索模型、能力、参数")} />
                <button className="secondary-button" onClick={() => selectedEntry && selectCatalog(selectedEntry)} type="button">
                  {tx("重新加载")}
                </button>
              </div>
            </div>
            <div className="provider-model-list">
              {modelLoading ? (
                <div className="empty">{tx("正在加载模型列表...")}</div>
              ) : modelError ? (
                <div className="empty">{modelError}</div>
              ) : filteredModels.length === 0 ? (
                <div className="empty">{models.length === 0 ? tx("该渠道商暂无可匹配当前标准模型目录的上游模型") : tx("没有匹配的模型")}</div>
              ) : filteredModels.map((model) => (
                <label className="model-option" key={model.id}>
                  <input
                    checked={autoRouteEnabled && selectedModels[model.id] === true}
                    disabled={!autoRouteEnabled}
                    onChange={(event) => setSelectedModels((current) => ({ ...current, [model.id]: event.target.checked }))}
                    type="checkbox"
                  />
                  <div>
                    <strong>{model.display_name || model.name}</strong>
                    <span>{model.canonical_name || model.id} ← {model.id}</span>
                    <small>
                      {modelCategoryLabel(modelCategoryForCatalog(model))} · {model.family || "model"} · {model.type || "chat"} · {formatModelPrice(model)} · {model.context_window ? `${compactNumber(model.context_window)} ctx` : "ctx -"}
                      {existingRouteModels.has(model.canonical_name || canonicalModelNameForUI(model.id, model.display_name)) ? ` · ${tx("已有路由")}` : ""}
                    </small>
                    <div className="capability-row">
                      {modelCapabilities(model).map((capability) => <em key={capability}>{capability}</em>)}
                    </div>
                  </div>
                </label>
              ))}
            </div>
            <p className="provider-import-hint">
              {!autoRouteEnabled
                ? tx("已关闭自动路由：保存后只创建 Provider，不生成路由策略。")
                : selectedRouteCount > 0
                  ? `${tx("保存后会为")} ${selectedRouteCount} ${tx("个已选")} ${modelCategoryLabel(modelCategory)} ${tx("模型创建缺失的默认路由。")}`
                  : tx("当前没有勾选模型，保存后不会生成路由策略。")}
            </p>
              </>
            ) : null}
          </section>
        </div>
        <div className="modal-actions">
          <button className="secondary-button" onClick={onClose} type="button">{tx("取消")}</button>
          {mode === "create" && createStep > 0 ? (
            <button className="secondary-button" onClick={() => setCreateStep((current) => Math.max(current - 1, 0))} type="button" disabled={loading}>
              {tx("上一步")}
            </button>
          ) : null}
          <button className="button" disabled={loading} type="submit">
            {mode === "create"
              ? createStep === lastCreateStep
                ? loading ? tx("保存中") : tx("保存 Provider")
                : tx("下一步")
              : tx("保存")}
          </button>
        </div>
      </form>
    </div>
  );
}

function QuotaMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="provider-quota-metric">
      <span>{tx(label)}</span>
      <strong>{tx(value)}</strong>
    </div>
  );
}

function formatQuotaPercent(value: number) {
  return Number.isFinite(value) ? String(Math.round(value * 10) / 10) : "0";
}

function quotaWindowResetLabel(window?: OpenAIQuotaWindow) {
  if (!window) return "-";
  if (window.reset_at > 0) return new Date(window.reset_at * 1000).toLocaleString();
  if (window.reset_after_seconds > 0) {
    const minutes = Math.ceil(window.reset_after_seconds / 60);
    return minutes >= 60 ? `${Math.ceil(minutes / 60)} ${tx("小时后")}` : `${minutes} ${tx("分钟后")}`;
  }
  return "-";
}

export function ProviderInlineField({
  field,
  value,
  values,
  onChange,
}: {
  field: FieldConfig;
  value: string;
  values: Record<string, string>;
  onChange: (value: string) => void;
}) {
  if (!(field.visible?.(values) ?? true)) return null;
  const autoComplete = field.autoComplete ?? "off";
  const inputName = `tokenhub-provider-account-${field.key}`;
  const options = (field.options ?? []).map((option) => ({ value: option, label: enumOptionLabel(field.key, option) }));
  if (field.type === "select") {
    return (
      <label className="field" data-field-key={field.key}>
        <span>{tx(field.label)}</span>
        <select value={value} onChange={(event) => { clearCustomValidity(event); onChange(event.target.value); }} onInvalid={handleRequiredFieldInvalid} required={field.required}>
          <option value="">{tx("请选择")}</option>
          {options.map((option) => (
            <option key={option.value} value={option.value}>{tx(option.label)}</option>
          ))}
        </select>
        {field.help ? <small>{tx(field.help)}</small> : null}
      </label>
    );
  }
  if (field.type === "textarea") {
    return (
      <label className="field" data-field-key={field.key}>
        <span>{tx(field.label)}</span>
        <textarea
          autoComplete={autoComplete}
          data-1p-ignore={autoComplete === "off" || autoComplete === "new-password" ? "true" : undefined}
          data-lpignore={autoComplete === "off" || autoComplete === "new-password" ? "true" : undefined}
          name={inputName}
          value={value}
          onChange={(event) => { clearCustomValidity(event); onChange(event.target.value); }}
          onInvalid={handleRequiredFieldInvalid}
          placeholder={tx(field.placeholder)}
          required={field.required}
        />
        {field.help ? <small>{tx(field.help)}</small> : null}
      </label>
    );
  }
  if (field.type === "boolean") {
    const checked = value === "true";
    return (
      <label className="field" data-field-key={field.key}>
        <span>{tx(field.label)}</span>
        <div className="boolean-toggle" role="radiogroup" aria-label={tx(field.label)}>
          <button aria-checked={checked} className={checked ? "active" : ""} onClick={() => onChange("true")} role="radio" type="button">
            {tx("开启")}
          </button>
          <button aria-checked={!checked} className={!checked ? "active" : ""} onClick={() => onChange("false")} role="radio" type="button">
            {tx("关闭开关")}
          </button>
        </div>
        {field.help ? <small>{tx(field.help)}</small> : null}
      </label>
    );
  }
  return (
    <label className="field" data-field-key={field.key}>
      <span>{tx(field.label)}</span>
      <input
        autoComplete={autoComplete}
        data-1p-ignore={autoComplete === "off" || autoComplete === "new-password" ? "true" : undefined}
        data-lpignore={autoComplete === "off" || autoComplete === "new-password" ? "true" : undefined}
        name={inputName}
        value={value}
        type={field.type === "number" ? "number" : field.type === "password" ? "password" : "text"}
        onChange={(event) => { clearCustomValidity(event); onChange(event.target.value); }}
        onInvalid={handleRequiredFieldInvalid}
        placeholder={tx(field.placeholder)}
        required={field.required}
      />
      {field.help ? <small>{tx(field.help)}</small> : null}
    </label>
  );
}

export function providerCredentialOptions(): Array<{ key: ProviderCredentialMode; label: string; description: string; icon: typeof KeyRound }> {
  return [
    {
      key: "provider_api_key",
      label: "直接 API Key",
      description: "把上游 Key 保存到 Provider，适合单账号或兼容 API。",
      icon: KeyRound,
    },
    {
      key: "account_integration",
      label: "账号资源池",
      description: "适合 OpenAI 账号、Subscription 或多账号轮询，默认通道会自动推荐。",
      icon: UserRoundCheck,
    },
    {
      key: "later",
      label: "稍后配置",
      description: "先创建 Provider 和路由，稍后再添加 Key 或账号资源。",
      icon: Settings,
    },
  ];
}

export function providerCreateWizardSteps(): Array<{ title: string; icon: typeof Search }> {
  return [
    { title: "接入方式", icon: UserRoundCheck },
    { title: "渠道信息", icon: Server },
    { title: "账号与凭据", icon: KeyRound },
    { title: "路由与确认", icon: Boxes },
  ];
}

export function providerCreateWizardStepTitle(title: string, credentialMode: ProviderCredentialMode) {
  if (credentialMode === "account_integration" && title === "渠道信息") return "默认通道";
  return title;
}

export function providerCredentialModeLabel(mode: ProviderCredentialMode) {
  return providerCredentialOptions().find((option) => option.key === mode)?.label ?? mode;
}

export function providerAccountResourceReady(values: Record<string, string>) {
  if (values.resource_type === "openai_subscription") {
    return Boolean(values.access_token?.trim() || values.refresh_token?.trim() || values.id_token?.trim());
  }
  return Boolean(values.api_key?.trim());
}

export function recommendedAccountProviderEntry(catalog: ProviderCatalogEntry[]) {
  const openAIEntries = catalog.filter((entry) => providerEntrySupportsCategory(entry, "openai"));
  const exactOpenAI = openAIEntries.find((entry) => {
    const candidates = [entry.id, entry.name, entry.display_name].map((item) => item?.trim().toLowerCase()).filter(Boolean);
    return candidates.some((item) => item === "openai" || item === "openai official");
  });
  if (exactOpenAI) return exactOpenAI;
  return openAIEntries.find((entry) => ["openai", "openai_compatible"].includes(entry.type)) ?? openAIEntries[0] ?? catalog[0];
}
