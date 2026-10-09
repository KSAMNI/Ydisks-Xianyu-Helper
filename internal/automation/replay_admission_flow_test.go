package automation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// replayTaskFromRun 为 t 的运行快照恢复 task；run 提供身份和原始 JSON，返回带延期标记的同身份任务。
func replayTaskFromRun(t *testing.T, run *db.AutomationRun) Task {
	t.Helper()
	// task 只存在于当前测试，不包含真实凭证或真实买家消息。
	var task Task
	if err := json.Unmarshal([]byte(run.RawEventJSON), &task); err != nil { // err 阻止损坏夹具继续测试业务行为。
		t.Fatal(err)
	}
	task.Raw = map[string]any{"automation_run_id": run.ID, "automation_rule_id": run.RuleID, "automation_deferred_replay": true}
	return task
}

// TestDeferredReplayAdmissionLifecycle 用 t 的确定性订单和队列覆盖终态取消、人工隔离、开关延期与合法成功。
func TestDeferredReplayAdmissionLifecycle(t *testing.T) {
	// scenarios 的 status 为本轮最新订单阶段；source 为原始来源；mutation 仅修改测试数据库，expectedRun、expectedQueue 是收口契约。
	scenarios := []struct {
		// name、status、source 说明每个独立的历史恢复场景。
		name, status, source string
		// mutation 是账号或规则的本地变更，空值不执行。
		mutation string
		// expectedRun、expectedQueue 保存预期持久化状态，空队列状态代表应删除任务。
		expectedRun, expectedQueue string
		// sends 为预期成功发送条数；review 表示是否必须发送独立人工处理通知。
		sends  int
		review bool
	}{
		{name: "合法付款", status: "pending_ship", source: "ws", expectedRun: "success", sends: 1},
		{name: "合法调度", status: "pending_ship", source: "scheduler", expectedRun: "success", sends: 1},
		{name: "取消订单", status: "canceled", source: "ws", expectedRun: "canceled"},
		{name: "完成订单", status: "completed", source: "ws", expectedRun: "canceled"},
		{name: "已发货订单", status: "shipped", source: "ws", expectedRun: "canceled"},
		{name: "缺少阶段", status: "unknown", source: "ws", expectedRun: "needs_review", expectedQueue: "dead_letter", review: true},
		{name: "人工来源", status: "pending_ship", source: "manual", expectedRun: "needs_review", expectedQueue: "dead_letter", review: true},
		{name: "关闭自动发货", status: "pending_ship", source: "ws", mutation: `UPDATE cookies SET auto_confirm=0 WHERE id='cid'`, expectedRun: "running", expectedQueue: "pending"},
		{name: "停用规则", status: "pending_ship", source: "ws", mutation: `UPDATE automation_rules SET enabled=0`, expectedRun: "needs_review", expectedQueue: "dead_letter", review: true},
	}
	// scenario 是本次要验证的队列收口分支。
	for _, scenario := range scenarios {
		// t 为当前场景提供独立数据库和断言上下文。
		t.Run(scenario.name, func(t *testing.T) {
			// runID、sender、scheduler、store、cleanup 隔离队列、发送记录与持久化生命周期。
			runID, sender, scheduler, store, cleanup := newPaidRecoveryFixture(t, scenario.status, scenario.source)
			defer cleanup()
			// ctx 控制所有本地数据库操作。
			ctx := context.Background()
			// run、err 读取原始待执行快照。
			run, err := store.Automation.GetRun(ctx, runID)
			if err != nil {
				t.Fatal(err)
			}
			// task 是测试将写入延期队列的旧付款任务。
			task := replayTaskFromRun(t, run)
			// probe 记录独立人工处理通知，保持实际通知器接口契约。
			probe := &triggerAwareNotificationProbe{}
			scheduler.center = NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{Notifier: probe})
			if err := scheduler.center.deferTask(ctx, task, time.Now().Add(-time.Minute).Unix()); err != nil { // err 检查队列夹具是否落库。
				t.Fatal(err)
			}
			if scenario.mutation != "" {
				// err 保证停用设置实际生效。
				if _, err := store.DB.ExecContext(ctx, scenario.mutation); err != nil {
					t.Fatal(err)
				}
			}
			// err 不应把成功隔离当作存储失败。
			if err := scheduler.runDeferredTasks(ctx); err != nil {
				t.Fatal(err)
			}
			run, err = store.Automation.GetRun(ctx, runID)
			if err != nil {
				t.Fatal(err)
			}
			if run.Status != scenario.expectedRun || len(sender.texts) != scenario.sends {
				t.Fatalf("运行状态=%s，发送数=%d", run.Status, len(sender.texts))
			}
			// queueState 聚合任务状态，空值表示已安全删除而非死信。
			var queueState string
			// err 记录队列断言查询错误。
			if err := store.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(status),'') FROM automation_pending_tasks`).Scan(&queueState); err != nil {
				t.Fatal(err)
			}
			if queueState != scenario.expectedQueue {
				t.Fatalf("队列状态=%s", queueState)
			}
			if scenario.review && probe.manualCalls == 0 {
				t.Fatal("人工处理必须发送独立通知")
			}
			if !scenario.review && probe.manualCalls != 0 {
				t.Fatal("合法或终态取消不得误报人工处理")
			}
		})
	}
}

// TestReplayActionRechecksAfterPreparation 验证 t 中慢准备结束前发生的订单取消或规则停用不能越过第二次准入。
func TestReplayActionRechecksAfterPreparation(t *testing.T) {
	// scenarios 保留取消与停用各自不同的收口策略。
	scenarios := []struct {
		// name、mutation、status 分别为场景名、准备结束时执行的 SQL 和预期运行状态。
		name, mutation, status string
	}{
		{"准备期间取消", `UPDATE orders SET order_status='canceled'`, "canceled"},
		{"准备期间停用", `UPDATE automation_rules SET enabled=0`, "needs_review"},
	}
	// scenario 为当前准备期间的状态变更。
	for _, scenario := range scenarios {
		// t 提供独立慢准备模拟和最终无副作用断言。
		t.Run(scenario.name, func(t *testing.T) {
			// runID、sender、scheduler、store、cleanup 保存可合法入队的付款任务环境。
			runID, sender, scheduler, store, cleanup := newPaidRecoveryFixture(t, "pending_ship", "ws")
			defer cleanup()
			// ctx 是本次本地重放上下文。
			ctx := context.Background()
			// run、err 读取任务快照。
			run, err := store.Automation.GetRun(ctx, runID)
			if err != nil {
				t.Fatal(err)
			}
			// prepare 保留真实准备实现；替身只在它完成后注入确定性状态变更。
			prepare := scheduler.center.runs.prepareTask
			scheduler.center.runs.prepareTask = func(ctx context.Context, task Task) (Task, error) { // ctx、task 原样传递；返回已准备任务和实际失败，不绕过原业务准备。
				// prepared、err 保存真实准备结果。
				prepared, err := prepare(ctx, task)
				if err != nil {
					return prepared, err
				}
				_, err = store.DB.ExecContext(ctx, scenario.mutation)
				return prepared, err
			}
			// err 确认延期任务落库。
			if err := scheduler.center.deferTask(ctx, replayTaskFromRun(t, run), 0); err != nil {
				t.Fatal(err)
			}
			// err 确认安全收口没有存储异常。
			if err := scheduler.runDeferredTasks(ctx); err != nil {
				t.Fatal(err)
			}
			run, err = store.Automation.GetRun(ctx, runID)
			if err != nil {
				t.Fatal(err)
			}
			if len(sender.texts) != 0 || run.Status != scenario.status {
				t.Fatalf("发送=%d，状态=%s", len(sender.texts), run.Status)
			}
		})
	}
}

// TestPaidReplayOrderRejectsMissingAndMismatchedFacts 覆盖 t 中来源、身份、缺失订单及查询取消，不允许错误退化为可执行。
func TestPaidReplayOrderRejectsMissingAndMismatchedFacts(t *testing.T) {
	// runID、scheduler、store、cleanup 提供完整且可信的初始快照。
	runID, _, scheduler, store, cleanup := newPaidRecoveryFixture(t, "pending_ship", "ws")
	defer cleanup()
	// ctx 只用于本地事实读取。
	ctx := context.Background()
	// run、err 取得未被修改的原始任务。
	run, err := store.Automation.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	// original 是各场景复制的可信快照。
	original := replayTaskFromRun(t, run)
	// variations 分别只改变一个身份字段，避免多个错误条件互相遮盖。
	variations := []Task{original, original, original, original, original, original, original}
	variations[0].Source = "manual"
	variations[1].OrderRole = OrderRoleBuyer
	variations[2].AccountID = "other"
	variations[3].ItemID = "other"
	variations[4].BuyerID = "other"
	variations[5].ChatID = "other"
	variations[6].OrderID = "missing"
	// index、task 分别是单字段错误序号及待检查快照。
	for index, task := range variations {
		// order、reason、err 证明拒绝时不返回可继续执行的订单。
		order, reason, err := paidReplayOrder(ctx, store, task)
		if err != nil || order != nil || reason == "" {
			t.Fatalf("错误身份 %d 未安全拒绝", index)
		}
	}
	// canceled、cancel 构造确定性取消，不依赖数据库网络故障。
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	// err 应保留上下文取消分类。
	if _, _, err := paidReplayOrder(canceled, store, original); !errors.Is(err, context.Canceled) {
		t.Fatal("查询取消必须向上传播")
	}
	// stopped、deferred、err 验证缺失运行进入人工处理，而非重新创建并发卡。
	original.Raw["automation_run_id"] = 999999
	// stopped、deferred、err 分别记录缺失运行是否停止、是否重排与人工处理错误。
	stopped, deferred, err := scheduler.center.guardDeferredReplay(ctx, original)
	if !stopped || deferred || !errors.Is(err, errReplayNeedsReview) {
		t.Fatal("缺失运行未停止")
	}
}
