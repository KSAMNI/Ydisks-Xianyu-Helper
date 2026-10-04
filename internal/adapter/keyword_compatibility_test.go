package adapter

import (
	"context"
	"errors"
	"reflect"
	"testing"

	keywordsapp "xianyu-go/internal/application/keywords"
)

// TestKeywordServicePreservesStoredConfiguration 验证真实 SQLite 上的旧客户端更新、批次拒绝与正则空白往返。
func TestKeywordServicePreservesStoredConfiguration(t *testing.T) {
	// store、cleanup 拥有当前测试独立的 SQLite 资源及清理责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是所有本地持久化操作共用的上下文。
	ctx := context.Background()
	// owner、ownerErr 保存测试账号的非敏感归属信息。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// service 通过真实适配器执行应用层校验和事务写入。
	service := keywordsapp.NewService(NewKeywordRepository(store))
	// id、addErr 保存初始多表达式规则的标识与创建结果。
	id, addErr := service.Add(ctx, owner.ID, "cid", keywordsapp.Draft{Expressions: []string{"^hello$", "^price$"}, MatchType: "regexp", Reply: "原回复"})
	if addErr != nil {
		t.Fatal(addErr)
	}
	// updateErr 保存旧客户端只修改回复正文的结果。
	updateErr := service.Update(ctx, owner.ID, "cid", id, keywordsapp.Draft{Keyword: "^hello$", Reply: "新回复"})
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	// rows、listErr 读取实际落库的匹配字段，不能只检查 API 返回成功。
	rows, listErr := service.List(ctx, owner.ID, "cid")
	if listErr != nil || len(rows) != 1 || rows[0].MatchType != "regexp" || rows[0].Reply != "新回复" || !reflect.DeepEqual(rows[0].Expressions, []string{"^hello$", "^price$"}) {
		t.Fatalf("旧更新改变了配置: rows=%+v err=%v", rows, listErr)
	}
	// replaceErr 验证无法无损表示旧规则的批次在删除旧数据前被拒绝。
	replaceErr := service.Replace(ctx, owner.ID, "cid", []keywordsapp.Draft{{Keyword: "^hello$", Reply: "覆盖回复"}})
	// validation 是稳定的业务校验错误，不能伪装成数据库错误。
	var validation *keywordsapp.ValidationError
	if !errors.As(replaceErr, &validation) {
		t.Fatalf("不安全批次应被拒绝: %v", replaceErr)
	}
	// unchanged、unchangedErr 确认拒绝不会留下部分删除或重建。
	unchanged, unchangedErr := service.List(ctx, owner.ID, "cid")
	if unchangedErr != nil || !reflect.DeepEqual(rows, unchanged) {
		t.Fatalf("失败批次修改了已有规则: rows=%+v err=%v", unchanged, unchangedErr)
	}
	// whitespaceErr 保存带转义空格和纯空格正则的完整更新结果。
	whitespaceErr := service.Update(ctx, owner.ID, "cid", id, keywordsapp.Draft{Expressions: []string{`foo\ `, " "}, MatchType: "regexp", Reply: "空白规则"})
	if whitespaceErr != nil {
		t.Fatal(whitespaceErr)
	}
	// runtimeRows、runtimeErr 经运行时使用的数据库入口验证表达式原文不变。
	runtimeRows, runtimeErr := store.Keywords.AllWithType(ctx, "cid")
	if runtimeErr != nil || len(runtimeRows) != 1 || !reflect.DeepEqual(runtimeRows[0].Expressions, []string{`foo\ `, " "}) {
		t.Fatalf("运行时读取裁剪了正则: rows=%+v err=%v", runtimeRows, runtimeErr)
	}
}
