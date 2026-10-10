package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// stopTestExec 在隔离数据库执行 query/args 夹具写入并返回主键；失败只报告 SQL 错误，不打印任务载荷。
func stopTestExec(t *testing.T, store *Store, query string, args ...any) int64 {
	t.Helper()
	// result、err 保存夹具写入结果，不允许忽略建模失败。
	result, err := store.DB.ExecContext(context.Background(), query, args...)
	if err != nil {
		t.Fatal(err)
	}
	// id 仅供 SQLite 插入夹具使用；更新语句的返回值不参与断言。
	id, _ := result.LastInsertId()
	return id
}

// orderStopFixture 建立同账号两个订单及同单的求评价运行和延期付款任务；所有连接由 t 清理。
func orderStopFixture(t *testing.T) (*Store, int64, string, int64, int64) {
	t.Helper()
	// store、cleanup 是隔离 SQLite 存储及其关闭责任。
	store, cleanup := newTestDB(t)
	t.Cleanup(cleanup)
	// userID、accountID 限定异常处理的合法所有者。
	userID, accountID := seedAccount(t, store)
	stopTestExec(t, store, `UPDATE cookies SET remark='测试账号' WHERE id=?`, accountID)
	stopTestExec(t, store, `INSERT INTO orders(order_id,cookie_id,item_id,buyer_id,chat_id,order_status) VALUES ('stop-order',?,'item','buyer','chat','pending_ship'),('other-order',?,'other','other-buyer','other-chat','pending_ship')`, accountID, accountID)
	stopTestExec(t, store, `INSERT INTO item_info(cookie_id,item_id,item_title) VALUES (?,'item','测试商品')`, accountID)
	// ruleID 是求评价规则，证明整单停用不能只作用于当前任务类型。
	ruleID := stopTestExec(t, store, `INSERT INTO automation_rules(user_id,cookie_id,name,trigger_type,enabled,config_json) VALUES (?,?,'测试规则','review_missing_timeout',1,'{}')`, userID, accountID)
	// raw 是明确匹配账号与订单的非敏感快照。
	raw := fmt.Sprintf(`{"AccountID":%q,"OrderID":"stop-order"}`, accountID)
	// runID、taskID 分别标识人工核对运行和死信延期任务。
	runID := stopTestExec(t, store, `INSERT INTO automation_runs(rule_id,cookie_id,order_id,trigger_type,trigger_key,status,raw_event_json,error_message) VALUES (?,?,'stop-order','review_missing_timeout','stop-run','needs_review',?,'无法确认结果')`, ruleID, accountID, raw)
	// taskID 是关联同一订单的付款延期异常主键。
	taskID := stopTestExec(t, store, `INSERT INTO automation_pending_tasks(task_key,cookie_id,trigger_type,task_json,status,attempt_count) VALUES ('stop-task',?,'order_paid',?,'dead_letter',5)`, accountID, raw)
	return store, userID, accountID, runID, taskID
}

// TestIssueOrderIdentityRejectsGuessing 验证当前与旧快照、缺字段及身份冲突只产生可靠关联。
func TestIssueOrderIdentityRejectsGuessing(t *testing.T) {
	// cases 列出每种身份边界；空 want 表示不能暴露可停用的关联订单。
	cases := []struct {
		// raw 是输入快照，不携带真实凭证。
		raw string
		// stored 是运行的不可变订单标识，延期任务没有此列。
		stored string
		// want 是预期的唯一订单标识。
		want string
	}{
		{`{"AccountID":"a","OrderID":"o"}`, "", "o"}, {`{"account_id":"a","order_id":"o"}`, "", "o"},
		{`{}`, "o", "o"}, {`{}`, "", ""}, {`bad`, "o", ""},
		{`{"AccountID":"b","OrderID":"o"}`, "o", ""}, {`{"AccountID":"a","OrderID":"other"}`, "o", ""},
		{`{"AccountID":"a","account_id":"b","OrderID":"o"}`, "", ""},
		{`{"AccountID":"a","OrderID":"o","order_id":"other"}`, "", ""},
		{`{"OrderID":"o"}`, "", ""}, {`{"AccountID":"a","OrderID":" "}`, "", ""},
	}
	for _, scenario := range cases { // scenario 是本轮待验证的快照与可信身份组合。
		if got := issueOrderID(scenario.raw, "a", scenario.stored); got != scenario.want {
			t.Fatalf("identity=%q want=%q", got, scenario.want)
		} // got 仅包含非敏感订单标识。
	}
}

// TestStopOrderFromEitherIssuePersistsAndBlocksAllEntrances 验证两种异常入口都按整单停止，并保留其他订单和未知动作证据。
func TestStopOrderFromEitherIssuePersistsAndBlocksAllEntrances(t *testing.T) {
	for _, deferred := range []bool{false, true} { // deferred 选择运行或延期异常处理入口。
		t.Run(fmt.Sprintf("deferred=%t", deferred), func(t *testing.T) { // t 拥有当前场景的独立数据库。
			// store、userID、accountID、runID、taskID 是同单两类异常和用户归属。
			store, userID, accountID, runID, taskID := orderStopFixture(t)
			// ctx 是全部本地检查的生命周期根。
			ctx := context.Background()
			// runs、tasks、err 验证新增摘要不要求读取账号凭证。
			runs, tasks, err := store.Automation.ListIssues(ctx, userID)
			if err != nil || len(runs) != 1 || len(tasks) != 1 {
				t.Fatalf("issue counts/error: %d %d %v", len(runs), len(tasks), err)
			}
			if runs[0].ItemTitle != "测试商品" || runs[0].AccountName != "测试账号" || !runs[0].CanStopOrder || tasks[0].OrderID != "stop-order" || !tasks[0].CanStopOrder || runs[0].TriggerType != "review_missing_timeout" || tasks[0].TriggerType != "order_paid" {
				t.Fatal("异常摘要没有保留任务类型或可靠订单信息")
			}
			// id 是用户本次操作选中的异常主键。
			id := runID
			if deferred {
				id = taskID
			}
			// err 验证越权用户无法通过异常主键停用他人订单。
			if err := store.Automation.StopOrderForIssue(ctx, userID+999, id, deferred); !errors.Is(err, ErrNotFound) {
				t.Fatalf("ownership error=%v", err)
			} // err 必须隐藏越权异常。
			// activeID 模拟另一条已有未知外部结果的同单运行，其检查点不可被停用动作清空。
			activeID := stopTestExec(t, store, `INSERT INTO automation_runs(rule_id,cookie_id,order_id,trigger_type,trigger_key,status,raw_event_json,action_started) SELECT rule_id,cookie_id,order_id,'buyer_reviewed','inflight','needs_review',raw_event_json,1 FROM automation_runs WHERE id=?`, runID)
			// err 是用户确认整单停用后的原子事务结果。
			if err := store.Automation.StopOrderForIssue(ctx, userID, id, deferred); err != nil {
				t.Fatal(err)
			} // err 是整单事务结果。
			// restarted 使用同一持久库的新仓储对象验证停用不依赖进程内缓存。
			restarted := NewStore(store.DB, DialectSQLite)
			// err 验证新仓储对象仍能读取持久停用，而非依赖内存缓存。
			if err := restarted.Automation.CheckOrderAutomation(ctx, accountID, "stop-order"); !errors.Is(err, ErrOrderAutomationStopped) {
				t.Fatalf("stop error=%v", err)
			} // err 验证持久停用哨兵。
			if err := restarted.Automation.CheckOrderAutomation(ctx, accountID, "other-order"); err != nil {
				t.Fatal(err)
			} // err 确认其他订单未被误停。
			if err := restarted.Automation.CheckOrderAutomation(ctx, "another-account", "stop-order"); err != nil {
				t.Fatal(err)
			} // err 确认停用有账号作用域。
			// active、activeErr 验证未知外部动作记录仍保留占用和审计状态。
			active, activeErr := store.Automation.GetRun(ctx, activeID)
			if activeErr != nil || !active.ActionStarted || active.Status != "needs_review" {
				t.Fatal("整单停用破坏了未知外部结果证据")
			}
			runs, tasks, err = store.Automation.ListIssues(ctx, userID)
			if err != nil || len(runs) != 0 || len(tasks) != 0 {
				t.Fatal("已停止订单仍反复出现在待处理列表")
			}
			// newRun 是相同订单未来的新规则触发，不能因 trigger_key 改变而绕过停用。
			newRun := AutomationRun{RuleID: active.RuleID, CookieID: accountID, OrderID: "stop-order", TriggerType: "buyer_reviewed", TriggerKey: "future", RawEventJSON: `{}`}
			// err 验证未来新的触发键仍受订单停用约束。
			if _, _, err := store.Automation.TryStartRun(ctx, newRun); !errors.Is(err, ErrOrderAutomationStopped) {
				t.Fatalf("new run error=%v", err)
			} // err 校验新触发入口。
			if _, err := store.Automation.StartRunAction(ctx, activeID, active.AttemptCount, 0, 100); !errors.Is(err, ErrOrderAutomationStopped) {
				t.Fatalf("action error=%v", err)
			} // err 校验动作领取入口。
			if _, err := store.Automation.ReopenRunForRecovery(ctx, activeID, active.AttemptCount, 100); !errors.Is(err, ErrOrderAutomationStopped) {
				t.Fatalf("recovery error=%v", err)
			} // err 校验历史恢复入口。
			if err := store.Automation.DeferTask(ctx, DeferredAutomationTask{TaskKey: "new-task", CookieID: accountID, TriggerType: "order_paid", TaskJSON: fmt.Sprintf(`{"AccountID":%q,"OrderID":"stop-order"}`, accountID)}); !errors.Is(err, ErrOrderAutomationStopped) {
				t.Fatalf("defer error=%v", err)
			} // err 校验延期入队入口。
		})
	}
}

// TestStopOrderRejectsInvalidIdentityAndRollsBack 验证缺订单、冲突、并发已处理及数据库故障都不留下半完成停用。
func TestStopOrderRejectsInvalidIdentityAndRollsBack(t *testing.T) {
	// mutations 在停用前制造不同不可接受的订单或异常事实。
	mutations := []string{
		`UPDATE automation_pending_tasks SET task_json='{}'`,
		`UPDATE automation_pending_tasks SET task_json='{"AccountID":"other","OrderID":"stop-order"}'`,
		`DELETE FROM orders WHERE order_id='stop-order'`,
		`UPDATE automation_pending_tasks SET status='pending'`,
		`CREATE TRIGGER reject_stop BEFORE UPDATE ON automation_runs BEGIN SELECT RAISE(FAIL,'synthetic stop failure'); END`,
	}
	for index, mutation := range mutations { // index 标识隔离子场景；mutation 是固定的测试 SQL。
		t.Run(fmt.Sprint(index), func(t *testing.T) { // t 清理本场景数据，失败不影响下一场景。
			// store、userID、accountID、taskID 限定本场景的合法用户和延期异常。
			store, userID, accountID, _, taskID := orderStopFixture(t)
			stopTestExec(t, store, mutation)
			// err 必须拒绝当前场景的缺失身份、失效任务或事务故障。
			if err := store.Automation.StopOrderForIssue(context.Background(), userID, taskID, true); err == nil {
				t.Fatal("invalid stop accepted")
			} // err 必须拒绝不安全或未持久化的停用。
			if err := store.Automation.CheckOrderAutomation(context.Background(), accountID, "stop-order"); err != nil {
				t.Fatalf("rollback leaked stop: %v", err)
			} // err 证明没有半完成的停用标记。
		})
	}
}

// TestIssueContextMissingOrderAndCancelledQueries 验证缺失订单明确展示且不授权停用，取消和存储失败不当作放行。
func TestIssueContextMissingOrderAndCancelledQueries(t *testing.T) {
	// store、userID、accountID、taskID 是需要改造为缺订单场景的夹具。
	store, userID, accountID, _, taskID := orderStopFixture(t)
	stopTestExec(t, store, `UPDATE automation_pending_tasks SET task_json=? WHERE id=?`, fmt.Sprintf(`{"AccountID":%q,"OrderID":"missing-order"}`, accountID), taskID)
	// tasks、err 保存延期摘要，订单号可展示但不允许整单停用。
	_, tasks, err := store.Automation.ListIssues(context.Background(), userID)
	if err != nil || len(tasks) != 1 || tasks[0].OrderID != "missing-order" || tasks[0].CanStopOrder {
		t.Fatal("缺订单摘要错误")
	}
	// ctx、cancel 模拟客户端已取消查询，不得把查询故障当作没有停用。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// err 保留上下文取消语义，不把查询失败视作未停用。
	if err := store.Automation.CheckOrderAutomation(ctx, accountID, "stop-order"); !errors.Is(err, context.Canceled) {
		t.Fatalf("query cancellation=%v", err)
	} // err 保留取消语义。
	if err := store.Automation.StopOrderForIssue(ctx, userID, taskID, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("stop cancellation=%v", err)
	} // err 不得写入已取消请求。
	if _, _, err := store.Automation.ListIssues(ctx, userID); err == nil {
		t.Fatal("取消查询未返回失败")
	} // err 阻止不完整列表返回。
}

// TestRunIssueBackfillsOnlyExplicitOrderIdentity 验证历史运行缺订单列时使用同账号明确快照，冲突时不回填或授予整单停用。
func TestRunIssueBackfillsOnlyExplicitOrderIdentity(t *testing.T) {
	// store、userID、runID 是需要验证历史订单字段缺失的运行夹具。
	store, userID, _, runID, _ := orderStopFixture(t)
	stopTestExec(t, store, `UPDATE automation_runs SET order_id='' WHERE id=?`, runID)
	// runs、err 验证与本地订单匹配的原始快照可以可靠补齐订单号。
	runs, _, err := store.Automation.ListIssues(context.Background(), userID)
	if err != nil || len(runs) != 1 || runs[0].OrderID != "stop-order" || !runs[0].CanStopOrder {
		t.Fatal("明确快照未回填历史运行订单")
	}
	stopTestExec(t, store, `UPDATE automation_runs SET raw_event_json='{"AccountID":"other","OrderID":"stop-order"}' WHERE id=?`, runID)
	runs, _, err = store.Automation.ListIssues(context.Background(), userID)
	if err != nil || len(runs) != 1 || runs[0].OrderID != "" || runs[0].CanStopOrder {
		t.Fatal("冲突快照错误关联订单")
	}
}
