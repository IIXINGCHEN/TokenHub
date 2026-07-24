import { Plus, Search, Trash2, UserRoundCheck, X } from "lucide-react";
import { useEffect, useState } from "react";
import { type AdminResource, type AdminUser, type AppData, type Project, type Provider, type ProviderResource, type ReportExportHistoryItem, type RequestLog, type ResourceAction, type ResourceConfig, type ToolbarAction } from "../core/types";
import { notificationChannelLabel } from "../domain/catalog";
import { projectMembersForProject, providerRoutesFor, stringifyValue } from "../domain/entities";
import { activeRouteCount, formatNumber, formatTime } from "../domain/formatting";
import { approvalTriggerLabel, enumValueLabel, providerTypeLabel, reportDatasetLabel, roleLabel } from "../domain/labels";
import { countWithUnit, languageLocale, tx } from "../i18n/runtime";
import { reportExportDefinitions } from "../resources/governance-config";
import { pendingProjectQuotaApproval, projectQuotaIssue, projectQuotaPolicy, projectQuotaValues, requestProjectQuotaIncrease, saveProjectQuota } from "../resources/payloads";
import { DataSection, SimpleTable, StatusPill } from "../shared/ui";
import { ModelCategoryTabs, NotificationChannelTabs } from "./model-catalog";
import { latencyDisplay, requestLogFailed } from "./overview";
import { APIKeyFlowHint, EntityTable, PaginationControls, type PaginationState, resultCountLabel, RouteStrategyHint } from "./settings-table";

export function CrudView<T>({
  config,
  data,
  items,
  monitorItems = items,
  totalItems,
  loading = false,
  query,
  pagination,
  categoryFilter,
  onCategoryFilter,
  onQuery,
  onCreate,
  onEdit,
  onDelete,
  onAction,
  onProjectMemberCreate,
  onProjectMemberEdit,
  onProjectMemberDelete,
  onToolbarAction,
  currentUser = null,
}: {
  config: ResourceConfig<T>;
  data: AppData;
  items: T[];
  monitorItems?: T[];
  totalItems: number;
  loading?: boolean;
  query: string;
  pagination: PaginationState;
  categoryFilter: string;
  onCategoryFilter: (value: string) => void;
  onQuery: (value: string) => void;
  onCreate: () => void;
  onEdit: (item: T) => void;
  onDelete: (item: T) => void;
  onAction: (action: ResourceAction<T>, item: T) => void;
  onProjectMemberCreate?: (project: Project) => void;
  onProjectMemberEdit?: (member: AdminResource) => void;
  onProjectMemberDelete?: (member: AdminResource) => void;
  onToolbarAction: (action: ToolbarAction) => void;
  currentUser?: AdminUser | null;
}) {
  const [selectedTeamID, setSelectedTeamID] = useState("");
  const [selectedProjectID, setSelectedProjectID] = useState("");
  const isTeamView = config.view === "teams";
  const isProjectView = config.view === "projects";
  const selectedTeam = isTeamView
    ? (items as AdminResource[]).find((item) => item.id === selectedTeamID)
    : undefined;
  const selectedProject = isProjectView
    ? (items as Project[]).find((item) => item.id === selectedProjectID)
    : undefined;

  useEffect(() => {
    if (!isTeamView) return;
    const teamItems = items as AdminResource[];
    if (!selectedTeamID || !teamItems.some((item) => item.id === selectedTeamID)) {
      setSelectedTeamID("");
    }
  }, [isTeamView, items, selectedTeamID]);

  useEffect(() => {
    if (!isProjectView) return;
    const projectItems = items as Project[];
    if (!selectedProjectID || !projectItems.some((item) => item.id === selectedProjectID)) {
      setSelectedProjectID("");
    }
  }, [isProjectView, items, selectedProjectID]);

  const detailPanelOpen = (isTeamView && selectedTeam) || (isProjectView && selectedProject);

  return (
    <DataSection title={config.eyebrow}>
      {config.view === "api-keys" ? <APIKeyFlowHint data={data} /> : null}
      {config.view === "routes" ? <RouteStrategyHint data={data} /> : null}
      {config.view === "providers" || config.view === "models" ? (
        <ModelCategoryTabs
          data={data}
          view={config.view}
          active={categoryFilter}
          onChange={onCategoryFilter}
        />
      ) : null}
      {config.view === "providers" ? <ProviderAvailabilityMonitor data={data} providers={monitorItems as Provider[]} /> : null}
      {config.view === "notification-channels" ? (
        <NotificationChannelTabs
          data={data}
          active={categoryFilter}
          onChange={onCategoryFilter}
        />
      ) : null}
      <div className="table-toolbar">
        <div className="search-box">
          <Search size={16} />
          <input value={query} onChange={(event) => onQuery(event.target.value)} placeholder={tx("搜索名称、ID、状态")} />
        </div>
        <div className="table-toolbar-actions">
          <span className="table-result-count">{resultCountLabel(totalItems, query)}</span>
          {config.create ? (
            <button className="button" onClick={onCreate} type="button">
              <Plus size={17} />
              {config.view === "notification-channels" ? `${tx("配置")} ${notificationChannelLabel(categoryFilter)}` : tx(config.createLabel ?? "新增")}
            </button>
          ) : null}
          {(config.toolbarActions ?? []).map((action) => (
            <button className="secondary-button" key={action.label} onClick={() => onToolbarAction(action)} title={tx(action.title ?? action.label)} type="button">
              {tx(action.label)}
            </button>
          ))}
        </div>
      </div>
      <div className={detailPanelOpen ? "resource-detail-layout with-panel" : "resource-detail-layout"}>
        <div className="resource-table-pane">
          <EntityTable
            config={config}
            data={data}
            items={items}
            loading={loading}
            query={query}
            currentUser={currentUser}
            onCreate={config.create ? onCreate : undefined}
            onEdit={onEdit}
            onDelete={onDelete}
            onAction={onAction}
            onRowClick={
              isTeamView
                ? (item) => setSelectedTeamID((item as AdminResource).id)
                : isProjectView
                  ? (item) => setSelectedProjectID((item as Project).id)
                  : undefined
            }
            selectedRowID={isTeamView ? selectedTeam?.id : isProjectView ? selectedProject?.id : undefined}
          />
          <PaginationControls pagination={pagination} totalItems={totalItems} />
        </div>
        {isTeamView && selectedTeam ? (
          <TeamMembersPanel data={data} team={selectedTeam} onClose={() => setSelectedTeamID("")} />
        ) : null}
        {isProjectView && selectedProject ? (
          <ProjectQuotaPanel
            data={data}
            project={selectedProject}
            onClose={() => setSelectedProjectID("")}
            onAction={(action) => onAction(action as unknown as ResourceAction<T>, selectedProject as T)}
            onCreateMember={() => onProjectMemberCreate?.(selectedProject)}
            onEditMember={(member) => onProjectMemberEdit?.(member)}
            onDeleteMember={(member) => onProjectMemberDelete?.(member)}
          />
        ) : null}
      </div>
    </DataSection>
  );
}

export type ProviderMonitorTone = "healthy" | "degraded" | "down";

export type ProviderProbeTone = "ok" | "warn" | "down" | "na";

export type ProviderTrendTone = "success" | "warning" | "failure" | "none";

export type ProviderMonitorRow = {
  provider: Provider;
  resources: ProviderResource[];
  routeCount: number;
  activeRouteCount: number;
  statusTone: ProviderMonitorTone;
  statusLabel: string;
  statusDetail: string;
  basicPrimaryTone: ProviderProbeTone;
  basicPrimaryDetail: string;
  basicSecondaryTone: ProviderProbeTone;
  basicSecondaryDetail: string;
  realTone: ProviderProbeTone;
  realDetail: string;
  latencyMS: number;
  availability24h: number;
  observed24h: boolean;
  qualityScore: number;
  trend: ProviderTrendTone[];
};

export function ProviderAvailabilityMonitor({ data, providers }: { data: AppData; providers: Provider[] }) {
  if (providers.length === 0) return null;
  const rows = providerMonitorRows(data, providers);
  const summary = providerMonitorSummary(rows);
  return (
    <section className="provider-monitor-card" aria-label={tx("Provider 可用性监控")}>
      <div className="provider-monitor-head">
        <div>
          <p className="eyebrow">Provider Availability</p>
          <h2>{tx("Provider 可用性监控")}</h2>
          <span>{tx("按健康检测、账号资源和真实请求日志汇总上游渠道可用性。")}</span>
        </div>
        <div className="provider-monitor-summary" aria-label={tx("Provider 健康摘要")}>
          <span><strong>{summary.healthy}</strong>{tx("正常")}</span>
          <span><strong>{summary.degraded}</strong>{tx("降级")}</span>
          <span><strong>{summary.down}</strong>{tx("故障")}</span>
        </div>
      </div>
      <div className="provider-monitor-table-wrap">
        <table className="provider-monitor-table">
          <thead>
            <tr>
              <th>{tx("服务商 / 通道")}</th>
              <th>{tx("综合状态")}</th>
              <th>{tx("基础监控 · L1/L2")}</th>
              <th>{tx("真实监控 · L3")}</th>
              <th>{tx("真实延迟")}</th>
              <th>{tx("24H 可用率")}</th>
              <th>{tx("质量评分")}</th>
              <th>{tx("近30天趋势")}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.provider.id}>
                <td>
                  <div className="provider-monitor-name">
                    <span className={`provider-monitor-avatar ${row.statusTone}`}>{providerInitial(row.provider)}</span>
                    <div>
                      <strong>{row.provider.name || row.provider.id}</strong>
                      <span>{providerTypeLabel(row.provider.type)} · {row.activeRouteCount}/{row.routeCount || 0} {tx("启用路由")} · {row.resources.length || 0} {tx("账号资源")}</span>
                    </div>
                  </div>
                </td>
                <td>
                  <div className="provider-monitor-status-cell">
                    <span className={`provider-monitor-status ${row.statusTone}`}>
                      <i />
                      {tx(row.statusLabel)}
                    </span>
                    <small>{row.statusDetail}</small>
                  </div>
                </td>
                <td>
                  <ProviderProbeLine tone={row.basicPrimaryTone} detail={row.basicPrimaryDetail} />
                  <ProviderProbeLine tone={row.basicSecondaryTone} detail={row.basicSecondaryDetail} />
                </td>
                <td>
                  <ProviderProbeLine tone={row.realTone} detail={row.realDetail} />
                  <small className="provider-monitor-subtle">{row.observed24h ? tx("真实请求样本") : tx("无请求样本")}</small>
                </td>
                <td><strong className="provider-monitor-metric">{latencyDisplay(row.latencyMS)}</strong></td>
                <td><strong className="provider-monitor-metric">{providerPercent(row.availability24h)}</strong></td>
                <td>
                  <div className="provider-quality-score">
                    <strong>{row.qualityScore}</strong>
                    <span><i style={{ width: `${row.qualityScore}%` }} /></span>
                  </div>
                </td>
                <td>
                  <div className="provider-trend-bars" aria-label={tx("近30天趋势")}>
                    {row.trend.map((tone, index) => <span className={tone} key={`${row.provider.id}-trend-${index}`} />)}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="provider-monitor-legend">
        <span><i className="success" />{tx("正常")}</span>
        <span><i className="warning" />{tx("降级/慢响应")}</span>
        <span><i className="failure" />{tx("故障")}</span>
        <em>{tx("真实监控来自最近请求日志；基础监控来自 Provider 和账号资源健康状态。")}</em>
      </div>
    </section>
  );
}

export function ProviderProbeLine({ tone, detail }: { tone: ProviderProbeTone; detail: string }) {
  return (
    <span className={`provider-probe-line ${tone}`}>
      <i />
      {tx(providerProbeLabel(tone))}
      <small>{detail}</small>
    </span>
  );
}

export function providerMonitorRows(data: AppData, providers: Provider[]): ProviderMonitorRow[] {
  return providers
    .slice()
    .sort((left, right) => (left.priority - right.priority) || left.name.localeCompare(right.name))
    .map((provider) => providerMonitorRow(data, provider));
}

export function providerMonitorRow(data: AppData, provider: Provider): ProviderMonitorRow {
  const resources = data.providerResources.filter((resource) => resource.provider_id === provider.id);
  const routes = providerRoutesFor(provider, data);
  const logs = providerLogsFor(data, provider, resources);
  const now = Date.now();
  const recent24h = logs.filter((log) => now - safeTime(log.created_at) <= 24 * 60 * 60 * 1000);
  const success24h = recent24h.filter((log) => !requestLogFailed(log));
  const warning24h = recent24h.filter((log) => !requestLogFailed(log) && (log.status_code >= 300 || log.latency_ms >= 5000));
  const failed24h = recent24h.length - success24h.length;
  const observed24h = recent24h.length > 0;
  const activeResources = resources.filter((resource) => resource.status === "active");
  const healthyResources = activeResources.filter((resource) => resource.healthy);
  const healthyProvider = provider.status === "active" && provider.healthy;
  const resourceScore = activeResources.length > 0 ? (healthyResources.length / activeResources.length) * 100 : (healthyProvider ? 100 : 0);
  const availability24h = observed24h ? (success24h.length / recent24h.length) * 100 : (healthyProvider ? 100 : 0);
  const latencyLogs = (success24h.length ? success24h : logs.filter((log) => !requestLogFailed(log))).filter((log) => log.latency_ms > 0);
  const latencyMS = percentileLatency(latencyLogs, 0.5);
  const statusTone = providerMonitorTone(provider, observed24h, availability24h, warning24h.length, failed24h, activeResources.length, healthyResources.length);
  const activeRouteCount = routes.filter((route) => route.status === "active").length;
  return {
    provider,
    resources,
    routeCount: routes.length,
    activeRouteCount,
    statusTone,
    statusLabel: providerStatusLabel(statusTone),
    statusDetail: providerStatusDetail(provider, logs, resources),
    basicPrimaryTone: healthyProvider ? "ok" : "down",
    basicPrimaryDetail: provider.status === "active" ? tx("Provider 在线") : enumValueLabel(provider.status),
    basicSecondaryTone: providerResourceProbeTone(activeResources.length, healthyResources.length),
    basicSecondaryDetail: activeResources.length > 0
      ? `${formatNumber(healthyResources.length)}/${formatNumber(activeResources.length)} ${tx("资源健康")}`
      : tx("未配置账号资源"),
    realTone: providerRealProbeTone(observed24h, availability24h, warning24h.length, failed24h),
    realDetail: observed24h
      ? `${providerPercent(availability24h)} · ${formatNumber(recent24h.length)} ${tx("次请求")}`
      : tx("无真实请求"),
    latencyMS,
    availability24h,
    observed24h,
    qualityScore: providerQualityScore(availability24h, latencyMS, resourceScore, observed24h, healthyProvider),
    trend: providerTrend(data, provider, resources),
  };
}

export function providerMonitorSummary(rows: ProviderMonitorRow[]) {
  return rows.reduce(
    (summary, row) => {
      summary[row.statusTone] += 1;
      return summary;
    },
    { healthy: 0, degraded: 0, down: 0 } as Record<ProviderMonitorTone, number>,
  );
}

export function providerLogsFor(data: AppData, provider: Provider, resources: ProviderResource[]) {
  const resourceIDs = new Set(resources.map((resource) => resource.id));
  return data.logs
    .filter((log) => log.provider_id === provider.id || (log.provider_resource_id ? resourceIDs.has(log.provider_resource_id) : false))
    .sort((left, right) => safeTime(left.created_at) - safeTime(right.created_at));
}

export function providerMonitorTone(provider: Provider, observed: boolean, availability: number, warnings: number, failures: number, activeResources: number, healthyResources: number): ProviderMonitorTone {
  if (provider.status !== "active" || !provider.healthy) return "down";
  if (observed && (availability < 90 || failures > 0 && availability < 95)) return "down";
  if (activeResources > 0 && healthyResources === 0) return "down";
  if ((observed && availability < 99) || warnings > 0 || (activeResources > 0 && healthyResources < activeResources)) return "degraded";
  return "healthy";
}

export function providerStatusLabel(tone: ProviderMonitorTone) {
  if (tone === "healthy") return "Healthy";
  if (tone === "degraded") return "Degraded";
  return "Functional Down";
}

export function providerStatusDetail(provider: Provider, logs: RequestLog[], resources: ProviderResource[]) {
  const latestLog = logs.slice().sort((left, right) => safeTime(right.created_at) - safeTime(left.created_at))[0];
  if (latestLog?.error_code) return `${timeLabel(latestLog.created_at)} · ${latestLog.error_code}`;
  if (latestLog) return timeLabel(latestLog.created_at);
  const latestResourceCheck = resources
    .map((resource) => resource.last_checked_at || resource.updated_at || "")
    .filter(Boolean)
    .sort((left, right) => safeTime(right) - safeTime(left))[0];
  if (latestResourceCheck) return timeLabel(latestResourceCheck);
  return enumValueLabel(provider.status);
}

export function providerResourceProbeTone(total: number, healthy: number): ProviderProbeTone {
  if (total === 0) return "na";
  if (healthy === total) return "ok";
  if (healthy > 0) return "warn";
  return "down";
}

export function providerRealProbeTone(observed: boolean, availability: number, warnings: number, failures: number): ProviderProbeTone {
  if (!observed) return "na";
  if (availability < 90 || failures > 0 && availability < 95) return "down";
  if (availability < 99 || warnings > 0 || failures > 0) return "warn";
  return "ok";
}

export function providerProbeLabel(tone: ProviderProbeTone) {
  if (tone === "ok") return "ok";
  if (tone === "warn") return "warn";
  if (tone === "down") return "down";
  return "na";
}

export function percentileLatency(logs: RequestLog[], percentile: number) {
  const values = logs.map((log) => log.latency_ms || 0).filter((value) => value > 0).sort((left, right) => left - right);
  if (values.length === 0) return 0;
  const index = Math.min(values.length - 1, Math.max(0, Math.floor((values.length - 1) * percentile)));
  return values[index];
}

export function providerQualityScore(availability: number, latencyMS: number, resourceScore: number, observed: boolean, healthyProvider: boolean) {
  const availabilityScore = observed ? availability : (healthyProvider ? 95 : 20);
  const latencyScore = latencyMS === 0
    ? (healthyProvider ? 86 : 25)
    : latencyMS <= 250
      ? 100
      : latencyMS <= 800
        ? 94
        : latencyMS <= 1800
          ? 84
          : latencyMS <= 3500
            ? 68
            : latencyMS <= 6000
              ? 48
              : 30;
  return Math.round(clampNumber(availabilityScore * 0.62 + latencyScore * 0.24 + resourceScore * 0.14, 0, 100));
}

export function providerTrend(data: AppData, provider: Provider, resources: ProviderResource[]) {
  const logs = providerLogsFor(data, provider, resources);
  const days = 30;
  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  return Array.from({ length: days }, (_, index) => {
    const dayStart = today - (days - 1 - index) * 24 * 60 * 60 * 1000;
    const dayEnd = dayStart + 24 * 60 * 60 * 1000;
    const dayLogs = logs.filter((log) => {
      const time = safeTime(log.created_at);
      return time >= dayStart && time < dayEnd;
    });
    if (dayLogs.length === 0) return "none" as ProviderTrendTone;
    const failures = dayLogs.filter((log) => requestLogFailed(log)).length;
    const slow = dayLogs.filter((log) => !requestLogFailed(log) && log.latency_ms >= 5000).length;
    const availability = ((dayLogs.length - failures) / dayLogs.length) * 100;
    if (availability < 90) return "failure" as ProviderTrendTone;
    if (failures > 0 || slow > 0 || availability < 99) return "warning" as ProviderTrendTone;
    return "success" as ProviderTrendTone;
  });
}

export function providerInitial(provider: Provider) {
  return (provider.name || provider.type || provider.id || "P").trim().slice(0, 1).toUpperCase();
}

export function providerPercent(value: number) {
  return `${clampNumber(value, 0, 100).toFixed(1)}%`;
}

export function safeTime(value: string | undefined) {
  if (!value) return 0;
  const time = new Date(value).getTime();
  return Number.isFinite(time) ? time : 0;
}

export function timeLabel(value: string | undefined) {
  const time = safeTime(value);
  if (!time) return "-";
  return new Intl.DateTimeFormat(languageLocale(), { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(new Date(time));
}

export function clampNumber(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, Number.isFinite(value) ? value : min));
}

export function TeamMembersPanel({ data, team, onClose }: { data: AppData; team: AdminResource; onClose: () => void }) {
  const users = data.users
    .filter((user) => user.team_id === team.id)
    .sort((left, right) => (left.name || left.username).localeCompare(right.name || right.username));
  return (
    <div className="team-members-panel">
      <div className="team-members-head">
        <div>
          <span>{tx("团队用户")}</span>
          <strong>{team.name || team.id}</strong>
        </div>
        <span>{countWithUnit(users.length, "人", "member", "人")}</span>
        <button className="icon-button subtle" onClick={onClose} type="button" title={tx("关闭成员列表")}>
          <X size={15} />
        </button>
      </div>
      <SimpleTable
        columns={["姓名", "邮箱", "用户名", "角色", "状态", "最近登录"]}
        rows={users.map((user) => [
          user.name || "-",
          user.email || "-",
          user.username || "-",
          roleLabel(user.role),
          <StatusPill key={user.id} status={user.status} />,
          formatTime(user.last_login_at ?? ""),
        ])}
      />
    </div>
  );
}

export type ProjectQuotaValues = {
  status: string;
  daily_requests: string;
  monthly_requests: string;
  daily_tokens: string;
  monthly_tokens: string;
  daily_cost_usd: string;
  monthly_cost_usd: string;
  max_concurrency: string;
};

export const projectQuotaFields: Array<{ key: keyof ProjectQuotaValues; label: string; suffix?: string }> = [
  { key: "daily_requests", label: "日请求" },
  { key: "monthly_requests", label: "月请求" },
  { key: "daily_tokens", label: "日 Token" },
  { key: "monthly_tokens", label: "月 Token" },
  { key: "daily_cost_usd", label: "日成本", suffix: "USD" },
  { key: "monthly_cost_usd", label: "月成本", suffix: "USD" },
  { key: "max_concurrency", label: "最大并发" },
];

export function ProjectQuotaPanel({
  data,
  project,
  onClose,
  onAction,
  onCreateMember,
  onEditMember,
  onDeleteMember,
}: {
  data: AppData;
  project: Project;
  onClose: () => void;
  onAction: (action: ResourceAction<Project>) => void;
  onCreateMember?: () => void;
  onEditMember?: (member: AdminResource) => void;
  onDeleteMember?: (member: AdminResource) => void;
}) {
  const quota = projectQuotaPolicy(data, project);
  const [values, setValues] = useState<ProjectQuotaValues>(() => projectQuotaValues(quota));

  useEffect(() => {
    setValues(projectQuotaValues(quota));
  }, [project.id, quota?.id]);

  const hasQuota = Boolean(quota);
  const quotaIssue = projectQuotaIssue(data, project);
  const pendingApproval = pendingProjectQuotaApproval(data, project);
  const members = projectMembersForProject(data, project.id);
  return (
    <div className="project-quota-panel project-detail-panel">
      <div className="project-quota-head">
        <div>
          <span>{tx("项目详情")}</span>
          <strong>{project.name || project.id}</strong>
        </div>
        <button className="icon-button subtle" onClick={onClose} type="button" title={tx("关闭项目详情")}>
          <X size={15} />
        </button>
      </div>
      <div className="project-quota-body">
        <div className="project-panel-section-head">
          <div>
            <strong>{tx("项目成员")}</strong>
            <span>{countWithUnit(members.length, "人", "member", "人")}</span>
          </div>
          <button className="secondary-button compact-button" onClick={onCreateMember} type="button">
            <Plus size={15} />
            {tx("添加成员")}
          </button>
        </div>
        <div className="project-member-list">
          {members.length === 0 ? (
            <div className="empty compact-empty">{tx("暂无项目成员")}</div>
          ) : members.map((member) => (
            <ProjectMemberRow
              key={member.id}
              data={data}
              member={member}
              onEdit={() => onEditMember?.(member)}
              onDelete={() => onDeleteMember?.(member)}
            />
          ))}
        </div>

        <div className="project-panel-section-head">
          <div>
            <strong>{tx("项目额度")}</strong>
            <span>{hasQuota ? tx("已配置项目专属额度") : tx("未配置项目专属额度")}</span>
          </div>
        </div>
        <div className="quota-status-row">
          <div>
            <strong>{hasQuota ? tx("已配置项目专属额度") : tx("未配置项目专属额度")}</strong>
            <span>{tx("留空或填 0 表示该项不限额；Key 自身额度仍会叠加生效。")}</span>
          </div>
          <StatusPill status={values.status || "active"} />
        </div>

        {quotaIssue || pendingApproval ? (
          <div className="quota-request-banner">
            <div>
              <strong>{pendingApproval ? tx("已有额度提升申请待审批") : tx("最近触发了项目额度限制")}</strong>
              <span>
                {pendingApproval
                  ? `${approvalTriggerLabel(pendingApproval.trigger)} ${pendingApproval.id}，${tx("可在审批记录中处理。")}`
                  : `${formatNumber(quotaIssue?.count ?? 0)} ${tx("次额度不足，请填写希望提升后的目标额度再提交审批。")}`}
              </span>
            </div>
            {pendingApproval ? <StatusPill status="pending" label="待审批" /> : <StatusPill status="warning" label="需提升" />}
          </div>
        ) : null}

        <label className="field">
          <span>{tx("状态")}</span>
          <select value={values.status} onChange={(event) => setValues((current) => ({ ...current, status: event.target.value }))}>
            <option value="active">{tx("启用")}</option>
            <option value="disabled">{tx("停用")}</option>
          </select>
        </label>

        <div className="project-quota-grid">
          {projectQuotaFields.map((field) => (
            <label className="field" key={field.key}>
              <span>{tx(field.label)}</span>
              <input
                min="0"
                type="number"
                value={values[field.key]}
                onChange={(event) => setValues((current) => ({ ...current, [field.key]: event.target.value }))}
              />
              {field.suffix ? <small>{field.suffix}</small> : null}
            </label>
          ))}
        </div>

        <div className="project-quota-actions">
          {quotaIssue && !pendingApproval ? (
            <button
              className="secondary-button"
              onClick={() =>
                onAction({
                  label: "提升额度申请",
                  title: "提交项目额度提升审批",
                  run: (ctx) => requestProjectQuotaIncrease(ctx, project, quota, values),
                  doneMessage: () => `${project.name || project.id} 的额度提升申请已提交`,
                })
              }
              type="button"
            >
              {tx("提升额度申请")}
            </button>
          ) : null}
          <button
            className="button"
            onClick={() =>
              onAction({
                label: "保存额度",
                title: "保存项目额度",
                run: (ctx) => saveProjectQuota(ctx, project, quota, values),
                doneMessage: () => `${project.name || project.id} 的额度已保存`,
              })
            }
            type="button"
          >
            {tx("保存额度")}
          </button>
        </div>
      </div>
    </div>
  );
}

export function ProjectMemberRow({
  data,
  member,
  onEdit,
  onDelete,
}: {
  data: AppData;
  member: AdminResource;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const userID = stringifyValue(member.fields?.user_id);
  const user = data.users.find((item) => item.id === userID);
  const title = user ? user.name || user.username : userID || "-";
  const subtitle = user ? [user.email, user.username].filter(Boolean).join(" / ") : userID;
  return (
    <div className="project-member-row">
      <div className="project-member-user">
        <span className="project-member-avatar"><UserRoundCheck size={16} /></span>
        <div>
          <strong>{title}</strong>
          <span>{subtitle || "-"}</span>
        </div>
      </div>
      <div className="project-member-actions">
        <button className="text-button" onClick={onEdit} type="button">{tx("编辑")}</button>
        <button className="danger-button" onClick={onDelete} type="button" title={tx("删除")}>
          <Trash2 size={15} />
        </button>
      </div>
    </div>
  );
}

export function ReportsView({
  config,
  data,
  history,
  loading,
  onCreate,
  onEdit,
  onDelete,
  onAction,
  onExport,
}: {
  config: ResourceConfig<AdminResource>;
  data: AppData;
  history: ReportExportHistoryItem[];
  loading: boolean;
  onCreate: () => void;
  onEdit: (item: AdminResource) => void;
  onDelete: (item: AdminResource) => void;
  onAction: (action: ResourceAction<AdminResource>, item: AdminResource) => void;
  onExport: (dataset: string) => void;
}) {
  const savedReports = config.list(data);
  const exports = reportExportDefinitions();
  return (
    <div className="reports-center">
      <div className="reports-export-head">
        <div>
          <h2>{tx("按需导出")}</h2>
          <span>CSV</span>
        </div>
      </div>
      <div className="reports-export-grid">
        {exports.map((item) => {
          const Icon = item.icon;
          return (
            <button
              className={`report-export-card ${item.tone}`}
              disabled={loading}
              key={item.dataset}
              onClick={() => onExport(item.dataset)}
              title={`${tx("导出")} ${item.label}`}
              type="button"
            >
              <span className="report-export-icon">
                <Icon size={18} />
              </span>
              <span className="report-export-copy">
                <strong>{item.label}</strong>
                <span>{tx(item.description)}</span>
              </span>
              <em>CSV</em>
            </button>
          );
        })}
      </div>

      {history.length > 0 ? (
        <DataSection title="最近导出">
          <SimpleTable
            columns={["数据集", "文件", "时间", "账期"]}
            rows={history.map((item) => [
              reportDatasetLabel(item.dataset),
              item.file_name,
              formatTime(item.exported_at),
              item.period || "-",
            ])}
          />
        </DataSection>
      ) : null}

      {savedReports.length > 0 ? (
        <DataSection title="自动导出配置">
          <div className="reports-config-toolbar">
            <button className="button" onClick={onCreate} type="button">
              <Plus size={16} />
              {tx("新增配置")}
            </button>
          </div>
          <EntityTable
            config={config}
            data={data}
            items={savedReports}
            onEdit={onEdit}
            onDelete={onDelete}
            onAction={onAction}
          />
        </DataSection>
      ) : null}
    </div>
  );
}
