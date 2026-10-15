package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/xianyu/mtop"
)

// loginStatusRecorderHandler 记录协议续期调用次数与账号事件，供 token 获取失败的登录态预检分支断言使用。
type loginStatusRecorderHandler struct {
	// refreshCalls 保存 OnPasswordLoginRefresh 被调用的次数，用于证明预检是否跳过了完整续期。
	refreshCalls int
	// refreshResult 保存续期回调返回值；为真时调用方进入重连，为假时进入需要人工处理的终止路径。
	refreshResult bool
	// events 保存按顺序收到的账号事件类型，用于断言风控告警类别。
	events []string
	// levels 保存与 events 同序的告警级别。
	levels []string
}

// HandleChatMessage 满足 Engine Handler 接口的聊天处理方法；本测试不处理聊天消息。
func (*loginStatusRecorderHandler) HandleChatMessage(context.Context, ChatMessage) error { return nil }

// HandleSystemEvent 满足 Engine Handler 接口的系统事件处理方法；本测试不处理系统事件。
func (*loginStatusRecorderHandler) HandleSystemEvent(context.Context, automation.Task) error {
	return nil
}

// OnPasswordLoginRefresh 记录协议续期调用次数并返回预设结果，用于区分“跳过续期”与“继续续期”。
func (h *loginStatusRecorderHandler) OnPasswordLoginRefresh(context.Context, string) bool {
	h.refreshCalls++
	return h.refreshResult
}

// OnAccountAlert 满足 Engine Handler 接口的告警方法；类型化事件由 OnAccountEvent 记录。
func (*loginStatusRecorderHandler) OnAccountAlert(context.Context, string, string, string, string) {}

// OnAccountEvent 记录带事件类别与级别的账号事件，覆盖登录态预检命中风控时的用户可见告警。
func (h *loginStatusRecorderHandler) OnAccountEvent(_ context.Context, _, eventType, level, _, _ string) {
	h.events = append(h.events, eventType)
	h.levels = append(h.levels, level)
}

// TestHandleTokenAcquisitionFailureRecoversByLoginStatusWithoutRenewal 验证登录态预检恢复凭证时跳过协议续期。
func TestHandleTokenAcquisitionFailureRecoversByLoginStatusWithoutRenewal(t *testing.T) {
	// newCookie 是 loginuser.get 响应头下发的新签名 Cookie，代表无需完整续期即可恢复的登录态。
	newCookie := "unb=123; _m_h5_tk=loginuser_recovered; sgcookie=abc"
	// client 返回“令牌已刷新并下发新 Cookie”的登录态结论。
	client := &statusMtop{
		fakeRunMtop: fakeRunMtop{token: "tok-1"},
		result:      &mtop.LoginStatusResult{Status: mtop.LoginStatusTokenRefreshed, UpdatedCookies: newCookie, Message: "令牌已刷新"},
	}
	// acc、cleanup 提供真实账号与数据库清理函数。
	acc, _, _, cleanup := newRunAccount(t, client)
	defer cleanup()
	// handler 记录协议续期次数，本分支要求为零。
	handler := &loginStatusRecorderHandler{}
	acc.handler = handler

	// retry、err 保存 token 获取失败后的重连决定与终止错误。
	retry, err := (&connectionCoordinator{account: acc}).handleTokenAcquisitionFailure(context.Background(), &fakeWSConn{}, errors.New("FAIL_SYS_SESSION_EXPIRED"))
	if !retry || err != nil || handler.refreshCalls != 0 {
		t.Fatalf("预检恢复应直接重连且不续期: retry=%v err=%v refresh=%d", retry, err, handler.refreshCalls)
	}
	if // got 保存预检恢复后账号实际使用的 Cookie。
	got := acc.currentCookieStr(); got != newCookie {
		t.Fatalf("预检恢复 Cookie=%q want %q", got, newCookie)
	}
	if // state 保存预检恢复后的运行状态。
	state := acc.RuntimeStatus().State; state != RuntimeConnecting {
		t.Fatalf("预检恢复后状态=%q want %q", state, RuntimeConnecting)
	}
}

// TestHandleTokenAcquisitionFailureStopsOnLoginStatusRisk 验证登录态预检命中风控时停止续期并要求人工验证。
func TestHandleTokenAcquisitionFailureStopsOnLoginStatusRisk(t *testing.T) {
	// client 返回平台明确的风控验证要求，且不下发可用 Cookie。
	client := &statusMtop{
		fakeRunMtop: fakeRunMtop{token: "tok-1"},
		result:      &mtop.LoginStatusResult{Status: mtop.LoginStatusRiskRequired, VerificationURL: "https://example.invalid/verify"},
	}
	// acc、cleanup 提供真实账号与数据库清理函数。
	acc, _, _, cleanup := newRunAccount(t, client)
	defer cleanup()
	// handler 记录续期次数与账号事件，风控分支要求不续期且恰好一次安全验证告警。
	handler := &loginStatusRecorderHandler{refreshResult: true}
	acc.handler = handler

	// retry、err 保存风控预检后的重连决定与终止错误。
	retry, err := (&connectionCoordinator{account: acc}).handleTokenAcquisitionFailure(context.Background(), &fakeWSConn{}, errors.New("FAIL_SYS_SESSION_EXPIRED"))
	if retry || err == nil || handler.refreshCalls != 0 {
		t.Fatalf("风控预检应终止且不续期: retry=%v err=%v refresh=%d", retry, err, handler.refreshCalls)
	}
	if // state 保存风控预检后的运行状态。
	state := acc.RuntimeStatus().State; state != RuntimeVerificationRequired {
		t.Fatalf("风控预检后状态=%q want %q", state, RuntimeVerificationRequired)
	}
	if len(handler.events) != 1 || handler.events[0] != EventSecurityVerification || handler.levels[0] != AlertLevelWarn {
		t.Fatalf("风控预检应发一次安全验证告警: events=%v levels=%v", handler.events, handler.levels)
	}
}

// TestHandleTokenAcquisitionFailureFallsBackWhenLoginStatusUnavailable 验证运行时未实现登录态检查时保持原有续期语义。
func TestHandleTokenAcquisitionFailureFallsBackWhenLoginStatusUnavailable(t *testing.T) {
	// acc、cleanup 提供只实现 Token 能力、未实现登录态检查的账号，覆盖安全降级分支。
	acc, _, _, cleanup := newRunAccount(t, &fakeRunMtop{token: "tok-1"})
	defer cleanup()
	// handler 返回续期成功，证明缺少预检能力时仍按原路径续期。
	handler := &loginStatusRecorderHandler{refreshResult: true}
	acc.handler = handler

	// retry、err 保存缺少预检能力时的重连决定与终止错误。
	retry, err := (&connectionCoordinator{account: acc}).handleTokenAcquisitionFailure(context.Background(), &fakeWSConn{}, errors.New("FAIL_SYS_SESSION_EXPIRED"))
	if !retry || err != nil || handler.refreshCalls != 1 {
		t.Fatalf("缺少登录态检查能力时应回退到协议续期: retry=%v err=%v refresh=%d", retry, err, handler.refreshCalls)
	}
}

// TestHandleTokenAcquisitionFailureRenewsWhenLoginStatusConfirmsExpiry 验证预检明确报告会话失效时继续协议续期。
func TestHandleTokenAcquisitionFailureRenewsWhenLoginStatusConfirmsExpiry(t *testing.T) {
	// client 明确返回会话失效且未下发可用 Cookie，预检结论不改变续期决策。
	client := &statusMtop{
		fakeRunMtop: fakeRunMtop{token: "tok-1"},
		result:      &mtop.LoginStatusResult{Status: mtop.LoginStatusSessionExpired},
	}
	// acc、cleanup 提供真实账号与数据库清理函数。
	acc, _, _, cleanup := newRunAccount(t, client)
	defer cleanup()
	// handler 返回续期成功，驱动既有重连分支。
	handler := &loginStatusRecorderHandler{refreshResult: true}
	acc.handler = handler

	// retry、err 保存会话确认失效后的重连决定与终止错误。
	retry, err := (&connectionCoordinator{account: acc}).handleTokenAcquisitionFailure(context.Background(), &fakeWSConn{}, errors.New("FAIL_SYS_SESSION_EXPIRED"))
	if !retry || err != nil || handler.refreshCalls != 1 {
		t.Fatalf("预检确认会话失效后应继续续期: retry=%v err=%v refresh=%d", retry, err, handler.refreshCalls)
	}
}

// TestHandleTokenAcquisitionFailureKeepsTerminalPathWhenRenewalFails 验证预检未恢复且续期失败时保持需要人工处理的终止语义。
func TestHandleTokenAcquisitionFailureKeepsTerminalPathWhenRenewalFails(t *testing.T) {
	// client 明确返回会话失效，预检不提供任何恢复能力。
	client := &statusMtop{
		fakeRunMtop: fakeRunMtop{token: "tok-1"},
		result:      &mtop.LoginStatusResult{Status: mtop.LoginStatusSessionExpired},
	}
	// acc、cleanup 提供真实账号与数据库清理函数。
	acc, _, _, cleanup := newRunAccount(t, client)
	defer cleanup()
	// handler 拒绝续期，驱动既有的终止路径。
	handler := &loginStatusRecorderHandler{refreshResult: false}
	acc.handler = handler

	// retry、err 保存续期失败后的重连决定与终止错误。
	retry, err := (&connectionCoordinator{account: acc}).handleTokenAcquisitionFailure(context.Background(), &fakeWSConn{}, errors.New("FAIL_SYS_SESSION_EXPIRED"))
	if retry || err == nil || handler.refreshCalls != 1 {
		t.Fatalf("续期失败应终止账号循环: retry=%v err=%v refresh=%d", retry, err, handler.refreshCalls)
	}
	if // state 保存续期失败后的运行状态。
	state := acc.RuntimeStatus().State; state != RuntimeAuthExpired {
		t.Fatalf("续期失败后状态=%q want %q", state, RuntimeAuthExpired)
	}
}

// TestHandleTokenAcquisitionFailureStopsRecoveryWaitOnCanceledContext 验证恢复等待期间账号停止会终止循环。
func TestHandleTokenAcquisitionFailureStopsRecoveryWaitOnCanceledContext(t *testing.T) {
	// newCookie 是 loginuser.get 响应头下发的新签名 Cookie。
	newCookie := "unb=123; _m_h5_tk=loginuser_canceled; sgcookie=abc"
	// client 返回“令牌已刷新并下发新 Cookie”的登录态结论，使流程进入恢复等待。
	client := &statusMtop{
		fakeRunMtop: fakeRunMtop{token: "tok-1"},
		result:      &mtop.LoginStatusResult{Status: mtop.LoginStatusTokenRefreshed, UpdatedCookies: newCookie},
	}
	// acc、cleanup 提供真实账号与数据库清理函数。
	acc, _, _, cleanup := newRunAccount(t, client)
	defer cleanup()
	// handler 记录续期次数，恢复路径不应调用协议续期。
	handler := &loginStatusRecorderHandler{}
	acc.handler = handler
	// ctx、cancel 把固定恢复等待压缩为可观察的取消结果。
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// retry、err 保存恢复等待被取消后的重连决定与终止错误。
	retry, err := (&connectionCoordinator{account: acc}).handleTokenAcquisitionFailure(ctx, &fakeWSConn{}, errors.New("FAIL_SYS_SESSION_EXPIRED"))
	if retry || !errors.Is(err, context.DeadlineExceeded) || handler.refreshCalls != 0 {
		t.Fatalf("恢复等待取消应终止账号循环: retry=%v err=%v refresh=%d", retry, err, handler.refreshCalls)
	}
}
