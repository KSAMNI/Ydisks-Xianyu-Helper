package db

import (
	"context"
	"testing"
)

// TestIsPendingShipStatus 验证待发货判断统一覆盖规范文本、历史 paid、数字码与 pending_delivery，并排除部分发货语义。
func TestIsPendingShipStatus(t *testing.T) {
	// cases 保存状态输入与期望判断。
	cases := map[string]bool{
		"pending_ship": true, "paid": true, "2": true, "pending_delivery": true, " pending_ship ": true,
		"partial_success": false, "partial_pending_finalize": false, "shipped": false, "": false, "unknown": false, "cancelled": false,
	}
	// status、want 是当前状态输入与期望结果。
	for status, want := range cases {
		// got 保存被测函数的实际返回值，用于与期望值比较。
		if got := IsPendingShipStatus(status); got != want {
			t.Fatalf("IsPendingShipStatus(%q)=%v want %v", status, got, want)
		}
	}
	// got 保存被测函数的实际返回值，用于与期望值比较。
	if got := pendingShipStatusSQLList(); got != "('pending_ship','paid','2','pending_delivery')" {
		t.Fatalf("待发货 SQL 列表异常: %s", got)
	}
}

// TestPendingShipQueriesShareStatusDefinition 验证兜底候选、续跑候选、取消查询与会话回填使用同一待发货定义。
func TestPendingShipQueriesShareStatusDefinition(t *testing.T) {
	// s、cleanup 保存测试数据库及关闭责任。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存本测试共用的数据库上下文。
	ctx := context.Background()
	// userID、cookieID 保存测试账号主键与账号标识。
	userID, cookieID := seedAccount(t, s)
	// ruleID 是同时含发卡与确认发货的启用规则。
	ruleID := seedCatchupRule(t, s, userID, cookieID, "item-1", "状态定义规则", true,
		[]AutomationActionInput{{ActionType: "send_card", MessageTemplate: "card", Enabled: true}, {ActionType: "confirm_shipment", Enabled: true}})
	// statuses 列出应进入与不应进入待发货查询的存量状态。
	statuses := map[string]bool{
		"pending_ship": true, "paid": true, "2": true, "pending_delivery": true,
		"partial_success": false, "partial_pending_finalize": false, "shipped": false,
	}
	// status 是订单状态，是否属于待发货由后续断言核对。
	for status := range statuses {
		seedCatchupOrder(t, s, "o-"+status, cookieID, "item-1", "chat-"+status, status)
	}
	// candidates 保存无运行订单的兜底扫描结果。
	candidates, err := s.Automation.PendingShipOrdersWithoutPaidRunAfter(ctx, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	// got 按订单号收集兜底候选。
	got := map[string]bool{}
	// candidate 是一条兜底候选订单。
	for _, candidate := range candidates {
		got[candidate.OrderID] = true
	}
	// status、expected 逐一核对候选集合。
	for status, expected := range statuses {
		if got["o-"+status] != expected {
			t.Fatalf("兜底候选对状态 %q 的判断=%v want %v", status, got["o-"+status], expected)
		}
	}

	// 对每个订单写入失败运行：续跑候选与取消查询必须同样识别别名状态。
	runIDs := map[string]int64{}
	// status 是订单状态，运行改写为待续跑形态。
	for status := range statuses {
		// runID 保存本订单运行夹具的主键。
		runID := seedCatchupRun(t, s, ruleID, cookieID, "o-"+status)
		// updateErr 保存把运行改写为待续跑形态的错误。
		if _, updateErr := s.DB.ExecContext(ctx, `UPDATE automation_runs SET status='needs_review', attempt_count=1, action_cursor=1, action_started=0, next_retry_at=0, lease_expires_at=0 WHERE id=?`, runID); updateErr != nil {
			t.Fatal(updateErr)
		}
		runIDs[status] = runID
	}
	// resumes 保存续跑扫描结果。
	resumes, resumeErr := s.Automation.PendingShipResumableRunsAfter(ctx, "", 5, 200)
	if resumeErr != nil {
		t.Fatal(resumeErr)
	}
	// resumed 按订单号收集续跑候选。
	resumed := map[string]bool{}
	// resume 是一条续跑候选。
	for _, resume := range resumes {
		resumed[resume.Order.OrderID] = true
	}
	// status、expected 逐一核对续跑集合与取消行为。
	for status, expected := range statuses {
		if resumed["o-"+status] != expected {
			t.Fatalf("续跑候选对状态 %q 的判断=%v want %v", status, resumed["o-"+status], expected)
		}
		// run 是该订单的运行快照，供条件取消比对。
		run, getErr := s.Automation.GetRun(ctx, runIDs[status])
		if getErr != nil {
			t.Fatal(getErr)
		}
		// canceled 表示该订单已不再待发货而被取消运行。
		canceled, cancelErr := s.Automation.CancelObsoletePaidRecoveryRun(ctx, *run, "测试取消")
		if cancelErr != nil {
			t.Fatal(cancelErr)
		}
		if canceled == expected {
			t.Fatalf("取消查询对状态 %q 的判断异常: canceled=%v 待发货=%v", status, canceled, expected)
		}
	}
}

// TestFindLatestPendingByChatUsesSharedStatusSet 验证会话回填接受 pending_delivery 别名，并拒绝部分发货状态。
func TestFindLatestPendingByChatUsesSharedStatusSet(t *testing.T) {
	// s、cleanup 保存测试数据库及关闭责任。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存本测试共用的数据库上下文。
	ctx := context.Background()
	// _, cookieID 保存测试账号归属。
	_, cookieID := seedAccount(t, s)
	seedCatchupOrder(t, s, "o-alias", cookieID, "item-1", "chat-alias", "pending_delivery")
	seedCatchupOrder(t, s, "o-partial", cookieID, "item-1", "chat-partial", "partial_success")
	// aliasOrder、aliasErr 是别名状态订单的回填结果。
	aliasOrder, aliasErr := s.Orders.FindLatestPendingByChat(ctx, cookieID, "chat-alias", "", "")
	if aliasErr != nil || aliasOrder == nil || aliasOrder.OrderID != "o-alias" {
		t.Fatalf("pending_delivery 应可回填: order=%+v err=%v", aliasOrder, aliasErr)
	}
	// partialOrder、partialErr 是部分发货订单的回填结果。
	partialOrder, partialErr := s.Orders.FindLatestPendingByChat(ctx, cookieID, "chat-partial", "", "")
	if partialErr != nil || partialOrder != nil {
		t.Fatalf("partial_success 不应回填: order=%+v err=%v", partialOrder, partialErr)
	}
}
