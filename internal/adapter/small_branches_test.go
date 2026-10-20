package adapter

import (
	"context"
	"errors"
	"fmt"
	"testing"

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/xianyu/ws"
)

// TestNewAIModelClientBuildsTrustedEndpointClients 覆盖生产默认的两类端点客户端工厂（常规与测试连接超时）。
func TestNewAIModelClientBuildsTrustedEndpointClients(t *testing.T) {
	// client 是生产默认配置的模型目录适配器。
	client := NewAIModelClient()
	if client.newHTTPClient == nil || client.newTestHTTPClient == nil {
		t.Fatal("端点客户端工厂未装配")
	}
	// httpErr 保存该步骤返回的错误，用于断言预期的成功或失败路径。
	if _, httpErr := client.newHTTPClient("::not-a-url::"); httpErr == nil {
		t.Fatal("非法地址应被端点策略拒绝")
	}
	// testErr 保存该步骤返回的错误，用于断言预期的成功或失败路径。
	if _, testErr := client.newTestHTTPClient("::not-a-url::"); testErr == nil {
		t.Fatal("非法地址应被端点策略拒绝")
	}
}

// TestClassifyChatPlatformError 覆盖平台发送错误到聊天应用错误的全部分类。
func TestClassifyChatPlatformError(t *testing.T) {
	if classifyChatPlatformError(nil) != nil {
		t.Fatal("nil 应保持 nil")
	}
	// notSent 是自动化确定未发送的错误，必须原样保留以便安全重试。
	notSent := fmt.Errorf("%w: 离线", automation.ErrMessageNotSent)
	// got 保存本次调用的返回值，用于与期望结果比较。
	if got := classifyChatPlatformError(notSent); got != notSent {
		t.Fatalf("not sent got %v", got)
	}
	// uncertain 是类型化的结果不确定错误。
	uncertain := &ws.SendError{Kind: ws.SendUncertain}
	// got 保存本次调用的返回值，用于与期望结果比较。
	if got := classifyChatPlatformError(uncertain); !errors.Is(got, chatapp.ErrSendUncertain) {
		t.Fatalf("uncertain got %v", got)
	}
	// rejected 是平台明确拒绝，不应被改写为不确定。
	rejected := &ws.SendError{Kind: ws.SendRejected}
	// got 保存本次调用的返回值，用于与期望结果比较。
	if got := classifyChatPlatformError(rejected); errors.Is(got, chatapp.ErrSendUncertain) {
		t.Fatalf("rejected got %v", got)
	}
}

// TestChatReplyDeliveryWithoutService 覆盖聊天应用不可用时的自动回复发送保护。
func TestChatReplyDeliveryWithoutService(t *testing.T) {
	// result、sendErr 是缺少聊天服务时的发送结果。
	result, sendErr := NewChatReplyDelivery(nil).SendReply(context.Background(), engine.ReplyMessage{})
	if !errors.Is(sendErr, chatapp.ErrUnavailable) || result.TextSent || result.ImageSent || result.Uncertain {
		t.Fatalf("result=%+v err=%v", result, sendErr)
	}
}

// TestOrderRuntimeUpdateRunningCookieForwardsToHook 覆盖订单运行时 Cookie 同步回调的调用与空保护。
func TestOrderRuntimeUpdateRunningCookieForwardsToHook(t *testing.T) {
	// received 记录回调收到的账号与值。
	var received [2]string
	// runtime 装配了同步回调。
	runtime := &OrderRuntime{hooks: OrderRuntimeHooks{UpdateRunningCookie: func(_ context.Context, cookieID, value string) { received = [2]string{cookieID, value} }}}
	runtime.UpdateRunningCookie(context.Background(), "cid", "v")
	if received != [2]string{"cid", "v"} {
		t.Fatalf("received=%v", received)
	}
	// 缺少回调与 nil 接收者都应是空操作。
	(&OrderRuntime{}).UpdateRunningCookie(context.Background(), "cid", "v")
	(*OrderRuntime)(nil).UpdateRunningCookie(context.Background(), "cid", "v")
}

// TestOrderRuntimeAdapterWithOrderDetailsAndNilRuntime 覆盖带订单详情协调器的构造器与未装配运行时的聊天联系人刷新。
func TestOrderRuntimeAdapterWithOrderDetailsAndNilRuntime(t *testing.T) {
	// refreshed 记录聊天联系人刷新回调是否被装配。
	refresh := func(context.Context, string) error { return nil }
	// built 是仅隔离测试使用的无数据库依赖适配器。
	built := NewOrderRuntimeAdapterWithOrderDetails(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, refresh)
	// zero 是未装配运行时的适配器：联系人刷新是空操作，抓取返回确定性错误。
	zero := OrderRuntimeAdapter{}
	// refreshErr 保存该步骤返回的错误，用于断言预期的成功或失败路径。
	if refreshErr := zero.RefreshChatConversations(context.Background(), "cid"); refreshErr != nil {
		t.Fatal(refreshErr)
	}
	// fetchErr 保存该步骤返回的错误，用于断言预期的成功或失败路径。
	if _, fetchErr := zero.FetchSoldOrders(context.Background(), nil); fetchErr == nil {
		t.Fatal("未装配运行时应返回错误")
	}
	_ = built
}
