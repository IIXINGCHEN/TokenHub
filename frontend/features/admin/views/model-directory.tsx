"use client";

import { AlertTriangle, Boxes, ChevronRight, CircleCheck, CircleDashed, Link2, Plus, Search, Settings2, X } from "lucide-react";
import { type FormEvent, useMemo, useState } from "react";
import { type ApiContext, type AppData, type Model, type ModelRoute, type ProviderModel, type ResourceConfig } from "../core/types";
import { modelCategory, modelCategoryLabel, priceMetric } from "../domain/catalog";
import { findProvider, modelRoutesFor } from "../domain/entities";
import { externalModels, filterExternalModels, isCustomModelAlias, modelPublicationState, modelRuntimeState, type ModelPublicationState } from "../domain/model-directory";
import { compactNumber } from "../domain/formatting";
import { tx } from "../i18n/runtime";
import { adminFetch, readAdminError } from "../resources/payloads";
import { DataSection, StatusPill } from "../shared/ui";
import { ModelBrandIcon } from "./model-catalog";

export function ModelDirectoryView({
  api,
  config,
  data,
  loading,
  readOnly = false,
  onReload,
  onCreateModel,
  onOpenRoutes,
  onEditModel,
  onDeleteModel,
  onEditRoute,
  onDeleteRoute,
}: {
  api: ApiContext;
  config: ResourceConfig<Model>;
  data: AppData;
  loading: boolean;
  readOnly?: boolean;
  onReload: () => Promise<void> | void;
  onCreateModel: () => void;
  onOpenRoutes: () => void;
  onEditModel: (model: Model) => void;
  onDeleteModel: (model: Model) => void;
  onEditRoute: (route: ModelRoute) => void;
  onDeleteRoute: (route: ModelRoute) => void;
}) {
  const [publication, setPublication] = useState<"all" | ModelPublicationState>(readOnly ? "all" : "published");
  const [query, setQuery] = useState("");
  const [providerID, setProviderID] = useState("");
  const [mappingModel, setMappingModel] = useState<Model | null>(null);
  const [detailModel, setDetailModel] = useState<Model | null>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");

  const publishedModels = useMemo(() => externalModels(data, readOnly), [data, readOnly]);
  const filteredExternal = useMemo(
    () => filterExternalModels(publishedModels, data, publication, query, providerID),
    [data, providerID, publication, publishedModels, query],
  );
  const stats = useMemo(() => modelDirectoryStats(publishedModels, data), [data, publishedModels]);

  async function setPublished(model: Model, published: boolean) {
    setBusy(true);
    setError("");
    try {
      const resp = await adminFetch(api, `/api/admin/models/${encodeURIComponent(model.name)}`, {
        method: "PATCH",
        body: JSON.stringify({ ...model, status: published ? "active" : "disabled" }),
      });
      if (!resp.ok) throw new Error(await readAdminError(resp, tx(published ? "发布模型" : "下线模型")));
      setNotice(tx(published ? "模型已发布" : "模型已下线，映射线路已保留"));
      await onReload();
    } catch (err) {
      setError(err instanceof Error ? err.message : tx("操作失败"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <DataSection title={config.eyebrow}>
      <div className="model-directory">
        <header className="model-directory-hero">
          <div>
            <p className="eyebrow">{tx("对外模型目录")}</p>
            <h2>{tx("统一模型名称、能力与对外价格")}</h2>
            <span>{tx("这里的模型和价格面向客户端统一生效，不随实际命中的 Provider 改变。")}</span>
          </div>
          {!readOnly ? (
            <div className="model-directory-hero-actions">
              <button className="secondary-button" onClick={onCreateModel} type="button">
                <Plus size={16} />
                {tx("新建对外模型")}
              </button>
              <button className="button" onClick={onOpenRoutes} type="button">
                <Link2 size={16} />
                {tx("配置模型路由")}
              </button>
            </div>
          ) : null}
        </header>

        {!readOnly ? <ModelDirectoryStats stats={stats} /> : null}
        {!readOnly ? <GovernanceFlow /> : null}
        {notice ? <div className="inline-notice success"><CircleCheck size={15} />{notice}</div> : null}
        {error ? <div className="inline-notice error"><AlertTriangle size={15} />{error}</div> : null}

        <div className="model-directory-toolbar">
          <div className="search-box model-directory-search">
            <Search size={16} />
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={tx("搜索对外模型、Provider 或上游模型")} />
          </div>
          {!readOnly ? (
            <select aria-label={tx("筛选 Provider")} value={providerID} onChange={(event) => setProviderID(event.target.value)}>
              <option value="">{tx("全部 Provider")}</option>
              {data.providers.map((provider) => <option key={provider.id} value={provider.id}>{provider.name}</option>)}
            </select>
          ) : null}
          {!readOnly ? (
            <div className="model-publication-filter" role="group" aria-label={tx("发布状态")}>
              {(["published", "draft", "disabled", "all"] as const).map((state) => (
                <button className={publication === state ? "active" : ""} key={state} onClick={() => setPublication(state)} type="button">
                  {tx(publicationLabel(state))}
                </button>
              ))}
            </div>
          ) : null}
        </div>

        <ExternalModelsTable
          data={data}
          models={filteredExternal}
          readOnly={readOnly}
          busy={busy || loading}
          onDetails={setDetailModel}
          onEdit={onEditModel}
          onDelete={onDeleteModel}
          onPublish={setPublished}
        />
      </div>

      {mappingModel ? (
        <ModelMappingModal
          api={api}
          data={data}
          model={mappingModel}
          onClose={() => setMappingModel(null)}
          onSaved={async (message) => {
            setMappingModel(null);
            setNotice(message);
            await onReload();
          }}
        />
      ) : null}
      {detailModel ? (
        <ModelMappingDrawer
          data={data}
          model={detailModel}
          onAdd={() => setMappingModel(detailModel)}
          onClose={() => setDetailModel(null)}
          onDeleteRoute={onDeleteRoute}
          onEditRoute={onEditRoute}
        />
      ) : null}
    </DataSection>
  );
}

function GovernanceFlow() {
  const steps = [
    { index: "1", title: "Provider 渠道", detail: "引入上游模型 · 维护真实成本" },
    { index: "2", title: "模型目录", detail: "定义对外名称 · 选择初始线路 · 设置统一价格" },
    { index: "3", title: "路由策略", detail: "调整优先级、权重与流量策略" },
  ];
  return (
    <div className="model-governance-flow" aria-label={tx("模型治理流程")}>
      {steps.map((step, index) => (
        <div className="model-governance-flow-step" key={step.index}>
          <span>{step.index}</span>
          <div><strong>{tx(step.title)}</strong><small>{tx(step.detail)}</small></div>
          {index < steps.length - 1 ? <ChevronRight size={16} /> : null}
        </div>
      ))}
    </div>
  );
}

function ModelDirectoryStats({ stats }: { stats: ReturnType<typeof modelDirectoryStats> }) {
  const items = [
    { label: "已发布", value: stats.published, icon: CircleCheck, tone: "healthy" },
    { label: "草稿/待映射", value: stats.draft, icon: CircleDashed, tone: "draft" },
    { label: "正常可用", value: stats.healthy, icon: Link2, tone: "healthy" },
    { label: "线路异常", value: stats.issues, icon: AlertTriangle, tone: "warning" },
  ];
  return (
    <div className="model-directory-stats">
      {items.map((item) => (
        <div className={item.tone} key={item.label}>
          <item.icon size={17} />
          <span>{tx(item.label)}</span>
          <strong>{item.value}</strong>
        </div>
      ))}
    </div>
  );
}

function ExternalModelsTable({ data, models, readOnly, busy, onDetails, onEdit, onDelete, onPublish }: {
  data: AppData;
  models: Model[];
  readOnly: boolean;
  busy: boolean;
  onDetails: (model: Model) => void;
  onEdit: (model: Model) => void;
  onDelete: (model: Model) => void;
  onPublish: (model: Model, published: boolean) => void;
}) {
  if (models.length === 0) {
    return (
      <div className="model-directory-empty">
        <Boxes size={28} />
        <strong>{tx(readOnly ? "当前没有可见模型" : "当前范围没有对外模型")}</strong>
        <span>{tx(readOnly ? "请联系管理员发布模型并授予 API Key 访问范围。" : "请创建对外模型、选择已引入的 Provider 模型并设置统一价格；创建后可在路由策略中细调流量。")}</span>
      </div>
    );
  }
  return (
    <div className="model-directory-table-wrap">
      <table className="model-directory-table">
        <thead><tr><th>{tx("对外模型")}</th><th>{tx("类型与能力")}</th>{!readOnly ? <><th>{tx("真实上游映射")}</th><th>{tx("发布")}</th><th>{tx("线路")}</th></> : <th>{tx("可用状态")}</th>}<th>{tx("对外统一价")}</th>{!readOnly ? <th>{tx("操作")}</th> : null}</tr></thead>
        <tbody>
          {models.map((model) => {
            const routes = modelRoutesFor(model, data);
            const activeRoutes = routes.filter((route) => route.status === "active");
            const primary = activeRoutes[0] ?? routes[0];
            const provider = primary ? findProvider(data, primary.provider_id) : undefined;
            const publication = modelPublicationState(model, data);
            const runtime = modelRuntimeState(model, data);
            const customAlias = isCustomModelAlias(model, routes);
            return (
              <tr key={model.name}>
                <td>
                  <div className="directory-model-name">
                    <ModelBrandIcon category={modelCategory(model)} label={modelCategoryLabel(modelCategory(model))} />
                    <div><strong>{model.name}</strong>{!readOnly ? <span>{customAlias ? tx("自定义别名") : tx("同名 1:1")}</span> : null}</div>
                  </div>
                </td>
                <td><strong>{model.modality || "chat"}</strong><span>{compactNumber(model.context_window || 0)} ctx · {(model.capabilities ?? []).slice(0, 2).join(" / ") || model.family || "-"}</span></td>
                {!readOnly ? <>
                  <td>
                    {primary ? <button className="mapping-summary" onClick={() => onDetails(model)} type="button"><span>{provider?.name || primary.provider_id}</span><strong>{primary.provider_model}</strong>{routes.length > 1 ? <em>+{routes.length - 1}</em> : null}<ChevronRight size={14} /></button> : <span className="muted">{tx("尚未映射 Provider")}</span>}
                  </td>
                  <td><StatusPill status={publication === "published" ? "active" : "disabled"} label={tx(publicationLabel(publication))} /></td>
                  <td><RuntimeStatus state={runtime} active={activeRoutes.length} total={routes.length} /></td>
                </> : <td><StatusPill status="active" label={tx("当前账号可用")} /></td>}
                <td><strong>{priceMetric(model.input_price_usd_per_1m)}</strong><span>{tx("输入")} · {priceMetric(model.output_price_usd_per_1m)} {tx("输出")}</span></td>
                {!readOnly ? (
                  <td><div className="directory-row-actions"><button className="text-button" onClick={() => onDetails(model)} type="button">{tx("管理映射")}</button><button className="text-button" onClick={() => onEdit(model)} type="button">{tx("编辑")}</button><button className="text-button" disabled={busy || (publication !== "published" && activeRoutes.length === 0)} onClick={() => onPublish(model, publication !== "published")} type="button">{tx(publication === "published" ? "下线" : "发布")}</button><button className="danger-button" onClick={() => onDelete(model)} type="button">{tx("删除")}</button></div></td>
                ) : null}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function RuntimeStatus({ state, active, total }: { state: ReturnType<typeof modelRuntimeState>; active: number; total: number }) {
  const config = {
    healthy: { label: "正常", status: "healthy" },
    degraded: { label: "部分异常", status: "warning" },
    unavailable: { label: "全部异常", status: "down" },
    unmapped: { label: "未映射", status: "disabled" },
  }[state];
  return <div className="runtime-status"><StatusPill status={config.status} label={tx(config.label)} /><span>{active}/{total} {tx("条启用")}</span></div>;
}

function ModelMappingModal({ api, data, model, onClose, onSaved }: { api: ApiContext; data: AppData; model: Model; onClose: () => void; onSaved: (message: string) => Promise<void> | void }) {
  const [providerID, setProviderID] = useState(data.providers[0]?.id || "");
  const [upstreamModel, setUpstreamModel] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const providerModels = data.providerModels.filter((model) => model.provider_id === providerID);
  const selectedProviderModel = providerModels.find((model) => model.upstream_model === upstreamModel);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!providerID || !upstreamModel.trim()) {
      setError(tx("请填写对外模型、Provider 和上游模型。"));
      return;
    }
    if (!selectedProviderModel) {
      setError(tx("请先从 Provider 引入上游模型，再创建对外映射。"));
      return;
    }
    setBusy(true);
    setError("");
    try {
      const route = { model_name: model.name, provider_id: providerID, provider_model: upstreamModel.trim(), status: "active", priority: 0, weight: 100, quality_score: 60, cost_score: 60, strategy: "balanced" };
      const resp = await adminFetch(api, "/api/admin/routing-rules", { method: "POST", body: JSON.stringify(route) });
      if (!resp.ok) throw new Error(await readAdminError(resp, tx("保存模型映射")));
      await onSaved(tx("已添加新的 Provider 映射"));
    } catch (err) {
      setError(err instanceof Error ? err.message : tx("保存失败"));
    } finally {
      setBusy(false);
    }
  }

  return <div className="modal-backdrop" role="presentation"><form className="modal model-mapping-modal" onSubmit={submit}>
    <div className="modal-header"><div><p className="eyebrow">{tx("添加 Provider 映射")}</p><h2>{model.name}</h2></div><button className="icon-button" onClick={onClose} type="button"><X size={18} /></button></div>
    <div className="modal-body">
      {error ? <div className="inline-notice error"><AlertTriangle size={15} />{error}</div> : null}
      <div className="mapping-chain-editor"><div><span>{tx("客户端请求")}</span><strong>{model.name}</strong></div><ChevronRight /><div><span>Provider</span><strong>{findProvider(data, providerID)?.name || tx("请选择")}</strong></div><ChevronRight /><div><span>{tx("上游模型")}</span><strong>{upstreamModel || "provider model"}</strong></div></div>
      <div className="form-grid two"><label className="field"><span>Provider *</span><select value={providerID} onChange={(event) => { setProviderID(event.target.value); setUpstreamModel(""); }} required>{data.providers.map((provider) => <option key={provider.id} value={provider.id}>{provider.name}</option>)}</select></label><label className="field"><span>{tx("已引入的上游模型")} *</span><select disabled={providerModels.length === 0} value={upstreamModel} onChange={(event) => setUpstreamModel(event.target.value)} required><option value="">{tx("请选择上游模型")}</option>{providerModels.map((model) => <option key={model.id} value={model.upstream_model}>{model.upstream_model}{model.display_name && model.display_name !== model.upstream_model ? ` · ${model.display_name}` : ""}</option>)}</select><small>{providerModels.length ? tx("这里只能选择已引入当前 Provider 的上游模型。") : tx("该 Provider 暂无已引入模型，请先从 Provider 引入。")}</small></label></div>
      <div className="mapping-mismatch-warning"><AlertTriangle size={18} /><div><strong>{tx("路由不会改变对外价格")}</strong><span>{tx("Provider 模型价格用于真实成本审计；客户端仍按模型目录中的统一价格计费。")}</span></div></div>
    </div>
    <div className="modal-actions"><button className="secondary-button" onClick={onClose} type="button">{tx("取消")}</button><button className="button" disabled={busy || !selectedProviderModel} type="submit">{busy ? tx("保存中") : tx("添加映射")}</button></div>
  </form></div>;
}

function ModelMappingDrawer({ data, model, onAdd, onClose, onEditRoute, onDeleteRoute }: { data: AppData; model: Model; onAdd: () => void; onClose: () => void; onEditRoute: (route: ModelRoute) => void; onDeleteRoute: (route: ModelRoute) => void }) {
  const routes = modelRoutesFor(model, data);
  return <div className="model-drawer-backdrop" onMouseDown={(event) => { if (event.currentTarget === event.target) onClose(); }} role="presentation"><aside className="model-mapping-drawer" role="dialog" aria-modal="true">
    <header><div><p className="eyebrow">{tx("对外模型映射")}</p><h2>{model.name}</h2><span>{tx("客户端模型名保持不变，可以替换或增加真实上游线路。")}</span></div><button className="icon-button" onClick={onClose} type="button"><X size={18} /></button></header>
    <div className="drawer-state-row"><StatusPill status={modelPublicationState(model, data) === "published" ? "active" : "disabled"} label={tx(publicationLabel(modelPublicationState(model, data)))} /><RuntimeStatus state={modelRuntimeState(model, data)} active={routes.filter((route) => route.status === "active").length} total={routes.length} /></div>
    <section><div className="drawer-section-head"><div><strong>{tx("上游线路")}</strong><span>{routes.length <= 1 ? tx("单线路保持简单；新增第二条后再配置主备和分流。") : tx("按优先级和权重执行故障转移与分流。")}</span></div><button className="secondary-button" onClick={onAdd} type="button"><Plus size={15} />{tx("添加线路")}</button></div>
      {routes.length === 0 ? <div className="model-directory-empty compact"><Link2 size={24} /><strong>{tx("尚未配置 Provider 映射")}</strong></div> : <div className="drawer-route-list">{routes.map((route, index) => { const provider = findProvider(data, route.provider_id); return <article key={route.id}><div className="route-role">{index === 0 ? tx("主线路") : route.priority === routes[0]?.priority ? tx("参与分流") : tx("故障备用")}</div><div className="route-chain"><strong>{provider?.name || route.provider_id}</strong><ChevronRight size={14} /><strong>{route.provider_model}</strong></div><div className="route-meta"><StatusPill status={route.status} /><span>P{route.priority} · W{route.weight} · {route.strategy || "balanced"}</span></div><div className="directory-row-actions"><button className="text-button" onClick={() => onEditRoute(route)} type="button">{tx("编辑")}</button><button className="danger-button" onClick={() => onDeleteRoute(route)} type="button">{tx("删除")}</button></div></article>; })}</div>}
    </section>
    <section className="drawer-contract"><strong>{tx("能力与计价口径")}</strong><div><span>{tx("能力")}</span><b>{(model.capabilities ?? []).join(" / ") || model.modality || "-"}</b></div><div><span>{tx("上下文")}</span><b>{compactNumber(model.context_window || 0)}</b></div><div><span>{tx("对外统一价")}</span><b>{priceMetric(model.input_price_usd_per_1m)} / {priceMetric(model.output_price_usd_per_1m)}</b></div><small>{tx("对外模型价格对所有路由统一；命中 Provider 的真实成本在请求审计中单独记录。")}</small></section>
  </aside></div>;
}

function modelDirectoryStats(models: Model[], data: AppData) {
  return models.reduce((stats, model) => {
    const publication = modelPublicationState(model, data);
    const runtime = modelRuntimeState(model, data);
    if (publication === "published") stats.published += 1;
    if (publication === "draft") stats.draft += 1;
    if (runtime === "healthy") stats.healthy += 1;
    if (runtime === "degraded" || runtime === "unavailable") stats.issues += 1;
    return stats;
  }, { published: 0, draft: 0, healthy: 0, issues: 0 });
}

function publicationLabel(state: "all" | ModelPublicationState) {
  return { all: "全部", published: "已发布", draft: "草稿/待映射", disabled: "已下线" }[state];
}
