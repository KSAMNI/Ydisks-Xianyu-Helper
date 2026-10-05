package keywords

import (
	"context"
	"errors"
	"testing"
)

// UpdateItemReply 用 f 的当前商品配置运行 build，模拟事务内合并与失败回滚。
func (f *keywordRepositoryFake) UpdateItemReply(_ context.Context, _ int64, cookieID, itemID string, build func(ItemReply) (ItemReply, error)) error {
	if f.itemErr != nil {
		return f.itemErr
	}
	// current 保存目标商品的当前配置，缺失时允许创建。
	current := ItemReply{CookieID: cookieID, ItemID: itemID}
	if len(f.itemRows) > 0 {
		current = f.itemRows[0]
	}
	// next、err 是应用合并结果及输入拒绝原因。
	next, err := build(current)
	if err != nil {
		return err
	}
	f.itemRows = []ItemReply{next}
	return nil
}

// TestItemReplyDraftPreservesImages 验证商品旧字段缺省、本地图与 URL 互斥、显式清除及输入验证。
func TestItemReplyDraftPreservesImages(t *testing.T) {
	// repository、service 分别保存商品当前快照和应用服务。
	repository := &keywordRepositoryFake{itemRows: []ItemReply{{CookieID: "account", ItemID: "item", ReplyImagePath: "详情.png"}}}
	// service 在内存快照上执行商品图文兼容校验。
	service := NewService(repository)
	// ctx 用于纯内存用例测试。
	ctx := context.Background()
	if err := service.SetItemReplyDraft(ctx, 1, "account", "item", ItemReplyDraft{ReplyContent: "更新"}); err != nil { // err 是旧客户端式保存结果。
		t.Fatal(err)
	}
	if repository.itemRows[0].ReplyImagePath != "详情.png" || repository.itemRows[0].ReplyContent != "更新" {
		t.Fatalf("缺省图片字段丢失: %+v", repository.itemRows)
	}
	// remote、empty、invalid 表示新网络来源、清空值和目录穿越引用。
	remote, empty, invalid := "https://example.test/image.png", "", "../outside.png"
	if err := service.SetItemReplyDraft(ctx, 1, "account", "item", ItemReplyDraft{ReplyImageURL: &remote}); err == nil { // err 应拒绝与当前本地图冲突的来源。
		t.Fatal("来源冲突被接受")
	}
	if err := service.SetItemReplyDraft(ctx, 1, "account", "item", ItemReplyDraft{ReplyImagePath: &invalid}); err == nil { // err 应拒绝穿越路径。
		t.Fatal("越界路径被接受")
	}
	if repository.itemRows[0].ReplyContent != "更新" {
		t.Fatal("失败修改了正文")
	}
	if err := service.SetItemReplyDraft(ctx, 1, "account", "item", ItemReplyDraft{ReplyImageURL: &remote, ReplyImagePath: &empty}); err != nil { // err 不应阻止明确来源切换。
		t.Fatal(err)
	}
	if repository.itemRows[0].ReplyImageURL != remote || repository.itemRows[0].ReplyImagePath != "" {
		t.Fatal("未切换图片来源")
	}
	if err := service.SetItemReplyDraft(ctx, 0, "account", "item", ItemReplyDraft{}); !errors.Is(err, ErrInvalidUser) { // err 应拒绝无效身份。
		t.Fatalf("身份错误=%v", err)
	}
	if err := service.SetItemReplyDraft(ctx, 1, "account", "", ItemReplyDraft{}); err == nil { // err 应拒绝空商品。
		t.Fatal("空商品被接受")
	}
}
