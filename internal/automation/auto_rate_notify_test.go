package automation

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// autoRateNotification 记录一次自动评价终态通知的关键字段，用于断言通知次数、终态和内容。
type autoRateNotification struct {
	// AccountID 是通知所属账号标识。
	AccountID string
	// OrderID 是通知关联的订单标识。
	OrderID string
	// Status 是运行终态编码，failed 表示重试耗尽或永久失败，needs_review 表示人工核对。
	Status string
	// Reason 是失败原因摘要，禁止包含凭证。
	Reason string
	// IdempotencyKey 是通知业务幂等键，同一运行同一终态必须保持一致。
	IdempotencyKey string
}

// recordingAutoRateNotifier 记录自动评价终态通知，同时满足自动化通知器最小接口。
type recordingAutoRateNotifier struct {
	// calls 按发送顺序保存收到的自动评价通知。
	calls []autoRateNotification
}

// NotifyAutomationRun 记录非自动评价类别的自动化终态通知；自动评价场景不应调用该旧入口。
func (n *recordingAutoRateNotifier) NotifyAutomationRun(context.Context, int64, string, string, string, string, string, string) {
}

// NotifyAutoRateFailure 记录一次自动评价终态通知。
func (n *recordingAutoRateNotifier) NotifyAutoRateFailure(_ context.Context, accountID, orderID, status, reason, idempotencyKey string) {
	n.calls = append(n.calls, autoRateNotification{
		AccountID: accountID, OrderID: orderID, Status: status, Reason: reason, IdempotencyKey: idempotencyKey,
	})
}

// legacyAutomationNotifier 只实现旧自动化终态入口，用于验证缺少自动评价类别时评价结果不受影响。
type legacyAutomationNotifier struct {
	// calls 记录旧入口被调用的次数，自动评价通知不应走到该入口。
	calls int
}

// NotifyAutomationRun 记录旧自动化终态通知。
func (n *legacyAutomationNotifier) NotifyAutomationRun(context.Context, int64, string, string, string, string, string, string) {
	n.calls++
}

// seedAutoRateSettings 写入启用自动评价的账号任务配置，供自动评价通知测试复用。
func seedAutoRateSettings(t *testing.T, store *db.Store, ctx context.Context) {
	t.Helper()
	// settingsErr 保存自动评价配置写入错误，缺少配置时扫描不会执行评价。
	settingsErr := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true,
		RateContent: "交易愉快", PolishTime: "03:00"})
	if settingsErr != nil {
		t.Fatalf("写入自动评价配置: %v", settingsErr)
	}
}

// TestAutoRateNotifyOnlyOnRetryExhaustion 验证自动评价只在上限耗尽时通知一次，普通失败仍静默重试。
func TestAutoRateNotifyOnlyOnRetryExhaustion(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 自动化存储及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是自动评价通知测试共用的数据库上下文。
	ctx := context.Background()
	// client 每次评价都返回可重试的平台失败，模拟重试上限之前的普通失败。
	client := &fakeAccountTaskClient{rateErr: errors.New("平台临时失败")}
	// notifier 记录自动评价终态通知。
	notifier := &recordingAutoRateNotifier{}
	// center 是注入平台替身和通知替身后的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil,
		CenterDependencies{AccountTaskClient: client, Notifier: notifier})
	seedAutoRateSettings(t, store, ctx)
	seedLocalCompletedOrders(t, store, ctx, "order-retry")
	// runKey 是自动评价运行记录的稳定业务键，同时决定通知幂等键。
	const runKey = "rate:cid:order-retry"
	// attempt 表示当前第几次自动执行；第 6 次失败时数据库把 next_retry_at 置 0，属于终态失败。
	for attempt := 1; attempt <= 6; attempt++ {
		// summary、err 保存本轮自动评价结果及其错误。
		summary, err := center.RunAccountTask(ctx, "cid", TaskAutoRate)
		if err != nil || summary.Failed != 1 {
			t.Fatalf("第 %d 次自动评价失败结果: summary=%+v err=%v", attempt, summary, err)
		}
		if attempt == 6 {
			break
		}
		if len(notifier.calls) != 0 {
			t.Fatalf("第 %d 次普通失败仍会自动重试，不应通知: %+v", attempt, notifier.calls)
		}
		// 清除冷却时间，模拟 10 分钟后的下一轮自动重试。
		if _, updateErr := store.DB.ExecContext(ctx, `UPDATE account_task_runs SET next_retry_at=0 WHERE run_key=?`, runKey); updateErr != nil {
			t.Fatalf("重置冷却时间: %v", updateErr)
		}
	}
	if len(notifier.calls) != 1 {
		t.Fatalf("重试耗尽应只通知一次: %+v", notifier.calls)
	}
	// call 是唯一一条终态失败通知。
	call := notifier.calls[0]
	if call.AccountID != "cid" || call.OrderID != "order-retry" || call.Status != "failed" ||
		call.IdempotencyKey != "account-task:"+runKey+":failed" || !strings.Contains(call.Reason, "平台临时失败") {
		t.Fatalf("终态失败通知内容错误: %+v", call)
	}
	// nextRetryAt 保存运行记录持久化的下一次自动重试时间，重试耗尽必须为零。
	var nextRetryAt int64
	// err 保存读取运行记录下一次自动重试时间的数据库错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT next_retry_at FROM account_task_runs WHERE run_key=?`, runKey).Scan(&nextRetryAt); err != nil || nextRetryAt != 0 {
		t.Fatalf("重试耗尽运行记录错误: next_retry_at=%d err=%v", nextRetryAt, err)
	}
}

// TestAutoRateNotifyOnPermanentFailure 验证平台明确永久失败时立即发送一次终态失败通知。
func TestAutoRateNotifyOnPermanentFailure(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 自动化存储及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是自动评价通知测试共用的数据库上下文。
	ctx := context.Background()
	// expiredErr 保存平台明确拒绝超过 30 天订单的业务错误，属于不可自动重试的永久失败。
	expiredErr := &mtop.MTopResponseError{API: "mtop.taobao.idle.rate.create", Kind: mtop.MTopErrorBusiness,
		Ret: []string{"FAIL_BIZ_BAD_REQUEST::超出30天的订单不允许评价||rate failed"}}
	// client 模拟平台对超期订单返回永久失败。
	client := &fakeAccountTaskClient{rateErr: expiredErr}
	// notifier 记录自动评价终态通知。
	notifier := &recordingAutoRateNotifier{}
	// center 是注入平台替身和通知替身后的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil,
		CenterDependencies{AccountTaskClient: client, Notifier: notifier})
	seedAutoRateSettings(t, store, ctx)
	seedLocalCompletedOrders(t, store, ctx, "order-expired")
	// summary、err 保存首次识别永久失败后的执行结果。
	summary, err := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	if err != nil || summary.Failed != 1 || client.rateCalls != 1 {
		t.Fatalf("永久失败评价结果错误: summary=%+v calls=%d err=%v", summary, client.rateCalls, err)
	}
	if len(notifier.calls) != 1 {
		t.Fatalf("永久失败应通知一次: %+v", notifier.calls)
	}
	// call 是唯一一条永久失败通知，原因必须保留不可重试标记以便用户区分。
	call := notifier.calls[0]
	if call.Status != "failed" || call.OrderID != "order-expired" || !strings.Contains(call.Reason, db.NoRetryErrorPrefix) {
		t.Fatalf("永久失败通知内容错误: %+v", call)
	}
}

// TestAutoRateNotifyOnNeedsReviewQuarantine 验证运行被隔离为人工核对时发送一次人工处理通知。
func TestAutoRateNotifyOnNeedsReviewQuarantine(t *testing.T) {
	// cookieErr 是平台返回新 Cookie 后无法落库的本地故障，动作结果因此无法确认。
	cookieErr := errors.New("任务 Cookie 写入失败")
	// repository 让 Cookie 落库失败，从而把已执行的评价动作隔离为人工核对。
	repository := &accountTaskFlowRepository{value: "old", dueOrderIDs: []string{"order-review"}, claimed: true, updateErr: cookieErr}
	// notifier 记录自动评价终态通知。
	notifier := &recordingAutoRateNotifier{}
	// coordinator 使用失败仓储和记录型通知器。
	coordinator := &accountTaskCoordinator{
		repository: repository,
		client: func() AccountTaskClient {
			return &accountTaskFlowClient{rateResult: &mtop.AccountTaskResult{Success: true, UpdatedCookies: "new"}}
		},
		notifier: func() Notifier { return notifier },
		logger:   slog.Default(),
	}
	// summary、err 保存人工核对收口结果；错误必须保留本地故障原因。
	summary, err := coordinator.runAutoRate(context.Background(), db.AccountTaskSettings{CookieID: "account", RateContent: "感谢光临"}, false)
	if !errors.Is(err, cookieErr) || !errors.Is(err, errAutomationNeedsReview) || summary.Failed != 0 {
		t.Fatalf("人工核对收口结果错误: summary=%+v err=%v", summary, err)
	}
	if len(notifier.calls) != 1 {
		t.Fatalf("人工核对应只通知一次: %+v", notifier.calls)
	}
	// call 是唯一一条人工核对通知。
	call := notifier.calls[0]
	if call.Status != "needs_review" || call.AccountID != "account" || call.OrderID != "order-review" ||
		call.IdempotencyKey != "account-task:rate:account:order-review:needs_review" {
		t.Fatalf("人工核对通知内容错误: %+v", call)
	}
	if !strings.Contains(call.Reason, "任务 Cookie 写入失败") || !strings.Contains(call.Reason, "运行状态收口失败") {
		t.Fatalf("人工核对通知缺少本地故障原因: %+v", call)
	}
}

// TestAutoRateNotificationDegradesSafely 验证通知器未注入或缺少自动评价类别时评价结果与收口保持不变。
func TestAutoRateNotificationDegradesSafely(t *testing.T) {
	// nilCoordinator 模拟未初始化的协调器，通知入口必须安全返回。
	var nilCoordinator *accountTaskCoordinator
	nilCoordinator.notifyAutoRateTerminal(context.Background(), "cid", "order-1", "rate:cid:order-1", "failed", "原因", 0, nil)
	// settings 是自动评价流程共用的账号任务配置。
	settings := db.AccountTaskSettings{CookieID: "account", RateContent: "感谢光临"}
	// legacyNotifier 只实现旧自动化终态入口，不具备自动评价类别。
	legacyNotifier := &legacyAutomationNotifier{}
	// legacyCoordinator 使用不支持自动评价类别的通知器。
	legacyCoordinator := &accountTaskCoordinator{
		repository: &accountTaskFlowRepository{value: "cookie", dueOrderIDs: []string{"order-legacy"}, claimed: true},
		client: func() AccountTaskClient {
			return &accountTaskFlowClient{rateResult: &mtop.AccountTaskResult{Success: false, Message: "平台拒绝"}}
		},
		notifier: func() Notifier { return legacyNotifier },
		logger:   slog.Default(),
	}
	// legacySummary、legacyErr 保存缺少通知能力时的评价结果。
	legacySummary, legacyErr := legacyCoordinator.runAutoRate(context.Background(), settings, false)
	if legacyErr != nil || legacySummary.Failed != 1 || legacyNotifier.calls != 0 {
		t.Fatalf("缺少通知能力不应改变评价结果: summary=%+v calls=%d err=%v", legacySummary, legacyNotifier.calls, legacyErr)
	}
	// noNotifierCoordinator 模拟通知器未注入的装配，评价结果必须同样不变。
	noNotifierCoordinator := &accountTaskCoordinator{
		repository: &accountTaskFlowRepository{value: "cookie", dueOrderIDs: []string{"order-none"}, claimed: true},
		client: func() AccountTaskClient {
			return &accountTaskFlowClient{rateResult: &mtop.AccountTaskResult{Success: false, Message: "平台拒绝"}}
		},
		logger: slog.Default(),
	}
	// noNotifierSummary、noNotifierErr 保存未注入通知器时的评价结果。
	noNotifierSummary, noNotifierErr := noNotifierCoordinator.runAutoRate(context.Background(), settings, false)
	if noNotifierErr != nil || noNotifierSummary.Failed != 1 {
		t.Fatalf("未注入通知器不应改变评价结果: summary=%+v err=%v", noNotifierSummary, noNotifierErr)
	}
}

// TestAutoRateRetryExhaustedSkipsUnsupportedRepository 验证仓储缺少重试状态回读能力时不误报终态通知。
func TestAutoRateRetryExhaustedSkipsUnsupportedRepository(t *testing.T) {
	// coordinator 使用不支持回读运行重试状态的内存仓储。
	coordinator := &accountTaskCoordinator{
		repository: &accountTaskFlowRepository{value: "cookie"},
		client:     func() AccountTaskClient { return &accountTaskFlowClient{} },
		logger:     slog.Default(),
	}
	if coordinator.autoRateRetryExhausted(context.Background(), "rate:account:order-1") {
		t.Fatal("缺少回读能力时不应判定重试已耗尽")
	}
	// nilCoordinator 模拟未初始化的协调器，判断入口必须安全返回。
	var nilCoordinator *accountTaskCoordinator
	if nilCoordinator.autoRateRetryExhausted(context.Background(), "rate:account:order-1") {
		t.Fatal("未初始化协调器不应判定重试已耗尽")
	}
}
