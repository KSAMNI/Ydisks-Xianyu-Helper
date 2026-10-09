package db

import (
	"context"
	"testing"
	"time"
)

// TestRunAdmissionMatchesAtomicReclaim 用 t 的隔离数据库逐项证明只读准入与真正的原子重领条件一致。
func TestRunAdmissionMatchesAtomicReclaim(t *testing.T) {
	// cases 覆盖成功、隔离、在途、退避和安全失败，不凭 sent_count=0 放宽结果不确定的运行。
	cases := []struct {
		// name 是子测试名称；status 是注入的持久化状态。
		name, status string
		// started、attempt、sent 是动作检查点、重试代次和累计成功动作数。
		started, attempt, sent int
		// lease、retry 是相对当前时间的秒数；正值表示尚未到期。
		lease, retry int64
		// message 是不含凭证的测试失败标记；reason 是预期跳过分类。
		message, reason string
		// allowed 是预期是否允许真正原子领取。
		allowed bool
	}{
		{name: "成功不重放", status: "success", reason: "success"},
		{name: "隔离不重放", status: "needs_review", reason: "needs_review"},
		{name: "取消不重放", status: "cancelled", reason: "cancelled"},
		{name: "在途不重放", status: "running", lease: 3600, reason: "running"},
		{name: "失租动作未开始", status: "running", lease: -60, allowed: true},
		{name: "失租但动作已开始", status: "running", started: 1, lease: -60, reason: "running"},
		{name: "安全未发送失败", status: "failed", attempt: 1, retry: -60, allowed: true},
		{name: "安全部分动作续跑", status: "failed", attempt: 1, sent: 1, message: SafeRetryErrorPrefix + "未发送", allowed: true},
		{name: "普通部分成功禁重放", status: "failed", attempt: 1, sent: 1, reason: "unsafe_retry"},
		{name: "明确禁止重试", status: "failed", attempt: 1, message: NoRetryErrorPrefix + "不确定", reason: "unsafe_retry"},
		{name: "失败动作已开始", status: "failed", attempt: 1, started: 1, reason: "unsafe_retry"},
		{name: "安全失败尚在退避", status: "failed", attempt: 1, retry: 3600, reason: "retry_not_due"},
		{name: "安全失败次数耗尽", status: "failed", attempt: 3, reason: "retry_exhausted"},
	}
	// scenario 是当前待验证的持久化状态组合。
	for _, scenario := range cases {
		// t 管理该状态组合的数据库生命周期和独立断言。
		t.Run(scenario.name, func(t *testing.T) {
			// store、cleanup 隔离每种领取状态，防止重领结果影响其他分支。
			store, cleanup := newTestDB(t)
			defer cleanup()
			// ctx 是数据库操作的取消根；userID、accountID 提供运行归属。
			ctx := context.Background()
			// userID、accountID 是本地测试管理员和账号，不包含真实账号凭证。
			userID, accountID := seedAccount(t, store)
			// ruleID、createErr 建立独立求评价规则，避免外键和租户约束被测试绕开。
			ruleID, createErr := store.Automation.Create(ctx, makeAutomationRule(accountID, userID, "item", "review_missing_timeout", true, 1))
			if createErr != nil {
				t.Fatal(createErr)
			}
			// run 是供准入与原子重领共用的相同身份。
			run := AutomationRun{RuleID: ruleID, CookieID: accountID, OrderID: "order", TriggerType: "review_missing_timeout", TriggerKey: "review_missing_timeout:order:1"}
			// runID、started、startErr 验证第一次创建确实取得执行权。
			runID, started, startErr := store.Automation.TryStartRun(ctx, run)
			if startErr != nil || !started {
				t.Fatalf("创建运行失败: started=%v err=%v", started, startErr)
			}
			// now 固定准备数据的时刻，预留一分钟避免秒边界引入不稳定。
			now := time.Now().UTC().Unix()
			// updateErr 注入只影响本测试的终态、检查点和退避数据。
			if _, updateErr := store.DB.ExecContext(ctx, `UPDATE automation_runs SET status=?,action_started=?,attempt_count=?,sent_count=?,lease_expires_at=?,next_retry_at=?,error_message=? WHERE id=?`, scenario.status, scenario.started, scenario.attempt, scenario.sent, now+scenario.lease, now+scenario.retry, scenario.message, runID); updateErr != nil {
				t.Fatal(updateErr)
			}
			// admission、readErr 是尚未更改数据库的准入快照。
			admission, readErr := store.Automation.CheckRunAdmission(ctx, ruleID, run.TriggerKey, accountID, "order")
			if readErr != nil {
				t.Fatal(readErr)
			}
			if admission.Allowed != scenario.allowed || admission.Reason != scenario.reason || admission.RunID != runID {
				t.Fatalf("准入不符: %+v", admission)
			}
			// acquired、claimErr 验证真正领取与只读判断一致，而非仅检查分类字符串。
			_, acquired, claimErr := store.Automation.TryStartRun(ctx, run)
			if claimErr != nil || acquired != scenario.allowed {
				t.Fatalf("原子领取不符: acquired=%v err=%v", acquired, claimErr)
			}
			// mismatch、mismatchErr 验证相同幂等键不能用于另一账号的运行。
			mismatch, mismatchErr := store.Automation.CheckRunAdmission(ctx, ruleID, run.TriggerKey, "other-account", "order")
			if mismatchErr != nil || mismatch.Allowed || mismatch.Reason != "identity_mismatch" {
				t.Fatalf("未阻止跨账号准入: %+v err=%v", mismatch, mismatchErr)
			}
			// next、nextErr 验证下一业务提醒轮次不被当前幂等运行阻挡。
			next, nextErr := store.Automation.CheckRunAdmission(ctx, ruleID, "review_missing_timeout:order:2", accountID, "order")
			if nextErr != nil || !next.Allowed || next.RunID != 0 {
				t.Fatalf("新轮次准入错误: %+v err=%v", next, nextErr)
			}
		})
	}
}

// TestRunAdmissionPropagatesReadFailure 用 t 验证数据库读取失败不返回允许执行的默认投影。
func TestRunAdmissionPropagatesReadFailure(t *testing.T) {
	// store、cleanup 提供可取消查询的真实 SQLite 存储。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx、cancel 构造已取消上下文，在数据库查询前确定性失败。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// admission、readErr 必须表示查询失败而不是新事件可执行。
	admission, readErr := store.Automation.CheckRunAdmission(ctx, 1, "review_missing_timeout:order:1", "account", "order")
	if readErr == nil || admission != nil {
		t.Fatalf("未安全传播准入读取错误: %+v err=%v", admission, readErr)
	}
}
