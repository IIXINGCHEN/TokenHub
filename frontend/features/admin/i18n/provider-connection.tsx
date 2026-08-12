const en: Record<string, string> = {
  "正在测试": "Testing",
  "测试 Provider 连接": "Test Provider connection",
  "请填写 Base URL 和 API Key 后测试。": "Enter the Base URL and API key before testing.",
  "API Key 配置有效": "API key is valid",
  "Provider 连接测试失败": "Provider connection test failed",
  "Anthropic 认证方式": "Anthropic Authentication",
  "x-api-key（Anthropic 官方）": "x-api-key (Anthropic official)",
  "Authorization Bearer（兼容服务）": "Authorization Bearer (compatible services)",
  "认证密钥始终使用上面的加密 API Key，不需要写入自定义 Headers。": "Authentication always uses the encrypted API key above; do not add it to custom headers.",
};

const ja: Record<string, string> = {
  "正在测试": "テスト中",
  "测试 Provider 连接": "Provider 接続をテスト",
  "请填写 Base URL 和 API Key 后测试。": "テストする前に Base URL と API Key を入力してください。",
  "API Key 配置有效": "API Key は有効です",
  "Provider 连接测试失败": "Provider 接続テストに失敗しました",
  "Anthropic 认证方式": "Anthropic 認証方式",
  "x-api-key（Anthropic 官方）": "x-api-key（Anthropic 公式）",
  "Authorization Bearer（兼容服务）": "Authorization Bearer（互換サービス）",
  "认证密钥始终使用上面的加密 API Key，不需要写入自定义 Headers。": "認証には上の暗号化された API Key を使用します。カスタム Header に追加する必要はありません。",
};

export const providerConnectionTranslations = { en, ja };
