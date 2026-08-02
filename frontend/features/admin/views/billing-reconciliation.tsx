import { CalendarRange, CirclePause, CirclePlay, Download, Eye, LockKeyhole, Plus, RefreshCw, X } from "lucide-react";
import { type FormEvent, useState } from "react";
import { type ApiContext, type AppData, type ReconciliationDetail, type ReconciliationItem, type ReconciliationRule, type ReconciliationRun } from "../core/types";
import { languageLocale, tx } from "../i18n/runtime";
import { adminFetch, readAdminError } from "../resources/payloads";
import { DataSection, SimpleTable, StatusPill } from "../shared/ui";

export function ReconciliationManager({ api, data, loading, onReload }: { api: ApiContext; data: AppData; loading: boolean; onReload: () => Promise<void> }) {
  const [ruleEditorOpen, setRuleEditorOpen] = useState(false);
  const [runRule, setRunRule] = useState<ReconciliationRule | null>(null);
  const [detail, setDetail] = useState<ReconciliationDetail | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const connectorNames = new Map(data.billingConnectors.map((connector) => [connector.id, connector.name]));
  const ruleNames = new Map(data.reconciliationRules.map((rule) => [rule.id, rule.name]));

  async function loadDetail(runID: string) {
    setBusy(`detail:${runID}`);
    setError("");
    try {
      const response = await adminFetch(api, `/api/admin/billing/reconciliations/${encodeURIComponent(runID)}`);
      if (!response.ok) throw new Error(await readAdminError(response, tx("对账明细加载失败")));
      setDetail((await response.json()) as ReconciliationDetail);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : tx("操作失败"));
    } finally {
      setBusy("");
    }
  }

  async function toggleRule(rule: ReconciliationRule) {
    const status = rule.status === "active" ? "disabled" : "active";
    setBusy(`rule:${rule.id}`);
    setError("");
    setNotice("");
    try {
      const response = await adminFetch(api, `/api/admin/billing/reconciliation-rules/${encodeURIComponent(rule.id)}`, {
        method: "PATCH",
        body: JSON.stringify({ status }),
      });
      if (!response.ok) throw new Error(await readAdminError(response, tx("对账规则状态更新失败")));
      setNotice(tx(status === "active" ? "对账规则已启用" : "对账规则已停用"));
      await onReload();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : tx("操作失败"));
    } finally {
      setBusy("");
    }
  }

  async function runAction(run: ReconciliationRun, action: "lock" | "recalculate") {
    setBusy(`${run.id}:${action}`);
    setError("");
    setNotice("");
    try {
      const response = await adminFetch(api, `/api/admin/billing/reconciliations/${encodeURIComponent(run.id)}/${action}`, {
        method: "POST",
        body: JSON.stringify({}),
      });
      if (!response.ok) throw new Error(await readAdminError(response, tx(action === "lock" ? "对账结果锁定失败" : "对账重新计算失败")));
      setNotice(tx(action === "lock" ? "对账结果已锁定" : "对账重新计算完成"));
      await onReload();
      await loadDetail(run.id);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : tx("操作失败"));
    } finally {
      setBusy("");
    }
  }

  async function exportRun(run: ReconciliationRun) {
    setBusy(`${run.id}:export`);
    setError("");
    try {
      const response = await adminFetch(api, `/api/admin/billing/reconciliations/${encodeURIComponent(run.id)}/export`);
      if (!response.ok) throw new Error(await readAdminError(response, tx("差异报告导出失败")));
      const blobURL = window.URL.createObjectURL(await response.blob());
      const anchor = document.createElement("a");
      anchor.href = blobURL;
      anchor.download = `reconciliation-${run.id}.csv`;
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      window.URL.revokeObjectURL(blobURL);
      setNotice(tx("差异报告已导出"));
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : tx("导出失败"));
    } finally {
      setBusy("");
    }
  }

  return (
    <>
      <DataSection title="成本对账规则">
        <div className="billing-connector-toolbar">
          <div className="billing-connector-status" aria-live="polite">
            {error ? <span className="billing-inline-error">{error}</span> : null}
            {!error && notice ? <span className="billing-inline-notice">{notice}</span> : null}
          </div>
          <button className="button" disabled={data.billingConnectors.length === 0} onClick={() => setRuleEditorOpen(true)} type="button">
            <Plus size={15} />{tx("新增对账规则")}
          </button>
        </div>
        {data.billingConnectors.length === 0 ? <p className="reconciliation-empty-hint">{tx("请先配置并同步外部账单连接器。")}</p> : null}
        <SimpleTable
          columns={["规则", "账单来源", "粒度 / 维度", "容差", "调度 / 延迟", "状态 / 版本", "操作"]}
          paginationKey="reconciliation-rules"
          rows={data.reconciliationRules.map((rule) => [
            <strong key={`${rule.id}:name`}>{rule.name}</strong>,
            connectorNames.get(rule.connector_id) || rule.connector_id,
            <span key={`${rule.id}:matching`}>{reconciliationGranularityLabel(rule.granularity)}<small>{rule.match_dimensions.map(reconciliationDimensionLabel).join(" · ")}</small></span>,
            `${formatReconciliationMoney(rule.amount_tolerance, rule.currency || "USD")} / ${formatRatio(rule.ratio_tolerance)}`,
            `${rule.schedule_interval_minutes > 0 ? formatMinutes(rule.schedule_interval_minutes) : tx("手动")} / ${formatMinutes(rule.billing_delay_minutes)}`,
            <span key={`${rule.id}:status`}><StatusPill status={rule.status} /> <small>v{formatReconciliationNumber(rule.version)}</small></span>,
            <div className="billing-row-actions" key={`${rule.id}:actions`}>
              <button className="icon-button subtle" disabled={Boolean(busy) || loading || rule.status !== "active"} onClick={() => setRunRule(rule)} title={tx("发起对账")} type="button"><CalendarRange size={15} /></button>
              <button className="icon-button subtle" disabled={Boolean(busy) || loading} onClick={() => void toggleRule(rule)} title={tx(rule.status === "active" ? "停用对账规则" : "启用对账规则")} type="button">
                {rule.status === "active" ? <CirclePause size={15} /> : <CirclePlay size={15} />}
              </button>
            </div>,
          ])}
        />
      </DataSection>

      <DataSection title="最近对账结果">
        <SimpleTable
          columns={["规则 / 账期", "状态", "四类结果", "Provider / TokenHub", "差异", "规则 / 输入", "操作"]}
          paginationKey="reconciliation-runs"
          rows={data.reconciliationRuns.map((run) => [
            <span key={`${run.id}:period`}><strong>{ruleNames.get(run.rule_id) || run.rule_id}</strong><small>{formatReconciliationDate(run.period_start)} – {formatReconciliationDate(run.period_end)}</small></span>,
            <span key={`${run.id}:status`}><StatusPill status={run.status} />{run.locked_at ? <small>{tx("已锁定")}</small> : null}</span>,
            [run.matched_count, run.provider_only_count, run.tokenhub_only_count, run.amount_mismatch_count].map(formatReconciliationNumber).join(" / "),
            `${formatReconciliationMoney(run.provider_amount, run.currency || "USD")} / ${formatReconciliationMoney(run.tokenhub_amount, run.currency || "USD")}`,
            formatReconciliationMoney(run.difference_amount, run.currency || "USD"),
            <span key={`${run.id}:hash`}>v{formatReconciliationNumber(run.rule_version)}<small>{shortHash(run.input_hash)}</small></span>,
            <div className="billing-row-actions" key={`${run.id}:actions`}>
              <button className="icon-button subtle" disabled={Boolean(busy)} onClick={() => void loadDetail(run.id)} title={tx("查看差异明细")} type="button"><Eye size={15} /></button>
              <button className="icon-button subtle" disabled={Boolean(busy) || Boolean(run.locked_at) || run.status !== "succeeded"} onClick={() => void runAction(run, "recalculate")} title={tx("重新计算")} type="button"><RefreshCw size={15} /></button>
              <button className="icon-button subtle" disabled={Boolean(busy) || Boolean(run.locked_at) || run.status !== "succeeded"} onClick={() => void runAction(run, "lock")} title={tx("锁定结果")} type="button"><LockKeyhole size={15} /></button>
              <button className="icon-button subtle" disabled={Boolean(busy) || run.status !== "succeeded"} onClick={() => void exportRun(run)} title={tx("导出差异报告")} type="button"><Download size={15} /></button>
            </div>,
          ])}
        />
      </DataSection>

      {detail ? <ReconciliationDetailPanel detail={detail} onClose={() => setDetail(null)} /> : null}
      {ruleEditorOpen ? (
        <ReconciliationRuleEditor
          api={api}
          connectors={data.billingConnectors}
          onClose={() => setRuleEditorOpen(false)}
          onSaved={async () => {
            setRuleEditorOpen(false);
            setNotice(tx("对账规则已保存"));
            await onReload();
          }}
        />
      ) : null}
      {runRule ? (
        <ReconciliationRunDialog
          api={api}
          rule={runRule}
          onClose={() => setRunRule(null)}
          onStarted={async (run) => {
            setRunRule(null);
            setNotice(tx("对账执行完成"));
            await onReload();
            await loadDetail(run.id);
          }}
        />
      ) : null}
    </>
  );
}

function ReconciliationDetailPanel({ detail, onClose }: { detail: ReconciliationDetail; onClose: () => void }) {
  const run = detail.run;
  return (
    <DataSection title="对账差异明细">
      <div className="reconciliation-detail-head">
        <div className="reconciliation-summary-grid">
          <ReconciliationMetric label="已匹配" value={run.matched_count} />
          <ReconciliationMetric label="仅 Provider 存在" value={run.provider_only_count} />
          <ReconciliationMetric label="仅 TokenHub 存在" value={run.tokenhub_only_count} />
          <ReconciliationMetric label="金额不一致" value={run.amount_mismatch_count} />
        </div>
        <button className="icon-button subtle" onClick={onClose} title={tx("关闭")} type="button"><X size={16} /></button>
      </div>
      <p className="reconciliation-fingerprint">{tx("输入指纹")}：{run.input_hash} · {tx("规则版本")}：v{formatReconciliationNumber(run.rule_version)}</p>
      <SimpleTable
        columns={["结果", "时间桶", "匹配维度", "Provider 金额", "TokenHub 金额", "差异 / 比例", "可能原因", "源记录"]}
        paginationKey={`reconciliation-items-${run.id}`}
        rows={detail.items.map((item) => [
          <StatusPill key={`${item.id}:status`} status={item.status} />,
          formatReconciliationDate(item.bucket_start),
          reconciliationDimensionSummary(item),
          formatReconciliationMoney(item.provider_amount, item.currency),
          formatReconciliationMoney(item.tokenhub_amount, item.currency),
          `${formatReconciliationMoney(item.difference_amount, item.currency)} / ${formatRatio(item.difference_ratio)}`,
          reconciliationReasonLabel(item.possible_reason),
          <ReconciliationSourceRecords item={item} key={`${item.id}:sources`} />,
        ])}
      />
    </DataSection>
  );
}

function ReconciliationMetric({ label, value }: { label: string; value: number }) {
  return <div><span>{tx(label)}</span><strong>{formatReconciliationNumber(value)}</strong></div>;
}

function ReconciliationSourceRecords({ item }: { item: ReconciliationItem }) {
  return (
    <details className="reconciliation-source-records">
      <summary>{tx("账单")} {formatReconciliationNumber(item.provider_record_ids.length)} / {tx("用量")} {formatReconciliationNumber(item.tokenhub_record_ids.length)}</summary>
      <small>{tx("账单记录 ID")}：{item.provider_record_ids.join(", ") || "-"}</small>
      <small>{tx("用量记录 ID")}：{item.tokenhub_record_ids.join(", ") || "-"}</small>
    </details>
  );
}

function ReconciliationRuleEditor({ api, connectors, onClose, onSaved }: { api: ApiContext; connectors: AppData["billingConnectors"]; onClose: () => void; onSaved: () => Promise<void> }) {
  const [values, setValues] = useState(() => ({
    name: "", connector_id: connectors[0]?.id ?? "", granularity: "detail", match_dimensions: ["request_id", "model", "currency"],
    amount_tolerance: "0.01", ratio_tolerance: "0.01", time_window_minutes: "15", billing_delay_minutes: "60",
    schedule_interval_minutes: "0", timezone: "UTC", currency: "USD", usd_exchange_rate: "1", dimension_mappings: "{}",
  }));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const update = (key: string, value: string) => setValues((current) => ({ ...current, [key]: value }));

  function updateGranularity(granularity: string) {
    setValues((current) => ({ ...current, granularity, match_dimensions: granularity === "detail" ? ["request_id", "model", "currency"] : ["model", "currency"] }));
  }

  function toggleDimension(dimension: string) {
    setValues((current) => ({
      ...current,
      match_dimensions: current.match_dimensions.includes(dimension)
        ? current.match_dimensions.filter((item) => item !== dimension)
        : [...current.match_dimensions, dimension],
    }));
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      let dimensionMappings: unknown;
      try {
        dimensionMappings = JSON.parse(values.dimension_mappings) as unknown;
      } catch {
        throw new Error(tx("维度映射必须是有效的 JSON 对象"));
      }
      if (!dimensionMappings || typeof dimensionMappings !== "object" || Array.isArray(dimensionMappings)) {
        throw new Error(tx("维度映射必须是有效的 JSON 对象"));
      }
      const response = await adminFetch(api, "/api/admin/billing/reconciliation-rules", {
        method: "POST",
        body: JSON.stringify({
          ...values,
          dimension_mappings: dimensionMappings,
          time_window_minutes: Number(values.time_window_minutes),
          billing_delay_minutes: Number(values.billing_delay_minutes),
          schedule_interval_minutes: Number(values.schedule_interval_minutes),
        }),
      });
      if (!response.ok) throw new Error(await readAdminError(response, tx("对账规则保存失败")));
      await onSaved();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : tx("保存失败"));
    } finally {
      setBusy(false);
    }
  }

  const dimensions = values.granularity === "detail"
    ? ["request_id", "provider", "resource_account", "model", "project", "currency"]
    : ["provider", "resource_account", "model", "project", "currency"];
  return (
    <div className="modal-backdrop" role="presentation">
      <form className="modal reconciliation-rule-modal" onSubmit={submit}>
        <div className="modal-header"><div><p className="eyebrow">FinOps</p><h3>{tx("新增对账规则")}</h3></div><button className="icon-button" onClick={onClose} type="button"><X size={18} /></button></div>
        <div className="modal-body reconciliation-rule-form">
          <label className="field"><span>{tx("名称")} *</span><input value={values.name} onChange={(event) => update("name", event.target.value)} required /></label>
          <label className="field"><span>{tx("账单来源")} *</span><select value={values.connector_id} onChange={(event) => update("connector_id", event.target.value)}>{connectors.map((connector) => <option key={connector.id} value={connector.id}>{connector.name}</option>)}</select></label>
          <label className="field"><span>{tx("对账粒度")}</span><select value={values.granularity} onChange={(event) => updateGranularity(event.target.value)}><option value="detail">{tx("明细")}</option><option value="hour">{tx("小时")}</option><option value="day">{tx("天")}</option><option value="month">{tx("月")}</option></select></label>
          <label className="field"><span>{tx("时区")}</span><input list="reconciliation-timezones" value={values.timezone} onChange={(event) => update("timezone", event.target.value)} required /><datalist id="reconciliation-timezones"><option value="UTC" /><option value="Asia/Shanghai" /><option value="Asia/Tokyo" /></datalist></label>
          <fieldset className="reconciliation-dimensions"><legend>{tx("匹配维度")}</legend>{dimensions.map((dimension) => { const required = dimension === "currency" || dimension === "request_id"; return <label key={dimension}><input checked={values.match_dimensions.includes(dimension)} disabled={required} onChange={() => toggleDimension(dimension)} type="checkbox" />{tx(reconciliationDimensionLabel(dimension))}</label>; })}</fieldset>
          <label className="field"><span>{tx("金额容差")}</span><input inputMode="decimal" value={values.amount_tolerance} onChange={(event) => update("amount_tolerance", event.target.value)} required /></label>
          <label className="field"><span>{tx("比例容差（0-1）")}</span><input inputMode="decimal" value={values.ratio_tolerance} onChange={(event) => update("ratio_tolerance", event.target.value)} required /></label>
          <label className="field"><span>{tx("明细时间窗口（分钟）")}</span><input min="0" max="1440" type="number" value={values.time_window_minutes} onChange={(event) => update("time_window_minutes", event.target.value)} /></label>
          <label className="field"><span>{tx("账单延迟（分钟）")}</span><input min="0" max="43200" type="number" value={values.billing_delay_minutes} onChange={(event) => update("billing_delay_minutes", event.target.value)} /></label>
          <label className="field"><span>{tx("定时周期（分钟，0 为手动）")}</span><input min="0" max="43200" type="number" value={values.schedule_interval_minutes} onChange={(event) => update("schedule_interval_minutes", event.target.value)} /></label>
          <label className="field"><span>{tx("币种")}</span><input maxLength={3} placeholder="USD" value={values.currency} onChange={(event) => update("currency", event.target.value.toUpperCase())} required /></label>
          <label className="field"><span>{tx("USD 汇率（1 USD = 目标币种）")}</span><input inputMode="decimal" value={values.usd_exchange_rate} onChange={(event) => update("usd_exchange_rate", event.target.value)} required /></label>
          <label className="field reconciliation-wide-field"><span>{tx("Provider 维度映射（JSON）")}</span><textarea rows={5} value={values.dimension_mappings} onChange={(event) => update("dimension_mappings", event.target.value)} /><small>{tx("外部值映射到 TokenHub 的 Provider、资源账号、模型或项目标识。")}</small></label>
          {error ? <div className="billing-inline-error reconciliation-wide-field" role="alert">{error}</div> : null}
        </div>
        <div className="modal-actions"><button className="button secondary" onClick={onClose} type="button">{tx("取消")}</button><button className="button" disabled={busy} type="submit">{busy ? tx("保存中...") : tx("保存")}</button></div>
      </form>
    </div>
  );
}

function ReconciliationRunDialog({ api, rule, onClose, onStarted }: { api: ApiContext; rule: ReconciliationRule; onClose: () => void; onStarted: (run: ReconciliationRun) => Promise<void> }) {
  const [from, setFrom] = useState(() => defaultReconciliationPeriod().from);
  const [to, setTo] = useState(() => defaultReconciliationPeriod().to);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const response = await adminFetch(api, `/api/admin/billing/reconciliation-rules/${encodeURIComponent(rule.id)}/run`, {
        method: "POST",
        body: JSON.stringify({ period_start: new Date(from).toISOString(), period_end: new Date(to).toISOString() }),
      });
      if (!response.ok) throw new Error(await readAdminError(response, tx("对账执行失败")));
      await onStarted((await response.json()) as ReconciliationRun);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : tx("操作失败"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="modal-backdrop" role="presentation">
      <form className="modal reconciliation-run-modal" onSubmit={submit}>
        <div className="modal-header"><div><p className="eyebrow">FinOps</p><h3>{tx("发起对账")}</h3><span>{rule.name}</span></div><button className="icon-button" onClick={onClose} type="button"><X size={18} /></button></div>
        <div className="modal-body reconciliation-run-form">
          <label className="field"><span>{tx("账期开始")}</span><input type="datetime-local" value={from} onChange={(event) => setFrom(event.target.value)} required /></label>
          <label className="field"><span>{tx("账期结束")}</span><input type="datetime-local" value={to} onChange={(event) => setTo(event.target.value)} required /></label>
          {error ? <div className="billing-inline-error reconciliation-wide-field" role="alert">{error}</div> : null}
        </div>
        <div className="modal-actions"><button className="button secondary" onClick={onClose} type="button">{tx("取消")}</button><button className="button" disabled={busy} type="submit">{busy ? tx("对账中...") : tx("执行对账")}</button></div>
      </form>
    </div>
  );
}

function reconciliationDimensionSummary(item: ReconciliationItem) {
  return [item.request_id, item.provider, item.resource_account, item.model, item.project].filter(Boolean).join(" · ") || "-";
}

function reconciliationGranularityLabel(value: string) {
  return tx(({ detail: "明细", hour: "小时", day: "天", month: "月" } as Record<string, string>)[value] || value);
}

function reconciliationDimensionLabel(value: string) {
  return ({ request_id: "请求 ID", provider: "Provider", resource_account: "资源账号", model: "模型", project: "项目", currency: "币种" } as Record<string, string>)[value] || value;
}

function reconciliationReasonLabel(value: string) {
  return tx(({
    within_tolerance: "在容差范围内", provider_amount_higher: "Provider 金额更高", tokenhub_amount_higher: "TokenHub 金额更高",
    missing_tokenhub_usage_or_late_data: "TokenHub 漏记或迟到数据", provider_bill_delayed_or_unmapped: "Provider 账单延迟或未映射",
    currency_mismatch_or_missing_fx: "币种不一致或缺少汇率", outside_time_window: "超出匹配时间窗口",
  } as Record<string, string>)[value] || value);
}

function formatReconciliationDate(value?: string) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(languageLocale(), { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(date);
}

function formatRatio(value: string) {
  const ratio = Number(value);
  return Number.isFinite(ratio) ? new Intl.NumberFormat(languageLocale(), { style: "percent", maximumFractionDigits: 2 }).format(ratio) : value;
}

function formatReconciliationNumber(value: number) {
  return new Intl.NumberFormat(languageLocale()).format(value);
}

function formatMinutes(value: number) {
  return new Intl.NumberFormat(languageLocale(), { style: "unit", unit: "minute", unitDisplay: "short" }).format(value);
}

function formatReconciliationMoney(value: string, currency: string) {
  const amount = Number(value);
  if (!Number.isFinite(amount) || currency.length !== 3) return `${currency} ${value}`.trim();
  return new Intl.NumberFormat(languageLocale(), {
    style: "currency", currency, currencyDisplay: "code", minimumFractionDigits: 0, maximumFractionDigits: 6,
  }).format(amount);
}

function shortHash(value: string) {
  return value ? `${value.slice(0, 15)}…` : "-";
}

function defaultReconciliationPeriod() {
  const now = new Date();
  const start = new Date(now.getFullYear(), now.getMonth(), 1, 0, 0, 0, 0);
  return { from: localDateTimeValue(start), to: localDateTimeValue(now) };
}

function localDateTimeValue(value: Date) {
  const shifted = new Date(value.getTime() - value.getTimezoneOffset() * 60_000);
  return shifted.toISOString().slice(0, 16);
}
