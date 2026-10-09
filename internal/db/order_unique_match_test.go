package db

import (
	"context"
	"testing"
)

// TestFindUniquePendingByChat 用 t 验证唯一待发货事实、可选身份筛选和多候选阻断，并保留显式卖家最近订单兼容入口。
func TestFindUniquePendingByChat(t *testing.T) {
	// store、cleanup 管理已迁移的隔离数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 控制本地订单写入和事实查询。
	ctx := context.Background()
	// accountID 是测试账号；不需要读取其平台凭证。
	_, accountID := seedAccount(t, store)
	// orderID 遍历同会话不同商品的两个待发货订单。
	for _, orderID := range []string{"order-a", "order-b"} {
		// writeErr 创建同账号同买家候选，商品号用订单号区分。
		if writeErr := store.Orders.Upsert(ctx, orderID, OrderUpsertOpts{CookieID: accountID, ChatID: "chat", BuyerID: "buyer", ItemID: orderID, OrderStatus: "pending_ship"}); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	// cases 验证空身份、错误归属、错误商品、多候选和完整限定后的唯一命中。
	cases := []struct {
		// name 是场景名；account、chat、buyer、item 是查询约束，expected 是唯一候选标识。
		name, account, chat, buyer, item, expected string
	}{
		{name: "空账号", chat: "chat"},
		{name: "空会话", account: accountID},
		{name: "其他账号", account: "other", chat: "chat"},
		{name: "多候选不择近", account: accountID, chat: "chat", buyer: "buyer"},
		{name: "错误买家", account: accountID, chat: "chat", buyer: "other"},
		{name: "错误商品", account: accountID, chat: "chat", item: "other"},
		{name: "身份限定唯一", account: accountID, chat: "chat@goofish", buyer: "buyer@goofish", item: "order-a", expected: "order-a"},
	}
	// scenario 是当前查询的身份组合。
	for _, scenario := range cases {
		// t 管理每次唯一性查询的断言。
		t.Run(scenario.name, func(t *testing.T) {
			// order、queryErr 是在给定身份约束下找到的唯一事实。
			order, queryErr := store.Orders.FindUniquePendingByChat(ctx, scenario.account, scenario.chat, scenario.buyer, scenario.item)
			if queryErr != nil {
				t.Fatal(queryErr)
			}
			if scenario.expected == "" {
				if order != nil {
					t.Fatal("证据不足仍关联订单")
				}
			} else if order == nil || order.OrderID != scenario.expected {
				t.Fatal("唯一订单未匹配")
			}
		})
	}
	// legacy、legacyErr 确认既有最近订单入口没有被本修复删除或改变多候选兼容策略。
	legacy, legacyErr := store.Orders.FindLatestPendingByChat(ctx, accountID, "chat", "buyer", "")
	if legacyErr != nil || legacy == nil {
		t.Fatalf("旧查询兼容失败: %v", legacyErr)
	}
	// finishErr 将第二笔候选结束，唯一查询只允许剩余待发货订单。
	if _, finishErr := store.DB.ExecContext(ctx, `UPDATE orders SET order_status='shipped' WHERE order_id='order-b'`); finishErr != nil {
		t.Fatal(finishErr)
	}
	// remaining、remainingErr 验证已发货订单不会抢占无号事件。
	remaining, remainingErr := store.Orders.FindUniquePendingByChat(ctx, accountID, "chat", "buyer", "")
	if remainingErr != nil || remaining == nil || remaining.OrderID != "order-a" {
		t.Fatalf("未排除已结束订单: %v", remainingErr)
	}
	// deleteErr 模拟本地订单被软删除，不能再作为卖家身份事实。
	if _, deleteErr := store.DB.ExecContext(ctx, `UPDATE orders SET deleted_at=CURRENT_TIMESTAMP WHERE order_id='order-a'`); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	remaining, remainingErr = store.Orders.FindUniquePendingByChat(ctx, accountID, "chat", "buyer", "")
	if remainingErr != nil || remaining != nil {
		t.Fatal("已删除订单仍被关联")
	}
	// cancelled、cancel 提供真实查询取消错误，不需要损坏数据库结构。
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	// queryErr 必须传播取消后的数据库读取失败，不能误认为没有唯一候选。
	if _, queryErr := store.Orders.FindUniquePendingByChat(cancelled, accountID, "chat", "", ""); queryErr == nil {
		t.Fatal("吞掉数据库取消错误")
	}
}
