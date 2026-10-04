package db

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// TestKeywordMutationCallbacksRollback 验证事务内应用回调失败、取消和资源缺失都不会写入部分状态。
func TestKeywordMutationCallbacksRollback(t *testing.T) {
	// store、cleanup 保存独立 SQLite 存储与关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// cookieID 是测试账号，关键词事务只读取其非敏感标识。
	_, cookieID := seedAccount(t, store)
	// ctx 是正常写入使用的上下文。
	ctx := context.Background()
	// id、addErr 保存原始规则的主键与写入结果。
	id, addErr := store.Keywords.AddWithExpressions(ctx, cookieID, []string{"foo ", `bar\ `}, "原回复", "", "text", "regexp", "")
	if addErr != nil {
		t.Fatal(addErr)
	}
	// original、listErr 保存任何失败操作前的完整快照。
	original, listErr := store.Keywords.AllRows(ctx, cookieID)
	if listErr != nil {
		t.Fatal(listErr)
	}
	// rejected 是应用层纯校验失败的哨兵错误。
	rejected := errors.New("拒绝不完整配置")
	// updateErr 保存应用回调拒绝更新的结果。
	updateErr := store.Keywords.UpdateWithCurrent(ctx, cookieID, id, func(current KeywordRow) (KeywordRow, error) {
		// current 是事务内读取的原规则，修改返回值不能在错误情况下落库。
		current.Reply = "不应写入"
		return current, rejected
	})
	if !errors.Is(updateErr, rejected) {
		t.Fatalf("更新错误未透传: %v", updateErr)
	}
	// replaceErr 验证批次校验失败发生在删除旧规则之前。
	replaceErr := store.Keywords.ReplaceWithCurrent(ctx, cookieID, func(current []KeywordRow) ([]KeywordRow, error) {
		// current 应与操作前快照一致，回调失败时整个事务回滚。
		if !reflect.DeepEqual(current, original) {
			t.Errorf("事务快照不一致: %+v", current)
		}
		return nil, rejected
	})
	if !errors.Is(replaceErr, rejected) {
		t.Fatalf("替换错误未透传: %v", replaceErr)
	}
	// called 统计目标缺失时不应发生的应用回调。
	called := false
	// missingErr 是同账号不存在规则的更新结果。
	missingErr := store.Keywords.UpdateWithCurrent(ctx, cookieID, id+100, func(current KeywordRow) (KeywordRow, error) {
		// current 不应被构造，防止缺失规则被当作默认配置写回。
		called = true
		return current, nil
	})
	if !errors.Is(missingErr, ErrNotFound) || called {
		t.Fatalf("缺失规则进入回调: err=%v called=%v", missingErr, called)
	}
	// missingAccountErr 是不存在账号的原子批次写入结果。
	missingAccountErr := store.Keywords.ReplaceForCookie(ctx, "missing-account", nil)
	if !errors.Is(missingAccountErr, ErrNotFound) {
		t.Fatalf("缺失账号错误=%v", missingAccountErr)
	}
	// canceled、cancel 拥有已取消的调用上下文，不能获取写入事务。
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	// err 保存预先取消后申请事务的错误，不能进入实际写入。
	if err := store.Keywords.UpdateByID(canceled, original[0]); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消未阻止更新: %v", err)
	}
	// after、afterErr 验证所有失败路径完成后原始主键、表达式和回复都未变化。
	after, afterErr := store.Keywords.AllRows(ctx, cookieID)
	if afterErr != nil || !reflect.DeepEqual(after, original) {
		t.Fatalf("失败事务修改了规则: rows=%+v err=%v", after, afterErr)
	}
}

// TestKeywordUpdateUsesLatestSnapshot 验证连续回调在事务中读取最新配置，且不能用返回行改变账号或主键。
func TestKeywordUpdateUsesLatestSnapshot(t *testing.T) {
	// store、cleanup 保存独立 SQLite 存储与关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// cookieID 是本次快照更新所属账号。
	_, cookieID := seedAccount(t, store)
	// ctx 是本测试的数据库上下文。
	ctx := context.Background()
	// id、addErr 保存初始规则标识和创建错误。
	id, addErr := store.Keywords.Add(ctx, cookieID, "旧词", "旧回复", "", "text", "")
	if addErr != nil {
		t.Fatal(addErr)
	}
	// firstErr 保存首次完整配置更新的结果。
	firstErr := store.Keywords.UpdateByID(ctx, KeywordRow{ID: id, CookieID: cookieID, Expressions: []string{"^new$", " second "}, MatchType: "regexp", Reply: "新回复"})
	if firstErr != nil {
		t.Fatal(firstErr)
	}
	// secondErr 验证回调继承第一次更新后的匹配配置，并只改回复。
	secondErr := store.Keywords.UpdateWithCurrent(ctx, cookieID, id, func(current KeywordRow) (KeywordRow, error) {
		// current 必须是当前事务内的最新持久化快照。
		if current.MatchType != "regexp" || !reflect.DeepEqual(current.Expressions, []string{"^new$", " second "}) {
			t.Errorf("使用了过期快照: %+v", current)
		}
		current.ID, current.CookieID = id+1, "other-account"
		current.Reply = "仅更新回复"
		return current, nil
	})
	if secondErr != nil {
		t.Fatal(secondErr)
	}
	// rows、listErr 确认最终记录归属未被回调返回值改变。
	rows, listErr := store.Keywords.AllRows(ctx, cookieID)
	if listErr != nil || len(rows) != 1 || rows[0].ID != id || rows[0].Reply != "仅更新回复" || rows[0].MatchType != "regexp" {
		t.Fatalf("事务写回改变身份或丢失模式: rows=%+v err=%v", rows, listErr)
	}
}
