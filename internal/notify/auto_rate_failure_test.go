package notify

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// failingOutboxRepository 让通知入队固定失败，用于验证通知故障不会向业务调用方传播。
type failingOutboxRepository struct {
	// Repository 嵌入未覆盖的方法，调用到未实现能力时会立即暴露测试缺口。
	Repository
	// channels 保存账号可用的通知渠道，保证事件确实通过订阅筛选后才尝试入队。
	channels []db.NotificationChannel
}

// AccountChannels 返回预置的可用渠道。
func (r failingOutboxRepository) AccountChannels(context.Context, string) ([]db.NotificationChannel, error) {
	return r.channels, nil
}

// EnqueueOutbox 返回固定入队失败，模拟通知存储故障。
func (r failingOutboxRepository) EnqueueOutbox(context.Context, []db.NotificationOutboxInput) error {
	return errors.New("通知 outbox 写入失败")
}

// addEventScopedChannel 插入只订阅指定事件类别的 webhook 渠道并绑定到账号，返回渠道主键。
func addEventScopedChannel(t *testing.T, store *db.Store, cookieID, name, eventTypes string) int64 {
	t.Helper()
	// res、err 保存渠道写入结果和数据库错误。
	res, err := store.DB.ExecContext(context.Background(),
		`INSERT INTO notification_channels (name,type,config,event_types,enabled,user_id) VALUES (?,?,?,?,1,1)`,
		name, "webhook", `{"webhook_url":"http://127.0.0.1:1"}`, eventTypes)
	if err != nil {
		t.Fatalf("插入通知渠道: %v", err)
	}
	// channelID 是本次插入的渠道主键，用于绑定账号。
	channelID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("读取渠道主键: %v", err)
	}
	// err 保存通知渠道绑定写入错误。
	if _, err := store.DB.ExecContext(context.Background(),
		`INSERT INTO message_notifications (cookie_id,channel_id,enabled) VALUES (?,?,1)`, cookieID, channelID); err != nil {
		t.Fatalf("绑定通知渠道: %v", err)
	}
	return channelID
}

// TestNotifyAutoRateFailureQueuesEachTerminalStateOnce 验证自动评价两类终态各只入队一条通知且正文不含凭证。
func TestNotifyAutoRateFailureQueuesEachTerminalStateOnce(t *testing.T) {
	// cases 保存两类终态及其期望的告警级别和标题。
	cases := []struct {
		// status 是运行终态编码。
		status string
		// levelText 是正文中期望出现的告警级别名称。
		levelText string
		// title 是期望出现的通知标题。
		title string
	}{
		{status: "failed", levelText: "[警告]", title: "自动评价失败"},
		{status: "needs_review", levelText: "[严重]", title: "自动评价需要人工处理"},
	}
	// testCase 是当前验证的终态样本。
	for _, testCase := range cases {
		t.Run(testCase.status, func(t *testing.T) {
			// store、cleanup 是当前终态样本独立的本地通知 outbox，避免两类终态的渠道互相干扰。
			store, cleanup := newNotifyStoreBare(t)
			defer cleanup()
			// notifier 使用真实通知格式化和 outbox 幂等逻辑。
			notifier := New("cid", store, nilLogger())
			addWebhookChannel(t, store, "cid", "自动评价渠道 "+testCase.status, "http://127.0.0.1:1")
			// key 是自动化层按运行键与终态生成的稳定业务幂等键。
			key := "account-task:rate:cid:order-1:" + testCase.status
			notifier.NotifyAutoRateFailure(context.Background(), "cid", "order-1", testCase.status, "平台返回临时失败", key)
			notifier.NotifyAutoRateFailure(context.Background(), "cid", "order-1", testCase.status, "平台返回临时失败", key)
			// body、eventType 保存实际通知正文和通知类别。
			var body, eventType string
			// count 保存同一幂等键实际入队的通知条数，用于验证去重。
			var count int
			// err 保存读取通知正文、类别和去重数量的数据库错误。
			if err := store.DB.QueryRowContext(context.Background(),
				`SELECT MIN(body),MIN(event_type),COUNT(*) FROM notification_outbox WHERE idempotency_key=?`, key).
				Scan(&body, &eventType, &count); err != nil {
				t.Fatal(err)
			}
			if count != 1 || eventType != EventAutomationAutoRate {
				t.Fatalf("终态 %s 通知类别或去重错误: count=%d event=%q", testCase.status, count, eventType)
			}
			if !strings.Contains(body, testCase.title) || !strings.Contains(body, testCase.levelText) ||
				!strings.Contains(body, "自动评价") || !strings.Contains(body, "order-1") || !strings.Contains(body, "平台返回临时失败") {
				t.Fatalf("终态 %s 通知正文缺少任务类型、订单、级别或原因: %s", testCase.status, body)
			}
			if strings.Contains(body, "unb=123") || strings.Contains(body, "_m_h5_tk") {
				t.Fatalf("通知正文泄漏账号凭证: %s", body)
			}
		})
	}
}

// TestNotifyAutoRateFailureHonorsSubscription 验证新类别只投递给显式订阅该类别或全部事件的渠道。
func TestNotifyAutoRateFailureHonorsSubscription(t *testing.T) {
	// store、cleanup 是本地通知 outbox。
	store, cleanup := newNotifyStoreBare(t)
	defer cleanup()
	// notifier 使用真实订阅筛选逻辑。
	notifier := New("cid", store, nilLogger())
	addEventScopedChannel(t, store, "cid", "只订阅系统错误", `["system_error"]`)
	notifier.NotifyAutoRateFailure(context.Background(), "cid", "order-1", "failed", "平台返回临时失败", "account-task:rate:cid:order-1:failed")
	// queued 保存未订阅渠道场景下的入队数量，必须为零。
	var queued int
	// err 保存统计未订阅渠道场景入队数量的数据库错误。
	if err := store.DB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM notification_outbox`).Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("未订阅渠道不应收到自动评价通知: queued=%d err=%v", queued, err)
	}
	addEventScopedChannel(t, store, "cid", "只订阅自动评价", `["automation_auto_rate"]`)
	notifier.NotifyAutoRateFailure(context.Background(), "cid", "order-1", "failed", "平台返回临时失败", "account-task:rate:cid:order-1:failed")
	// err 保存统计自动评价类别入队数量的数据库错误。
	if err := store.DB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM notification_outbox WHERE event_type=?`, EventAutomationAutoRate).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("显式订阅渠道应收到一条自动评价通知: queued=%d err=%v", queued, err)
	}
}

// TestNotifyAutoRateFailureSwallowsEnqueueFailure 验证通知入队失败只影响通知链路，不会向调用方传播。
func TestNotifyAutoRateFailureSwallowsEnqueueFailure(t *testing.T) {
	// repository 让入队固定失败，并提供一个显式订阅自动评价的渠道。
	repository := failingOutboxRepository{channels: []db.NotificationChannel{{ID: 1, Type: "webhook", EventTypes: EventAutomationAutoRate}}}
	// notifier 使用会失败的通知仓储，验证调用不 panic 也不返回错误。
	notifier := NewWithRepository("cid", repository, nilLogger())
	notifier.NotifyAutoRateFailure(context.Background(), "cid", "order-1", "needs_review", "本地落库失败", "account-task:rate:cid:order-1:needs_review")
}

// TestNotifyAutoRateFailureIgnoresInvalidInputs 验证缺少账号或幂等键时不会写入通知。
func TestNotifyAutoRateFailureIgnoresInvalidInputs(t *testing.T) {
	// store、cleanup 是本地通知 outbox。
	store, cleanup := newNotifyStoreBare(t)
	defer cleanup()
	addWebhookChannel(t, store, "cid", "自动评价渠道", "http://127.0.0.1:1")
	// notifier 使用真实 outbox，便于确认无效输入没有入队。
	notifier := New("cid", store, nilLogger())
	notifier.NotifyAutoRateFailure(context.Background(), "", "order-1", "failed", "原因", "key-1")
	notifier.NotifyAutoRateFailure(context.Background(), "cid", "order-1", "failed", "原因", "  ")
	// nilNotifier 模拟未装配通知器时的兼容调用。
	var nilNotifier *Notifier
	nilNotifier.NotifyAutoRateFailure(context.Background(), "cid", "order-1", "failed", "原因", "key-2")
	// queued 保存无效输入场景下的入队数量。
	var queued int
	// err 保存读取无效输入入队数量的数据库错误。
	if err := store.DB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM notification_outbox`).Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("无效输入不应入队: queued=%d err=%v", queued, err)
	}
}

// TestAutoRateFailureStageAndLabel 验证终态编码映射与类别中文名称，未知编码按普通失败处理。
func TestAutoRateFailureStageAndLabel(t *testing.T) {
	// stage、label 保存需要人工核对终态的告警级别和中文名称。
	stage, label := autoRateFailureStage("needs_review")
	if stage != "critical" || label != "需要人工处理" {
		t.Fatalf("人工核对终态映射错误: level=%q label=%q", stage, label)
	}
	// unknownStage、unknownLabel 保存未知终态的默认映射，必须按普通失败处理。
	unknownStage, unknownLabel := autoRateFailureStage("future_status")
	if unknownStage != "warn" || unknownLabel != "失败" {
		t.Fatalf("未知终态应回退为普通失败: level=%q label=%q", unknownStage, unknownLabel)
	}
	// got 是自动评价类别返回的中文名称，必须与渠道选择项保持一致。
	if got := eventLabel(EventAutomationAutoRate); got != "自动评价" {
		t.Fatalf("自动评价类别名称=%q", got)
	}
}
