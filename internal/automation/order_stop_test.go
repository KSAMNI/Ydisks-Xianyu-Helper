package automation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// stopRuntimeOrder 为 t 的本地场景持久停止订单，不调用真实服务或读取凭证。
func stopRuntimeOrder(t *testing.T, store *db.Store, task Task) {
	t.Helper()
	// err 保存停用标记插入失败，场景不能在未停用时继续断言。
	if _, err := store.DB.ExecContext(context.Background(), `INSERT INTO order_automation_stops(cookie_id,order_id,stopped_by) VALUES (?,?,1)`, task.AccountID, task.OrderID); err != nil {
		t.Fatal(err)
	}
}

// TestStoppedOrderBlocksTriggersRecoveryAndExternalActions 验证未来不同类型的任务、求评价扫描和外部动作均不再执行。
func TestStoppedOrderBlocksTriggersRecoveryAndExternalActions(t *testing.T) {
	// center、rule、task、sender、logs 提供真实 SQLite 自动化链路和可观测本地发送器。
	center, rule, task, sender, logs := reviewResultFixture(t)
	stopRuntimeOrder(t, center.store, task)
	for _, trigger := range []string{TriggerOrderPaid, TriggerOrderCreated, TriggerBuyerReviewed, TriggerReviewMissingTimeout, TriggerBargainPending} { // trigger 分别模拟发货、改价、赠品、求评价及免拼的新事件。
		// next 保留同一订单但更换实际业务类型。
		next := task
		next.TriggerType = trigger
		// err 验证停用被解释为正常跳过，而非继续发货或自动重试。
		if err := center.HandleTask(context.Background(), next); err != nil {
			t.Fatal(err)
		} // err 不把正常停用误报自动化失败。
	}
	// outcome、err 验证调度器直接执行规则也会安全跳过。
	outcome, err := center.runs.executeRuleWithResult(context.Background(), task, rule)
	if err != nil || outcome.Status != ruleExecutionSkipped || outcome.SkipReason != "order_stopped" {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	for _, action := range []string{ActionSendText, ActionSendCard, ActionSendTemplate, ActionConfirmShipment, ActionAdjustPrice} { // action 遍历共享执行器的全部交易动作。
		if _, err := center.executeAction(context.Background(), task, db.AutomationAction{ActionType: action}); !errors.Is(err, db.ErrOrderAutomationStopped) {
			t.Fatalf("action=%s err=%v", action, err)
		} // err 必须在取卡、模板或平台请求之前返回停用哨兵。
	}
	// err 验证独立免拼动作也会在调用平台前被停用门禁拦截。
	if err := center.actions.freeShipBargainAttempt(context.Background(), task, false); !errors.Is(err, db.ErrOrderAutomationStopped) {
		t.Fatalf("bargain=%v", err)
	} // err 验证独立免拼入口。
	if len(sender.texts) != 0 || strings.Contains(logs.String(), "自动化规则执行成功") {
		t.Fatal("停用订单仍执行或误报成功")
	}
}

// TestStopBetweenActionsPreservesFirstResultAndSkipsNext 验证动作一成功后停用，必须保存已有结果而不再执行动作二。
func TestStopBetweenActionsPreservesFirstResultAndSkipsNext(t *testing.T) {
	// center、rule、task、sender 提供两步消息动作的真实检查点链路。
	center, rule, task, sender, _ := reviewResultFixture(t)
	task.ActionPlan = []db.AutomationAction{{ActionType: ActionSendText, MessageTemplate: "第一步", Enabled: true}, {ActionType: ActionSendText, MessageTemplate: "第二步", Enabled: true}}
	// execute 是执行器原始能力，替身只在第一个动作返回后模拟用户提交停用。
	execute := center.runs.executeAction
	center.runs.executeAction = func(ctx context.Context, current Task, action db.AutomationAction, proof shipmentDeliveryProof) (actionExecutionResult, error) { // ctx/current/action/proof 保持原运行参数及已有发货凭证不变。
		// result、err 保存真实动作结果；只在确认成功后写入停用标记。
		result, err := execute(ctx, current, action, proof)
		if err == nil {
			stopRuntimeOrder(t, center.store, current)
		}
		return result, err
	}
	// outcome、err 验证停用发生在已执行动作之后时仍归类为安全停止。
	outcome, err := center.runs.executeRuleWithResult(context.Background(), task, rule)
	if err != nil || outcome.Status != ruleExecutionSkipped || len(sender.texts) != 1 {
		t.Fatalf("result=%+v sends=%d err=%v", outcome, len(sender.texts), err)
	}
	// run、readErr 验证已完成的第一步没有被回滚或错误重放。
	run, readErr := center.store.Automation.GetRun(context.Background(), outcome.RunID)
	if readErr != nil || run.Status != "canceled" || run.SentCount != 1 || run.ActionCursor != 1 || run.ActionStarted {
		t.Fatal("停用未保留第一步检查点或未关闭运行")
	}
}

// TestOrderStopStorageFailureFailsClosed 验证停用查询故障与未装配存储都不会放行订单动作。
func TestOrderStopStorageFailureFailsClosed(t *testing.T) {
	// center、rule、task、sender 是随后关闭数据库的隔离运行环境。
	center, rule, task, sender, _ := reviewResultFixture(t)
	// err 确保本测试确实关闭持久化连接以模拟存储故障。
	if err := center.store.DB.Close(); err != nil {
		t.Fatal(err)
	} // err 确保本场景实际进入存储不可用状态。
	if _, err := center.runs.executeRuleWithResult(context.Background(), task, rule); err == nil {
		t.Fatal("存储失败仍放行规则")
	} // err 应阻止规则准备。
	if err := center.HandleTask(context.Background(), task); err == nil {
		t.Fatal("存储失败仍放行事件")
	} // err 应阻止事件执行。
	// executor 缺少必要的订单停用仓储，不能把配置错误当作未停用。
	executor := automationActionExecutor{}
	// err 验证缺少必要存储依赖时不会放行订单动作。
	if err := executor.checkOrderAutomation(context.Background(), task); err == nil {
		t.Fatal("缺少存储仍放行订单")
	} // err 保留必需依赖错误。
	if err := executor.checkOrderAutomation(context.Background(), Task{}); err != nil {
		t.Fatal(err)
	} // err 验证无订单任务兼容分支。
	if len(sender.texts) != 0 {
		t.Fatal("查询失败仍发送消息")
	}
}

// stoppedRateRepository 注入评价前停用检查结果，其余行为使用现有可控账号任务夹具。
type stoppedRateRepository struct {
	// accountTaskFlowRepository 提供领取及收口等本地测试行为。
	*accountTaskFlowRepository
	// stopErr 控制订单停用检查的返回值。
	stopErr error
}

// CheckOrderAutomation 返回当前场景预设的停用或存储故障，不查询平台。
func (r stoppedRateRepository) CheckOrderAutomation(context.Context, string, string) error {
	return r.stopErr
}

// TestAutoRateHonorsStopAndQueryFailure 验证账号自动评价不会绕过整单停用或吞掉查询错误。
func TestAutoRateHonorsStopAndQueryFailure(t *testing.T) {
	for _, stopErr := range []error{db.ErrOrderAutomationStopped, errors.New("synthetic query failure")} { // stopErr 区分用户主动停用与数据库故障。
		// repository 是可领取的本地账号任务场景。
		repository := &accountTaskFlowRepository{value: "synthetic", dueOrderIDs: []string{"order"}, claimed: true}
		// center 复用现有夹具并注入本场景的停用端口。
		center := newAccountTaskFlowCoordinator(repository, &accountTaskFlowClient{rateErr: errors.New("must not reach platform")})
		center.repository = stoppedRateRepository{accountTaskFlowRepository: repository, stopErr: stopErr}
		// summary、err 验证停用按跳过收口，其余故障按错误返回。
		summary, err := center.runAutoRate(context.Background(), db.AccountTaskSettings{CookieID: "account", RateContent: "好评"})
		if errors.Is(stopErr, db.ErrOrderAutomationStopped) {
			if err != nil || summary.Skipped != 1 {
				t.Fatalf("skip=%+v err=%v", summary, err)
			}
		} else if !errors.Is(err, stopErr) {
			t.Fatalf("query error=%v", err)
		}
	}
}

// TestNeedsReviewNotificationContainsTaskOrderAndProgress 验证普通运行的人工通知明确包含任务、订单、编号和已确认动作数。
func TestNeedsReviewNotificationContainsTaskOrderAndProgress(t *testing.T) {
	// recorder 保存通知正文，所有外部发送均在本地替身中完成。
	recorder := &recordingNotifier{}
	// notifier 使用真实的人工处理消息格式和本地接收器。
	notifier := deliveryNotifier{current: func() Notifier { return recorder }} // 回调仅返回当前测试通知接收器。
	notifier.notifyResult(context.Background(), Task{AccountID: "account", OrderID: "order", TriggerType: TriggerReviewMissingTimeout}, 7, "needs_review", 2, "发送结果未知")
	notifier.notifyResult(context.Background(), Task{AccountID: "account", TriggerType: TriggerOrderPaid}, 8, "needs_review", 0, "缺少订单号")
	notifier.notifyResult(context.Background(), Task{AccountID: "account", OrderID: "order", TriggerType: TriggerOrderPaid}, 9, "canceled", 0, "")
	// messages 是两条人工处理通知，主动取消不产生新的失败告警。
	messages := recorder.messages()
	if len(messages) != 2 || !strings.Contains(messages[0], "任务：求评价") || !strings.Contains(messages[0], "订单：order") || !strings.Contains(messages[0], "运行编号：7") || !strings.Contains(messages[0], "已确认完成动作：2") || !strings.Contains(messages[1], "任务：付款发货") || !strings.Contains(messages[1], "尚未关联订单") {
		t.Fatal("人工通知缺少明确任务类型、订单或执行进度")
	}
}
