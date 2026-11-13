package automation

import (
	"context"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// TestSendTemplateRendersDeliveryMomentInBeijingTime 验证发货时间变量按北京时间渲染，且同一动作的多条消息共用同一个发货时刻。
func TestSendTemplateRendersDeliveryMomentInBeijingTime(t *testing.T) {
	// store、cleanup 保存发货时间变量测试使用的数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 保存模板动作与数据库共用的测试上下文。
	ctx := context.Background()
	// sender 保存模板动作实际发出的消息正文。
	sender := &testSender{}
	// moment 是注入的固定时刻：UTC 07:04 必须渲染成北京时间 15:04，证明渲染不依赖服务器时区。
	moment := time.Date(2026, 1, 2, 7, 4, 0, 0, time.UTC)
	// executor 是注入固定时钟的执行器，使断言不随真实系统时间漂移。
	executor := automationActionExecutor{store: store, senders: testSenderProvider{sender: sender}, now: func() time.Time { return moment }}
	// action 保存分别引用日期变量和日期时间变量的两条模板消息。
	action := db.AutomationAction{ActionType: ActionSendTemplate, ConfigJSON: "{}", TemplateMessages: []string{"日期 {{delivery_date}}", "时间 {{delivery_time}}"}}
	// result、sendErr 保存模板消息执行统计、确认凭证和错误。
	result, sendErr := executor.sendTemplate(ctx, Task{AccountID: "cid", OrderID: "order-moment", ChatID: "chat", BuyerID: "buyer", Quantity: "1", TriggerType: TriggerOrderPaid}, action)
	if sendErr != nil || result.sent != 2 || len(sender.texts) != 2 {
		t.Fatalf("发货时间模板渲染错误 sent=%d texts=%v err=%v", result.sent, sender.texts, sendErr)
	}
	if sender.texts[0] != "日期 2026-01-02" || sender.texts[1] != "时间 2026-01-02 15:04" {
		t.Fatalf("发货时间变量未按北京时间渲染 texts=%v", sender.texts)
	}
	if result.proof.tradeText != sender.texts[0]+"\n"+sender.texts[1] {
		t.Fatalf("发货时间模板凭证顺序不一致 proof=%q", result.proof.tradeText)
	}
}

// TestCurrentTimeFallsBackToSystemClock 验证未注入时钟时回退到系统时钟，保证生产构造点不需要额外依赖。
func TestCurrentTimeFallsBackToSystemClock(t *testing.T) {
	// executor 是不带注入时钟的执行器，模拟生产构造路径。
	executor := automationActionExecutor{}
	// before 是调用前的系统时间下界，用于确认回退结果落在合理区间。
	before := time.Now().Add(-time.Second)
	// got 保存回退时钟返回的当前时刻。
	got := executor.currentTime()
	// after 是调用后的系统时间上界，与 before 共同界定可接受的回退结果。
	after := time.Now().Add(time.Second)
	if got.Before(before) || got.After(after) {
		t.Fatalf("currentTime 未回退到系统时钟 got=%v", got)
	}
	// fixed 是注入时钟后应原样返回的时刻。
	fixed := time.Date(2026, 3, 4, 5, 6, 0, 0, time.UTC)
	executor.now = func() time.Time { return fixed }
	if executor.currentTime() != fixed {
		t.Fatalf("currentTime 未使用注入时钟 got=%v", executor.currentTime())
	}
}
