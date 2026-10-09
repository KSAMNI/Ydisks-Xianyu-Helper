# 自动化重复成功日志与延期事件诊断修复（2026-10-09）

## 问题与证据

实机日志有 495 条求评价计划任务成功日志，集中在同一规则的七个订单；没有对应的求评价“自动化规则执行成功”。旧代码把幂等跳过的 `nil` 返回也视为成功，且先准备任务再争抢运行，造成按分钟重复准备和误报。不能由这些日志判断历史消息是否实际送达，也不能据此清空计数或重发。

另两条未知角色事件缺少可用于核验的订单或商品事实。5/10/20 分钟间隔符合已有队列退避，并非每分钟重试。日志里的“暂停期间”是队列的历史名称，不证明账号当时处于暂停状态。单次 Token 过期与求评价问题没有足够证据建立因果关系。

## 实现

1. `executeRuleWithResult` 返回本次调用的 `success/skipped/deferred/needs_review/failed`、运行 ID、动作数和跳过原因。兼容错误型入口保留；只有动作完成且终态保存成功才报告成功。`sent_count` 是累计完成动作数，不是卡密张数、发货次数或平台送达回执。
2. 仅对调度器发起的新求评价轮次，在任务准备前检查规则、幂等键、账号和订单的运行投影；不读取原始事件或凭证。已成功、人工核对、有效租约、退避未到期、重试耗尽等情况直接跳过。只读检查与原子重领共用同一个 SQL 条件，原子领取仍是最终并发防线。显式延期续跑与其他触发类型保留原路径。
3. 调度跳过仅记 Debug，不再刷 INFO 成功。实际成功带 `run_id/rule_id/order_id/attempt/sent_count`，延期与失败也有本次结果分类。仍执行有界的只读候选扫描，不使用跨轮缓存，也不自动修改历史计数。
4. 未知角色事件区分 `missing_item_id`、`missing_local_item`、`missing_order_id`、`missing_local_order`。延期错误保留精确原因并兼容原有 `errors.Is` 判断。日志补充队列任务 ID、领取代次、类型、已知身份和最终重试标记；不输出 Cookie、任务正文或原始事件。
5. 未知角色且缺订单号时，会话回填必须在账号及已知买家/商品约束下恰有一笔待发货候选。多候选不按最近时间猜单；明确卖家事件的旧查询入口保留兼容。没有扩展查询去猜测已发货或已结束邻单。
6. 明确同单已不在待发货阶段时，沿用卖家门禁安全忽略并保留原因日志；队列日志改为“处理完成”，不再暗示发生了外部动作。证据不足仍阻断，第五次失败进入 `dead_letter` 并通知人工。已有通知 action 标签保留兼容。

## 明确不做的事情

- 不改生产数据库，不重置求评价次数，不删除或重建历史运行，不自动补发七笔订单。
- 不把无号事件按相近时间、最近订单或已发货邻单强行关闭。
- 不放宽卖家身份、砍价 WebSocket 阶段、自动发卡及确认发货顺序。
- 不修改冻结 CAPTCHA、账号凭证续期策略、HTTP API、数据库 schema 或前端。
- 保留工作区原有前端与嵌入静态资源改动；未提交、部署或重启生产服务。

## 历史数据处置与上线验收

1. 升级前备份；只读核对指定规则和七笔订单的 `automation_runs`：`id/rule_id/cookie_id/order_id/trigger_key/status/action_started/action_cursor/sent_count/attempt_count/next_retry_at`，以及订单的 `review_request_count/last_review_request_at`。不要导出 `raw_event_json`、消息内容、Cookie 或发货凭证。
2. 对延期任务只读核对 `id/cookie_id/trigger_type/status/attempt_count/due_at/error_message` 及必要的白名单身份字段。任务是否属于同一平台事件必须有可靠身份依据；本次未取得实机数据库，不能代替该核验。
3. 发布后至少观察两个一分钟扫描周期：被阻塞的历史轮次没有新的 INFO 成功或发送动作；健康且到期的新任务只执行一次并更新计数；实际成功日志能关联运行 ID。
4. 对仍缺卖家证据的事件观察 `kind=role_verification` 与精确 `reason`；第五次失败后 `terminal=true`，进入人工处理，而不是无限自动重放。历史死信不会被本修复自动重新入队。
5. 只有人工核对平台聊天记录、运行检查点和订单状态后，才能决定个别历史任务如何处理；这不是批量清计数/重试操作的授权。

## 验证与环境边界

- 定向回归：七单 × 七十轮（490 次跳过）、真实成功及计数、下一轮次和其他规则独立、准入与原子领取一致、竞争窗口、查询取消、明确未发送、发送结果未知、延期、计数失败、成功终态保存失败。
- 延期与身份回归：四种字段/事实缺失原因、第五次死信与一次人工通知、明确已结束同单、无号已发货邻单不关联、多待发货候选阻断、单候选和身份后缀兼容、软删除和错误归属过滤、日志不输出敏感正文。
- SQLite 使用真实迁移后的临时数据库。MySQL/PostgreSQL 无连接配置且本机无 Docker，未运行外部多数据库回归；没有 schema 迁移。
- 普通 Go 覆盖率：`go test '-coverprofile=cover.out' ./...`，未设置 `RUN_BROWSER_INTEGRATION=1`；总 statements **81.4%**。覆盖率不是全部业务分支已覆盖的声明。
- 本地浏览器：设置 `RUN_BROWSER_INTEGRATION=1` 后运行 `go test '-coverprofile=cover-browser.out' ./internal/browser`，初次通过，statements **64.0%**。随后 `-v -count=1` 复测失败，报告 64.1% 不能作为通过证据：冻结测试 `TestReferencePlaywrightSliderBrowserIntegration` 实测拖动 1520ms，超出既有 1300ms 上限。单项 `-count=3` 复核也因 1610/1426/1536ms 失败；其余轨迹/成功条件通过。未修改或放宽冻结实现/测试，也没有把失败改为跳过。仅测试本地 Chromium 页面，不连接真实交易账号。既有 `TestInstallPlaywrightRuntimeCancelsInstaller` 在 Windows 因 Unix 子进程脚本语义跳过，未改该测试。
- 前端：`npm run test:coverage --prefix frontend`，98 个测试文件、657 个测试通过，statements **80.87%**（5395/6671）。未修改前端代码或覆盖率配置。
- `go vet ./...`、`go build ./cmd/server`、Go/前端中文注释检查、架构检查、Go `tools/apicheck` 和 `git diff --check` 通过。未安装 golangci-lint，未执行该工具。
- `npm run api:check --prefix frontend` 受现有 Windows 工作区换行影响失败：生成 schema 是 LF，工作区是 13703 个 CRLF；归一换行后完全一致，且 Git HEAD 的 schema 与新生成结果逐字节一致。本修复没有改 API/schema，也没有绕过或修改门禁。
- race 因 `CGO_ENABLED=0` 且无 gcc/clang 未能运行；启动隔离服务器的本地 smoke 命令被执行策略拒绝，因此没有宣称服务器进程健康检查通过，也没有更换方式绕过限制。
- 真实账号/外部平台例外：生产订单及队列身份核对、历史求评价是否送达、真实 WebSocket 重投、平台 Token 过期恢复、买家侧实际收信与生产通知渠道送达均未验证。本地确定性业务测试没有为这些理由跳过。
- 遗留覆盖缺口分类：协调器原有异常持久化补偿分支属于确定性后续补测；`prepareRuleRun` 成功却返回 nil 运行的防御分支在当前具体实现下不可达；进程启动/race/其他数据库属于环境验证；真实交易送达属于外部平台验证。未降低门槛或删除测试。

覆盖率文件、临时数据库、测试日志和构建产物都是本地验证产物，不提交。
