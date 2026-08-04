import { type FieldConfig } from "../core/types";
import { tx } from "../i18n/runtime";

const reasoningOptionKeys = [
  "reasoning_effort_values",
  "reasoning_effort_map",
  "reasoning_effort_unsupported",
  "reasoning_budget_map",
  "preserve_reasoning_content",
] as const;

export function providerReasoningFieldConfigs(visible?: FieldConfig["visible"]): FieldConfig[] {
  return [
    {
      key: "reasoning_effort_values",
      label: "推理强度允许值",
      placeholder: "none, low, medium, high, max",
      help: "填写上游实际接受的 reasoning_effort 值，逗号分隔；留空表示不限制。",
      visible,
    },
    {
      key: "reasoning_effort_map",
      label: "推理强度映射",
      type: "textarea",
      placeholder: '{"minimal":"low","xhigh":"max"}',
      help: "把 Claude Code 的推理强度转换为上游值；填写 JSON 对象。",
      visible,
    },
    {
      key: "reasoning_effort_unsupported",
      label: "不支持值处理",
      type: "select",
      options: ["passthrough", "omit", "reject"],
      help: "透传原值、删除参数，或在调用上游前返回明确错误。",
      visible,
    },
    {
      key: "reasoning_budget_map",
      label: "推理预算映射",
      type: "textarea",
      placeholder: '{"2048":"low","8192":"medium","16384":"high","*":"max"}',
      help: "按 thinking.budget_tokens 的最大 Token 数映射推理强度；* 表示兜底值。",
      visible,
    },
    {
      key: "preserve_reasoning_content",
      label: "回传推理内容",
      type: "boolean",
      help: "仅在上游支持后续 assistant 消息接收 reasoning_content 时开启。",
      visible,
    },
  ];
}

export function providerReasoningFormValues(options?: Record<string, string>) {
  const source = options ?? {};
  return {
    _existing_options: JSON.stringify(source),
    reasoning_effort_values: source.reasoning_effort_values ?? "",
    reasoning_effort_map: source.reasoning_effort_map ?? "",
    reasoning_effort_unsupported: source.reasoning_effort_unsupported || "omit",
    reasoning_budget_map: source.reasoning_budget_map ?? "",
    preserve_reasoning_content: source.preserve_reasoning_content === "true" ? "true" : "false",
  };
}

export function providerReasoningOptions(values: Record<string, string>, base?: Record<string, string>) {
  const options = { ...readExistingOptions(values._existing_options), ...base };
  for (const key of reasoningOptionKeys) delete options[key];

  const allowed = values.reasoning_effort_values
    ?.split(",")
    .map((value) => value.trim().toLowerCase())
    .filter(Boolean)
    .join(",");
  if (allowed) options.reasoning_effort_values = allowed;

  const effortMap = normalizedJSONObject(values.reasoning_effort_map, "推理强度映射");
  if (effortMap) options.reasoning_effort_map = effortMap;

  const unsupported = values.reasoning_effort_unsupported?.trim().toLowerCase();
  if (unsupported && unsupported !== "omit") options.reasoning_effort_unsupported = unsupported;

  const budgetMap = normalizedJSONObject(values.reasoning_budget_map, "推理预算映射");
  if (budgetMap) options.reasoning_budget_map = budgetMap;

  if (values.preserve_reasoning_content === "true") options.preserve_reasoning_content = "true";
  return options;
}

function readExistingOptions(raw?: string) {
  if (!raw?.trim()) return {};
  try {
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? parsed as Record<string, string> : {};
  } catch {
    return {};
  }
}

function normalizedJSONObject(raw: string | undefined, label: string) {
  if (!raw?.trim()) return "";
  try {
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed) || Object.values(parsed).some((value) => typeof value !== "string")) throw new Error();
    return JSON.stringify(parsed);
  } catch {
    throw new Error(tx(`${label}必须是键值均为字符串的 JSON 对象。`));
  }
}
