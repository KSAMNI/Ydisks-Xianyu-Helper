# 人工处理信息增强与订单自动化停用（2026-10-10）

## 用户可见行为

- 每条异常首行显示任务类型：付款发货、拍下改价、评价赠品、求评价、砍价自动免拼等。未知类型保留原值，不猜测为发货。
- 运行和延期异常均展示可靠订单号、账号备注、商品标题/标识、买家标识、会话、订单阶段、更新时间和停止原因。运行展示已确认动作数与检查点；延期展示累计尝试次数。
- 订单号支持复制以及打开闲鱼卖家订单页。未可靠关联时显示“尚未关联订单”；已记录订单号但本地事实缺失时仍可定位，却不能整单停用。
- “终止本次任务”“忽略本次任务”保持原有单任务语义。
- 新增“停止此订单全部自动化”，二次确认具体任务、账号、订单和影响范围。它不是取消闲鱼订单，不会撤销已发送内容或在途请求，不改变其他订单。
- 停用持久生效；包括发货、确认发货、改价、免拼、赠品、求评价及自动评价买家。当前版本不提供自动解除或历史批量重放入口。

## 实现与安全边界

- SQLite/MySQL/PostgreSQL 同步新增 00056 `order_automation_stops`，记录账号、订单、操作用户和首次时间；独立于规则及运行，删除规则、重新创建触发键或进程重启不会解除停用。
- 两种已有 `/api/v1/automation-*/{id}/resolve` 操作新增 `stop_order`，OpenAPI 明确登记全部合法请求值。客户端只提交异常主键，后端按账号→订单锁顺序再次核验当前用户、异常状态和固定身份；不能由客户端指定另一个订单。
- 关联仅取异常固定订单或同账号明确快照，不根据时间、最近订单或不唯一聊天猜测。原始事件、卡密、Cookie、Token 和收货资料不进入新摘要字段。
- 停用事务同时收口同单未开始的运行和待执行/死信延期；已占用或结果未知的动作保留证据。已停止订单不再出现在待处理列表，数据库记录仍保留。
- WebSocket 事件、规则准备、历史恢复扫描、账号评价候选和动作/延期领取均检查停用；领取使用与停用一致的短数据库锁域。消息、图片、卡密及交易接口调用前再次检查，存储失败不放行。
- 不跨网络持数据库锁；已经进入平台的动作无法撤回。第一步成功后停用会保留第一步检查点，后续动作停止。不改变结果未知时禁止重放、砍价等待最终待发货阶段以及冻结 CAPTCHA 行为。
- 前端请求按当前账号和请求代次隔离，防双击、取消确认、切换账号及卸载后的晚到响应；纯组件不直接访问 HTTP。
- 普通运行和独立人工处理通知均明确任务类型；保留人工处理类别、旧类别订阅兼容和 outbox 稳定去重。

## 验证记录

以下为 PowerShell 原生参数调用；覆盖率使用独立绝对路径，避免输出路径被误解析成 Go 包。覆盖产物不提交。

- `go test ./... -count=1 -timeout=600s -coverprofile <workspace>\.planning\2026-10-10-manual-intervention-design\go-final-cover.out`：通过，未设置 `RUN_BROWSER_INTEGRATION=1`；Go statements **81.6%**。另补充执行历史缺订单回填、首尾空白停用请求脱敏和真实 handler 定向回归，全部通过。
- `npm run test:coverage --prefix frontend`：**103 文件 / 678 测试通过，statements 81.23%**。新增 `issueActions.ts`、`issueState.ts` statements 100%；未降低原有分片预算或覆盖阈值。
- `go vet ./...`、`go run ./tools/architecturecheck`、`go run ./tools/commentlint -mode check -root .`、`npm run comments:check --prefix frontend`、`npm run typecheck --prefix frontend`、`npm run api:check --prefix frontend`、`go run ./tools/apicheck`、`git diff --check`：通过。
- `npm run build --prefix frontend` 及 `go build -o <临时二进制> ./cmd/server`：通过，嵌入前端已更新。
- `RUN_BROWSER_INTEGRATION=1` 下以参数数组执行 `go test -p 1 -count=1 -timeout=600s -coverprofile <独立绝对路径> ./internal/browser ./internal/webui`：**未完全通过**。浏览器统计 statements 64.1%；唯一失败为冻结的 `TestReferencePlaywrightSliderBrowserIntegration`，`token_captcha_fallback_integration_test.go:125` 实测 `moves=77 duration_ms=1391 vertical_span=22 overshoot=9`，耗时超过原有上限。`internal/webui` 通过。没有修改或放宽该测试，不把有失败的报告称作通过。
- 构建二进制在独立 `.planning/.../smoke-data` 和随机回环端口启动，未使用 `-no-browser`；日志确认 Chromium `149.0.7827.55` 就绪，`/health` 和 SPA 均 HTTP 200。Ctrl+C 后 `http_completed=true application_completed=true`，端点不可达；没有更改生产数据或停止用户已有服务。

已知工具/环境差异：当前 commentlint 不再接受历史 `-baseline` 参数，按仓库当前 Makefile 执行无 baseline 检查。一次注释扫描与全测临时目录清理交错失败，待测试结束重跑通过。初次未引用覆盖路径的 PowerShell 命令未生成预期报告/将路径当成额外包，已用参数数组和绝对路径重跑；未使用根目录旧报告冒充本次结果。

## 环境与未覆盖边界

- 真实闲鱼发送、交易改价、确认发货、免拼、评价以及真实渠道投递未执行；全部变更场景使用本地 SQLite、HTTP handler、发送器替身和 jsdom。
- 三方言迁移均提供；本机未配置 MySQL/PostgreSQL 实库，本次只实测 SQLite。
- 本机 CGO_ENABLED=0，未运行 race；golangci-lint 未安装。未安装或升级无关依赖。
- 数据库驱动级读游标/提交故障等未逐条故障注入的分支归类为后续确定性覆盖工作，不宣称所有新增函数达到 100%。没有降低覆盖阈值、跳过现有测试或扩大例外名单。
- 当前仓库已没有历史注释 baseline 参数或文件；本次没有生成或扩展注释豁免。新增及提取的异常摘要与执行收口逻辑按现行中文 AST 门禁校验。


## 本次发布授权与已知例外

2026-10-10 用户在了解失败用途后明确允许忽略此滑块错误，并要求提交、推送、构建新镜像。发布前复测仍只有冻结 `TestReferencePlaywrightSliderBrowserIntegration` 的耗时断言失败（1360ms，上限1300ms）；此结果作为已接受的本地已知例外保留，不称作浏览器全测通过，不改实现、测试、超时或CI门禁。普通Go与前端测试、完整CI、原生amd64/arm64镜像内实际Chromium启动及health仍须通过后才发布manifest。沿用现有main和完整sha标签；没有授权改发latest或语义版本标签。
