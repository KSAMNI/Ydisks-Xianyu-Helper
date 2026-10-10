package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAutomationOrderStopRealHandlerContract 验证真实 HTTP 列表包含任务类型及可靠订单，并通过两种异常入口持久停用。
func TestAutomationOrderStopRealHandlerContract(t *testing.T) {
	for _, deferred := range []bool{false, true} { // deferred 选择运行或延期入口，二者均校验真实 OpenAPI 契约。
		t.Run(fmt.Sprintf("deferred=%t", deferred), func(t *testing.T) { // t 拥有当前真实服务及 SQLite 数据库。
			// srv、store、cleanup 提供隔离 HTTP 应用和持久化夹具。
			srv, store, cleanup := newTestServer(t)
			defer cleanup()
			// ctx 是本场景本地数据库操作的生命周期根。
			ctx := context.Background()
			// statements 创建一个可靠订单和两类人工处理任务。
			statements := []string{
				`INSERT INTO orders(order_id,cookie_id,item_id,buyer_id,chat_id,order_status) VALUES ('contract-stop','acc1','item','buyer','chat','pending_ship')`,
				`INSERT INTO automation_rules(id,user_id,cookie_id,name,trigger_type,enabled,config_json) VALUES (712,1,'acc1','contract-stop-rule','review_missing_timeout',1,'{}')`,
				`INSERT INTO automation_runs(id,rule_id,cookie_id,order_id,trigger_type,trigger_key,status,raw_event_json,error_message) VALUES (713,712,'acc1','contract-stop','review_missing_timeout','contract-stop','needs_review','{"AccountID":"acc1","OrderID":"contract-stop"}','等待人工核对')`,
				`INSERT INTO automation_pending_tasks(id,task_key,cookie_id,trigger_type,task_json,status,attempt_count) VALUES (714,'contract-stop','acc1','order_paid','{"AccountID":"acc1","OrderID":"contract-stop"}','dead_letter',5)`,
			}
			for _, statement := range statements { // statement 是固定测试 SQL，不读取真实账号凭证。
				if _, err := store.DB.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				} // err 保证夹具完整性。
			}
			// handler、sessionCookie 是真实路由和测试管理员会话。
			handler := srv.Router()
			// sessionCookie 仅用于本地 HTTP 认证，不输出到测试失败日志。
			sessionCookie := loginHelper(t, handler)
			// listRequest、listResponse 验证摘要查询的实际契约。
			listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/automation-issues", nil)
			listRequest.AddCookie(sessionCookie)
			// listResponse 捕获不含数据库模型或敏感载荷的响应。
			listResponse := httptest.NewRecorder()
			handler.ServeHTTP(listResponse, listRequest)
			assertOpenAPIRecordedSuccessResponse(t, listRequest, listResponse)
			// issues 使用显式 DTO 解码，核对前端实际消费的字段。
			var issues automationIssuesResponse
			// err 验证真实 HTTP 列表可以解码到明确的传输 DTO。
			if err := json.Unmarshal(listResponse.Body.Bytes(), &issues); err != nil {
				t.Fatal(err)
			} // err 校验 JSON 响应形状。
			if len(issues.Runs) != 1 || len(issues.PendingTasks) != 1 || !issues.Runs[0].CanStopOrder || issues.Runs[0].TriggerType != "review_missing_timeout" || issues.PendingTasks[0].OrderID != "contract-stop" || issues.PendingTasks[0].TriggerType != "order_paid" {
				t.Fatal("人工处理摘要缺少任务类型或订单身份")
			}
			// route 只在两条已登记的版本化 operation 中选择。
			route := "/api/v1/automation-runs/713/resolve"
			if deferred {
				route = "/api/v1/automation-pending-tasks/714/resolve"
			}
			// request、response 捕获确认停止后的真实 HTTP 契约。
			request := httptest.NewRequest(http.MethodPost, route, strings.NewReader(`{"resolution":"stop_order"}`))
			request.Header.Set("Content-Type", "application/json")
			request.AddCookie(sessionCookie)
			// response 是最终写回的成功响应，内部仓储错误不能泄漏。
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assertOpenAPIRecordedSuccessResponse(t, request, response)
			// stopped 验证该用户订单的停用记录已经持久化，不只是隐藏前端异常。
			var stopped int
			// err 验证 HTTP 成功意味着当前用户订单已实际写入停用记录。
			if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_automation_stops WHERE cookie_id='acc1' AND order_id='contract-stop' AND stopped_by=1`).Scan(&stopped); err != nil || stopped != 1 {
				t.Fatalf("stopped=%d err=%v", stopped, err)
			} // err 验证真正落库。
		})
	}
}

// TestAutomationStopRejectsMissingOrderAndHidesStorageFailure 验证无订单不授权整单停用、存储失败不泄漏底层错误。
func TestAutomationStopRejectsMissingOrderAndHidesStorageFailure(t *testing.T) {
	// srv、store、cleanup 提供真实版本化 API 场景。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// err 确认没有订单号的死信夹具已成功创建。
	if _, err := store.DB.ExecContext(context.Background(), `INSERT INTO automation_pending_tasks(id,task_key,cookie_id,trigger_type,task_json,status) VALUES (714,'no-order','acc1','order_paid','{}','dead_letter')`); err != nil {
		t.Fatal(err)
	} // err 确认缺身份任务已建立。
	// handler、sessionCookie 提供真实认证边界。
	handler := srv.Router()
	// sessionCookie 只在本地测试请求中使用。
	sessionCookie := loginHelper(t, handler)
	for _, status := range []int{http.StatusBadRequest, http.StatusInternalServerError} { // status 区分身份不可确认和数据库故障。
		if status == http.StatusInternalServerError {
			// err 确认下一次 API 调用确实进入数据库不可用路径。
			if err := store.DB.Close(); err != nil {
				t.Fatal(err)
			}
		} // err 模拟存储故障，接口必须返回通用错误。
		// request 是同一条整单停用请求，不能由客户端补填订单号强行绕过。
		request := httptest.NewRequest(http.MethodPost, "/api/v1/automation-pending-tasks/714/resolve", strings.NewReader(`{"resolution":"stop_order"}`))
		request.AddCookie(sessionCookie)
		// response 保存 HTTP 状态和脱敏错误响应。
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != status {
			t.Fatalf("status=%d want=%d", response.Code, status)
		}
		if strings.Contains(response.Body.String(), "database is closed") {
			t.Fatal("内部数据库错误泄漏")
		}
	}
}

// TestStopRunWhitespaceCannotExposeStorageError 验证兼容首尾空白的整单停用请求同样隐藏数据库错误。
func TestStopRunWhitespaceCannotExposeStorageError(t *testing.T) {
	// srv、store、cleanup 提供将被主动关闭的本地数据库与真实路由。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是本场景真实版本化 Router。
	handler := srv.Router()
	// sessionCookie 在关闭数据库前取得合法认证，避免测试误入登录失败分支。
	sessionCookie := loginHelper(t, handler)
	// closeErr 确认后续仓储调用实际失败，而不是使用模拟 HTTP 错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// request 使用历史客户端允许的空白形式，规范化后仍应走整单停用错误边界。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/automation-runs/713/resolve", strings.NewReader(`{"resolution":" stop_order "}`))
	request.AddCookie(sessionCookie)
	// response 捕获公共状态和错误文本，不允许暴露数据库内部细节。
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "database is closed") {
		t.Fatalf("unsafe stop error status=%d", response.Code)
	}
}
