package keywords

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// TestServiceRegexpPreservesWhitespace 验证正则的空白具有语法意义，而包含匹配继续裁剪输入。
func TestServiceRegexpPreservesWhitespace(t *testing.T) {
	// cases 覆盖边界空白、转义空格、纯空格及历史单字段输入。
	cases := []struct {
		// name 标识当前表达式输入方式。
		name string
		// draft 是用户提交的规则，不允许校验改写正则原文。
		draft Draft
		// want 是应交给仓储的最终表达式。
		want string
	}{
		{name: "regexp trailing space", draft: Draft{Expressions: []string{"foo "}, MatchType: "regexp", Reply: "回复"}, want: "foo "},
		{name: "regexp escaped space", draft: Draft{Expressions: []string{`foo\ `}, MatchType: "regexp", Reply: "回复"}, want: `foo\ `},
		{name: "regexp whitespace only", draft: Draft{Expressions: []string{" "}, MatchType: "regexp", Reply: "回复"}, want: " "},
		{name: "regexp legacy field", draft: Draft{Keyword: " foo ", MatchType: "regexp", Reply: "回复"}, want: " foo "},
		{name: "contains trims", draft: Draft{Expressions: []string{" foo "}, Reply: "回复"}, want: "foo"},
	}
	// scenario 保存当前表达式原文与预期持久化形式。
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			// repository 记录真实服务规范化后交给持久化层的内容。
			repository := &keywordRepositoryFake{}
			// err 保存规则创建时的语法校验错误。
			_, err := NewService(repository).Add(context.Background(), 1, "account", scenario.draft)
			if err != nil || repository.addedDraft.Keyword != scenario.want || !reflect.DeepEqual(repository.addedDraft.Expressions, []string{scenario.want}) {
				t.Fatalf("保存改写了表达式: draft=%+v err=%v", repository.addedDraft, err)
			}
		})
	}
}

// TestServiceLegacyUpdatePreservesMatchConfiguration 验证更新缺省的新字段继承当前规则而不是创建默认值。
func TestServiceLegacyUpdatePreservesMatchConfiguration(t *testing.T) {
	// repository 模拟已持久化的多表达式正则规则。
	repository := &keywordRepositoryFake{listRows: []Keyword{{ID: 3, Keyword: "^hello$", Expressions: []string{"^hello$", "^price$"}, MatchType: "regexp"}}}
	// service 对旧格式更新复用现有匹配配置。
	service := NewService(repository)
	// err 保存仅修改回复正文的兼容更新结果。
	err := service.Update(context.Background(), 1, "account", 3, Draft{Keyword: "^hello$", Reply: "新回复"})
	if err != nil || repository.updatedDraft.MatchType != "regexp" || !reflect.DeepEqual(repository.updatedDraft.Expressions, []string{"^hello$", "^price$"}) {
		t.Fatalf("旧更新丢失匹配配置: draft=%+v err=%v", repository.updatedDraft, err)
	}
	// invalidErr 验证旧格式改写首表达式时仍按已有正则模式校验。
	invalidErr := service.Update(context.Background(), 1, "account", 3, Draft{Keyword: "[", Reply: "无效更新"})
	if invalidErr == nil || repository.updateCalls != 1 {
		t.Fatalf("无效兼容更新触发写入: err=%v writes=%d", invalidErr, repository.updateCalls)
	}
}

// TestServiceLegacyReplacementRejectsLossyConfiguration 验证缺少新字段的批量替换不会删除账号已有高级规则。
func TestServiceLegacyReplacementRejectsLossyConfiguration(t *testing.T) {
	// repository 模拟批量替换前已有无法由旧格式完整表达的规则。
	repository := &keywordRepositoryFake{listRows: []Keyword{{ID: 3, Keyword: "hello", Expressions: []string{"hello", "price"}, MatchType: "contains"}}}
	// service 拥有是否接受整个替换批次的校验责任。
	service := NewService(repository)
	// err 保存旧格式批次的整体校验结果。
	err := service.Replace(context.Background(), 1, "account", []Draft{{Keyword: "hello", Reply: "新回复"}})
	// validation 表示应能安全展示给客户端的拒绝原因。
	var validation *ValidationError
	if !errors.As(err, &validation) || repository.replaceCalls != 0 {
		t.Fatalf("旧批次不应静默删除表达式: err=%v writes=%d", err, repository.replaceCalls)
	}
	// explicitErr 验证显式指定全部匹配字段时仍允许整体覆盖。
	explicitErr := service.Replace(context.Background(), 1, "account", []Draft{{Expressions: []string{"new"}, MatchType: "contains", Reply: "新规则"}})
	if explicitErr != nil || repository.replaceCalls != 1 {
		t.Fatalf("完整批次被错误阻止: err=%v writes=%d", explicitErr, repository.replaceCalls)
	}
}
