import { useState } from "react";
import type { ApiContext, ProviderResource } from "../core/types";
import { tx } from "../i18n/runtime";
import { adminFetch, isAuthExpiredError, readAdminError } from "../resources/payloads";

export function ProviderAccountImageTest({ api, resource, onTested }: { api: ApiContext; resource: ProviderResource; onTested: () => Promise<void> }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function testImageGeneration() {
    setBusy(true);
    setError("");
    try {
      const response = await adminFetch(api, `/api/admin/provider-resources/${encodeURIComponent(resource.id)}/image-test`, { method: "POST" });
      if (!response.ok) throw new Error(await readAdminError(response, tx("生图测试失败")));
    } catch (reason) {
      if (!isAuthExpiredError(reason)) setError(reason instanceof Error ? reason.message : tx("生图测试失败"));
    } finally {
      try {
        await onTested();
      } catch (reason) {
        if (!isAuthExpiredError(reason)) setError(reason instanceof Error ? reason.message : tx("生图测试失败"));
      }
      setBusy(false);
    }
  }

  return <>
    <button className="secondary-button" disabled={busy || resource.status !== "active"} onClick={() => void testImageGeneration()} type="button">
      {tx(busy ? "生图测试中" : "生图测试")}
    </button>
    {error ? <p aria-live="polite" className="provider-quota-error" role="alert">{error}</p> : null}
  </>;
}
