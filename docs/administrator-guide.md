# Administrator Guide

Language: English | [简体中文](zh-CN/administrator-guide.md) | [日本語](ja/administrator-guide.md)

This guide is for platform administrators, security operators, and infrastructure owners who run TokenHub as an enterprise AI gateway.

## Administrator Scope

| Area | Responsibility |
| --- | --- |
| Provider Channels | Configure upstream connections, import model inventory, and maintain actual Provider costs |
| Model Directory | Create external API models, choose initial Provider routes, and set unified client-facing prices |
| Routing Policies | Fine-tune Provider mappings, priority, weight, project scope, and failover strategy |
| Projects and Teams | Define ownership boundaries for keys, quota, and cost attribution |
| Identity Sources | Configure OAuth or OIDC login providers for enterprise sign-in |
| Security and Audit | Review request logs, admin events, key rotation, and policy changes |

## Production Setup Order

1. Configure at least one identity source and keep a controlled administrator account.
2. Add an upstream Provider such as `OpenAI Production`, `Azure East US`, or `Internal Model Gateway`, and import the upstream models that it can serve.
3. Record each imported Provider model's actual input, cache-read, and output costs for audit.
4. Create the external models to expose to applications and set their unified client-facing prices.
5. Add routes from each external model to one or more imported Provider models.
6. Create teams, projects, cost centers, and default quota policies.
7. Validate the flow with Model Playground and request logs.
8. Review usage attribution before issuing keys broadly.

## API Key Ownership and Usage Attribution

When issuing an API Key, select the actual user in **Owner User**. The issuer remains in audit metadata, but the Key's usage is attributed to its owner. Platform administrators may select any active user; team leaders may select an active user in their own team; ordinary users can only assign Keys to themselves.

Each new usage record snapshots the attributed user, so later ownership changes or Key deletion do not rewrite that recorded history. Records created before this field existed fall back to the Key's current owner, then its legacy issuer, then the project owner, and finally `unknown`. The individual ranking shows distinct used Keys and currently owned non-revoked Keys separately.

## Provider Catalog Availability

TokenHub stores the last known-good provider catalog in the database. On every backend startup, it validates and loads the configured local `provider-catalog.json`, then atomically replaces the database snapshot. Ordinary **Provider Channels** requests only read the database snapshot, and administrators can manually refresh the same local catalog. If local catalog reading, parsing, or completeness validation fails, TokenHub keeps using the last known-good snapshot.

## Provider Inventory, Model Directory, and Publication

TokenHub separates the model lifecycle into three control areas:

| Control area | Meaning |
| --- | --- |
| **Provider Channels** | Upstream connections and their imported model inventory. Creating a catalog-based Provider requires selecting at least one model, but importing inventory alone never exposes it to clients. Custom Providers can be created empty and populated after the upstream connection is available. |
| **Model Directory** | Only the external models that form the API contract for applications. When creating one, select zero or more available imported Provider models to create its initial routes. Its prices are the unified client-facing prices, independent of which Provider route serves a request. |
| **Routing Policies** | Manage an external model's Provider mappings and fine-tune priority, weight, project scope, traffic allocation, and failover strategy. |

The responsibilities remain separate: add a Provider and import inventory first; then create an external model, select its initial Provider routes, and set its unified external price in one operation. Use Routing Policies afterward for advanced traffic configuration. An external model can also be saved without an initial route as a draft awaiting mapping. For example, an administrator can expose the external model `DeepSeek` while routing it to `OpenAI Production / gpt-4.5`. The same Provider model may back several external aliases, and one external model may route to several Providers.

Provider-model prices represent actual upstream cost and are used for internal audit. Model Directory prices represent the unified external charge used for client billing estimates, quota accounting, metrics, and usage reports. A route selects the upstream implementation but does not change the external price.

Publication and runtime health are different states. Membership in `GET /v1/models` requires an active external `Model`, at least one active `ModelRoute`, and API-key access when a model allowlist is configured. It does not change when a Provider or Provider Resource is temporarily unhealthy. Health affects whether a request can be served and is shown separately in the directory and routing diagnostics. Disabling the external model removes it from `GET /v1/models` while retaining its mappings for later re-publication.

## Model Routing Policies

The admin console configures one routing strategy for the whole external model. Open the model card and select a strategy tab; the active tab explains its best use case, actual selection behaviour, parameter meaning, and a concrete example. Adjust the Provider parameters shown for that strategy, then choose **Apply Strategy**. The policy and every Provider parameter are saved atomically, so a model never runs with a partially updated configuration.

For fixed-ratio routing, enter the relative weight beside each Provider. Two Providers with weights 75 and 25 display target shares of 75% and 25%. Adaptive routing uses the same values as base weights and dynamically adjusts effective shares. Quality, cost, and balanced modes expose only their relevant scores. All of these strategies place eligible Providers in one traffic-allocation pool. Sequential failover is the only mode that uses Provider order; drag the rows to set first, second, and later choices.

| Strategy | Behaviour |
| --- | --- |
| `priority_weighted` | Uses the configured weights as the target traffic ratio across routes at the same priority. For example, weights 75 and 25 target a 75:25 split over a representative request volume. |
| `adaptive` | Starts from the configured weights and adjusts the effective weights using invoked attempts from the last 15 minutes. A route begins adapting after 5 samples; recent success rate and successful-request latency influence its share, with bounded adjustments to prevent starvation or extreme shifts. |
| `quality` | Always tries the highest quality score first, with weight used only to break score ties. |
| `cost` | Always tries the highest cost-efficiency score first. A higher score means a cheaper, more preferred Provider. |
| `priority_only` | Uses the Provider list as a strict primary/backup order and does not distribute normal traffic. |
| `balanced` | Preserves legacy behaviour by using `weight + quality score + cost score` as the effective probabilistic weight. New configurations should normally use fixed ratio or adaptive routing instead. |

Provider connection details and project restrictions remain route-specific. Editing one Provider route changes its upstream model, project scope, sticky-session setting, or status; overall strategy, weight, and scores are edited in the model policy instead. `all` makes a route available to every project, `include` limits it to the selected projects, and `exclude` makes it available to every project except those selected. Project scope is evaluated before traffic allocation and failover, and displayed traffic shares are recalculated across the eligible Providers.

For a private-project boundary, create an internal Provider route with scope `include` and select the private projects. Create the corresponding external Provider route with scope `exclude` and select the same projects. The private projects can then use only the internal route, while other projects continue to use the external Provider.

Project route scope also controls model discovery: `GET /v1/models` includes an external model only when the calling API key's project has at least one active eligible route, in addition to the normal model and API-key allowlist checks.

## Provider Resource Recovery

A provider resource that fails `TOKENHUB_RESOURCE_FAILURE_THRESHOLD` times in a row is parked: it stops receiving traffic and enters a cooldown. Recovery is automatic and needs no admin action.

| Phase | Behaviour |
| --- | --- |
| Parked | The resource is excluded from routing for `TOKENHUB_RESOURCE_COOLDOWN_SECONDS` |
| Half-open | Once the cooldown lapses, exactly one request is allowed through as a trial; every other request is still rejected |
| Recovered | The trial reaching the upstream successfully clears the breaker, resets the failure count and raises a `provider_resource_recovered` alert |
| Re-parked | A failed trial immediately arms the next cooldown, doubling each time up to `TOKENHUB_RESOURCE_COOLDOWN_MAX_SECONDS` |

Only the trial request's own success closes the breaker. A request that was already in flight when the breaker tripped cannot close it, however it ends. A client that disconnects mid-stream, a policy refusal, or an unsupported model counts neither for nor against the resource: it adds no failure, but it also clears none, so an alternating failure/disconnect pattern still trips the breaker.

Testing a resource from the console still recovers it immediately when the adapter supports probing, because that probe issues a real upstream request. Disabling a resource remains an administrative override: a disabled resource is never readmitted by recovery, whatever the upstream reports.

## Request Usage Audit

Each request row in **Request Logs** includes its total tokens and external billing amount. For administrators with global operations visibility, the detail panel also shows the Provider's actual cost calculated from the selected Provider model; other users do not receive that cost field. The panel retains the upstream billing breakdown when it is available: cached, cache-write, and audio input tokens, plus reasoning, audio, accepted-prediction, and rejected-prediction output tokens. Providers that do not return a field are shown as zero. Input and output totals already contain their detail categories, so do not add the detail values to the totals again.

## Metrics

TokenHub can expose Prometheus metrics at `GET /metrics`. Collection is off by default; set `TOKENHUB_METRICS_ENABLED=true` to enable it. While disabled, nothing is collected and the endpoint returns 404. The endpoint always authenticates: metrics disclose model names, provider and resource identifiers, and spend, so it is never anonymous. Send `Authorization: Bearer <token>` using `TOKENHUB_METRICS_TOKEN`, or the admin token when that variable is empty. A dedicated token is recommended so the scrape configuration does not carry the admin credential. A token supplied in the query string is rejected, because it would be captured in access logs.

| Metric | Type | Meaning |
| --- | --- | --- |
| `tokenhub_gateway_requests_total` | counter | Logical model API requests. A request that failed over across several candidates counts once. |
| `tokenhub_gateway_request_duration_seconds` | histogram | End-to-end latency including failover attempts. Buckets run to 300s. |
| `tokenhub_gateway_requests_in_flight` | gauge | Model API requests currently being served. Admin traffic and scrapes are excluded. |
| `tokenhub_gateway_tokens_total` | counter | Tokens by kind: `prompt`, `completion`, `cached`, `cache_write`, `reasoning`. |
| `tokenhub_gateway_cost_usd_total` | counter | Unified external billing estimate from Model Directory prices. Provider actual cost remains in privileged request audit rather than this metric. |

Go runtime and process metrics are exposed alongside them.

**Token kinds are not a partition and must not be summed.** `prompt` already contains the `cached` and `cache_write` tokens, and `reasoning` is a subset of `completion`. Summing the kinds double-counts.

Requests refused before routing — a bad API key, an exhausted quota, an unknown model — increment the request counter only. They never reached a provider, so they contribute no tokens, cost or duration. A model name that the catalog does not know is reported as `unknown` rather than verbatim, so a client looping over invented model names cannot inflate the series count.

Labels are `model`, `provider_type`, `provider_id`, `resource_id`, `status_code`, `error_code` and `stream`. Setting `TOKENHUB_METRICS_PROJECT_LABEL=true` adds `project_id`, which multiplies the series count of every gateway metric by the number of active projects; leave it off unless you need per-project dashboards, and use the usage reports for per-key attribution instead.

To push instead of scrape, point an OpenTelemetry Collector's `prometheus` receiver at this endpoint and forward from there; the gateway itself speaks only the Prometheus exposition format.

## Prompt Cache Pricing

The model catalog accepts an optional cache read price in USD per 1 million tokens. When it is configured, cached input tokens use that price in estimated costs. When it is left blank, TokenHub estimates the cache read price at about 0.83% of the standard input price for DeepSeek V4 Pro, 2% for other DeepSeek models, and 10% for other non-embedding models. The model pricing table marks estimated values and explains the applied ratio on hover.

## Catalog Metadata Recovery

Deleting an external model removes its database record and routes, but it does not edit `data/model-catalog.yaml` or the file configured by `TOKENHUB_MODEL_CATALOG_FILE`. Backend startup synchronizes tracked catalog metadata from that file again; administrators can trigger the same synchronization without a restart from **Settings → Base Settings → Sync Model Reference Catalog**. This synchronization does not import a model for a Provider, create a route, or publish it in `GET /v1/models`; those remain explicit administrative actions in their respective control areas.

## Security Checklist

| Control | Requirement |
| --- | --- |
| API keys | Show the full secret once, then store only prefix and suffix |
| OAuth redirect URI | Register local and production callback URLs with the identity provider |
| RBAC | Separate user, team leader, administrator, finance, security, and operator scopes |
| Audit retention | Keep request logs and admin events long enough for compliance review |
| Cost controls | Attribute every request to user, project, team, and cost center when possible |

## Chinese Enterprise Identity Sources

In **Identity Sources**, select a built-in DingTalk, Feishu, or WeCom template. The template fills the public endpoints and claim mappings; only override the advanced endpoints when traffic must pass through an enterprise proxy or a compatible private deployment.

Creating an identity source uses three required steps: choose the source, enter its connection settings, and configure the login entry plus first-login grants. The connection step links to the selected provider's official setup guide so you can create the application and obtain its credentials. Generic OIDC and OAuth2 templates instead tell you to consult the actual provider's application-registration guide and link to the relevant protocol reference. From the third step, templates with complete endpoint defaults can use **Skip and Finish**; otherwise the advanced endpoint fields become required. You can also open advanced settings to override endpoint, scope, and claim defaults. Editing an existing source keeps the complete form available on one screen.

Use the public TokenHub backend URL with the callback path `/api/admin/auth/oauth/callback`. You may leave Callback URL blank to derive it from the incoming backend host; when setting it explicitly, the complete URL must exactly match the redirect URL registered with the identity provider.

| Provider | Required application configuration | TokenHub behavior |
| --- | --- | --- |
| DingTalk | Create a web application, enable user authorization, register the callback URL, and copy its App Key and App Secret | Uses the DingTalk v1.0 JSON token API and user access-token header. If the authorized profile has no email, TokenHub derives a stable internal email from `unionId`. |
| Feishu | Create an enterprise self-built application, enable web authorization, register the callback URL, and copy its App ID and App Secret. Grant profile and enterprise-email access when available. | Uses the Feishu OAuth v2 token API and unwraps the `data` user-info response. If email is unavailable, TokenHub derives a stable internal email from `union_id`. |
| WeCom | Create a custom application and configure its trusted web authorization domain. Copy the Corp ID, application Secret, and Agent ID, and grant the application permission to read the required directory members. | Uses WeCom CorpApp login, exchanges the application token, resolves the callback code to `UserId`, and then reads the member profile. `biz_mail` is preferred; a stable internal email is derived from `userid` when needed. |

The derived addresses end in `<provider>.tokenhub.local`. They are internal account identifiers, not deliverable mailboxes. Keep a controlled password administrator until the new login has been tested end to end.

## Screenshot

![Routing policies](assets/screenshots/routes-en.png)
