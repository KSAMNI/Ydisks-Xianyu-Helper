package server

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

// TestKeywordCompatibilityHTTPUpdate 验证新旧 HTTP 入口更新正则规则时都保留客户端未提供的新字段。
func TestKeywordCompatibilityHTTPUpdate(t *testing.T) {
	// versioned 决定本次验证使用版本化还是旧兼容入口。
	for _, versioned := range []bool{false, true} {
		t.Run(fmt.Sprintf("versioned=%v", versioned), func(t *testing.T) {
			// server、store、cleanup 拥有当前测试的路由和 SQLite 状态。
			server, store, cleanup := newTestServer(t)
			defer cleanup()
			// handler、cookie 是使用真实认证中间件的测试入口。
			handler := server.Router()
			// cookie 是当前测试管理员的认证会话。
			cookie := loginHelper(t, handler)
			// createPath、updatePath 是同一业务的两套兼容路由。
			createPath, updatePath := "/keywords-with-item-id/acc1", "/keywords-with-type/acc1/"
			if versioned {
				createPath, updatePath = "/api/v1/reply-rules/acc1/items", "/api/v1/reply-rules/acc1/typed/"
			}
			// createBody 包含浏览器 JS 不支持的合法 RE2 标志与具有语法意义的尾空格。
			createBody := `{"keyword":"(?m)^foo$","expressions":["(?m)^foo$","bar\\ "],"match_type":"regexp","reply":"原回复","item_id":"","item_ids":[],"type":"text","image_url":""}`
			// status 验证真实 HTTP 入口对当前请求的状态码契约。
			if status := requestReplyRoute(t, handler, cookie, http.MethodPost, createPath, createBody); status != http.StatusOK {
				t.Fatalf("合法 RE2 创建状态=%d", status)
			}
			// initial、initialErr 保存创建后的真实数据库记录。
			initial, initialErr := store.Keywords.AllRows(context.Background(), "acc1")
			if initialErr != nil || len(initial) != 1 {
				t.Fatalf("创建记录缺失: rows=%+v err=%v", initial, initialErr)
			}
			// legacyBody 不包含 expressions/match_type，模拟旧客户端仅修改回复。
			legacyBody := `{"keyword":"(?m)^foo$","reply":"新回复","item_id":"","item_ids":[],"type":"text","image_url":""}`
			// status 验证真实 HTTP 入口对当前请求的状态码契约。
			if status := requestReplyRoute(t, handler, cookie, http.MethodPut, fmt.Sprintf("%s%d", updatePath, initial[0].ID), legacyBody); status != http.StatusOK {
				t.Fatalf("旧格式更新状态=%d", status)
			}
			// updated、updatedErr 读取更新后的完整匹配配置，验证字段未被默认值覆盖。
			updated, updatedErr := store.Keywords.AllRows(context.Background(), "acc1")
			if updatedErr != nil || len(updated) != 1 || updated[0].Reply != "新回复" || updated[0].MatchType != "regexp" || !reflect.DeepEqual(updated[0].Expressions, []string{"(?m)^foo$", `bar\ `}) {
				t.Fatalf("旧客户端更新破坏配置: rows=%+v err=%v", updated, updatedErr)
			}
			// batchBody 模拟旧客户端提交无法完整表达现有规则的全量替换。
			batchBody := `{"keywords":[{"keyword":"(?m)^foo$","reply":"批量覆盖","type":"text"}]}`
			// status 验证真实 HTTP 入口对当前请求的状态码契约。
			if status := requestReplyRoute(t, handler, cookie, http.MethodPost, createPath, batchBody); status != http.StatusBadRequest {
				t.Fatalf("不安全批次应返回 400，实际=%d", status)
			}
			// after、afterErr 确认失败批次既不删除旧记录，也不更改主键和回复。
			after, afterErr := store.Keywords.AllRows(context.Background(), "acc1")
			if afterErr != nil || !reflect.DeepEqual(after, updated) {
				t.Fatalf("失败批次未回滚: rows=%+v err=%v", after, afterErr)
			}
		})
	}
}
