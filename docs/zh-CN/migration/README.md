# TokenHub 迁移框架

TokenHub 迁移框架提供可重复、幂等的工作流，将竞争 AI 网关迁入 TokenHub。

## 架构

参见 [architecture.md](./architecture.md) 了解框架设计和扩展指南。

## 支持的源

| 源 | 适配器 | 支持的版本 | 状态 |
|--------|---------|-------------------|--------|
| LiteLLM | `litellm` | ≥1.52.0, <1.70.0 | 基础 |

参见 [litellm.md](./litellm.md) 了解 LiteLLM 详情。

## 规范包

源适配器和 TokenHub 接收器之间使用的中间表示。参见 [bundle-schema.md](./bundle-schema.md) 了解规范和兼容性策略。

## 密钥处理

包中的密钥以 `{"$secretRef": "ENV_NAME"}` 引用形式存储。接收器在应用时从环境变量、文件或交互式提示中解析。包中不嵌入明文密钥。

## 文档

- [架构](./architecture.md)
- [包规范](./bundle-schema.md)
- [LiteLLM 适配器](./litellm.md)
- [CLI 参考](./cli.md)
- [端到端测试](./e2e.md)
