package db

import (
	"context"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestItemGreetingLocalImagePersistence 用 t 验证图片引用完整读写、商品命中、停用引用统计及删除释放。
func TestItemGreetingLocalImagePersistence(t *testing.T) {
	// store、cleanup 是独立 SQLite 数据库及清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 只用于本地数据库操作。
	ctx := context.Background()
	// userID、cookieID 是查询归属和商品匹配所需的非敏感身份。
	userID, cookieID := seedAccount(t, store)
	// rule 是不含文字的共享图片首次回复。
	rule := ItemGreetingRule{CookieID: cookieID, Enabled: true, ReplyImagePath: "_library/guide.png", ItemIDs: []string{"i1"}}
	// id、err 保存新建记录主键及写入错误。
	id, err := store.ItemGreetings.Create(ctx, rule)
	if err != nil {
		t.Fatal(err)
	}
	rule.ID = id
	// readers 覆盖用户列表与账号列表，两者都必须读取路径。
	readers := []func() ([]ItemGreetingRule, error){
		func() ([]ItemGreetingRule, error) { // 返回当前账号的完整首次回复列表。
			return store.ItemGreetings.ListByAccount(ctx, cookieID)
		},
		func() ([]ItemGreetingRule, error) { // 返回当前用户拥有账号的完整首次回复列表。
			return store.ItemGreetings.ListForUser(ctx, userID)
		},
	}
	// read 是一个已绑定非敏感身份的列表入口。
	for _, read := range readers {
		// rules、readErr 是列表查询结果和扫描错误。
		rules, readErr := read()
		if readErr != nil || len(rules) != 1 || rules[0].ReplyImagePath != rule.ReplyImagePath {
			t.Fatalf("列表没有保留本地图: %v", readErr)
		}
	}
	// hit、hitErr 验证实际引擎匹配入口读取同一个路径。
	hit, hitErr := store.ItemGreetings.FindEnabledForItem(ctx, cookieID, "i1")
	if hitErr != nil || hit.ReplyImagePath != rule.ReplyImagePath {
		t.Fatalf("商品匹配丢失图片: %v", hitErr)
	}
	rule.Enabled = false
	rule.ReplyImagePath = "_library/new.png"
	// updateErr 检查修改来源并停用后仍保留图片引用。
	if updateErr := store.ItemGreetings.Update(ctx, rule); updateErr != nil {
		t.Fatal(updateErr)
	}
	// loaded、loadErr 验证按主键读取更新后的来源。
	loaded, loadErr := store.ItemGreetings.Get(ctx, id)
	if loadErr != nil || loaded.ReplyImagePath != rule.ReplyImagePath || loaded.Enabled {
		t.Fatalf("更新回读失败: %v", loadErr)
	}
	// counts、countErr 验证旧引用释放，新引用即使停用也受删除保护。
	counts, countErr := store.ImageAssets.ReferenceCounts(ctx)
	if countErr != nil || counts["_library/guide.png"] != 0 || counts[rule.ReplyImagePath] != 1 {
		t.Fatalf("首次回复引用计数错误: %v", countErr)
	}
	// deleteErr 删除规则后必须释放图片引用。
	if deleteErr := store.ItemGreetings.Delete(ctx, id); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	counts, countErr = store.ImageAssets.ReferenceCounts(ctx)
	if countErr != nil || counts[rule.ReplyImagePath] != 0 {
		t.Fatalf("删除未释放图片: %v", countErr)
	}
	// insertErr 模拟旧客户端不填写新列，默认值必须为空而非 NULL。
	if _, insertErr := store.DB.ExecContext(ctx, `INSERT INTO item_greeting_rules(cookie_id,name,enabled,reply_content,reply_image_url) VALUES (?, '旧规则',1,'','https://example.test/a.png')`, cookieID); insertErr != nil {
		t.Fatal(insertErr)
	}
	// legacy、legacyErr 验证旧形状行可被新读取器完整扫描。
	legacy, legacyErr := store.ItemGreetings.ListByAccount(ctx, cookieID)
	if legacyErr != nil || len(legacy) != 1 || legacy[0].ReplyImagePath != "" || legacy[0].ReplyImageURL == "" {
		t.Fatalf("旧URL默认路径不兼容: %v", legacyErr)
	}
}

// TestItemGreetingLocalImageUpgrade 用 t 验证 64→65 升级保留历史 URL 规则，并为新增路径填入安全空值。
func TestItemGreetingLocalImageUpgrade(t *testing.T) {
	// store、cleanup 拥有测试数据库与关闭责任；迁移账本只在此隔离库中回退。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是迁移后插入和回读共用的本地上下文。
	ctx := context.Background()
	// cookieID 是旧规则所属账号，不涉及平台凭证读取。
	_, cookieID := seedAccount(t, store)
	goose.SetBaseFS(migrationsFS)
	// downErr 只回退新增路径列，以真实旧 schema 构造兼容夹具。
	if downErr := goose.DownTo(store.DB, "migrations/sqlite", 64); downErr != nil {
		t.Fatal(downErr)
	}
	// insertErr 在没有新列的旧表中写入 URL 规则。
	if _, insertErr := store.DB.ExecContext(ctx, `INSERT INTO item_greeting_rules(cookie_id,name,enabled,reply_content,reply_image_url) VALUES (?,'升级前',1,'原文','https://example.test/legacy.png')`, cookieID); insertErr != nil {
		t.Fatal(insertErr)
	}
	// upErr 重新执行新增迁移，禁止通过重建规则来伪造升级结果。
	if upErr := goose.Up(store.DB, "migrations/sqlite"); upErr != nil {
		t.Fatal(upErr)
	}
	// rules、readErr 检查旧规则内容与URL逐项保持。
	rules, readErr := store.ItemGreetings.ListByAccount(ctx, cookieID)
	if readErr != nil || len(rules) != 1 || rules[0].ReplyImagePath != "" || rules[0].ReplyImageURL != "https://example.test/legacy.png" || rules[0].ReplyContent != "原文" {
		t.Fatalf("升级未保留历史规则: %v", readErr)
	}
}
