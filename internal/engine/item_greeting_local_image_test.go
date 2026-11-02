package engine

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/db"
)

// TestItemGreetingLocalImageDelivery 用 t 验证本地图传递、纯图片、图文与一次性成功和未知结果保护。
func TestItemGreetingLocalImageDelivery(t *testing.T) {
	// scenario 定义本次投递结果和是否含文字；每例拥有独立记录表。
	for _, scenario := range []struct {
		// name 是场景标签；text 是可为空的首次回复文字。
		name, text string
		// uncertain 表示外部发送结果未知，不能自动重放。
		uncertain bool
	}{
		{name: "纯图片成功"},
		{name: "图文成功", text: "欢迎 {item_id}"},
		{name: "图片结果未知", uncertain: true},
	} {
		t.Run(scenario.name, func(t *testing.T) { // t 报告当前来源和投递结果场景。
			// store、cleanup 是隔离账号数据库及其释放函数。
			store, cleanup := newReplyStore(t)
			defer cleanup()
			// ctx 是本地引擎请求上下文。
			ctx := context.Background()
			// createErr 验证规则持久化，不手工绕过匹配入口。
			if _, createErr := store.ItemGreetings.Create(ctx, db.ItemGreetingRule{CookieID: "cid", Enabled: true, ReplyContent: scenario.text, ReplyImagePath: "_library/guide.png", ItemIDs: []string{"item1"}}); createErr != nil {
				t.Fatal(createErr)
			}
			// delivery 捕获真实投递消息，结果未知时模拟超时而不是重试成功。
			delivery := &recordingReplyDelivery{result: ReplySendResult{ImageSent: true, TextSent: true}}
			if scenario.uncertain {
				delivery.result = ReplySendResult{Uncertain: true}
				delivery.err = errors.New("测试发送结果未知")
			}
			// service 使用生产首次回复匹配与分段状态机。
			service := NewReplyService("cid", store, delivery, nil, nil, nil)
			service.handleItemGreeting(ctx, chatMsg("hi", "item1", "chat1"))
			service.handleItemGreeting(ctx, chatMsg("again", "item1", "chat1"))
			if len(delivery.messages) != 1 || delivery.messages[0].ImagePath != "_library/guide.png" || delivery.messages[0].ImageURL != "" {
				t.Fatal("本地图必须只投递一次，并通过本地引用而不是URL发送")
			}
			if scenario.text != "" && delivery.messages[0].Text != "欢迎 item1" {
				t.Fatal("图文回复必须保留模板渲染")
			}
		})
	}
}

// TestItemGreetingLocalImagePartialRetry 用 t 验证图片成功文字失败后只重试文字，不再次上传或发送图片。
func TestItemGreetingLocalImagePartialRetry(t *testing.T) {
	// store、cleanup 是本场景的隔离数据库和释放函数。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 只驱动本地替身投递。
	ctx := context.Background()
	// createErr 确认图文规则建立成功。
	if _, createErr := store.ItemGreetings.Create(ctx, db.ItemGreetingRule{CookieID: "cid", Enabled: true, ReplyContent: "欢迎", ReplyImagePath: "_library/guide.png", ItemIDs: []string{"item1"}}); createErr != nil {
		t.Fatal(createErr)
	}
	// delivery 首次仅确认图片，文字确定失败后允许安全续发。
	delivery := &recordingReplyDelivery{result: ReplySendResult{ImageSent: true}, err: errors.New("文字确定未发出")}
	// service 复用生产首次回复分段记录。
	service := NewReplyService("cid", store, delivery, nil, nil, nil)
	service.handleItemGreeting(ctx, chatMsg("hi", "item1", "chat1"))
	delivery.err = nil
	delivery.result = ReplySendResult{TextSent: true}
	service.handleItemGreeting(ctx, chatMsg("again", "item1", "chat1"))
	if len(delivery.messages) != 2 || delivery.messages[1].ImagePath != "" || delivery.messages[1].Text != "欢迎" {
		t.Fatal("续发只能处理尚未成功的文字分段")
	}
}
