import { useEffect, useMemo, useState } from "react";
import type { ApiContext, ProviderResource } from "../core/types";
import { providerReasoningFieldConfigs, providerReasoningFormValues } from "../domain/provider-reasoning";
import { tx } from "../i18n/runtime";
import { adminFetch, isAuthExpiredError, providerResourceToForm, providerResourceUpdatePayload, readAdminError } from "../resources/payloads";
import { ProviderInlineField } from "./provider-editor-fields";

export function ProviderResourceReasoningSettings({
  api,
  providerID,
  resources,
  onSaved,
}: {
  api: ApiContext;
  providerID: string;
  resources: ProviderResource[];
  onSaved: () => Promise<void> | void;
}) {
  const scopedResources = useMemo(
    () => resources.filter((resource) => resource.provider_id === providerID),
    [providerID, resources],
  );
  const [drafts, setDrafts] = useState<Record<string, Record<string, string>>>({});
  const [busyID, setBusyID] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [savedID, setSavedID] = useState("");

  useEffect(() => {
    setDrafts((current) => Object.fromEntries(scopedResources.map((resource) => [
      resource.id,
      current[resource.id] ?? providerReasoningFormValues(resource.options),
    ])));
  }, [scopedResources]);

  function update(resourceID: string, key: string, value: string) {
    setDrafts((current) => ({
      ...current,
      [resourceID]: { ...current[resourceID], [key]: value },
    }));
    setErrors((current) => ({ ...current, [resourceID]: "" }));
    setSavedID("");
  }

  async function save(resource: ProviderResource) {
    const draft = drafts[resource.id] ?? providerReasoningFormValues(resource.options);
    setBusyID(resource.id);
    setSavedID("");
    setErrors((current) => ({ ...current, [resource.id]: "" }));
    try {
      const payload = providerResourceUpdatePayload({ ...providerResourceToForm(resource), ...draft });
      const response = await adminFetch(api, `/api/admin/provider-resources/${encodeURIComponent(resource.id)}`, {
        method: "PATCH",
        body: JSON.stringify(payload),
      });
      if (!response.ok) throw new Error(await readAdminError(response, tx("保存 Provider 资源推理兼容设置")));
      const updated = await response.json() as ProviderResource;
      setDrafts((current) => ({ ...current, [resource.id]: providerReasoningFormValues(updated.options) }));
      setSavedID(resource.id);
      await onSaved();
    } catch (error) {
      if (isAuthExpiredError(error)) return;
      setErrors((current) => ({ ...current, [resource.id]: error instanceof Error ? error.message : tx("保存失败") }));
    } finally {
      setBusyID("");
    }
  }

  if (scopedResources.length === 0) return null;
  return (
    <section className="provider-quota-panel">
      <div className="wizard-panel-head">
        <h3>{tx("Provider 资源推理兼容")}</h3>
        <p>{tx("资源级配置覆盖 Provider 配置，适合不同上游端点使用不同的推理参数规则。")}</p>
      </div>
      <div className="provider-quota-list">
        {scopedResources.map((resource) => {
          const values = drafts[resource.id] ?? providerReasoningFormValues(resource.options);
          return (
            <details className="provider-account-runtime" key={resource.id} data-resource-id={resource.id}>
              <summary>
                <strong>{resource.name}</strong>
                <span>{resource.resource_type} · {resource.base_url || tx("继承 Provider Base URL")}</span>
              </summary>
              <div className="provider-account-fields">
                {providerReasoningFieldConfigs().map((field) => (
                  <ProviderInlineField
                    field={field}
                    key={`${resource.id}-${field.key}`}
                    onChange={(value) => update(resource.id, field.key, value)}
                    value={values[field.key] ?? ""}
                    values={values}
                  />
                ))}
              </div>
              {errors[resource.id] ? <p className="provider-quota-error">{errors[resource.id]}</p> : null}
              {savedID === resource.id ? <p className="provider-credential-note">{tx("Provider 资源推理兼容设置已保存。")}</p> : null}
              <div className="provider-form-actions">
                <button className="secondary-button" disabled={busyID === resource.id} onClick={() => void save(resource)} type="button">
                  {tx(busyID === resource.id ? "保存中" : "保存资源设置")}
                </button>
              </div>
            </details>
          );
        })}
      </div>
    </section>
  );
}
