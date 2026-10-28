package automation

import (
	"context"
	"errors"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// TestManualRateBypassesCooldownButSchedulerHonorsIt 验证人工「立即评价」在失败冷却期内仍能再执行一次，
// 而同一冷却期内的调度扫描必须被跳过；两条路径共享 6 次累计上限与永久失败排除。
func TestManualRateBypassesCooldownButSchedulerHonorsIt(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 自动化存储及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是任务执行与数据库断言共用的上下文。
	ctx := context.Background()
	// failure 是首次评价动作的平台业务失败，用于产生带 10 分钟冷却的失败运行记录。
	failure := &mtop.MTopResponseError{API: "mtop.taobao.idle.rate.create", Kind: mtop.MTopErrorBusiness,
		Ret: []string{"FAIL_BIZ_BAD_REQUEST::系统繁忙"}}
	// client 记录评价接口调用次数，并可切换后续响应结果。
	client := &fakeAccountTaskClient{rateErr: failure}
	// center 是注入平台替身后的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{AccountTaskClient: client})
	// settings 保存启用自动评价的账号任务配置。
	settings := db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true, RateContent: "交易愉快", PolishTime: "03:00"}
	if // err 表示账号任务配置写入失败。
	err := store.AccountTasks.Upsert(ctx, settings); err != nil {
		t.Fatal(err)
	}
	seedLocalCompletedOrders(t, store, ctx, "order-manual")
	// firstSummary、firstErr 保存人工入口首次执行留下的失败结果。
	firstSummary, firstErr := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	if firstErr != nil || firstSummary.Failed != 1 || client.rateCalls != 1 {
		t.Fatalf("首次人工评价失败结果错误: summary=%+v calls=%d err=%v", firstSummary, client.rateCalls, firstErr)
	}
	// status、nextRetryAt 保存失败运行记录的状态和冷却截止时间。
	var status string
	// nextRetryAt 保存失败运行记录的下一次自动执行时间，必须晚于当前时间才构成冷却期。
	var nextRetryAt int64
	// queryErr 表示读取失败运行记录的数据库错误。
	if queryErr := store.DB.QueryRowContext(ctx, `SELECT status,next_retry_at FROM account_task_runs WHERE run_key=?`,
		"rate:cid:order-manual").Scan(&status, &nextRetryAt); queryErr != nil {
		t.Fatal(queryErr)
	}
	if status != "failed" || nextRetryAt <= time.Now().UTC().Unix() {
		t.Fatalf("失败运行未进入冷却期: status=%q next_retry_at=%d", status, nextRetryAt)
	}
	// 调度扫描必须遵守冷却时间，不能触达评价接口。
	center.scanAccountTasks(ctx)
	if client.rateCalls != 1 {
		t.Fatalf("冷却期内调度扫描不应调用评价接口 rate=%d", client.rateCalls)
	}
	// 人工入口必须越过冷却时间立即再执行一次。
	client.rateErr = nil
	// manualSummary、manualErr 保存人工入口越过冷却后的执行结果。
	manualSummary, manualErr := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	if manualErr != nil || manualSummary.Success != 1 || manualSummary.Skipped != 0 || client.rateCalls != 2 {
		t.Fatalf("人工入口应越过冷却立即执行: summary=%+v calls=%d err=%v", manualSummary, client.rateCalls, manualErr)
	}
}

// TestManualRateStillHonorsAttemptLimit 验证人工入口绕过冷却时间后仍受 6 次累计执行上限约束。
func TestManualRateStillHonorsAttemptLimit(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 自动化存储及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是任务执行与数据库断言共用的上下文。
	ctx := context.Background()
	// failure 是持续失败的普通平台错误，让每次人工重试都留下可重试的失败记录。
	failure := errors.New("platform busy")
	// client 记录评价接口调用次数。
	client := &fakeAccountTaskClient{rateErr: failure}
	// center 是注入平台替身后的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{AccountTaskClient: client})
	if // err 表示账号任务配置写入失败。
	err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true,
		RateContent: "交易愉快", PolishTime: "03:00"}); err != nil {
		t.Fatal(err)
	}
	seedLocalCompletedOrders(t, store, ctx, "order-limit")
	// attempt 是当前人工重试的序号，前 6 次都必须真正调用平台。
	for attempt := 1; attempt <= 6; attempt++ {
		// summary、runErr 保存本次人工重试的统计结果和错误。
		summary, runErr := center.RunAccountTask(ctx, "cid", TaskAutoRate)
		if runErr != nil || summary.Failed != 1 || client.rateCalls != attempt {
			t.Fatalf("第 %d 次人工重试结果错误: summary=%+v calls=%d err=%v", attempt, summary, client.rateCalls, runErr)
		}
	}
	// overSummary、overErr 保存超过上限后的执行结果，必须返回重试上限错误且不再调用平台。
	overSummary, overErr := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	if !errors.Is(overErr, db.ErrAccountTaskRetryLimit) || overSummary.Skipped != 0 || client.rateCalls != 6 {
		t.Fatalf("超过 6 次上限后不应继续评价: summary=%+v calls=%d err=%v", overSummary, client.rateCalls, overErr)
	}
}

// TestManualRateStillSkipsPermanentFailures 验证人工入口不会重试带永久失败前缀的运行记录。
func TestManualRateStillSkipsPermanentFailures(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 自动化存储及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是任务执行与数据库断言共用的上下文。
	ctx := context.Background()
	// expiredErr 是平台明确拒绝超过评价期限的永久业务失败。
	expiredErr := &mtop.MTopResponseError{API: "mtop.taobao.idle.rate.create", Kind: mtop.MTopErrorBusiness,
		Ret: []string{"FAIL_BIZ_BAD_REQUEST::超出30天的订单不允许评价||rate failed"}}
	// client 让评价接口持续返回同一永久失败。
	client := &fakeAccountTaskClient{rateErr: expiredErr}
	// center 是注入平台替身后的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{AccountTaskClient: client})
	if // err 表示账号任务配置写入失败。
	err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true,
		RateContent: "交易愉快", PolishTime: "03:00"}); err != nil {
		t.Fatal(err)
	}
	seedLocalCompletedOrders(t, store, ctx, "order-permanent")
	// firstSummary、firstErr 保存首次识别永久失败的执行结果。
	firstSummary, firstErr := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	if firstErr != nil || firstSummary.Failed != 1 || client.rateCalls != 1 {
		t.Fatalf("首次永久失败结果错误: summary=%+v calls=%d err=%v", firstSummary, client.rateCalls, firstErr)
	}
	// secondSummary、secondErr 保存人工再次触发时的结果，永久失败必须被跳过且不触达平台。
	secondSummary, secondErr := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	if secondErr != nil || secondSummary.Skipped != 1 || client.rateCalls != 1 {
		t.Fatalf("永久失败订单不应被人工入口重试: summary=%+v calls=%d err=%v", secondSummary, client.rateCalls, secondErr)
	}
}

// TestManualRateSkipsStoppedOrders 验证人工入口不会因为绕过冷却而重新评价用户已停用整单自动化的订单。
func TestManualRateSkipsStoppedOrders(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 自动化存储及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是任务执行与数据库断言共用的上下文。
	ctx := context.Background()
	// client 记录评价接口调用次数，停用订单不得触达平台。
	client := &fakeAccountTaskClient{}
	// center 是注入平台替身后的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{AccountTaskClient: client})
	if // err 表示账号任务配置写入失败。
	err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true,
		RateContent: "交易愉快", PolishTime: "03:00"}); err != nil {
		t.Fatal(err)
	}
	seedLocalCompletedOrders(t, store, ctx, "order-stopped")
	// stopErr 表示写入整单停用事实的数据库错误；停用事实直接落库，模拟用户在管理页停用该订单。
	if _, stopErr := store.DB.ExecContext(ctx, `INSERT INTO order_automation_stops(cookie_id,order_id,stopped_by) VALUES (?,?,?)`,
		"cid", "order-stopped", 1); stopErr != nil {
		t.Fatal(stopErr)
	}
	// summary、runErr 保存人工入口在停用订单上的执行结果。
	summary, runErr := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	if runErr != nil || summary.Found != 0 || summary.Skipped != 0 || client.rateCalls != 0 {
		t.Fatalf("停用订单不应被人工入口评价: summary=%+v calls=%d err=%v", summary, client.rateCalls, runErr)
	}
}
