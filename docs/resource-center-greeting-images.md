# 统一资源中心与商品首次回复图片

## 范围与兼容

- 侧边栏合并为一个“资源中心”入口，保留“商品模板 / 商品图片 / 消息图片”三个分类。
- `/app/library` 旧地址仍可访问，默认商品模板；`/app/image-library` 仍默认消息图片。两者均进入统一页面并高亮同一导航。
- 商品图片继续用于商品主图、规格图与模板发布；消息图片继续用于回复、图片卡密和发货模板。两套存储及已有引用不迁移、不复制，不改变原有归属和删除策略。
- 商品首次回复支持图片 URL 或资源中心已上传的消息图片，二者互斥。界面不增加文件上传或任意路径输入；允许纯图片回复，选择后可预览。
- 消息图片上传、改名、删除成功后通知全局选项刷新；本地列表请求可取消，旧响应不可覆盖新选项。无轮询或新增 MTOP 扫描。
- 编辑器取消、重新打开、账号切换不会带入旧图片草稿；旧保存响应不能关闭新编辑器。
- 既有 URL 规则保留；一次性发送、分段进度和不确定结果保护保持不变。
- 三方言迁移 `00065_item_greeting_local_image` 增加默认空值的 `reply_image_path`；SQLite 实测 64→65 升级后原 URL 和正文保持不变。
- 首次回复引用计入资源中心使用次数，包括停用规则；未显式确认强制删除时仍返回 409，沿用既有删除策略。

## 验证结果

前端 `npm run typecheck --prefix frontend`、`npm run comments:check --prefix frontend`、`npm run build --prefix frontend` 通过。
`npm run test:coverage --prefix frontend`：149 文件、976 测试通过，statement 93.78%。未排除新增业务代码或调整覆盖率阈值。

`RUN_BROWSER_INTEGRATION=1 go test ./internal/webui -run '^TestResourceCenterBrowserIntegration$' -count=1 -v -timeout=90s` 通过，实际加载嵌入 SPA，核对两个旧地址、分类切换、选图预览和真实 adapter 的纯图片保存请求；所有 API 使用本地夹具，无真实平台访问。

`RUN_BROWSER_INTEGRATION=1 go test -p 1 -coverprofile <scratch>/cover-browser.out ./internal/browser ./internal/webui -count=1 -timeout=600s`：webui 全包通过，statement 100%；browser statement 64.1%，但冻结 `TestReferencePlaywrightSliderBrowserIntegration` 失败，`duration_ms=1887` 超出现有墙钟范围。未修改冻结实现或放宽测试。

后端定向命令：`go test ./internal/application/itemgreeting ./internal/db ./internal/engine ./internal/server ./internal/adapter -run 'ItemGreeting|LocalImageDraft|ImageAsset|ImageLibrary|Migrate|Migration' -count=1 -timeout=180s` 全部通过，覆盖纯图/图文、来源互斥、路径拒绝、完整 CRUD、旧库升级、真实 handler 回读与删除保护、一次性成功与未知结果隔离、图片成功文字失败后的仅文字续发。

最终普通覆盖率命令：`go test -coverprofile <scratch>/cover-final.out ./... -count=1 -timeout=600s`（未设置 `RUN_BROWSER_INTEGRATION=1`），报告 Go statement **90.1%**，但命令失败，不代表全库通过。`adapter`、`application/itemgreeting`、`engine`、`server` 全包通过；首次回复 `normalizeDraft` 100%、商品匹配读取 `FindEnabledForItem` 100%。

未通过项与后端修改前的预检查一致：
- `cmd/server.TestLoadOrCreateDataKeyFailureBranches`：预期写权限错误，Windows 实际返回 nil。
- `internal/automation.TestPendingShipObservationResetsAfterOrderLeavesCandidates`：订单离开候选集合后观测记录仍为 1。
- `internal/db.TestSweepEmptyDatabaseHandlesZeroInputs`、`TestSweepWithEachTableMissing`：Windows 临时数据库清理时文件仍被占用。
- `tools/smoke.TestServerProcessStopGraceful`、`TestServerProcessStopTimeoutKills`、`TestServerProcessStopNonZeroExit`、`TestRunSuccessAgainstHelperServer`：终止信号不支持 Windows，并伴随临时目录清理失败。

最终 `go vet ./...`、`go build -o <scratch>/xianyu-server-final.exe ./cmd/server`、`go run ./tools/architecturecheck`、Go/TS 中文注释门禁、`npm run api:check --prefix frontend`、`go run ./tools/apicheck` 全部通过。最终生成类型与嵌入资源已同步。`git -c core.whitespace=cr-at-eol diff --check` 通过；默认 `git diff --check` 会把仓库原有 CRLF 文件的新增行末 CR 报为空白，保留各文件原换行格式，未更改 Git 设置。

覆盖边界：新增业务分支有聚焦回归；既有 DB 扫描/写入故障和首次回复依赖缺失、读取失败等防御分支未达到 100%，归为确定性覆盖补全，不归为真实账号例外。本次未扩大忽略、基线或覆盖率豁免。

## 环境与外部例外

- Windows PowerShell 7.6.9，Go 1.26.4，Node 22.17.0；没有 make，使用 Makefile 对应命令。
- 未配置 MySQL/PostgreSQL 测试 URL，三方言实库验证不可用；SQLite 使用隔离测试数据库，不迁移生产数据。
- CGO_ENABLED=0，未运行 race；未安装 golangci-lint。
- 未执行真实闲鱼账号登录、图片上传至平台或买家消息投递；这些属于真实账号/外部平台验证，不以本地夹具冒充实测。
- 不提交、不推送、不发布；保留原有未跟踪文件。
