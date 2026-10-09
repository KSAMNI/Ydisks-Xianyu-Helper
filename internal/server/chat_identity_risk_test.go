package server

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// chatRiskClient仅模拟资料查询失败，其余协议能力禁止在该测试中使用。
type chatRiskClient struct {
	// Client保留测试不触及的其他平台能力占位。
	mtop.Client
	// calls记录真实handler穿过应用和适配器后的资料调用数。
	calls atomic.Int32
}

// FetchChatUserInfo对ctx指定的accountCookie/chatID查询返回风险；敏感入参不保存也不打印。
func (c *chatRiskClient) FetchChatUserInfo(ctx context.Context, accountCookie, chatID string) (*mtop.ChatUserInfo, error) {
	c.calls.Add(1)
	return nil, &mtop.MTopResponseError{Kind: mtop.MTopErrorRiskVerification, Ret: []string{"FAIL_SYS_USER_VALIDATE", "RGV587_ERROR"}}
}

// TestChatIdentityRiskBudgetAcrossHTTPRoutes验证200会话的版本化/兼容列表及历史入口共享风险准入，响应契约不回退。
func TestChatIdentityRiskBudgetAcrossHTTPRoutes(t *testing.T) {
	// srv、store和cleanup提供完整Router、真实SQLite和独立测试生命周期。
	srv, store, cleanup := newTestServerWithChat(t)
	defer cleanup()
	// client是唯一本地风险替身，资料调用之外不允许平台访问。
	client := &chatRiskClient{}
	setTestMTop(srv, client)
	// index创建200条有真实消息的会话，避免空会话清理规则移除夹具。
	for index := 0; index < 200; index++ {
		// chatID和peerID分别定位会话与对端身份，不是任何真实账号值。
		chatID, peerID := fmt.Sprintf("profile-%03d", index), fmt.Sprintf("peer-%03d", index)
		// err保留真实消息/会话写入失败，必须在请求前完成全部夹具。
		if _, _, err := store.Chats.SaveMessage(context.Background(), db.ChatSession{CookieID: "acc1", ChatID: chatID, BuyerID: peerID}, db.ChatMessage{MessageKey: chatID, Direction: "incoming", SenderID: peerID, Content: "本地消息", MessageType: "text", Status: "received", SentAt: int64(index + 1)}, true); err != nil {
			t.Fatal(err)
		}
	}
	// handler和loginCookie只用于本地认证请求，不输出任何认证值。
	handler := srv.Router()
	// loginCookie只附加到本地测试请求，不参与日志或断言输出。
	loginCookie := loginHelper(t, handler)
	// route覆盖新旧列表以及历史读取，所有入口必须复用同一个服务状态。
	for _, route := range []string{"/api/v1/chat/sessions?account_id=acc1&refresh=1", "/api/chat/sessions?account_id=acc1&refresh=1", "/api/v1/chat/messages?account_id=acc1&chat_id=profile-001", "/api/chat/messages?account_id=acc1&chat_id=profile-002"} {
		// status由真实handler返回；辅助函数同时校验版本化成功响应的OpenAPI契约。
		status := requestChatTaskRoute(t, handler, loginCookie, http.MethodGet, route, "")
		if status != http.StatusOK {
			t.Fatalf("route=%s status=%d", route, status)
		}
	}
	if client.calls.Load() != 1 {
		t.Fatalf("风险后不应重复查询 calls=%d", client.calls.Load())
	}
}
