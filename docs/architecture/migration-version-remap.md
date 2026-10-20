# 迁移版本号重映射（fork main → 合并后主线）

## 背景

合并 `upstream/main` 之前，fork 的 `main` 与上游各自占用了相同的 Goose 版本号，但含义不同：

| 版本 | fork `main` 旧含义 | 合并后主线含义 |
|---|---|---|
| 00052 | keyword_expressions | ai_mode |
| 00053 | reply_images | item_ai_settings |
| 00054 | delivery_images | keyword_expressions |
| 00055 | item_reply_once | reply_images |
| 00056 | order_automation_stops | delivery_images |
| 00057 | image_assets | image_assets（不变） |
| 00058 | — | item_reply_once |
| 00059 | — | order_automation_stops |

合并后主线统一采用上游编号（52/53/54 归上游，fork 独有迁移顺延到 55–59）。因此**已经跑过 fork `main` 的数据库**必须做一次性版本重映射，否则 Goose 会把 52–57 当成已应用而跳过 `ai_mode`、`item_ai_settings` 等新迁移，或把已存在的表重复创建。

全新安装、以及原本就跑上游版本的数据库**不需要**本步骤。

## 步骤

### 1. 备份

先完整备份数据库文件（SQLite）或做一次快照/导出（MySQL、PostgreSQL）。重映射会直接改写 `goose_db_version` 账本。

### 2. 补齐低版本迁移的 schema

`ai_mode`（00052）与 `item_ai_settings`（00053）在旧 fork 库里从未执行过，而它们的版本号低于库中当前最大版本，Goose 不会再自动应用，必须先手工补齐（DDL 与迁移文件一致，可重复执行前请先确认列/表不存在）。

SQLite：

```sql
ALTER TABLE ai_reply_settings ADD COLUMN ai_mode TEXT NOT NULL DEFAULT 'bargain_only'
    CHECK (ai_mode IN ('bargain_only', 'full_service'));

CREATE TABLE IF NOT EXISTS item_ai_settings (
    cookie_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    ai_override TEXT NOT NULL DEFAULT 'inherit' CHECK (ai_override IN ('inherit', 'enabled', 'disabled')),
    item_context TEXT NOT NULL DEFAULT '' CHECK (length(item_context) <= 12000),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (cookie_id, item_id),
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
```

MySQL / PostgreSQL 请对照 `internal/db/migrations/mysql/00052_ai_mode.sql`、`00053_item_ai_settings.sql` 与 `postgres` 同名文件执行等价 DDL。

### 3. 重写迁移账本

必须按**版本号从大到小**更新，避免中间状态出现重复版本号。SQLite / MySQL / PostgreSQL 语句相同，只需把表名按方言改大小写习惯即可：

```sql
UPDATE goose_db_version SET version_id = 59 WHERE version_id = 56;  -- order_automation_stops
UPDATE goose_db_version SET version_id = 58 WHERE version_id = 55;  -- item_reply_once
UPDATE goose_db_version SET version_id = 56 WHERE version_id = 54;  -- delivery_images
UPDATE goose_db_version SET version_id = 55 WHERE version_id = 53;  -- reply_images
UPDATE goose_db_version SET version_id = 54 WHERE version_id = 52;  -- keyword_expressions
```

`00057 image_assets` 版本号不变，无需处理。

### 4. 登记补齐的两个版本

`is_applied` 的取值必须按方言区分：Goose 在 SQLite/MySQL 里是整数列，在 PostgreSQL 里是 `boolean`，写 `1` 会直接报类型错误。

SQLite / MySQL：

```sql
INSERT INTO goose_db_version (version_id, is_applied, tstamp) VALUES (52, 1, CURRENT_TIMESTAMP);
INSERT INTO goose_db_version (version_id, is_applied, tstamp) VALUES (53, 1, CURRENT_TIMESTAMP);
```

PostgreSQL：

```sql
INSERT INTO goose_db_version (version_id, is_applied, tstamp) VALUES (52, true, CURRENT_TIMESTAMP);
INSERT INTO goose_db_version (version_id, is_applied, tstamp) VALUES (53, true, CURRENT_TIMESTAMP);
```

MySQL 的 `tstamp` 若为 `TIMESTAMP NOT NULL`，同样可用 `CURRENT_TIMESTAMP`。

建议先在事务里试运行再提交（下面以 PostgreSQL 为例，其余方言同理）：

```bash
printf 'BEGIN;\n' > /tmp/remap-dry.sql
cat remap.sql >> /tmp/remap-dry.sql
printf 'ROLLBACK;\n' >> /tmp/remap-dry.sql
psql -v ON_ERROR_STOP=1 -f /tmp/remap-dry.sql   # 5 条 UPDATE 应各命中 1 行
```

### 5. 校验

启动服务后确认迁移不再执行、且下列对象存在：

```sql
SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1;   -- 期望 59
SELECT name FROM pragma_table_info('ai_reply_settings') WHERE name = 'ai_mode';   -- 期望 1 行
SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'item_ai_settings'; -- 期望 1 行
```

MySQL / PostgreSQL 分别用 `SHOW COLUMNS FROM ai_reply_settings LIKE 'ai_mode'` 与 `information_schema.columns`。

## 说明

- 本文件只记录一次性运维步骤，不进入自动化迁移链；Goose 账本重写属于显式的数据变更，必须由维护者在备份后手工执行。
- 未执行本步骤而直接升级旧 fork 数据库，会在启动迁移阶段报重复表或缺失列错误；出现该情况应先回滚到旧版本、完成备份与重映射后再升级。
