package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestMultiDB_AutomationRunLookupAndObsoleteCancel 验证按规则/幂等键与订单读取运行快照，以及订单不再待发货时取消付款运行恢复。
func TestMultiDB_AutomationRunLookupAndObsoleteCancel(t *testing.T) {
	for _, target := range allTestTargets(t) { // target 包含当前方言的真实存储和销毁函数。
		t.Run(target.name, func(t *testing.T) { // t 负责当前方言的断言。
			defer target.cleanup()
			// store、ctx 是当前子测试独占的仓储和上下文。
			store, ctx := target.store, context.Background()
			// userID、cookieID 是合法的账号归属。
			userID, cookieID := seedAccount(t, store)
			// ruleID、createErr 是带外键的付款规则。
			ruleID, createErr := store.Automation.Create(ctx, makeAutomationRule(cookieID, userID, "lookup-item", "order_paid", true, 1))
			if createErr != nil {
				t.Fatal(createErr)
			}
			// orderID 是与运行关联的订单；初始状态已付款待发货。
			const orderID = "lookup-order"
			// upsertErr 保存该步骤返回的错误，用于断言预期的成功或失败路径。
			if upsertErr := store.Orders.Upsert(ctx, orderID, OrderUpsertOpts{CookieID: cookieID, ItemID: "lookup-item", BuyerID: "buyer", OrderStatus: "pending_ship"}); upsertErr != nil {
				t.Fatal(upsertErr)
			}
			// runID、started、startErr 是首次创建的付款运行。
			runID, started, startErr := store.Automation.TryStartRun(ctx, AutomationRun{RuleID: ruleID, CookieID: cookieID, OrderID: orderID, TriggerType: "order_paid", TriggerKey: "lookup-key", LeaseExpiresAt: time.Now().Add(-time.Minute).Unix()})
			if startErr != nil || !started {
				t.Fatalf("start run started=%v err=%v", started, startErr)
			}
			// byKey、keyErr 按规则和幂等键读取。
			byKey, keyErr := store.Automation.GetRunByRuleAndTrigger(ctx, ruleID, "lookup-key")
			if keyErr != nil || byKey.ID != runID {
				t.Fatalf("by key=%+v err=%v", byKey, keyErr)
			}
			// missingErr 保存该步骤返回的错误，用于断言预期的成功或失败路径。
			if _, missingErr := store.Automation.GetRunByRuleAndTrigger(ctx, ruleID, "absent"); !errors.Is(missingErr, ErrNotFound) {
				t.Fatalf("missing key err=%v", missingErr)
			}
			// latest、latestErr 按账号与订单读取最近付款运行。
			latest, latestErr := store.Automation.GetLatestOrderDeliveryRun(ctx, cookieID, orderID)
			if latestErr != nil || latest.ID != runID {
				t.Fatalf("latest=%+v err=%v", latest, latestErr)
			}
			// otherErr 保存该步骤返回的错误，用于断言预期的成功或失败路径。
			if _, otherErr := store.Automation.GetLatestOrderDeliveryRun(ctx, cookieID, "other-order"); !errors.Is(otherErr, ErrNotFound) {
				t.Fatalf("other order err=%v", otherErr)
			}
			// 订单仍待发货时，不得取消。
			canceled, cancelErr := store.Automation.CancelObsoletePaidRecoveryRun(ctx, *byKey, "订单已变化")
			if cancelErr != nil || canceled {
				t.Fatalf("pending order canceled=%v err=%v", canceled, cancelErr)
			}
			// 订单被取消后，同一快照可以取消该运行。
			if upsertErr := store.Orders.Upsert(ctx, orderID, OrderUpsertOpts{CookieID: cookieID, OrderStatus: "cancelled", PlatformSnapshot: true}); upsertErr != nil {
				t.Fatal(upsertErr)
			}
			canceled, cancelErr = store.Automation.CancelObsoletePaidRecoveryRun(ctx, *byKey, "订单已变化")
			if cancelErr != nil || !canceled {
				t.Fatalf("cancelled order canceled=%v err=%v", canceled, cancelErr)
			}
			// 快照已过期（状态已变），再次取消必须失败。
			canceled, cancelErr = store.Automation.CancelObsoletePaidRecoveryRun(ctx, *byKey, "订单已变化")
			if cancelErr != nil || canceled {
				t.Fatalf("stale snapshot canceled=%v err=%v", canceled, cancelErr)
			}
		})
	}
}

// TestMultiDB_CardConditionalUpdateAndMetadata 验证卡券库存快照守卫更新与只改元数据的更新不会覆盖并发消费后的库存。
func TestMultiDB_CardConditionalUpdateAndMetadata(t *testing.T) {
	for _, target := range allTestTargets(t) { // target 包含当前方言的真实存储和销毁函数。
		t.Run(target.name, func(t *testing.T) { // t 负责当前方言的断言。
			defer target.cleanup()
			// store、ctx 是当前子测试独占的仓储和上下文。
			store, ctx := target.store, context.Background()
			// userID 是卡券所有者。
			userID, _ := seedAccount(t, store)
			// cardID、createErr 是新建的 data 卡。
			cardID, createErr := store.Cards.Create(ctx, &CardFull{Name: "卡", Type: "data", DataContent: "a\nb", Enabled: true, UserID: userID})
			if createErr != nil {
				t.Fatal(createErr)
			}
			// edited 是编辑者提交的新内容。
			edited := &CardFull{ID: cardID, Name: "新名", Type: "data", DataContent: "x", Enabled: true, UserID: userID}
			// updated、updateErr：库存快照一致时更新成功。
			updated, updateErr := store.Cards.UpdateIfDataUnchanged(ctx, edited, "a\nb")
			if updateErr != nil || !updated {
				t.Fatalf("matching snapshot updated=%v err=%v", updated, updateErr)
			}
			// 库存已被改变，旧快照更新必须被拒绝且不写入。
			updated, updateErr = store.Cards.UpdateIfDataUnchanged(ctx, &CardFull{ID: cardID, Name: "旧快照", Type: "data", DataContent: "overwrite", UserID: userID}, "a\nb")
			if updateErr != nil || updated {
				t.Fatalf("stale snapshot updated=%v err=%v", updated, updateErr)
			}
			// metadata 只修改元数据，库存正文保持。
			if metaErr := store.Cards.UpdateDataMetadata(ctx, &CardFull{ID: cardID, Name: "仅元数据", Type: "data", Description: "d", Enabled: false, UserID: userID}); metaErr != nil {
				t.Fatal(metaErr)
			}
			// card、getErr 是更新后的卡券。
			card, getErr := store.Cards.Get(ctx, cardID)
			if getErr != nil || card.Name != "仅元数据" || card.DataContent != "x" || card.Enabled {
				t.Fatalf("card=%+v err=%v", card, getErr)
			}
		})
	}
}
