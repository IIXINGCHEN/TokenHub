import { Download, ExternalLink, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import packageJSON from "../../../package.json";
import { tx } from "../i18n/runtime";

const currentVersion = packageJSON.version;
const defaultUpdateCheckURL = "https://api.github.com/repos/astaxie/TokenHub/tags";
const defaultReleaseURL = "https://github.com/astaxie/TokenHub/releases";

type VersionCheckStatus = "checking" | "available" | "current" | "error";

type VersionCheckResult = {
  latestVersion: string;
  releaseURL: string;
  changelogURL: string;
  notes?: string;
  error?: string;
};

type SettingsResource = {
  id?: string;
  fields?: Record<string, unknown>;
};

export function VersionCheck({ baseURL, adminToken }: { baseURL: string; adminToken: string }) {
  const [open, setOpen] = useState(false);
  const [status, setStatus] = useState<VersionCheckStatus>("checking");
  const [result, setResult] = useState<VersionCheckResult>(() => ({
    latestVersion: currentVersion,
    releaseURL: defaultReleaseURL,
    changelogURL: defaultReleaseURL,
  }));
  const rootRef = useRef<HTMLDivElement | null>(null);

  const checkVersion = useCallback(async (signal?: AbortSignal) => {
    setStatus("checking");
    try {
      const settings = await loadVersionSettings(baseURL, adminToken, signal);
      const release = await loadLatestRelease(settings.updateCheckURL, settings.releaseURL, signal);
      const latestVersion = release.latestVersion || currentVersion;
      const releaseURL = release.releaseURL || settings.releaseURL || defaultReleaseURL;
      const changelogURL = release.changelogURL || releaseURL;
      setResult({
        latestVersion,
        releaseURL,
        changelogURL,
        notes: release.notes,
      });
      setStatus(compareVersions(latestVersion, currentVersion) > 0 ? "available" : "current");
    } catch (err) {
      if (signal?.aborted) return;
      setResult((current) => ({
        ...current,
        error: err instanceof Error ? err.message : tx("版本检测失败"),
      }));
      setStatus("error");
    }
  }, [adminToken, baseURL]);

  useEffect(() => {
    if (!adminToken) return;
    const controller = new AbortController();
    void checkVersion(controller.signal);
    return () => controller.abort();
  }, [adminToken, checkVersion]);

  useEffect(() => {
    if (!open) return;
    function onPointerDown(event: PointerEvent) {
      if (!rootRef.current?.contains(event.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [open]);

  const updateAvailable = status === "available";
  return (
    <div className="version-check" ref={rootRef}>
      <button
        aria-expanded={open}
        aria-label={tx("查看版本更新")}
        className={updateAvailable ? "version version-check-trigger has-update" : "version version-check-trigger"}
        onClick={() => setOpen((value) => !value)}
        title={tx("查看版本更新")}
        type="button"
      >
        <span>v{currentVersion}</span>
        {updateAvailable ? <span className="version-update-dot" aria-hidden="true" /> : null}
      </button>

      {open ? (
        <div className="version-popover" role="dialog" aria-label={tx("版本检测")}>
          <header className="version-popover-header">
            <strong>{tx("当前版本")}</strong>
            <button
              aria-label={tx("重新检测")}
              className="icon-button subtle"
              disabled={status === "checking"}
              onClick={() => void checkVersion()}
              title={tx("重新检测")}
              type="button"
            >
              <RefreshCw className={status === "checking" ? "spinning" : undefined} size={18} />
            </button>
          </header>
          <div className="version-popover-body">
            <strong className="version-current">v{currentVersion}</strong>
            {status !== "error" ? <span className="version-latest">{tx("最新版本")}: v{result.latestVersion}</span> : null}
            {status === "available" ? (
              <div className="version-update-card">
                <span className="version-update-icon">
                  <Download size={22} />
                </span>
                <div>
                  <strong>{tx("有新版本可用！")}</strong>
                  <span>v{result.latestVersion}</span>
                </div>
              </div>
            ) : status === "checking" ? (
              <p className="version-status-text">{tx("正在检测新版本...")}</p>
            ) : status === "error" ? (
              <p className="version-status-text error">{result.error || tx("版本检测失败")}</p>
            ) : (
              <p className="version-status-text">{tx("已是最新版本")}</p>
            )}
            <a className="version-primary-action" href={result.releaseURL || defaultReleaseURL} target="_blank" rel="noreferrer">
              <Download size={19} />
              <span>{updateAvailable ? tx("立即更新") : tx("打开发布页面")}</span>
            </a>
            <a className="version-secondary-link" href={result.changelogURL || result.releaseURL || defaultReleaseURL} target="_blank" rel="noreferrer">
              <span>{tx("查看更新日志")}</span>
              <ExternalLink size={15} />
            </a>
          </div>
        </div>
      ) : null}
    </div>
  );
}

async function loadVersionSettings(baseURL: string, adminToken: string, signal?: AbortSignal) {
  const fallback = { updateCheckURL: defaultUpdateCheckURL, releaseURL: defaultReleaseURL };
  if (!adminToken) return fallback;
  try {
    const resp = await fetch(`${baseURL.replace(/\/$/, "")}/api/admin/resources/settings`, {
      headers: { authorization: `Bearer ${adminToken}` },
      signal,
    });
    if (!resp.ok) return fallback;
    const payload = (await resp.json()) as { data?: SettingsResource[] };
    const resource = (payload.data ?? []).find((item) => item.id === "cfg_gateway") ?? payload.data?.[0];
    return {
      updateCheckURL: textValue(resource?.fields?.version_update_url) || fallback.updateCheckURL,
      releaseURL: textValue(resource?.fields?.version_release_url) || fallback.releaseURL,
    };
  } catch (err) {
    if (signal?.aborted) throw err;
    return fallback;
  }
}

async function loadLatestRelease(updateURL: string, fallbackReleaseURL: string, signal?: AbortSignal) {
  const resp = await fetch(updateURL, {
    headers: { accept: "application/json" },
    cache: "no-store",
    signal,
  });
  if (!resp.ok) throw new Error(`${tx("版本检测失败")}: HTTP ${resp.status}`);
  const payload = await resp.json() as Record<string, unknown> | Array<Record<string, unknown>>;
  const release = Array.isArray(payload) ? payload[0] : payload;
  if (!release) throw new Error(tx("版本检测失败"));
  const latestVersion = normalizeVersion(textValue(release.tag_name) || textValue(release.latest_version) || textValue(release.latestVersion) || textValue(release.version) || textValue(release.name));
  if (!latestVersion) throw new Error(tx("版本检测失败"));
  return {
    latestVersion,
    releaseURL: textValue(release.html_url) || textValue(release.release_url) || textValue(release.download_url) || fallbackReleaseURL,
    changelogURL: textValue(release.changelog_url) || textValue(release.html_url) || fallbackReleaseURL,
    notes: textValue(release.body) || textValue(release.notes),
  };
}

function compareVersions(candidate: string, baseline: string) {
  const left = versionParts(candidate);
  const right = versionParts(baseline);
  for (let index = 0; index < Math.max(left.length, right.length); index += 1) {
    const delta = (left[index] ?? 0) - (right[index] ?? 0);
    if (delta !== 0) return delta;
  }
  return 0;
}

function versionParts(value: string) {
  return normalizeVersion(value)
    .split(/[.-]/)
    .map((part) => Number.parseInt(part, 10))
    .filter((part) => Number.isFinite(part));
}

function normalizeVersion(value: string) {
  return value.trim().replace(/^v/i, "");
}

function textValue(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}
