import { Check, Eye, EyeOff, KeyRound, Plus, Search, SlidersHorizontal } from "lucide-react";
import { useState } from "react";
import { type ProviderCatalogEntry } from "../core/types";
import { providerTypeLabel } from "../domain/labels";
import { clearCustomValidity, countWithUnit, handleRequiredFieldInvalid, tx } from "../i18n/runtime";
import { providerTypeOptions } from "../shared/ui";

export function ProviderAPIQuickCatalog({
  entries,
  total,
  selectedID,
  query,
  onQueryChange,
  onSelect,
  onSelectCustom,
}: {
  entries: ProviderCatalogEntry[];
  total: number;
  selectedID: string;
  query: string;
  onQueryChange: (value: string) => void;
  onSelect: (entry: ProviderCatalogEntry) => void;
  onSelectCustom: () => void;
}) {
  return (
    <section className="provider-catalog-pane provider-quick-catalog-pane">
      <div className="provider-catalog-head">
        <strong>{tx("选择渠道商")}</strong>
        <span>{countWithUnit(total, "个渠道", "provider", "Provider")}</span>
      </div>
      <div className="provider-template-search provider-quick-search">
        <Search size={14} />
        <input value={query} onChange={(event) => onQueryChange(event.target.value)} placeholder={tx("搜索渠道商名称或 ID")} />
      </div>
      <div className="provider-quick-catalog-list">
        {entries.length === 0 ? (
          <div className="empty compact-empty">{tx("没有匹配的渠道商")}</div>
        ) : entries.map((entry) => {
          const name = entry.display_name || entry.name;
          const active = entry.id === selectedID;
          return (
            <button className={active ? "provider-quick-catalog-item active" : "provider-quick-catalog-item"} key={entry.id} onClick={() => onSelect(entry)} type="button">
              <span className="provider-quick-catalog-avatar">{name.slice(0, 1).toUpperCase()}</span>
              <span className="provider-quick-catalog-copy">
                <strong>{name}</strong>
                <em>{providerTypeLabel(entry.type)} · {countWithUnit(entry.models_count, "个模型", "model", "モデル")}</em>
              </span>
              {active ? <Check size={15} /> : null}
            </button>
          );
        })}
      </div>
      <button className={selectedID === "custom" ? "custom-provider-button provider-quick-custom active" : "custom-provider-button provider-quick-custom"} onClick={onSelectCustom} type="button">
        <Plus size={14} />
        <span>{tx("自定义渠道商")}</span>
        <em>{tx("填写名称、Base URL 和 API Key")}</em>
      </button>
    </section>
  );
}

export function ProviderAPIQuickConnect({
  catalogID,
  entry,
  modelCount,
  values,
  onUpdate,
}: {
  catalogID: string;
  entry?: ProviderCatalogEntry;
  modelCount: number;
  values: Record<string, string>;
  onUpdate: (key: string, value: string) => void;
}) {
  const [showKey, setShowKey] = useState(false);
  const custom = catalogID === "custom";
  const name = values.name || entry?.display_name || entry?.name || tx("请选择渠道商");

  return (
    <section className="provider-api-quick-connect">
      <div className="provider-api-quick-hero">
        <div>
          <span>{tx("直接 API Key")}</span>
          <h3>{name}</h3>
          <p>{values.base_url || tx("填写 Base URL 后连接上游")}</p>
        </div>
        <strong>{countWithUnit(modelCount, "个可映射模型", "mappable model", "マッピング可能モデル")}</strong>
      </div>

      <div className="provider-api-quick-intro">
        <span><KeyRound size={18} /></span>
        <div>
          <strong>{tx(custom ? "填写连接信息" : "只需填写 API Key")}</strong>
          <p>{tx(custom ? "自定义渠道需要填写名称、Base URL 和 API Key。" : "使用推荐地址创建 Provider，并自动补齐可映射模型的默认路由。")}</p>
        </div>
      </div>

      {custom ? (
        <div className="provider-quick-required-grid">
          <label className="field">
            <span>{tx("渠道名称")}</span>
            <input value={values.name ?? ""} onChange={(event) => onUpdate("name", event.target.value)} required />
          </label>
          <label className="field">
            <span>Base URL</span>
            <input value={values.base_url ?? ""} onChange={(event) => onUpdate("base_url", event.target.value)} required />
          </label>
        </div>
      ) : null}

      <label className="field provider-quick-key-field">
        <span>API Key</span>
        <div className="provider-quick-key-input">
          <input
            autoComplete="new-password"
            autoFocus
            value={values.api_key ?? ""}
            type={showKey ? "text" : "password"}
            onChange={(event) => {
              clearCustomValidity(event);
              onUpdate("api_key", event.target.value);
            }}
            onInvalid={handleRequiredFieldInvalid}
            required
          />
          <button aria-label={tx(showKey ? "隐藏 API Key" : "显示 API Key")} onClick={() => setShowKey((current) => !current)} type="button">
            {showKey ? <EyeOff size={17} /> : <Eye size={17} />}
          </button>
        </div>
        {entry?.doc_url ? <a href={entry.doc_url} rel="noreferrer" target="_blank">{tx("获取 API Key")}</a> : null}
      </label>

      <details className="provider-quick-advanced">
        <summary>
          <SlidersHorizontal size={15} />
          <span><strong>{tx("高级配置")}</strong><em>{tx("名称、类型、Base URL 与默认路由")}</em></span>
        </summary>
        <div className="provider-form-grid provider-quick-advanced-grid">
          <label className="field">
            <span>Provider ID</span>
            <input value={values.id ?? ""} onChange={(event) => onUpdate("id", event.target.value)} placeholder={custom ? tx("例如 prv_company_proxy") : tx("留空自动生成")} />
          </label>
          {!custom ? (
            <label className="field">
              <span>{tx("渠道名称")}</span>
              <input value={values.name ?? ""} onChange={(event) => onUpdate("name", event.target.value)} required />
            </label>
          ) : null}
          <label className="field">
            <span>{tx("渠道商类型")}</span>
            <select value={values.type ?? ""} onChange={(event) => onUpdate("type", event.target.value)} required>
              {providerTypeOptions.map((option) => <option key={option} value={option}>{providerTypeLabel(option)}</option>)}
            </select>
          </label>
          {!custom ? (
            <label className="field">
              <span>Base URL</span>
              <input value={values.base_url ?? ""} onChange={(event) => onUpdate("base_url", event.target.value)} />
            </label>
          ) : null}
          <label className="field">
            <span>{tx("优先级")}</span>
            <input value={values.priority ?? "10"} type="number" onChange={(event) => onUpdate("priority", event.target.value)} />
          </label>
        </div>
        {custom ? (
          <p className="provider-quick-custom-note">{tx("自定义渠道创建后，可在模型映射中加载上游模型并配置路由。")}</p>
        ) : (
          <div className="provider-import-options provider-quick-route-option">
            <div><strong>{tx("自动路由")}</strong><span>{tx("创建后自动补齐可映射模型的默认路由。")}</span></div>
            <div className="boolean-toggle provider-route-toggle" role="radiogroup" aria-label={tx("自动路由")}>
              <button aria-checked={values.create_routes === "true"} className={values.create_routes === "true" ? "active" : ""} onClick={() => onUpdate("create_routes", "true")} role="radio" type="button">{tx("开启")}</button>
              <button aria-checked={values.create_routes !== "true"} className={values.create_routes !== "true" ? "active" : ""} onClick={() => onUpdate("create_routes", "false")} role="radio" type="button">{tx("关闭开关")}</button>
            </div>
          </div>
        )}
      </details>
    </section>
  );
}
