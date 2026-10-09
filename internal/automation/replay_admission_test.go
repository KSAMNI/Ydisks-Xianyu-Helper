package automation

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// TestDeferredCanceledOrderStopsDelivery 验证 t 管理的已取消订单不能通过延期队列再次发卡。
func TestDeferredCanceledOrderStopsDelivery(t *testing.T) {
	// runID、sender、scheduler、store、cleanup 提供入队时仍待发货的旧运行；排队后通过仓储取消订单，不访问真实平台。
	runID, sender, scheduler, store, cleanup := newPaidRecoveryFixture(t, "pending_ship", "ws")
	defer cleanup()
	// ctx 是本地数据库及重放的测试生命周期。
	ctx := context.Background()
	// run、readErr 读取延期前已保存的付款事件快照。
	run, readErr := store.Automation.GetRun(ctx, runID)
	if readErr != nil {
		t.Fatal(readErr)
	}
	// task 保留原始明确卖家付款事件；此时订单已经取消。
	var task Task
	if decodeErr := json.Unmarshal([]byte(run.RawEventJSON), &task); decodeErr != nil { // decodeErr 阻止损坏夹具继续执行。
		t.Fatal(decodeErr)
	}
	task.Raw = map[string]any{"automation_run_id": runID, "automation_rule_id": run.RuleID}
	if deferErr := scheduler.center.deferTask(ctx, task, time.Now().Add(-time.Minute).Unix()); deferErr != nil { // deferErr 记录到期任务保存结果。
		t.Fatal(deferErr)
	}
	if cancelErr := store.Orders.Upsert(ctx, task.OrderID, db.OrderUpsertOpts{CookieID: task.AccountID, OrderStatus: "canceled"}); cancelErr != nil { // cancelErr 保证排队之后订单已被正常仓储更新为取消。
		t.Fatal(cancelErr)
	}
	if replayErr := scheduler.runDeferredTasks(ctx); replayErr != nil { // replayErr 记录队列收口错误。
		t.Fatal(replayErr)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("已取消订单通过延期队列发送了 %d 条卡密消息", len(sender.texts))
	}
}

// TestDeferredDisabledRuleStopsDelivery 验证 t 管理的已停用规则不能经已排队任务继续发送。
func TestDeferredDisabledRuleStopsDelivery(t *testing.T) {
	// runID、sender、scheduler、store、cleanup 提供等待发卡的有效订单及隔离资源。
	runID, sender, scheduler, store, cleanup := newPaidRecoveryFixture(t, "pending_ship", "ws")
	defer cleanup()
	// ctx 控制本地数据库及调度调用，不允许真实平台访问。
	ctx := context.Background()
	// run、readErr 读取停用前的运行身份和事件。
	run, readErr := store.Automation.GetRun(ctx, runID)
	if readErr != nil {
		t.Fatal(readErr)
	}
	// task 表示动作延迟队列中保留的原始付款事件。
	var task Task
	if decodeErr := json.Unmarshal([]byte(run.RawEventJSON), &task); decodeErr != nil { // decodeErr 阻止损坏快照进入重放。
		t.Fatal(decodeErr)
	}
	task.Raw = map[string]any{"automation_run_id": runID, "automation_rule_id": run.RuleID}
	if deferErr := scheduler.center.deferTask(ctx, task, time.Now().Add(-time.Minute).Unix()); deferErr != nil { // deferErr 记录到期任务保存结果。
		t.Fatal(deferErr)
	}
	if _, disableErr := store.DB.ExecContext(ctx, `UPDATE automation_rules SET enabled=0 WHERE id=?`, run.RuleID); disableErr != nil { // disableErr 保证规则确已停用。
		t.Fatal(disableErr)
	}
	if replayErr := scheduler.runDeferredTasks(ctx); replayErr != nil { // replayErr 记录重放最终状态保存错误。
		t.Fatal(replayErr)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("已停用规则仍通过延期队列发送了 %d 条消息", len(sender.texts))
	}
}

// TestPendingShipResumeRejectsManualSource 验证 t 管理的人工来源旧运行不能通过兜底改写来源后执行确认动作。
func TestPendingShipResumeRejectsManualSource(t *testing.T) {
	// runID、scheduler、store、cleanup 提供人工来源待发货运行及隔离数据库；发送器无需使用。
	runID, _, scheduler, store, cleanup := newPaidRecoveryFixture(t, "pending_ship", "manual")
	defer cleanup()
	// ctx 控制整个本地恢复测试。
	ctx := context.Background()
	// run、readErr 读取原始人工来源快照。
	run, readErr := store.Automation.GetRun(ctx, runID)
	if readErr != nil {
		t.Fatal(readErr)
	}
	// rule、ruleErr 提供被冻结的合法卡密动作。
	rule, ruleErr := store.Automation.Get(ctx, run.RuleID)
	if ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// task 是原始人工任务，不允许自动重放改写其来源。
	var task Task
	if decodeErr := json.Unmarshal([]byte(run.RawEventJSON), &task); decodeErr != nil { // decodeErr 检验夹具快照格式。
		t.Fatal(decodeErr)
	}
	task.ActionPlan = append(rule.Actions, db.AutomationAction{ActionType: ActionConfirmShipment, Enabled: true})
	// raw、marshalErr 保存带完整冻结计划的原始人工任务。
	raw, marshalErr := json.Marshal(task)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if _, stateErr := store.DB.ExecContext(ctx, `UPDATE automation_runs SET status='needs_review',action_cursor=1,action_started=0,raw_event_json=? WHERE id=?`, string(raw), runID); stateErr != nil { // stateErr 保证进入尾动作续跑分支。
		t.Fatal(stateErr)
	}
	// actions 仅统计恢复链是否允许到达外部动作边界，不发真实消息或平台请求。
	actions := 0
	scheduler.center.runs.executeAction = func(_ context.Context, _ Task, _ db.AutomationAction, _ shipmentDeliveryProof) (actionExecutionResult, error) { // 回调忽略实际内容并返回成功，测试只检查是否越过动作授权边界。
		actions++
		return actionExecutionResult{}, nil
	}
	scheduler.scanPendingShipResumes(ctx)
	if actions != 0 {
		t.Fatalf("人工来源运行被重建为调度来源并执行了 %d 个外部动作", actions)
	}
}
