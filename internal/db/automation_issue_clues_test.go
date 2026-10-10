package db

import "testing"

// TestIssueSnapshotClues 验证延期快照线索只在归属一致且字段存在时回退展示，
// 快照损坏、归属不一致或字段缺失时一律返回零值，绝不猜测关联。
func TestIssueSnapshotClues(t *testing.T) {
	// cases 覆盖完整当前格式、旧字段兼容、归属冲突和损坏快照四类场景。
	cases := []struct {
		// name 是子测试名；raw 是任务快照；accountID 是记录账号；
		// wantItem、wantBuyer、wantChat 是预期回退展示的线索，空值表示不得展示。
		name, raw, accountID, wantItem, wantBuyer, wantChat string
	}{
		{
			name:      "当前格式完整线索",
			raw:       `{"AccountID":"cid","ItemID":"item-1","BuyerID":"buyer-1","ChatID":"chat-1"}`,
			accountID: "cid", wantItem: "item-1", wantBuyer: "buyer-1", wantChat: "chat-1",
		},
		{
			name:      "旧字段兼容",
			raw:       `{"account_id":"cid","item_id":"item-2","buyer_id":"buyer-2","chat_id":"chat-2"}`,
			accountID: "cid", wantItem: "item-2", wantBuyer: "buyer-2", wantChat: "chat-2",
		},
		{
			name:      "归属不一致不展示",
			raw:       `{"AccountID":"other","ItemID":"item-3","BuyerID":"buyer-3","ChatID":"chat-3"}`,
			accountID: "cid",
		},
		{
			name:      "损坏快照返回零值",
			raw:       `{not-json`,
			accountID: "cid",
		},
		{
			name:      "字段缺失部分回退",
			raw:       `{"AccountID":"cid","ItemID":"item-4"}`,
			accountID: "cid", wantItem: "item-4",
		},
	}
	// scenario 是当前的快照线索场景。
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			// clues 是从快照解析出的非敏感身份线索。
			clues := issueSnapshotClues(scenario.raw, scenario.accountID)
			if clues.ItemID != scenario.wantItem || clues.BuyerID != scenario.wantBuyer || clues.ChatID != scenario.wantChat {
				t.Fatalf("线索回退不符: got=(%s,%s,%s) want=(%s,%s,%s)",
					clues.ItemID, clues.BuyerID, clues.ChatID, scenario.wantItem, scenario.wantBuyer, scenario.wantChat)
			}
		})
	}
}
