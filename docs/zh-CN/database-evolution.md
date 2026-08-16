# 数据库演进

TokenHub 使用显式、只前进的迁移演进数据库。本文说明演进模型、维护命令，以及升级与回退和数据库的关系。

## 模型

- **采纳基线**：每个数据库携带迁移 ledger（`schema_migrations`）。旧版本创建的数据库会在下一次启动时被采纳：冻结的结构流程将其补齐，与参考快照做语义校验，然后记录基线。全新数据库直接由冻结的基线 SQL 创建，不再走 ORM 流程。
- **扩展迁移**添加兼容结构并在启动时自动执行；**收缩迁移**移除旧结构，绝不在启动时执行，只能通过满足前置条件的维护命令执行。
- **校验和与 dirty 状态**：每次启动都会校验已应用迁移的校验和。非事务迁移失败会留下 dirty 标记并拒绝启动，直到完成修复；事务迁移失败只回滚自身版本。
- **数据回填**记录在独立的 ledger（`data_backfills`）中。阻塞式回填必须完成后实例才就绪；在线回填以幂等分批执行，服务不中断，并通过 lease 在集群内协调为单一逻辑任务，执行者失效后由其他实例接管。
- **实例心跳**：每个运行中的实例以 TTL 发布自身版本。存在未过期心跳的实例时，收缩维护拒绝执行。
- **回退兼容性**：每个 Release 声明自己能完整运行的数据库状态范围。托管回退先执行只读预检：没有受验证兼容记录的 Release 以 `unknown` 拒绝；数据库状态超出目标范围以 `incompatible` 拒绝。管理界面将回退目标标注为数据库兼容、不兼容或未知。

`/readyz` 与 `/healthz` 在 ledger 处于 dirty、无法校验或阻塞式回填未完成时失败；在线回填挂起不影响就绪。

## 维护命令

主二进制提供 `db` 子命令：

```bash
tokenhub db status                                  # ledger、回填、在线实例
tokenhub db verify                                  # ledger 校验和 + 语义校验
tokenhub db migrate                                 # 执行待执行的扩展迁移
tokenhub db repair --version <n>                    # 清除 dirty 迁移（仅限受验证的修复）
tokenhub db contract --dry-run                      # 收缩迁移预检
tokenhub db contract --backup-reference <ref> --maintenance
```

数据库连接取自 `TOKENHUB_DATABASE_URL`（或默认 SQLite 路径）。

`contract` 在执行任何操作前要求：全部数据回填完成、不存在未过期的实例心跳、操作者提供已验证的备份引用、显式确认满足维护条件。SQLite 请先创建内置备份；PostgreSQL 请提供你自行验证过的外部备份引用。

## 运维要点

- 对没有采纳基线的数据库执行 `tokenhub db migrate` 会提示先正常启动一次服务：采纳在服务的串行结构流程中完成。
- 被拒绝的 contract 会说明失败的前置条件；此时没有执行任何操作。
- 回退到旧版本后，旧版本可在当前数据库上继续工作；新版本回归时重新校验 ledger 并继续演进。
- 管理界面展示只读的数据库演进区块（状态版本、就绪状态、兼容范围、回填、在线实例）；contract 与 repair 操作按设计只存在于 CLI。

## 开发者说明

- 迁移运行器位于 `backend/internal/dbschema`；冻结基线 SQL 按方言嵌入在 `backend/internal/dbschema/migrations/` 下。
- 模型变更后重新生成 SQLite 基线：`UPDATE_BASELINE=1 go test ./internal/server -run TestSQLiteBaselineSQLIsCurrent`；PostgreSQL 基线以同样方式生成，需设置 `TEST_POSTGRES_URL` 并加 `integration` 构建标签。基线过期时测试会失败。
- CI 运行 PostgreSQL 集成套件和 N-1 双向契约：旧版本二进制创建数据库，当前版本采纳并就绪，旧版本在采纳后的数据库上再次启动，当前版本回归继续服务。
