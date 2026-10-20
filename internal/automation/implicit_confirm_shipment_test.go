package automation

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
)

// implicitConfirmCase 描述一次“规则动作配置 × 账号自动确认发货开关”的组合及期望的平台确认调用次数。
type implicitConfirmCase struct {
	// name 是用例名称。
	name string
	// extraActions 是除发卡外附加到规则上的动作，用于构造缺少或显式停用确认发货的规则。
	extraActions []db.AutomationActionInput
	// autoConsign 是账号“自动确认发货”开关。
	autoConsign bool
	// wantConsignCalls 是期望的普通确认发货接口调用次数。
	wantConsignCalls int
}

// TestPaidRuleWithoutConfirmActionStillHonorsAccountConsignSwitch 验证付款规则缺少 confirm_shipment 动作时，账号开关仍决定是否确认发货。
func TestPaidRuleWithoutConfirmActionStillHonorsAccountConsignSwitch(t *testing.T) {
	// cases 覆盖缺少动作开/关、显式停用动作以及显式启用动作四种组合。
	cases := []implicitConfirmCase{
		{name: "规则缺少确认动作且账号开启", autoConsign: true, wantConsignCalls: 1},
		{name: "规则缺少确认动作且账号关闭", autoConsign: false, wantConsignCalls: 0},
		{name: "规则显式停用确认动作且账号开启", extraActions: []db.AutomationActionInput{{ActionType: ActionConfirmShipment, Enabled: false, SortOrder: 2}}, autoConsign: true, wantConsignCalls: 0},
		{name: "规则显式启用确认动作且账号开启不重复确认", extraActions: []db.AutomationActionInput{{ActionType: ActionConfirmShipment, Enabled: true, SortOrder: 2}}, autoConsign: true, wantConsignCalls: 1},
	}
	// testCase 是当前待验证的组合。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// cleanup、store 分别保存：测试数据库的关闭函数；隔离的测试数据库仓储。
			store, cleanup := newAutomationTestStore(t)
			defer cleanup()
			// ctx 限定本用例内的同步调用。
			ctx := context.Background()
			// admin、adminErr 分别保存：规则所属的测试管理员账号；读取管理员账号的错误。
			admin, adminErr := store.Users.GetByUsername(ctx, "admin")
			if adminErr != nil || admin == nil {
				t.Fatalf("读取测试管理员失败: %v", adminErr)
			}
			// consign 是账号自动确认发货开关的目标值。
			consign := testCase.autoConsign
			// updateErr 保存更新账号设置的错误。
			if _, updateErr := store.Cookies.UpdateSettings(ctx, "cid", db.AccountSettingsUpdate{UserID: admin.ID, AutoConsign: &consign}); updateErr != nil {
				t.Fatal(updateErr)
			}
			// actions 是规则动作：一个发卡动作加上用例指定的附加动作。
			actions := append([]db.AutomationActionInput{catchupCardAction(catchupCard(t, store, admin.ID))}, testCase.extraActions...)
			catchupRule(t, store, admin.ID, "cid", "implicit-item", actions)
			// upsertErr 保存写入订单夹具的错误。
			if upsertErr := store.Orders.Upsert(ctx, "implicit-order", db.OrderUpsertOpts{ItemID: "implicit-item", BuyerID: "buyer-1", CookieID: "cid", ChatID: "chat-1", OrderStatus: "pending_ship", Quantity: "1", Amount: "9.90", SpecName: "颜色", SpecValue: "黑"}); upsertErr != nil {
				t.Fatal(upsertErr)
			}
			// client 记录普通确认发货调用次数的平台替身。
			client := &fakeMTop{consignOk: true, consignRet: []string{"SUCCESS::调用成功"}}
			// sender 记录发货内容的在线替身。
			sender := &testSender{}
			// center 保存注入平台替身的自动化中心。
			center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{MTop: client})
			// task 是明确卖家角色的付款事件。
			task := Task{Source: "ws", AccountID: "cid", OrderRole: OrderRoleSeller, TriggerType: TriggerOrderPaid, ChatID: "chat-1", OrderID: "implicit-order", ItemID: "implicit-item", BuyerID: "buyer-1"}
			// handleErr 保存付款事件处理返回的错误。
			if handleErr := center.HandleTask(ctx, task); handleErr != nil {
				t.Fatalf("付款事件处理失败: %v", handleErr)
			}
			if len(sender.texts) != 1 {
				t.Fatalf("应恰好发送一次发货内容: %v", sender.texts)
			}
			if client.consignCalls != testCase.wantConsignCalls {
				t.Fatalf("确认发货调用次数=%d want %d", client.consignCalls, testCase.wantConsignCalls)
			}
		})
	}
}

// TestPlannerAppendsImplicitConfirmOnlyForPaidDelivery 验证动作计划仅在付款发货且规则完全没有确认动作时补隐式确认步骤。
func TestPlannerAppendsImplicitConfirmOnlyForPaidDelivery(t *testing.T) {
	// card 是匹配订单规格的发卡动作。
	card := db.AutomationAction{ActionType: ActionSendCard, CardID: 1, Enabled: true, ConfigJSON: `{"spec_name":"颜色","spec_value":"黑"}`}
	// paid 是带规格的付款任务。
	paid := Task{TriggerType: TriggerOrderPaid, SpecName: "颜色", SpecValue: "黑"}
	// plan 是缺少确认动作时的付款计划：发卡之后补一步确认发货。
	plan := (actionPlanner{}).plan(paid, []db.AutomationAction{card})
	if len(plan) != 2 || plan[1].ActionType != ActionConfirmShipment || !plan[1].Enabled {
		t.Fatalf("缺少确认动作时应补隐式确认: %+v", plan)
	}
	// explicit 是规则已显式启用确认动作的计划，不应重复补充。
	explicit := (actionPlanner{}).plan(paid, []db.AutomationAction{card, {ActionType: ActionConfirmShipment, Enabled: true}})
	if len(explicit) != 2 {
		t.Fatalf("已有确认动作时不应重复补充: %+v", explicit)
	}
	// disabled 是规则显式停用确认动作的计划，必须尊重规则作者的选择。
	disabled := (actionPlanner{}).plan(paid, []db.AutomationAction{card, {ActionType: ActionConfirmShipment, Enabled: false}})
	if len(disabled) != 1 {
		t.Fatalf("显式停用确认动作时不应补隐式确认: %+v", disabled)
	}
	// unmatched 是规格不匹配、没有任何发货动作的计划，不应只剩一个确认步骤。
	unmatched := (actionPlanner{}).plan(Task{TriggerType: TriggerOrderPaid, SpecName: "颜色", SpecValue: "白"}, []db.AutomationAction{card})
	if len(unmatched) != 0 {
		t.Fatalf("没有匹配发货动作时不应单独补确认: %+v", unmatched)
	}
	// review 是评价赠品触发的计划，不属于付款发货，不补确认步骤。
	review := (actionPlanner{}).plan(Task{TriggerType: TriggerBuyerReviewed}, []db.AutomationAction{{ActionType: ActionSendText, Enabled: true}})
	if len(review) != 1 {
		t.Fatalf("非付款触发不应补确认: %+v", review)
	}
}
