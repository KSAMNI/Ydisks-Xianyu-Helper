package automation

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/db"
)

// TestDeferredReplayRejectsUntrustedSnapshots 验证 t 中原始 JSON 损坏、来源改写和队列身份冲突均不能借用既有运行。
func TestDeferredReplayRejectsUntrustedSnapshots(t *testing.T) {
	// scenarios 只允许测试内硬编码的快照变更，不涉及真实任务或秘密。
	scenarios := []struct {
		// name、mutation 描述损坏类型及仅修改当前夹具的 SQL。
		name, mutation string
	}{
		{"原始快照损坏", `UPDATE automation_runs SET raw_event_json='{'`},
		{"原始快照来源变更", `UPDATE automation_runs SET raw_event_json=REPLACE(raw_event_json,'"ws"','"manual"')`},
	}
	// scenario 为当前独立的坏快照。
	for _, scenario := range scenarios {
		// t 管理该场景的运行及清理。
		t.Run(scenario.name, func(t *testing.T) {
			// runID、scheduler、store、cleanup 保存合法入队身份和被污染的原始运行；发送器不参与直接准入调用。
			runID, _, scheduler, store, cleanup := newPaidRecoveryFixture(t, "pending_ship", "ws")
			defer cleanup()
			// ctx 为本地读写上下文。
			ctx := context.Background()
			// run、err 是污染前的有效运行。
			run, err := store.Automation.GetRun(ctx, runID)
			if err != nil {
				t.Fatal(err)
			}
			// task 保留污染前的队列快照。
			task := replayTaskFromRun(t, run)
			// err 确认损坏真正写入持久化快照。
			if _, err := store.DB.ExecContext(ctx, scenario.mutation); err != nil {
				t.Fatal(err)
			}
			// stopped、deferred、err 验证坏快照直接停止且不会继续自动重排。
			stopped, deferred, err := scheduler.center.guardDeferredReplay(ctx, task)
			if !stopped || deferred || !errors.Is(err, errReplayNeedsReview) {
				t.Fatal("不可信快照未被隔离")
			}
		})
	}
	// runID、scheduler、store、cleanup 提供队列身份冲突及运行状态测试的独立环境。
	runID, _, scheduler, store, cleanup := newPaidRecoveryFixture(t, "pending_ship", "ws")
	defer cleanup()
	// ctx 控制本地数据库调用。
	ctx := context.Background()
	// run、err 保存原始快照。
	run, err := store.Automation.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	// task 是应归属 cid 的队列任务。
	task := replayTaskFromRun(t, run)
	// err 必须拒绝与队列持久化账号不一致的快照。
	if _, err := scheduler.handleDeferredReplay(ctx, db.DeferredAutomationTask{CookieID: "other", TriggerType: task.TriggerType}, task); !errors.Is(err, errReplayNeedsReview) {
		t.Fatal("队列身份冲突未停止")
	}
	task.OrderID = "other"
	// stopped、deferred、err 确认关联运行的订单冲突不会修改被借用的运行。
	stopped, deferred, err := scheduler.center.guardDeferredReplay(ctx, task)
	if !stopped || deferred || !errors.Is(err, errReplayNeedsReview) {
		t.Fatal("运行身份冲突未停止")
	}
	task = replayTaskFromRun(t, run)
	// err 将运行标为已有在途动作，模拟另一执行者先领取。
	if _, err := store.DB.ExecContext(ctx, `UPDATE automation_runs SET action_started=1 WHERE id=?`, runID); err != nil {
		t.Fatal(err)
	}
	stopped, _, err = scheduler.center.guardDeferredReplay(ctx, task)
	if !stopped || err == nil || errors.Is(err, errReplayNeedsReview) {
		t.Fatal("在途运行应等待而非隔离")
	}
	// err 将运行标为成功，重复队列应安全消费而不重新执行。
	if _, err := store.DB.ExecContext(ctx, `UPDATE automation_runs SET status='success',action_started=0 WHERE id=?`, runID); err != nil {
		t.Fatal(err)
	}
	stopped, _, err = scheduler.center.guardDeferredReplay(ctx, task)
	if !stopped || err != nil {
		t.Fatal("已完成运行应安全跳过")
	}
}

// TestReplayRejectionStorageFailureStillNotifies 验证 t 中安全收口写入失败不会误发消息，且独立人工处理通知仍送达替身。
func TestReplayRejectionStorageFailureStillNotifies(t *testing.T) {
	// runID、sender、scheduler、store、cleanup 提供不可自动恢复的人工来源付款任务。
	runID, sender, scheduler, store, cleanup := newPaidRecoveryFixture(t, "pending_ship", "manual")
	defer cleanup()
	// ctx 为本地队列执行上下文。
	ctx := context.Background()
	// run、err 取得待隔离任务。
	run, err := store.Automation.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	// probe 记录独立人工处理通知，而不调用外部通知渠道。
	probe := &triggerAwareNotificationProbe{}
	scheduler.center = NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{Notifier: probe})
	// err 检查队列创建成功后再注入持久化错误。
	if err := scheduler.center.deferTask(ctx, replayTaskFromRun(t, run), 0); err != nil {
		t.Fatal(err)
	}
	// statements 分别阻止运行隔离和死信收口，模拟本地存储写入故障。
	statements := []string{
		`CREATE TRIGGER deny_replay_quarantine BEFORE UPDATE OF status ON automation_runs WHEN NEW.status='needs_review' BEGIN SELECT RAISE(ABORT,'test quarantine failure'); END`,
		`CREATE TRIGGER deny_replay_dead_letter BEFORE UPDATE OF status ON automation_pending_tasks WHEN NEW.status='dead_letter' BEGIN SELECT RAISE(ABORT,'test dead letter failure'); END`,
	}
	// statement 是本测试独占的确定性失败触发器。
	for _, statement := range statements {
		// err 保证预期的失败路径已经注入。
		if _, err := store.DB.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	// err 必须报告收口失败，不能假装死信已可靠保存。
	if err := scheduler.runDeferredTasks(ctx); err == nil {
		t.Fatal("写入失败必须可见")
	}
	if len(sender.texts) != 0 || probe.manualCalls == 0 {
		t.Fatal("存储失败不得发送卡密，且必须通知人工处理")
	}
}

// TestDeferredReplayWithoutRunAndReadErrors 覆盖 t 中尚未创建运行的延期事件及数据库不可读情况。
func TestDeferredReplayWithoutRunAndReadErrors(t *testing.T) {
	// runID、scheduler、store、cleanup 提供完整订单，不使用其运行主键以模拟准备前延期。
	runID, _, scheduler, store, cleanup := newPaidRecoveryFixture(t, "pending_ship", "ws")
	defer cleanup()
	// ctx 为本地准入检查上下文。
	ctx := context.Background()
	// run、err 用于构造与现存订单一致的原始事件。
	run, err := store.Automation.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	// task 不携带 runID，但保留重放标记以要求当前状态检查。
	task := replayTaskFromRun(t, run)
	delete(task.Raw, "automation_run_id")
	// stopped、deferred、err 验证尚未创建运行的合法延期仍可进入规则。
	stopped, deferred, err := scheduler.center.guardDeferredReplay(ctx, task)
	if stopped || deferred || err != nil {
		t.Fatal("合法准备前延期被误拦截")
	}
	task.Source = "manual"
	stopped, _, err = scheduler.center.guardDeferredReplay(ctx, task)
	if !stopped || !errors.Is(err, errReplayNeedsReview) {
		t.Fatal("无运行人工来源也必须停止")
	}
	task.Source = "ws"
	// err 模拟在准备前延期期间订单被取消。
	if _, err := store.DB.ExecContext(ctx, `UPDATE orders SET order_status='canceled'`); err != nil {
		t.Fatal(err)
	}
	stopped, _, err = scheduler.center.guardDeferredReplay(ctx, task)
	if !stopped || err != nil {
		t.Fatal("无运行终态事件应直接安全停止")
	}
	task.TriggerType = TriggerBuyerReviewed
	stopped, _, err = scheduler.center.guardDeferredReplay(ctx, task)
	if stopped || err != nil {
		t.Fatal("非付款任务保持原有准入路径")
	}
	// canceled、cancel 提供确定性数据库读取取消信号。
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	task = replayTaskFromRun(t, run)
	stopped, _, err = scheduler.center.guardDeferredReplay(canceled, task)
	if !stopped || !errors.Is(err, context.Canceled) {
		t.Fatal("读取取消不得失败开放")
	}
	// err 验证动作前规则查询取消也会阻止发送。
	if err := scheduler.center.runs.guardReplayAction(canceled, task, run); !errors.Is(err, context.Canceled) {
		t.Fatal("动作前查询取消未传播")
	}
	// err 验证候选运行不存在时保持错误可见，不能隔离其他运行。
	if err := scheduler.rejectPendingShipCandidate(ctx, db.PendingShipResume{RunID: 999999}, "missing"); err == nil {
		t.Fatal("缺失候选不得被当作已隔离")
	}
	// err 验证旧代次扫描不改变现有运行。
	if err := scheduler.rejectPendingShipCandidate(ctx, db.PendingShipResume{RunID: runID, Attempt: run.AttemptCount + 1}, "stale"); err != nil {
		t.Fatal(err)
	}
}

// TestReplayAdmissionFailsClosedOnStorageAndOwnershipChanges 覆盖 t 中每一个剩余的读写失败和代次失权分支，不通过真实服务制造故障。
func TestReplayAdmissionFailsClosedOnStorageAndOwnershipChanges(t *testing.T) {
	// scenarios 以独立 SQLite 变更模拟具体失败点；action 选择实际动作前门禁，否则选择延期门禁。
	scenarios := []struct {
		// name 是当前失败点；statements 是本测试独占的数据库结构或状态变更。
		name       string
		statements []string
		// action、missingRun、missingRule 指定入口及已不存在的运行或规则身份。
		action, missingRun, missingRule bool
		// leaseLost 指定必须保留执行权失效分类的竞争结果。
		leaseLost bool
	}{
		{name: "延期规则读取失败", statements: []string{`ALTER TABLE automation_rules RENAME TO blocked_rules`}},
		{name: "延期订单读取失败", statements: []string{`ALTER TABLE orders RENAME TO blocked_orders`}},
		{name: "延期账号设置读取失败", statements: []string{`ALTER TABLE cookies RENAME TO blocked_cookies`}},
		{name: "关闭开关后重新延期失败", statements: []string{`UPDATE cookies SET auto_confirm=0`, `CREATE TRIGGER deny_requeue BEFORE INSERT ON automation_pending_tasks BEGIN SELECT RAISE(ABORT,'test requeue failure'); END`}},
		{name: "动作订单读取失败", action: true, statements: []string{`ALTER TABLE orders RENAME TO blocked_orders`}},
		{name: "取消时运行已不存在", action: true, missingRun: true, statements: []string{`UPDATE orders SET order_status='canceled'`}},
		{name: "取消时运行代次已改变", action: true, leaseLost: true, statements: []string{`UPDATE orders SET order_status='canceled'`, `UPDATE automation_runs SET attempt_count=attempt_count+1`}},
		{name: "取消状态写入失败", action: true, statements: []string{`UPDATE orders SET order_status='canceled'`, `CREATE TRIGGER deny_replay_cancel BEFORE UPDATE OF status ON automation_runs WHEN NEW.status='canceled' BEGIN SELECT RAISE(ABORT,'test cancel failure'); END`}},
		{name: "动作账号设置读取失败", action: true, statements: []string{`ALTER TABLE cookies RENAME TO blocked_cookies`}},
		{name: "动作前关闭总开关", action: true, statements: []string{`UPDATE cookies SET auto_confirm=0`}},
		{name: "隔离时运行已不存在", action: true, missingRun: true, missingRule: true},
		{name: "隔离时运行代次已改变", action: true, leaseLost: true, statements: []string{`UPDATE automation_rules SET enabled=0`, `UPDATE automation_runs SET attempt_count=attempt_count+1`}},
		{name: "动作前隔离写入失败", action: true, statements: []string{`UPDATE automation_rules SET enabled=0`, `CREATE TRIGGER deny_action_quarantine BEFORE UPDATE OF status ON automation_runs WHEN NEW.status='needs_review' BEGIN SELECT RAISE(ABORT,'test quarantine failure'); END`}},
		{name: "隔离时已有在途动作", action: true, leaseLost: true, statements: []string{`UPDATE automation_rules SET enabled=0`, `UPDATE automation_runs SET action_started=1`}},
	}
	// scenario 为本轮确定性失败位置。
	for _, scenario := range scenarios {
		// t 隔离每一处 SQL 故障，防止一种失败掩盖另一分支。
		t.Run(scenario.name, func(t *testing.T) {
			// runID、scheduler、store、cleanup 提供完整可信运行及本地生命周期。
			runID, _, scheduler, store, cleanup := newPaidRecoveryFixture(t, "pending_ship", "ws")
			defer cleanup()
			// ctx 是确定性的数据库操作上下文。
			ctx := context.Background()
			// run、err 保存故障发生前的快照身份。
			run, err := store.Automation.GetRun(ctx, runID)
			if err != nil {
				t.Fatal(err)
			}
			// task 保留可信来源与已有运行关联。
			task := replayTaskFromRun(t, run)
			// statement 仅包含上方硬编码的测试故障注入 SQL。
			for _, statement := range scenario.statements {
				// err 确保要验证的失败点已经准备就绪。
				if _, err := store.DB.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			if scenario.missingRun {
				run.ID = 999999
			}
			if scenario.missingRule {
				run.RuleID = 999999
			}
			if scenario.action {
				err = scheduler.center.runs.guardReplayAction(ctx, task, run)
			} else {
				// stopped 验证延期调用不仅返回错误，而且明确阻止继续进入事实写入。
				var stopped bool
				stopped, _, err = scheduler.center.guardDeferredReplay(ctx, task)
				if !stopped {
					t.Fatal("延期查询或写入失败后仍放行")
				}
			}
			if err == nil {
				t.Fatal("失败分支不应返回成功")
			}
			if scenario.leaseLost && !errors.Is(err, db.ErrAutomationRunLeaseLost) {
				t.Fatalf("执行权错误分类丢失: %v", err)
			}
		})
	}
}
