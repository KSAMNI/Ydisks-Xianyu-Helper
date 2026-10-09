package db

import (
	"context"
	"testing"
)

// TestQuarantineIdleRunUsesFullSnapshot 验证 t 中陈旧扫描不能覆盖新代次、续租、游标推进、终态或在途动作。
func TestQuarantineIdleRunUsesFullSnapshot(t *testing.T) {
	// cases 的 mutation 只改变测试运行；allowed 仅对原封不动的空闲快照为真。
	cases := []struct {
		// name、mutation、allowed 描述快照竞争场景、数据库变更和预期隔离权。
		name, mutation string
		allowed        bool
	}{
		{"空闲运行", "", true},
		{"新代次", "attempt_count=attempt_count+1", false},
		{"已续租", "lease_expires_at=lease_expires_at+60", false},
		{"已推进", "action_cursor=action_cursor+1", false},
		{"已成功", "status='success'", false},
		{"正在发送", "action_started=1", false},
	}
	// scenario 为当前独立的竞争变更。
	for _, scenario := range cases {
		// t 提供本场景的隔离数据库。
		t.Run(scenario.name, func(t *testing.T) {
			// store、cleanup 由测试独占，禁止触及用户数据库。
			store, cleanup := newTestDB(t)
			defer cleanup()
			// ctx 为所有本地写入的上下文。
			ctx := context.Background()
			// userID、accountID 创建满足外键约束的测试账号。
			userID, accountID := seedAccount(t, store)
			// ruleID、err 创建非付款规则，测试只关注 CAS 而不依赖交易阶段。
			ruleID, err := store.Automation.Create(ctx, makeAutomationRule(accountID, userID, "item", "buyer_reviewed", true, 1))
			if err != nil {
				t.Fatal(err)
			}
			// runID、started、err 验证初始运行确实被当前测试领取。
			runID, started, err := store.Automation.TryStartRun(ctx, AutomationRun{RuleID: ruleID, CookieID: accountID, TriggerType: "buyer_reviewed", TriggerKey: "idle-review"})
			if err != nil || !started {
				t.Fatal("初始运行领取失败")
			}
			// snapshot、err 是并发变更前冻结的完整运行状态。
			snapshot, err := store.Automation.GetRun(ctx, runID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario.mutation != "" {
				// err 仅执行硬编码测试变更。
				if _, err := store.DB.ExecContext(ctx, "UPDATE automation_runs SET "+scenario.mutation+" WHERE id=?", runID); err != nil {
					t.Fatal(err)
				}
			}
			// changed、err 验证隔离只在快照仍有效时成功。
			changed, err := store.Automation.QuarantineIdleRun(ctx, *snapshot, "测试人工核对")
			if err != nil || changed != scenario.allowed {
				t.Fatalf("隔离=%v，预期=%v，错误=%v", changed, scenario.allowed, err)
			}
			if scenario.allowed {
				// current、err 核对成功隔离后的终态和诊断持久化。
				current, err := store.Automation.GetRun(ctx, runID)
				if err != nil || current.Status != "needs_review" || current.ErrorMessage != "测试人工核对" {
					t.Fatal("隔离未持久化")
				}
			}
		})
	}
}

// TestRejectDeferredTaskPreservesClaimOwner 验证 t 中死信只收口当前领取代次，保留人工检查快照且不能再次领取。
func TestRejectDeferredTaskPreservesClaimOwner(t *testing.T) {
	// store、cleanup 保存隔离数据库及释放责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 为本地队列操作上下文。
	ctx := context.Background()
	// accountID 是该延期任务唯一归属账号；用户 ID 无需在本测试中读取。
	_, accountID := seedAccount(t, store)
	// err 确认任务成功持久化。
	if err := store.Automation.DeferTask(ctx, DeferredAutomationTask{TaskKey: "reject", CookieID: accountID, TriggerType: "order_paid", TaskJSON: `{}`, DueAt: 0}); err != nil {
		t.Fatal(err)
	}
	// tasks、err 读取当前持有领取代次的任务。
	tasks, err := store.Automation.ClaimDueDeferredTasks(ctx, 1)
	if err != nil || len(tasks) != 1 {
		t.Fatal("任务领取失败")
	}
	// task 固定需要收口的身份及代次。
	task := tasks[0]
	// err 应拒绝非拥有者。
	if err := store.Automation.RejectDeferredTask(ctx, task.ID, task.ClaimVersion+1, "旧任务"); err == nil {
		t.Fatal("错误代次不应收口")
	}
	// err 允许真实拥有者收口。
	if err := store.Automation.RejectDeferredTask(ctx, task.ID, task.ClaimVersion, "需要人工核对"); err != nil {
		t.Fatal(err)
	}
	// state、snapshot 保存死信状态和原任务快照，不允许通过删除逃避人工核对。
	var state, snapshot string
	// err 检查状态实际落库。
	if err := store.DB.QueryRowContext(ctx, `SELECT status,task_json FROM automation_pending_tasks WHERE id=?`, task.ID).Scan(&state, &snapshot); err != nil {
		t.Fatal(err)
	}
	if state != "dead_letter" || snapshot != "{}" {
		t.Fatal("死信状态或快照错误")
	}
	tasks, err = store.Automation.ClaimDueDeferredTasks(ctx, 1)
	if err != nil || len(tasks) != 0 {
		t.Fatal("死信不得自动重领")
	}
	// err 保证后续走确定性存储关闭错误分支。
	if err := store.DB.Close(); err != nil {
		t.Fatal(err)
	}
	// err 不能把数据库失败伪装为竞争失败。
	if _, err := store.Automation.QuarantineIdleRun(ctx, AutomationRun{}, "存储故障"); err == nil {
		t.Fatal("隔离失败必须传播")
	}
	// err 不能吞掉队列收口错误。
	if err := store.Automation.RejectDeferredTask(ctx, task.ID, task.ClaimVersion, "存储故障"); err == nil {
		t.Fatal("死信失败必须传播")
	}
}
