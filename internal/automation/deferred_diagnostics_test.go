package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestDeferredRoleEvidenceDiagnosticsAndDeadLetter 用 t 验证缺少字段的事件保持阻断，精确原因持久化，第五次失败仅通知一次。
func TestDeferredRoleEvidenceDiagnosticsAndDeadLetter(t *testing.T) {
	// cases 将缺商品号、商品归属未同步、缺会话、无待发货候选和订单未同步分别诊断。
	cases := []struct {
		// name、reason 分别是子测试名和预期稳定原因码。
		name, reason string
		// task 是无真实凭证的测试交易事件。
		task Task
	}{
		{name: "缺商品号", reason: "missing_item_id", task: Task{TriggerType: TriggerOrderCreated}},
		{name: "缺商品事实", reason: "missing_local_item", task: Task{TriggerType: TriggerOrderCreated, ItemID: "item"}},
		{name: "缺会话标识", reason: "missing_chat_id", task: Task{TriggerType: TriggerOrderPaid}},
		{name: "无待发货候选", reason: "no_pending_order_candidate", task: Task{TriggerType: TriggerOrderPaid, ChatID: "chat"}},
		{name: "缺订单事实", reason: "missing_local_order", task: Task{TriggerType: TriggerOrderPaid, OrderID: "missing-order"}},
	}
	// scenario 是本轮验证的字段或本地事实缺失场景。
	for _, scenario := range cases {
		// t 管理独立数据库及通知断言。
		t.Run(scenario.name, func(t *testing.T) {
			// store、cleanup 提供隔离的延期队列和订单表。
			store, cleanup := newAutomationTestStore(t)
			defer cleanup()
			// ctx 是队列写入和模拟重试的取消根。
			ctx := context.Background()
			// notifier 记录独立人工处理通知；sender 验证没有外部消息动作。
			notifier := &triggerAwareNotificationProbe{}
			// sender 仅记录本地模拟发送，缺失卖家证据时必须始终为空。
			sender := &testSender{}
			// logs 保存结构化诊断；仅本测试线程访问。
			logs := &bytes.Buffer{}
			// center 使用真实卖家门禁与延期调度，替换外部通知和发送依赖。
			center := NewWithDependencies(store, testSenderProvider{sender: sender}, slog.New(slog.NewJSONHandler(logs, nil)), CenterDependencies{Notifier: notifier})
			// task 固定来源、账号和合成敏感字段，验证日志与快照不输出 Cookie 或原始正文。
			task := scenario.task
			task.Source, task.AccountID, task.CookieStr, task.Text = "ws", "cid", "TEST-COOKIE-DO-NOT-LOG", "TEST-RAW-DO-NOT-LOG"
			// handleErr 保存首次门禁延期错误，不足证据不是立即丢弃。
			if handleErr := center.HandleTask(ctx, task); handleErr != nil {
				t.Fatal(handleErr)
			}
			// scheduler 仅手动驱动队列，不启动后台 goroutine。
			scheduler := NewScheduler(center)
			// attempt 模拟既有五次领取上限，绕过等待但不改变退避算法。
			for attempt := 1; attempt <= 5; attempt++ {
				// dueErr 只让本地测试队列到期，不改变状态和领取代次。
				if _, dueErr := store.DB.ExecContext(ctx, `UPDATE automation_pending_tasks SET due_at=0`); dueErr != nil {
					t.Fatal(dueErr)
				}
				// replayErr 保存单轮队列处理错误，业务失败由队列记录而非全局返回。
				if replayErr := scheduler.runDeferredTasks(ctx); replayErr != nil {
					t.Fatal(replayErr)
				}
			}
			// status、reason 保存最后一次失败的死信状态与精确核验原因。
			var status, reason string
			// stateErr 验证人工处理队列保留审计而不是静默删除事件。
			if stateErr := store.DB.QueryRowContext(ctx, `SELECT status,error_message FROM automation_pending_tasks`).Scan(&status, &reason); stateErr != nil {
				t.Fatal(stateErr)
			}
			if status != "dead_letter" || !strings.Contains(reason, scenario.reason) || notifier.manualCalls != 1 || len(sender.texts) != 0 {
				t.Fatalf("安全收口不符: status=%s reason=%s notifications=%d sends=%d", status, reason, notifier.manualCalls, len(sender.texts))
			}
			// replayErr 验证死信不会被第六轮再次领取或再次通知。
			if replayErr := scheduler.runDeferredTasks(ctx); replayErr != nil {
				t.Fatal(replayErr)
			}
			if notifier.manualCalls != 1 {
				t.Fatal("死信重复通知")
			}
			// output 是待检查的 JSON 日志，不向失败输出回显完整日志和敏感值。
			output := logs.String()
			if !strings.Contains(output, `"reason":"`+scenario.reason+`"`) || !strings.Contains(output, `"attempt":5`) || !strings.Contains(output, `"terminal":true`) || !strings.Contains(output, `"kind":"role_verification"`) {
				t.Fatal("缺少核验原因、类型、代次或终止诊断")
			}
			if strings.Contains(output, task.CookieStr) || strings.Contains(output, task.Text) || strings.Contains(output, "重放成功") {
				t.Fatal("日志泄漏敏感正文或误报外部成功")
			}
			// typedErr 验证改进诊断没有破坏已有 errors.Is 兼容判断。
			typedErr := newSellerRoleEvidenceError(task, scenario.reason)
			if !errors.Is(typedErr, errSellerRoleEvidencePending) {
				t.Fatal("丢失待核验哨兵兼容语义")
			}
		})
	}
}

// TestUnknownRoleReplayDoesNotGuessOrReopenOrders 用 t 验证多候选不串单、已结束同单只处理完成、邻近已发货订单不能用来关闭无号事件。
func TestUnknownRoleReplayDoesNotGuessOrReopenOrders(t *testing.T) {
	// cases 覆盖精确订单终态和没有唯一订单证据的两种情况。
	cases := []struct {
		// name 是子测试名称；orderID 非空才允许精确关联已结束订单。
		name, orderID string
		// pending 控制本地会话订单是否仍待发货；expectQueue 是预期保留的延期任务数。
		pending bool
		// expectQueue 为零表示明确终态安全关闭，为一表示证据不足必须保留。
		expectQueue int
	}{
		{name: "确切已发货同单", orderID: "order-a", expectQueue: 0},
		{name: "无号事件不能借用已发货邻单", expectQueue: 1},
		{name: "两笔待发货候选不能择近", pending: true, expectQueue: 1},
	}
	// scenario 指定此次安全收口所具备的证据。
	for _, scenario := range cases {
		// t 管理每组订单事实及日志断言。
		t.Run(scenario.name, func(t *testing.T) {
			// store、cleanup 提供本地订单与延期队列。
			store, cleanup := newAutomationTestStore(t)
			defer cleanup()
			// ctx 控制本地数据写入和单次队列处理。
			ctx := context.Background()
			// status 是测试准备的本地交易阶段。
			status := "shipped"
			if scenario.pending {
				status = "pending_ship"
			}
			// orderID 是同账号同会话两笔不同订单，时间远近不能成为选择证据。
			for _, orderID := range []string{"order-a", "order-b"} {
				// upsertErr 保存隔离的订单事实。
				if upsertErr := store.Orders.Upsert(ctx, orderID, db.OrderUpsertOpts{CookieID: "cid", ChatID: "chat", ItemID: "item", BuyerID: "buyer", OrderStatus: status}); upsertErr != nil {
					t.Fatal(upsertErr)
				}
			}
			// itemErr 让商品归属本身满足门禁，防止测试仅因商品不存在而误判没有串单。
			if itemErr := store.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: "cid", ItemID: "item"}); itemErr != nil {
				t.Fatal(itemErr)
			}
			// task 是来自 WebSocket 的旧未知角色事件，仅使用场景提供的可靠订单号。
			task := taskWithRoleVerificationKey(Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid, OrderID: scenario.orderID, ItemID: "item", BuyerID: "buyer", ChatID: "chat"}, "role-test")
			// snapshot、marshalErr 保存无凭证的延期快照。
			snapshot, marshalErr := json.Marshal(task)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			// deferErr 预置一个马上到期的真实延期任务。
			if deferErr := store.Automation.DeferTask(ctx, db.DeferredAutomationTask{TaskKey: "cid:role-test", CookieID: "cid", TriggerType: task.TriggerType, TaskJSON: string(snapshot)}); deferErr != nil {
				t.Fatal(deferErr)
			}
			// logs 记录安全忽略原因；sender 捕获不应发生的外部消息。
			logs := &bytes.Buffer{}
			// sender 不应被任何缺乏可执行订单证据的场景调用。
			sender := &testSender{}
			// center 是真实角色门禁和订单回填链路。
			center := New(store, testSenderProvider{sender: sender}, slog.New(slog.NewJSONHandler(logs, nil)))
			// resolveErr 和 resolved 验证无号多候选在执行前就保持未关联，而不是只因后续无规则才无动作。
			resolved, resolveErr := center.resolvePaidTaskOrder(ctx, task)
			if resolveErr != nil || resolved.OrderID != scenario.orderID {
				t.Fatalf("订单关联不符: order=%s err=%v", resolved.OrderID, resolveErr)
			}
			// replayErr 保存单轮队列结果。
			if replayErr := NewScheduler(center).runDeferredTasks(ctx); replayErr != nil {
				t.Fatal(replayErr)
			}
			// queueCount 是安全关闭或保留阻断后的队列行数。
			var queueCount int
			// countErr 保存队列审计查询错误。
			if countErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_pending_tasks`).Scan(&queueCount); countErr != nil {
				t.Fatal(countErr)
			}
			if queueCount != scenario.expectQueue || len(sender.texts) != 0 {
				t.Fatalf("安全处理不符: queue=%d sends=%d", queueCount, len(sender.texts))
			}
			if scenario.expectQueue == 0 && (!strings.Contains(logs.String(), "order_not_pending_ship") || !strings.Contains(logs.String(), "延期自动化事件处理完成")) {
				t.Fatal("明确终态收口缺少未执行外部动作的审计")
			}
		})
	}
}

// TestDeferredDiagnosticKinds 用 t 验证动作延迟和账号暂停分类不会被统一误报为角色待核验。
func TestDeferredDiagnosticKinds(t *testing.T) {
	// task 是保留原始角色幂等标记的动作续跑快照；运行 ID 必须优先判为动作延期。
	task := Task{Raw: map[string]any{"automation_run_id": int64(12), roleVerificationTaskKeyField: "original-role-key"}}
	// encoded、encodeErr 将白名单日志字段编码后检查分类，不输出任务原始快照。
	encoded, encodeErr := json.Marshal(deferredTaskLogFields(db.DeferredAutomationTask{}, task, nil))
	if encodeErr != nil || !strings.Contains(string(encoded), "action_delay") {
		t.Fatalf("动作延期分类失败: %v", encodeErr)
	}
	encoded, encodeErr = json.Marshal(deferredTaskLogFields(db.DeferredAutomationTask{}, Task{}, nil))
	if encodeErr != nil || !strings.Contains(string(encoded), "account_pause") {
		t.Fatalf("账号暂停分类失败: %v", encodeErr)
	}
}
