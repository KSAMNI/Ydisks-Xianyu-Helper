package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/logging"
)

// runtimeLogFixture 是运行日志契约测试直接写入进程内缓冲的已脱敏记录属性。
type runtimeLogFixture struct {
	// attrs 是模拟平台接口失败日志使用的属性集合。
	attrs []slog.Attr
}

// runtimeLogPageFixture 是运行日志接口响应体的最小解析模型，只覆盖契约断言需要的字段。
type runtimeLogPageFixture struct {
	// Entries 是本次返回的日志条目。
	Entries []struct {
		// Seq 是记录序号。
		Seq uint64 `json:"seq"`
		// Level 是记录等级。
		Level string `json:"level"`
		// Source 是记录来源分类。
		Source string `json:"source"`
		// Message 是记录消息。
		Message string `json:"message"`
		// API 是平台接口名称。
		API string `json:"api"`
		// Category 是平台失败分类。
		Category string `json:"category"`
	} `json:"entries"`
	// NextCursor 是下一次增量读取使用的排他游标。
	NextCursor uint64 `json:"next_cursor"`
	// Capacity 是服务端可保留的最大记录数。
	Capacity int `json:"capacity"`
}

// requestRuntimeLogs 发送带认证会话的运行日志查询请求并返回响应记录器。
func requestRuntimeLogs(t *testing.T, handler http.Handler, sessionCookie *http.Cookie, query string) *httptest.ResponseRecorder {
	t.Helper()
	// request 是本次运行日志查询请求，query 由调用方提供已编码的查询串。
	request := httptest.NewRequest(http.MethodGet, runtimeLogsPath+query, nil)
	if sessionCookie != nil {
		request.AddCookie(sessionCookie)
	}
	// recorder 捕获真实 Router 的状态码与响应体。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// decodeRuntimeLogPage 解析运行日志接口的成功响应体。
func decodeRuntimeLogPage(t *testing.T, recorder *httptest.ResponseRecorder) runtimeLogPageFixture {
	t.Helper()
	// page 是解析后的运行日志响应模型。
	var page runtimeLogPageFixture
	// err 表示响应体不是合法 JSON 时的解析失败原因。
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("解析运行日志响应失败: %v body=%s", err, recorder.Body.String())
	}
	return page
}

// TestOpenAPIRuntimeLogResponses 验证管理员运行日志查询接口的真实成功响应、过滤、参数校验和权限边界。
func TestOpenAPIRuntimeLogResponses(t *testing.T) {
	// buffer 是本次测试显式启用的进程级运行日志缓冲，测试结束后必须停用以避免影响其他用例。
	buffer := logging.EnableRuntimeBuffer(32)
	t.Cleanup(logging.DisableRuntimeBuffer)
	// srv、store、cleanup 分别是运行日志测试服务、测试数据库和资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)
	// fixture 是直接写入缓冲的模拟平台接口失败记录。
	fixture := runtimeLogFixture{attrs: []slog.Attr{
		slog.String("log_source", "mtop"),
		slog.String("api", "mtop.trade.order.render"),
		slog.String("category", "session_expired"),
		slog.String("ret", "FAIL_SYS_SESSION_EXPIRED"),
	}}
	buffer.Append(time.Now(), slog.LevelError, "MTOP 响应失败", fixture.attrs)

	// successRecorder 是未加过滤条件的真实成功响应。
	successRecorder := requestRuntimeLogs(t, handler, sessionCookie, "")
	if successRecorder.Code != http.StatusOK {
		t.Fatalf("运行日志查询 status=%d body=%s", successRecorder.Code, successRecorder.Body.String())
	}
	// successRequest 用于把同一请求交给 OpenAPI 响应契约校验。
	successRequest := httptest.NewRequest(http.MethodGet, runtimeLogsPath, nil)
	assertOpenAPISuccessResponse(t, successRequest, successRecorder)
	// page 是成功响应解析后的分页边界，用于确认游标与容量不是零值。
	if page := decodeRuntimeLogPage(t, successRecorder); page.Capacity == 0 || page.NextCursor == 0 {
		t.Fatalf("运行日志分页边界异常: %+v", page)
	}

	// filteredRecorder 是按来源与关键字过滤后的响应，必须命中写入的模拟平台失败记录。
	filteredRecorder := requestRuntimeLogs(t, handler, sessionCookie, "?source=mtop&keyword=mtop.trade.order.render&limit=10")
	if filteredRecorder.Code != http.StatusOK {
		t.Fatalf("过滤查询 status=%d body=%s", filteredRecorder.Code, filteredRecorder.Body.String())
	}
	// filteredPage 是过滤查询解析后的响应模型。
	filteredPage := decodeRuntimeLogPage(t, filteredRecorder)
	if len(filteredPage.Entries) != 1 {
		t.Fatalf("过滤查询命中 %d 条 want 1 body=%s", len(filteredPage.Entries), filteredRecorder.Body.String())
	}
	// entry 是唯一命中的运行日志条目。
	entry := filteredPage.Entries[0]
	if entry.Source != "mtop" || entry.Level != "error" || entry.API != "mtop.trade.order.render" || entry.Category != "session_expired" {
		t.Fatalf("运行日志条目字段异常: %+v", entry)
	}
	// levelRecorder 是按最低等级过滤后的响应，必须仍然命中错误等级记录。
	levelRecorder := requestRuntimeLogs(t, handler, sessionCookie, "?level=error&keyword=mtop.trade.order.render")
	if levelRecorder.Code != http.StatusOK || len(decodeRuntimeLogPage(t, levelRecorder).Entries) != 1 {
		t.Fatalf("等级过滤响应异常: status=%d body=%s", levelRecorder.Code, levelRecorder.Body.String())
	}
	// emptyRecorder 是关键字没有任何命中时的响应，用于校验空页也符合契约。
	emptyRecorder := requestRuntimeLogs(t, handler, sessionCookie, "?keyword=绝对不会命中的关键字")
	if emptyRecorder.Code != http.StatusOK {
		t.Fatalf("空页查询 status=%d body=%s", emptyRecorder.Code, emptyRecorder.Body.String())
	}
	// emptyRequest 用于把空页请求交给 OpenAPI 响应契约校验。
	emptyRequest := httptest.NewRequest(http.MethodGet, runtimeLogsPath, nil)
	assertOpenAPIResponse(t, emptyRequest, emptyRecorder)
	// emptyPage 是空页查询解析后的响应模型；游标必须推进到来源末尾而不是停在零。
	emptyPage := decodeRuntimeLogPage(t, emptyRecorder)
	if len(emptyPage.Entries) != 0 || emptyPage.NextCursor == 0 {
		t.Fatalf("空页响应异常: %+v", emptyPage)
	}
	// staleRecorder 是携带旧进程高游标的响应，必须回落到新缓冲的末尾而不是长期为空。
	staleRecorder := requestRuntimeLogs(t, handler, sessionCookie, "?since=100000")
	if staleRecorder.Code != http.StatusOK {
		t.Fatalf("旧游标查询 status=%d body=%s", staleRecorder.Code, staleRecorder.Body.String())
	}
	// stalePage 是旧游标查询解析后的响应模型。
	stalePage := decodeRuntimeLogPage(t, staleRecorder)
	if len(stalePage.Entries) == 0 || stalePage.NextCursor >= 100000 {
		t.Fatalf("旧游标未回落到来源末尾: %+v", stalePage)
	}

	// invalidCases 覆盖游标、条数和等级三类非法查询参数。
	invalidCases := []string{"?since=abc", "?limit=0", "?limit=-1", "?level=verbose"}
	// invalidQuery 是当前待验证的非法查询串。
	for _, invalidQuery := range invalidCases {
		// invalidRecorder 是非法查询参数的响应，必须返回 400 而不是静默放行。
		invalidRecorder := requestRuntimeLogs(t, handler, sessionCookie, invalidQuery)
		if invalidRecorder.Code != http.StatusBadRequest {
			t.Fatalf("非法查询 %s status=%d want 400", invalidQuery, invalidRecorder.Code)
		}
	}

	// anonymousRecorder 是未携带会话的响应，必须被认证边界拒绝。
	anonymousRecorder := requestRuntimeLogs(t, handler, nil, "")
	if anonymousRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("匿名访问 status=%d want 401", anonymousRecorder.Code)
	}
	// createErr 表示创建非管理员测试用户失败的原因。
	if _, createErr := store.Users.Create(context.Background(), "runtime-log-user", "runtime-log@example.com", "pw"); createErr != nil {
		t.Fatalf("创建非管理员用户失败: %v", createErr)
	}
	// loginRequest 是非管理员用户的登录请求。
	loginRequest := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"runtime-log-user","password":"pw"}`))
	// loginRecorder 捕获非管理员登录结果。
	loginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("非管理员登录 status=%d", loginRecorder.Code)
	}
	// memberRecorder 是非管理员访问运行日志接口的响应，必须返回 403。
	memberRecorder := requestRuntimeLogs(t, handler, loginRecorder.Result().Cookies()[0], "")
	if memberRecorder.Code != http.StatusForbidden {
		t.Fatalf("非管理员访问 status=%d want 403", memberRecorder.Code)
	}
}

// TestRuntimeLogHandlerRejectsMissingApplication 验证缺少运行日志应用端口时返回服务不可用。
func TestRuntimeLogHandlerRejectsMissingApplication(t *testing.T) {
	// server 是未注入应用端口集合的 Server，用于覆盖端口缺失分支。
	server := &Server{}
	// recorder 捕获端口缺失时的统一错误响应。
	recorder := httptest.NewRecorder()
	server.listRuntimeLogs(recorder, httptest.NewRequest(http.MethodGet, runtimeLogsPath, nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("缺少应用端口 status=%d want 503", recorder.Code)
	}
	// nilServer 是空接收者，用于覆盖空指针守卫分支。
	var nilServer *Server
	// nilRecorder 捕获空接收者时的统一错误响应。
	nilRecorder := httptest.NewRecorder()
	nilServer.listRuntimeLogs(nilRecorder, httptest.NewRequest(http.MethodGet, runtimeLogsPath, nil))
	if nilRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("空接收者 status=%d want 503", nilRecorder.Code)
	}
}

// TestRuntimeLogQueryParsesEmptySources 验证来源参数只包含空白项时视为不过滤。
func TestRuntimeLogQueryParsesEmptySources(t *testing.T) {
	// request 是来源参数只包含逗号与空白的查询请求。
	request := httptest.NewRequest(http.MethodGet, runtimeLogsPath+"?source=,%20,", nil)
	// query、err 分别是解析后的查询条件与错误。
	query, err := runtimeLogQueryFromRequest(request)
	if err != nil {
		t.Fatalf("来源参数解析失败: %v", err)
	}
	if len(query.Sources) != 0 {
		t.Fatalf("空来源项应被忽略: %+v", query.Sources)
	}
}
