package itemgreeting

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestLocalImageDraftValidation 用 t 验证本地图允许纯图片回复、空白归一、路径边界和 URL 互斥，旧 URL 行为不变。
func TestLocalImageDraftValidation(t *testing.T) {
	// cases 覆盖新增来源的所有确定性校验分支。
	cases := []struct {
		// name 是用例语义；path、url 是互斥图片来源；valid 表示是否允许保存。
		name, path, url string
		// valid 为真时必须保留原来源，否则必须返回输入错误。
		valid bool
	}{
		{"共享纯图", " _library/guide.png ", "", true},
		{"账号相对路径", "goods/guide.png", "", true},
		{"旧URL", "", "https://example.test/a.png", true},
		{"双来源", "_library/guide.png", "https://example.test/a.png", false},
		{"穿越", "../a.png", "", false},
		{"绝对路径", "/a.png", "", false},
		{"盘符", "C:/a.png", "", false},
		{"控制字符", "a\x00.png", "", false},
		{"过长路径", strings.Repeat("a", 1025), "", false},
		{"空图空文", "", "", false},
	}
	// scenario 是一组独立来源输入，服务使用新内存仓储防止商品占用影响校验。
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) { // t 收集当前输入组合的校验结果。
			// service 仅使用内存归属和规则数据，不连接平台。
			service := NewService(newFake())
			// rule、err 是纯图片草稿的保存结果及校验错误。
			rule, err := service.Create(context.Background(), 1, Draft{CookieID: "acc1", Enabled: true, ReplyImageURL: scenario.url, ReplyImagePath: scenario.path, ItemIDs: []string{"i1"}})
			if !scenario.valid {
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("非法来源应拒绝: %v", err)
				}
				return
			}
			if err != nil || rule.ReplyImagePath != strings.TrimSpace(scenario.path) || rule.ReplyImageURL != scenario.url {
				t.Fatalf("图片来源未正确保存: %v", err)
			}
			// updated、updateErr 验证更新可显式清空图片并切回文字，避免残留隐藏来源。
			updated, updateErr := service.Update(context.Background(), 1, rule.ID, Draft{ReplyContent: "文字", ItemIDs: []string{"i1"}})
			if updateErr != nil || updated.ReplyImagePath != "" || updated.ReplyImageURL != "" {
				t.Fatalf("清空图片失败: %v", updateErr)
			}
		})
	}
}
