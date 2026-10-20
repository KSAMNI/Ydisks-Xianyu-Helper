package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAIReplySettingsModeLifecycle 验证账号级 AI 工作模式的需求：
// 未配置账号读取到默认的仅砍价模式；保存时缺省模式按仅砍价处理；可切换为全场景客服；
// 非法模式必须作为请求错误（400）拒绝，且不改动已保存的配置。
func TestAIReplySettingsModeLifecycle(t *testing.T) {
	// srv、cleanup 是带管理员账号 acc1 的 HTTP 测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler、sessionCookie 是完整路由树及管理员会话。
	handler := srv.Router()
	// sessionCookie 保存 loginHelper 调用的返回结果，供随后的断言使用。
	sessionCookie := loginHelper(t, handler)
	// put 保存账号 AI 设置并返回响应；get 读取该账号的设置模式。
	put := func(body string) *httptest.ResponseRecorder {
		// request 保存 NewRequest 调用的返回结果，供随后的断言使用。
		request := httptest.NewRequest(http.MethodPut, "/ai-reply-settings/acc1", strings.NewReader(body))
		request.AddCookie(sessionCookie)
		// recorder 保存 NewRecorder 调用的返回结果，供随后的断言使用。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	// get 保存 func 调用的返回结果，供随后的断言使用。
	get := func() string {
		// request 保存 NewRequest 调用的返回结果，供随后的断言使用。
		request := httptest.NewRequest(http.MethodGet, "/ai-reply-settings/acc1", nil)
		request.AddCookie(sessionCookie)
		// recorder 保存 NewRecorder 调用的返回结果，供随后的断言使用。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		// value 保存响应 JSON 解码后的通用对象，仅用于读取 ai_mode 字段。
		var value map[string]any
		if json.Unmarshal(recorder.Body.Bytes(), &value) != nil {
			t.Fatalf("读取响应不是 JSON: %s", recorder.Body.String())
		}
		// mode 保存本断言步骤使用的中间值。
		mode, _ := value["ai_mode"].(string)
		return mode
	}
	// base 是除模式外的合法设置前缀，模式字段按用例追加。
	const base = `"ai_enabled":true,"auto_adjust_price_enabled":false,"max_discount_percent":10,"max_discount_amount":50,"max_bargain_rounds":3,"custom_prompts":""`

	// mode 保存 get 调用的返回结果，供随后的断言使用。
	if mode := get(); mode != "bargain_only" {
		t.Fatalf("未配置账号默认应为仅砍价模式，got %q", mode)
	}
	// recorder 保存 put 调用的返回结果，供随后的断言使用。
	if recorder := put(`{` + base + `}`); recorder.Code != http.StatusOK {
		t.Fatalf("缺省模式应可保存: %d %s", recorder.Code, recorder.Body.String())
	}
	// mode 保存 get 调用的返回结果，供随后的断言使用。
	if mode := get(); mode != "bargain_only" {
		t.Fatalf("缺省模式应落为仅砍价，got %q", mode)
	}
	// recorder 保存 put 调用的返回结果，供随后的断言使用。
	if recorder := put(`{` + base + `,"ai_mode":" full_service "}`); recorder.Code != http.StatusOK {
		t.Fatalf("全场景模式应可保存（含首尾空白）: %d %s", recorder.Code, recorder.Body.String())
	}
	// mode 保存 get 调用的返回结果，供随后的断言使用。
	if mode := get(); mode != "full_service" {
		t.Fatalf("保存后应为全场景模式，got %q", mode)
	}
	// 非法模式属于客户端输入错误：必须 400，且不得改动已保存的全场景配置。
	if recorder := put(`{` + base + `,"ai_mode":"always_reply"}`); recorder.Code != http.StatusBadRequest {
		t.Fatalf("非法模式应返回 400，got %d %s", recorder.Code, recorder.Body.String())
	}
	// mode 保存 get 调用的返回结果，供随后的断言使用。
	if mode := get(); mode != "full_service" {
		t.Fatalf("非法保存不得改动配置，got %q", mode)
	}
}
