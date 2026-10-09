# 聊天联系人资料风控修复（2026-10-10）

## 故障与修复范围

用户确认进入聊天列表后持续出现 `pc.user.query` 的 `FAIL_SYS_USER_VALIDATE / RGV587_ERROR`。修复前的确定性回归证明：200个缺资料会话会发起200次查询，已有完整资料也会重复查询。首次平台风控的外部成因不由这组日志确定，本修复不承诺解除平台已有风控。

这是已完成六阶段架构基线上的窄范围稳定性修复，未改变阶段状态、数据库schema、HTTP/OpenAPI、凭证续期协议、冻结CAPTCHA或交易流程；未提交、推送或发布。

### 已实现行为

- 应用服务持有可并发使用的进程内资料策略，列表与历史以及版本化/兼容入口共享。
- 每个列表请求最多准入一个逻辑资料查询；每账号最多一个在途资料查询，起始间隔5秒。MTOP内部Token换签的既有有界重试仍保留，不把逻辑预算误称为所有协议请求只有一次。
- 资料查询最多3秒并服从父Context取消；没有批量worker、后台轮询或排队等待。
- 成功结果（包括合法空字段）缓存24小时；已有完整资料在进程冷启动时按有效缓存处理。缓存键包括账号、会话和对端，账号状态和资料缓存各最多4096条；满容量时保守跳过补查，清理不能绕过在途或冷却。
- 第一次风险后暂停该账号资料请求5分钟；自然访问探测仍风险时依次10、20、30分钟，之后保持30分钟。冷却到期不自动请求，成功探测清除风险次数。普通网络/超时失败退避60秒，主动取消不判为风控。
- 风控由适配器映射为应用窄错误，保留MTOP错误链；失败响应Cookie仍先完成版本复核和写回，不触发错误的Token/Session恢复。
- 缓存命中、失败、限流不写回旧会话身份；成功时只写平台实际返回的非空字段，不重写旧对端标识。保存失败返回独立错误，仍保留取得的展示资料。
- 未知会话推送合并为300毫秒窗口内一次本地补读，明确使用refresh=false；在途事件最多追加一次补偿。不同账号独立排程；切换、卸载、刷新、删除和分页可取消旧请求，迟到回调不能提交。合并保留已加载分页、游标和当前会话已读状态。
- 首次联系人同步、手动刷新、历史分页和收发消息没有被关闭；昵称头像可能延迟补齐，但不会为展示资料持续冲击平台。

## 回归证据

- 首先执行 `go test ./internal/application/chat -run 'TestIdentityListRequestBudget|TestIdentityExistingProfilesAvoidPlatform' -count=1`：旧实现分别观测200次和1次查询，测试失败；修复后通过。
- 新增策略测试覆盖200缓存零调用、成功和失败批次预算、同账号40请求竞争、跨账号隔离、风险5/10/20/30分钟和恢复、成功空字段负缓存、对端切换、24小时有效期、容量与清理、取消/超时/存储错误、真实Context取消及不覆盖新缓存值。
- 真实SQLite+HTTP测试 `TestChatIdentityRiskBudgetAcrossHTTPRoutes` 写入200个含消息会话，在新旧列表和历史入口访问后资料总调用数为1，同时验证OpenAPI成功响应。
- `TestChatIdentityRiskPreservesCookieSettlement` 验证风险错误链未丢失、风险不误判为凭证过期，以及风险响应携带的Cookie已经提交；原有Cookie竞争回归继续通过。
- 前端测试验证短时间事件合并、两个账号独立、至多一次补偿、取消后迟到结果、已经进入事件队列的旧定时回调、卸载、失败后自然事件重试。真实useChat测试验证20条通知只新增一次本地读，第二页联系人及下一页游标保留，未读投影不回退。
- 原先要求失败写回旧资料及8worker排队的测试改为断言失败零写入、一次预算和取消零追加请求；没有删除测试、弱化断言或扩大白名单。当前仓库无历史注释baseline文件，新增/修改声明已通过中文注释检查。

## 验证命令与结果

Windows PowerShell没有make，本次使用对应原生命令。覆盖率均为生成产物，不提交。

- Go全量：`go test ./... -count=1 -timeout=600s '-coverprofile=I:/Code/Ydisks-Xianyu-Helper/.planning/chat-risk-final-cover.out'`；未设置RUN_BROWSER_INTEGRATION。通过，Go statement **81.5%**。
- 新逻辑覆盖率：`go test ./internal/application/chat '-coverprofile=I:/Code/Ydisks-Xianyu-Helper/.planning/chat-identity-cover.out' -count=1`；`identity_policy.go`与`identity_resolution.go`所有函数statement均100%，整个chat应用包92.3%。
- 前端全量：`npm run test:coverage --prefix frontend`；通过，**101文件、671测试**，frontend statement **81.06%**；新增useLocalSessionRefresh Hook的statement/branch/function均100%。
- `go vet ./...`、`go run ./tools/architecturecheck`、`go run ./tools/apicheck`、`npm run api:check --prefix frontend`、`go run ./tools/commentlint -mode check -root .`、`npm run comments:check --prefix frontend`、`npm run typecheck --prefix frontend`、`git diff --check`均通过。
- `npm run build --prefix frontend`完成并更新嵌入资源；前端全测包含既有分片预算，不提高预算。`go build -o .planning/chat-risk-server.exe ./cmd/server`成功。

### 浏览器验证：存在未通过门禁

执行对应make cover-browser的命令两次：设置 `RUN_BROWSER_INTEGRATION=1`，运行 `go test -p 1 ./internal/browser ./internal/webui -count=1 -timeout=300s -coverprofile=...`。

两次均在未修改的 `TestReferencePlaywrightSliderBrowserIntegration`、`token_captcha_fallback_integration_test.go:125` 失败，实际墙钟耗时分别1368ms、1719ms，超过既定1300ms上限；其余该断言报告的点数、纵向范围、超调在允许范围。浏览器包生成的statement报告64.0%，但整条命令失败，不能声明浏览器门禁通过；两次webui包均通过、statement100%。没有修改、跳过或放宽冻结滑块实现或测试，也没有反复重跑至成功后隐藏失败。

### 实际启动与关闭

使用新构建程序、独立临时SQLite和`-addr 127.0.0.1:59189 -workdir <本次smoke目录>`启动，未使用-no-browser。日志确认Chromium149.0.7827.55实际启动；`/health`返回200及database=ok。Ctrl+C后日志记录`http_completed=true application_completed=true`，健康端点不再可达。PowerShell PTY因中断返回1，不将其宣称为二进制退出码0；关闭证据以生命周期日志和端点不可达为准。

### 未执行或外部例外

- `go test -race ./internal/application/chat -run TestIdentity -count=1`因当前CGO_ENABLED=0拒绝运行，环境未发现gcc；不声明race通过。
- golangci-lint未安装，因此未执行该门禁；没有为了通过而改动依赖或linter配置。
- 没有配置MySQL/PostgreSQL实库；本次未改SQL/schema，SQLite回归已执行。
- 没有调用真实闲鱼账号、验证真实平台风控解除或真实收发/发货；这些必须在用户部署新版本后小范围观察，不能通过本地模拟替代。
- 准入状态是单后端进程内共享，不能宣称多副本间共享限流；重启会丢失冷却时间，但不会触发全量资料重扫。

## 发布与回滚边界

本轮只修改工作区、验证和构建，未修改用户已部署服务、未生成/推送Docker镜像。部署需使用包含重建前端的新程序/镜像。由于不改schema，可回退程序和匹配前端；回退旧版会重新暴露请求放大问题，回退时应关闭聊天页面。禁止通过频繁重启、刷新或强制重登试图跳过风控冷却。

## 后续发布授权

2026-10-10用户授权“推送构建新的镜像”。随后按既有流程提交至自有fork的main，先运行完整CI，再触发双架构Docker门禁；只更新main和完整sha-*标签，不主动推广latest，不修改上述本地验证失败记录或绕过发布门禁。此记录写入时远端构建尚未执行，最终运行链接与镜像摘要另行报告。
