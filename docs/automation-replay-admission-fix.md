# 自动化历史重放准入修复（2026-10-09）

## 修复范围

1. 延期付款任务在写回历史事件前核对当前订单状态、身份及来源。已取消、完成、发货的订单取消旧运行；缺失或不一致的事实进入人工处理，不复活订单。
2. 延期运行和实际动作开始前均检查规则启用状态。停用规则隔离旧运行；账号自动发货总开关关闭时保留延期任务，等待重新开启。详情获取期间发生的取消/停用同样阻止发送。
3. 待发货尾动作保留原始来源、角色、身份和冻结计划；人工来源、损坏快照或不可信阶段不得改写成 scheduler 来源。运行和延期任务的人工处理路径发送独立通知；存储失败也不会静默放行。
4. `.gitattributes` 将生成的 OpenAPI schema 固定为 LF。仍保留原有字节级漂移检查；通过真实 Git 临时仓库验证 `core.autocrlf=false/true` 下的检出一致性。

空闲运行隔离使用运行 ID、状态、尝试代次、游标、租约和未开始动作的 CAS；陈旧扫描不能覆盖正在发送或已换代的运行。不可安全重放的延期任务以领取代次校验后进入死信，原快照保留供人工处理。

## 测试与兼容

- 三项审查复现已转为正式回归，先验证失败，再修复通过。
- 新增终态订单、可信/人工来源、损坏/身份冲突快照、停用规则、开关关闭、慢准备期间状态变化、查询/取消/收口故障、独立通知和陈旧 CAS 场景。
- 两个历史正向夹具补全其原本声称代表的待发货状态与可信调度来源；既有业务断言未削弱。负向测试明确拒绝缺失状态和来源。
- 新文件与语义修改均补齐中文声明注释。未增加或放宽注释/架构 baseline。与恢复相关的 `isDeferredReplay` 移入新准入文件，保持中心文件 800 行门禁。
- 保留工作区起始已有的日志诊断、只读准入、唯一订单关联、模板 UI 与嵌入资源修改。没有修改冻结 CAPTCHA、API 契约、数据库 schema 或三方言迁移编号。

## 本轮验证

| 命令/验证 | 结果 |
| --- | --- |
| `go test ./... -count=1 -timeout=600s '-coverprofile=.planning/fix-review-20261009/cover-final.out'` | 全量通过，未设置 `RUN_BROWSER_INTEGRATION=1`；Go statement **81.5%** |
| `go tool cover '-func=.planning/fix-review-20261009/cover-final.out'` | 新增 `replay_admission.go`、`automation_replay_admission.go` 全部函数 statement **100%** |
| `$env:RUN_BROWSER_INTEGRATION='1'; go test ./internal/browser -count=1 -timeout=600s '-coverprofile=.planning/fix-review-20261009/cover-browser.out'` | 通过；browser 包独立 statement **64.1%**，不是全仓覆盖率 |
| `npm run test:coverage --prefix frontend` | 99 文件、659 测试通过；statement **80.87%**、branch **60.65%** |
| `go run ./tools/architecturecheck`、`go run ./tools/apicheck`、`go run ./tools/commentlint -mode check -root .`、`go vet ./...` | 全部通过 |
| `npm run comments:check --prefix frontend`、`npm run typecheck --prefix frontend`、`npm run api:check --prefix frontend` | 全部通过 |
| `npm run build --prefix frontend` | 通过，嵌入资源已重建 |
| `go build -o .planning/fix-review-20261009/xianyu-server.exe ./cmd/server` | 通过 |
| 临时空库、`127.0.0.1:59220` 启动 | 未使用 `-no-browser`；Chromium 就绪，`/health` HTTP 200；Ctrl+C 后 HTTP/application 均收束，端点不可达 |
| `git diff --check` | 使用仓库实际换行配置通过 |

## 未执行与覆盖率边界

- 本机 `CGO_ENABLED=0`，未发现 C 编译器和 golangci-lint：未执行 race 或 golangci-lint；没有安装工具或改动全局环境。
- 未执行 MySQL/PostgreSQL 实库回归；新增 SQL 只使用已有三方言支持的条件更新，无 schema 变化。
- 未访问真实闲鱼账号：QR 登录、WS/MTOP 真实发卡及确认发货、真实风控/CAPTCHA、外部通知渠道与第三方卡密服务均未进行真实平台验证。本地浏览器、SQLite、假发送器及通知替身行为已验证。
- 未执行跨平台安装包/签名、CI 远端发布或镜像发布；未提交或推送。
- 全仓剩余未覆盖代码仍包含既有确定性分支、平台/系统环境分支及真实账号外部依赖；上述 100% 仅指本次新增的准入和收口生产函数，不代表全仓无遗漏。
- 覆盖率、临时数据库、日志和构建文件仅为验证产物，不应提交。
