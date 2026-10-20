package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestKeywordHandlersRejectMissingSession 验证关键词与商品回复的每个处理器在缺少认证会话时直接返回 401，
// 不触达应用服务（服务端为空装配，一旦继续执行会 panic），即使认证中间件被绕过也不会泄露或修改规则。
func TestKeywordHandlersRejectMissingSession(t *testing.T) {
	// srv 是没有装配任何应用服务的服务端；缺少会话的请求必须在访问应用服务之前返回。
	srv := &Server{}
	// cases 列出每个处理器及其典型请求，请求不携带会话上下文。
	cases := []struct {
		// name 是处理器名称。
		name string
		// handler 是被测处理器。
		handler http.HandlerFunc
		// method、body 是请求方法与正文。
		method, body string
	}{
		{"listKeywords", srv.listKeywords, http.MethodGet, ""},
		{"addKeyword", srv.addKeyword, http.MethodPost, `{"keyword":"价格","reply":"50元"}`},
		{"listKeywordsWithItemID", srv.listKeywordsWithItemID, http.MethodGet, ""},
		{"addKeywordWithItemID", srv.addKeywordWithItemID, http.MethodPost, `{"keyword":"价格","reply":"50元"}`},
		{"listKeywordsWithType", srv.listKeywordsWithType, http.MethodGet, ""},
		{"updateKeywordByID", srv.updateKeywordByID, http.MethodPut, `{"keyword":"价格","reply":"50元"}`},
		{"deleteKeywordByID", srv.deleteKeywordByID, http.MethodDelete, ""},
		{"deleteKeyword", srv.deleteKeyword, http.MethodDelete, ""},
		{"listItemReplies", srv.listItemReplies, http.MethodGet, ""},
		{"getItemReply", srv.getItemReply, http.MethodGet, ""},
		{"setItemReply", srv.setItemReply, http.MethodPut, `{"reply_content":"回复"}`},
		{"deleteItemReply", srv.deleteItemReply, http.MethodDelete, ""},
	}
	// tc 是当前处理器场景。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// recorder 保存 NewRecorder 调用的返回结果，供随后的断言使用。
			recorder := httptest.NewRecorder()
			tc.handler(recorder, httptest.NewRequest(tc.method, "/x", strings.NewReader(tc.body)))
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("缺少会话应返回 401，got %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
