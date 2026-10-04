# 正则关键词审查修复（2026-10-04）

## 范围与行为

本次修复 `fcac03f` 的四项审查问题，不改变六阶段完成状态、数据库 schema、冻结 CAPTCHA、账号凭证或交易自动化顺序。全部原有架构、契约和注释门禁保持启用。

### 正则语法和原文

- 浏览器不再使用 JavaScript `RegExp` 作为 Go/RE2 正则的保存门槛。
- 正则语法由后端统一校验，默认忽略大小写，允许 RE2 内联标志；校验失败保留编辑草稿并展示稳定错误。
- `regexp` 模式在 UI、API adapter、应用服务、DB 编码/解码、运行时匹配中保留原文，包括转义空格和纯空格表达式；仅空字符串被拒绝。
- `contains` 模式保持首尾空白裁剪、大小写不敏感和表达式去重。
- 同一规则按 OR 匹配，商品级优先于账号级，API/关键词/AI/默认回复顺序不变。

### 兼容写入

- 按 ID 更新时，在数据库账号级事务内读取当前规则，由应用层回调合并和校验后写回。
- 未提供 `match_type` 时继承现有模式；未提供 `expressions` 时保留现有集合，旧 `keyword` 仅修改首表达式，其余表达式不丢失。
- 显式提供的新匹配字段继续作为完整更新目标，允许用户明确切换模式或删除表达式。
- 批量替换缺少稳定 ID，不能猜测旧客户端的字段对应关系。账号包含多表达式/非普通模式规则，且非空批次有项未提供 `expressions` 或 `match_type` 时，返回校验错误，整个批次不写入。
- 完整指定匹配字段的批次仍可替换；显式空批次仍代表清空全部规则。
- 所有关键词 CRUD 复用非敏感账号行锁。SQLite 先取得写锁；MySQL/PostgreSQL 锁定账号 ID 行。事务回调仅进行内存合并和校验，不调用外部服务或重入数据库。

### 结构与测试门禁

关键词 CRUD 从 `internal/db/extras.go` 拆至 `keyword_crud.go`；关键词响应 DTO 从 `success_contract_support.go` 拆至 `keyword_responses.go`。不降低 800 行阈值、不增加豁免。

同时修正审查期间发现的三个 Windows 测试夹具问题：前端路径使用系统 `sep` 归一化；浏览器相对路径测试在同卷临时工作目录运行；ZIP 安全路径断言使用 `filepath.Join`。路径逃逸断言保留，生产浏览器目录逻辑不变。补齐既有二维码测试内 `signal` 类型成员的中文注释，不改变测试行为。新增精确的 `.gitattributes` 规则固定 `frontend/index.html` 为 LF，避免 Windows 构建把输入中的 CRLF 残留到嵌入入口；生成物仍只由构建更新。

未新增或扩展注释基线；新增文件和语义修改声明使用中文说明，未触及冻结文件的历史豁免范围。

## 验证记录

- 新回归先红后绿：正则空白、旧更新保存配置、不安全批次拒绝、事务回滚、最新快照和账号/主键不可变。
- `go test ./internal/application/keywords ./internal/adapter ./internal/db ./internal/engine -count=1 -timeout=300s` 通过。
- server 新旧关键词接口及版本化契约定向测试通过。
- `go vet ./...` 通过。
- 前端 `typecheck`、`api:check` 通过。
- `npm --prefix frontend run test:coverage`：93 个文件、587 个测试通过，statement 79.23%。未降低覆盖率配置或排除业务代码。
- `npm --prefix frontend run build` 通过，嵌入资源经构建流程更新。
- `go test ./... -coverprofile <隔离目录>/go-cover.out -timeout 300s` 全量通过，Go statement 81.2%；未设置 `RUN_BROWSER_INTEGRATION=1`。
- `go run ./tools/architecturecheck`、`go run ./tools/commentlint -mode check -root .`、`go run ./tools/apicheck`、前端 `comments:check` 和 `git diff --check` 均通过，未绕过任何门禁。
- `go build -o <隔离目录>/xianyu-server.exe ./cmd/server` 通过。
- Go、React 和 TypeScript 专项静态复审均未确认新的功能问题；Go 复审代理因隔离策略不能运行主路径 Git diff，仅静态读取主路径代码，差异与测试由主审核验。

## 验证边界

未设置 `RUN_BROWSER_INTEGRATION=1`，未启动真实账号或调用外部平台。MySQL/PostgreSQL 实例未配置，本次三方言锁语法未做实例实测；现有方言矩阵的 SQLite 分支已运行。环境 `CGO_ENABLED=0`，未执行 race；未安装 `golangci-lint`，不将其记作通过。没有发布安装包或服务部署。

用户指定最终合并目标为 `KSAMNI/Ydisks-Xianyu-Helper` 的 `main`，不合并上游 PR #72。
