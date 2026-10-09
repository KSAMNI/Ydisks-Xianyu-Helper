package automation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// reviewResultFixture 为 t 建立到期求评价订单、规则、记录发送器和 JSON 日志；所有资源由 t.Cleanup 释放。
// 返回中心、规则、任务、发送器和日志缓冲，不读取真实平台状态。
func reviewResultFixture(t *testing.T) (*Center, db.AutomationRule, Task, *testSender, *bytes.Buffer) {
	t.Helper()
	// store、cleanup 提供隔离 SQLite 数据库，避免测试修改真实历史订单。
	store, cleanup := newAutomationTestStore(t)
	t.Cleanup(cleanup)
	// ctx 是本地数据准备的取消根。
	ctx := context.Background()
	// admin、adminErr 提供求评价规则的管理用户归属。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// ruleID、createErr 建立发货一小时后求评价一次的发送规则。
	ruleID, createErr := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: admin.ID, CookieID: "cid", Name: "结果回归", TriggerType: TriggerReviewMissingTimeout, Enabled: true,
		ConfigJSON: `{"after_shipped_hours":1,"max_attempts":1}`, Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "请评价", Enabled: true}}})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// rule、ruleErr 加载真实动作配置，测试不绕过动作计划器。
	rule, ruleErr := store.Automation.Get(ctx, ruleID)
	if ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// task 对应第一轮到期求评价，身份字段全部来自测试数据。
	task := Task{Source: "scheduler", AccountID: "cid", TriggerType: TriggerReviewMissingTimeout, OrderID: "review-result", ItemID: "item", ChatID: "chat", BuyerID: "buyer", Raw: map[string]any{"attempt": 1}}
	// insertErr 创建已经发货但尚未评价的候选订单。
	if _, insertErr := store.DB.ExecContext(ctx, `INSERT INTO orders(order_id,cookie_id,item_id,chat_id,buyer_id,system_shipped,shipped_at) VALUES (?,?,?,?,?,1,?)`, task.OrderID, task.AccountID, task.ItemID, task.ChatID, task.BuyerID, time.Now().UTC().Add(-2*time.Hour).Format("2006-01-02 15:04:05")); insertErr != nil {
		t.Fatal(insertErr)
	}
	// sender 捕获外部发送次数；logs 只供单线程测试读取，不与后台 worker 共享。
	sender := &testSender{}
	// logs 保存 Debug 及以上日志，以验证幂等跳过不会误报成功。
	logs := &bytes.Buffer{}
	return NewWithDependencies(store, testSenderProvider{sender: sender}, slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})), CenterDependencies{Notifier: &recordingNotifier{}}), *rule, task, sender, logs
}

// TestReviewRunResultClassifiesCurrentInvocation 用 t 验证成功、延期、错误和安全隔离必须反映本次调用而不是历史记录。
func TestReviewRunResultClassifiesCurrentInvocation(t *testing.T) {
	// scenarios 明确区分可证实未发送与发送结果不确定；本地计数/终态写入失败也不得报告成功。
	scenarios := []struct {
		// name 标识场景；want 是预期结构化状态。
		name string
		// want 约束当前调用的结果分类。
		want ruleExecutionStatus
		// sendErr 模拟消息发送器返回值，不访问平台。
		sendErr error
		// delay 是动作延迟秒数；trigger 是测试专用 SQLite 故障注入。
		delay int
		// trigger 阻止计数或成功终态落库，验证补偿后才决定结果。
		trigger string
	}{
		{name: "真实成功", want: ruleExecutionSucceeded},
		{name: "明确未发送", want: ruleExecutionFailed, sendErr: ErrMessageNotSent},
		{name: "发送结果未知", want: ruleExecutionNeedsReview, sendErr: errors.New("测试传输结果未知")},
		{name: "动作延期", want: ruleExecutionDeferred, delay: 60},
		{name: "计数写入失败", want: ruleExecutionNeedsReview, trigger: `CREATE TRIGGER reject_counter BEFORE UPDATE OF review_request_count ON orders BEGIN SELECT RAISE(FAIL,'test counter failure'); END`},
		{name: "成功终态写入失败", want: ruleExecutionNeedsReview, trigger: `CREATE TRIGGER reject_success BEFORE UPDATE OF status ON automation_runs WHEN NEW.status='success' BEGIN SELECT RAISE(FAIL,'test finish failure'); END`},
	}
	// scenario 表示本次要复现的执行或持久化结果。
	for _, scenario := range scenarios {
		// t 管理该场景的独立数据库和错误断言。
		t.Run(scenario.name, func(t *testing.T) {
			// center、rule、task、sender、logs 共用隔离的本地执行链路。
			center, rule, task, sender, logs := reviewResultFixture(t)
			sender.err = scenario.sendErr
			rule.Actions[0].DelaySeconds = scenario.delay
			if scenario.trigger != "" {
				// triggerErr 保存确定性数据库故障注入错误。
				if _, triggerErr := center.store.DB.ExecContext(context.Background(), scenario.trigger); triggerErr != nil {
					t.Fatal(triggerErr)
				}
			}
			// result、executeErr 是当前调用完成持久化补偿后的真实结果。
			result, executeErr := center.runs.executeRuleWithResult(context.Background(), task, rule)
			if result.Status != scenario.want || result.RunID == 0 {
				t.Fatalf("执行结果不符: %+v err=%v", result, executeErr)
			}
			if (executeErr == nil) != (scenario.want == ruleExecutionSucceeded) {
				t.Fatalf("错误与状态不一致: %+v err=%v", result, executeErr)
			}
			if scenario.want != ruleExecutionSucceeded && strings.Contains(logs.String(), "自动化规则执行成功") {
				t.Fatal("非成功结果误报实际执行成功")
			}
			if scenario.want == ruleExecutionSucceeded {
				if result.SentCount != 1 || len(sender.texts) != 1 {
					t.Fatalf("动作数不符: %+v messages=%d", result, len(sender.texts))
				}
				// skipped、skipErr 验证同一轮次的第二次调用不冒用第一次的 success。
				skipped, skipErr := center.runs.executeRuleWithResult(context.Background(), task, rule)
				if skipErr != nil || skipped.Status != ruleExecutionSkipped || skipped.RunID != result.RunID || skipped.SkipReason != "success" || skipped.SentCount != 0 {
					t.Fatalf("重复调用结果不符: %+v err=%v", skipped, skipErr)
				}
			}
		})
	}
}

// TestReviewSchedulerBlockedOrdersDoNotPrepareOrReportSuccess 用 t 复现实机七个订单连续七十轮扫描，不准备、不写入、不发送、不误报成功。
func TestReviewSchedulerBlockedOrdersDoNotPrepareOrReportSuccess(t *testing.T) {
	// center、rule、task、sender、logs 提供隔离环境及真实调度日志。
	center, rule, task, sender, logs := reviewResultFixture(t)
	// ctx 是所有扫描的根上下文；本测试不启动后台协程。
	ctx := context.Background()
	// statuses 覆盖实机未知的各种可能阻塞终态，不能推定七个订单都是同一种失败。
	statuses := []string{"success", "needs_review", "failed", "cancelled", "running", "needs_review", "success"}
	// index、status 分别为订单后缀和要预置的阻塞运行状态。
	for index, status := range statuses {
		// orderID 是每笔独立求评价订单的业务标识。
		orderID := task.OrderID
		if index > 0 {
			orderID = fmt.Sprintf("review-blocked-%d", index)
			// insertErr 从模板复制候选订单；计数保持零以模拟历史运行与计数不一致。
			if _, insertErr := center.store.DB.ExecContext(ctx, `INSERT INTO orders(order_id,cookie_id,item_id,chat_id,buyer_id,system_shipped,shipped_at) SELECT ?,cookie_id,item_id,chat_id,buyer_id,system_shipped,shipped_at FROM orders WHERE order_id=?`, orderID, task.OrderID); insertErr != nil {
				t.Fatal(insertErr)
			}
		}
		// runID、started、startErr 建立对应提醒轮次的唯一运行。
		runID, started, startErr := center.store.Automation.TryStartRun(ctx, db.AutomationRun{RuleID: rule.ID, CookieID: task.AccountID, OrderID: orderID, TriggerType: task.TriggerType, TriggerKey: "review_missing_timeout:" + orderID + ":1"})
		if startErr != nil || !started {
			t.Fatalf("预置运行失败: %v", startErr)
		}
		// updateErr 设置阻塞状态；failed 的重试预算耗尽，running 的租约仍有效。
		if _, updateErr := center.store.DB.ExecContext(ctx, `UPDATE automation_runs SET status=?,attempt_count=3,lease_expires_at=? WHERE id=?`, status, time.Now().UTC().Add(time.Hour).Unix(), runID); updateErr != nil {
			t.Fatal(updateErr)
		}
	}
	// prepareCount 仅由本测试单线程闭包递增，用于证明预检发生在任务准备之前。
	prepareCount := 0
	// prepareTask 在被错误调用时立即产生可见错误；ctx、task 是本次准备的取消域和事件。
	center.runs.prepareTask = func(ctx context.Context, task Task) (Task, error) {
		prepareCount++
		return task, errors.New("不应准备被阻塞的历史求评价")
	}
	// writeErr 安装只监视运行表 INSERT 的触发器，证明预检不再尝试写入同一幂等运行。
	if _, writeErr := center.store.DB.ExecContext(ctx, `CREATE TRIGGER reject_repeat_insert BEFORE INSERT ON automation_runs BEGIN SELECT RAISE(FAIL,'unexpected run insert'); END`); writeErr != nil {
		t.Fatal(writeErr)
	}
	// scheduler 逐轮执行真实分页、账号门禁和规则匹配。
	scheduler := NewScheduler(center)
	// scanIndex 表示模拟的分钟扫描轮次，不实际等待七十分钟。
	for scanIndex := 0; scanIndex < 70; scanIndex++ {
		scheduler.scan(ctx)
	}
	if prepareCount != 0 || len(sender.texts) != 0 {
		t.Fatalf("产生额外准备或发送: prepares=%d sends=%d", prepareCount, len(sender.texts))
	}
	if strings.Contains(logs.String(), "求评价计划任务执行成功") || strings.Contains(logs.String(), "unexpected run insert") {
		t.Fatal("重复扫描误报成功或尝试写入运行")
	}
	if strings.Count(logs.String(), "求评价计划任务已跳过") != 490 {
		t.Fatalf("跳过次数不符: %d", strings.Count(logs.String(), "求评价计划任务已跳过"))
	}
}

// TestReviewPreflightDoesNotReplaceAtomicClaim 用 t 模拟准入通过后被竞争者抢占，仍只能返回跳过且不发送。
func TestReviewPreflightDoesNotReplaceAtomicClaim(t *testing.T) {
	// center、rule、task、sender 是竞争窗口的本地执行环境；日志在此无需断言。
	center, rule, task, sender, _ := reviewResultFixture(t)
	// prepare 是原有任务准备函数，保留订单与凭证准备语义。
	prepare := center.runs.prepareTask
	// ctx、task 是准备入口收到的当前调用数据；闭包在准备阶段模拟另一个 worker 原子领取。
	center.runs.prepareTask = func(ctx context.Context, task Task) (Task, error) {
		// started、startErr 确认模拟竞争者在预检与实际领取之间获胜。
		_, started, startErr := center.store.Automation.TryStartRun(ctx, db.AutomationRun{RuleID: rule.ID, CookieID: task.AccountID, OrderID: task.OrderID, TriggerType: task.TriggerType, TriggerKey: buildTriggerKey(task), LeaseExpiresAt: time.Now().Add(time.Hour).Unix()})
		if startErr != nil || !started {
			t.Fatalf("模拟竞争领取失败: %v", startErr)
		}
		return prepare(ctx, task)
	}
	// result、executeErr 反映原子领取败者的本次结果，不从获胜者的数据库状态推断成功。
	result, executeErr := center.runs.executeRuleWithResult(context.Background(), task, rule)
	if executeErr != nil || result.Status != ruleExecutionSkipped || result.SkipReason != "idempotency_guard" || len(sender.texts) != 0 {
		t.Fatalf("竞争败者产生副作用: %+v err=%v", result, executeErr)
	}
}

// TestReviewAdmissionReadFailureStopsPreparation 用 t 验证查询失败时默认关闭执行，而不是把无法读取当成新运行。
func TestReviewAdmissionReadFailureStopsPreparation(t *testing.T) {
	// center、rule、task、sender 是本地执行环境；日志无需额外断言。
	center, rule, task, sender, _ := reviewResultFixture(t)
	// ctx、cancel 提供确定性的取消查询错误。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// ctx、task 为被禁止调用的准备输入；取消必须在前置查询处停止。
	center.runs.prepareTask = func(ctx context.Context, task Task) (Task, error) {
		t.Fatal("准入失败后仍准备任务")
		return task, nil
	}
	// result、executeErr 是被取消的只读准入结果。
	result, executeErr := center.runs.executeRuleWithResult(ctx, task, rule)
	if executeErr == nil || result.Status != ruleExecutionFailed || len(sender.texts) != 0 {
		t.Fatalf("准入失败未关闭执行: %+v err=%v", result, executeErr)
	}
}

// TestReviewResultFutureAttemptAndOtherRuleRemainEligible 用 t 验证只读准入不跨规则、不跨提醒轮次阻塞合法的新运行。
func TestReviewResultFutureAttemptAndOtherRuleRemainEligible(t *testing.T) {
	// center、rule、task、sender 是本地求评价执行链路，日志在此不参与判断。
	center, rule, task, sender, _ := reviewResultFixture(t)
	// ctx 控制本次三个独立幂等运行。
	ctx := context.Background()
	// first、firstErr 完成规则原始轮次。
	first, firstErr := center.runs.executeRuleWithResult(ctx, task, rule)
	if firstErr != nil || first.Status != ruleExecutionSucceeded {
		t.Fatalf("初始执行失败: %v", firstErr)
	}
	task.Raw = map[string]any{"attempt": 2}
	// next、nextErr 完成下一业务轮次；到期与最大次数仍由 scheduler 的配置门禁判断。
	next, nextErr := center.runs.executeRuleWithResult(ctx, task, rule)
	if nextErr != nil || next.Status != ruleExecutionSucceeded || next.RunID == first.RunID {
		t.Fatalf("下一轮次被错误阻断: %+v err=%v", next, nextErr)
	}
	// admin、adminErr 提供第二条独立规则的归属。
	admin, adminErr := center.store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// otherID、createErr 创建另一条规则并复用相同业务事件键。
	otherID, createErr := center.store.Automation.Create(ctx, db.AutomationRuleInput{UserID: admin.ID, CookieID: "cid", Name: "独立规则", TriggerType: TriggerReviewMissingTimeout, Enabled: true, Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "独立提醒", Enabled: true}}})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// other、readErr 加载独立规则及动作。
	other, readErr := center.store.Automation.Get(ctx, otherID)
	if readErr != nil {
		t.Fatal(readErr)
	}
	// separate、separateErr 验证不同规则不借用同一运行也不被其阻断。
	separate, separateErr := center.runs.executeRuleWithResult(ctx, task, *other)
	if separateErr != nil || separate.Status != ruleExecutionSucceeded || len(sender.texts) != 3 {
		t.Fatalf("不同规则准入错误: %+v err=%v sends=%d", separate, separateErr, len(sender.texts))
	}
}

// TestReviewSchedulerSuccessLogHasActualRun 用 t 验证调度成功日志必须携带本次运行主键和动作数，最大次数之后不再重复报告。
func TestReviewSchedulerSuccessLogHasActualRun(t *testing.T) {
	// center、sender、logs 观察真实调度器的发送与日志；规则和任务由数据库扫描取得。
	center, _, _, sender, logs := reviewResultFixture(t)
	// scheduler 通过单线程 scan 触发两轮扫描，不启动后台 worker。
	scheduler := NewScheduler(center)
	scheduler.scan(context.Background())
	scheduler.scan(context.Background())
	if len(sender.texts) != 1 || strings.Count(logs.String(), "求评价计划任务执行成功") != 1 {
		t.Fatal("调度成功或发送次数不符")
	}
	// line 是当前 JSON 日志，只有调度成功行需要同时包含真实执行摘要。
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, "求评价计划任务执行成功") && (!strings.Contains(line, `"run_id":1`) || !strings.Contains(line, `"sent_count":1`) || !strings.Contains(line, `"attempt":1`)) {
			t.Fatal("成功日志没有关联真实运行")
		}
	}
}

// TestReviewAdmissionWithoutStableKeyUsesExistingGuard 用 t 验证缺少订单与事件标识时不误查询其他运行，仍由准备后的旧防重门禁安全跳过。
func TestReviewAdmissionWithoutStableKeyUsesExistingGuard(t *testing.T) {
	// center、rule、task 是隔离环境；发送器和日志无需在只读准入测试中使用。
	center, rule, task, _, _ := reviewResultFixture(t)
	task.OrderID = ""
	// admission、readErr 验证不存在稳定键时不适用准备前优化，避免破坏旧任务补全规则。
	admission, readErr := center.runs.reviewRunAdmission(context.Background(), task, rule)
	if readErr != nil || admission != nil {
		t.Fatalf("无键任务预检不符: %+v err=%v", admission, readErr)
	}
}
