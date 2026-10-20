package engine

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/xianyu/renew"

	"log/slog"
)

// newPendingRenewResult 通过本地 httptest 服务构造一个官网 Promise 已超时、底层响应仍待交付的续期结果。
// release 关闭后服务端才返回响应；mode 为 "ok" 时返回携带 Cookie 的成功响应，为 "broken" 时响应头先到后中断正文。
func newPendingRenewResult(t *testing.T, release <-chan struct{}, mode string) *renew.Result {
	t.Helper()
	// srv 是受 release 控制的本地续期服务，测试结束时关闭。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "sdkSilent", Value: "1", Path: "/"})
		if mode == "broken" {
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte("{"))
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		_, _ = w.Write([]byte(`{"content":{"data":{"processFinished":true,"resultCode":100}}}`))
	}))
	t.Cleanup(srv.Close)
	// service 把 Promise 窗口缩短到毫秒级，使结果必然进入 pending 状态。
	service := renew.Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1, PromiseTimeout: 5 * time.Millisecond}
	// result、err 是 Promise 超时后的同步续期结果及错误。
	result, err := service.RenewAPIFirst(context.Background(), "unb=123; havana_lgc_exp=9999999999999")
	if err != nil || result == nil || !result.HasPending() {
		t.Fatalf("未得到 pending 续期结果: result=%+v err=%v", result, err)
	}
	return result
}

// joinPendingRenewal 在有限预算内等待协调器的全部迟到任务退出。
func joinPendingRenewal(t *testing.T, coordinator *pendingRenewalCoordinator) {
	t.Helper()
	// ctx、cancel 是等待迟到任务退出的有界上下文。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !coordinator.waitContext(ctx) {
		t.Fatal("迟到续期任务未在预算内退出")
	}
}

// TestPendingRenewalCoordinatorNilAndEmptyGuards 验证空协调器、空上下文与无任务时的安全行为。
func TestPendingRenewalCoordinatorNilAndEmptyGuards(t *testing.T) {
	// nilCoordinator 是空指针协调器。
	var nilCoordinator *pendingRenewalCoordinator
	nilCoordinator.beginTask()
	nilCoordinator.finishTask()
	if !nilCoordinator.waitContext(context.Background()) {
		t.Fatal("空协调器应视为已收束")
	}
	// coordinator 是没有登记任何任务的协调器。
	coordinator := &pendingRenewalCoordinator{}
	coordinator.finishTask()
	if coordinator.waitContext(nil) { //nolint:staticcheck // 刻意验证空 Context 的防御分支。
		t.Fatal("空 Context 不应判定为已收束")
	}
	if !coordinator.waitContext(context.Background()) {
		t.Fatal("无任务时应立即成功")
	}
	nilCoordinator.watch(context.Background(), nil, nil, nil, nil)
	coordinator.watch(context.Background(), nil, nil, nil, nil)
	coordinator.watch(context.Background(), nil, &renew.Result{}, nil, nil)
}

// TestPendingRenewalWatchRejectedByLifecycle 验证账号生命周期拒绝接纳任务时不启动等待协程。
func TestPendingRenewalWatchRejectedByLifecycle(t *testing.T) {
	// release 保持关闭前服务端阻塞；本测试不会等待响应。
	release := make(chan struct{})
	defer close(release)
	// result 是待交付的续期结果。
	result := newPendingRenewResult(t, release, "ok")
	// coordinator 是被测协调器。
	coordinator := &pendingRenewalCoordinator{}
	coordinator.watch(context.Background(), func() (context.Context, func(), bool) { return context.Background(), func() {}, false }, result, nil, nil)
	if coordinator.activeTasks != 0 {
		t.Fatal("被拒绝的任务不应登记")
	}
}

// TestPendingRenewalWatchPersistsLateCookie 验证迟到成功响应会被持久化，且空 parent 与空 logger 使用默认值。
func TestPendingRenewalWatchPersistsLateCookie(t *testing.T) {
	// release 控制服务端何时返回迟到响应。
	release := make(chan struct{})
	// result 是待交付的续期结果。
	result := newPendingRenewResult(t, release, "ok")
	// persisted 接收持久化回调收到的迟到结果。
	persisted := make(chan *renew.Result, 1)
	// coordinator 是被测协调器。
	coordinator := &pendingRenewalCoordinator{}
	// finished 记录账号生命周期释放函数被调用。
	finished := make(chan struct{})
	coordinator.watch(nilContext, func() (context.Context, func(), bool) {
		return context.Background(), func() { close(finished) }, true
	}, result, func(_ context.Context, late *renew.Result) error {
		persisted <- late
		return nil
	}, nil)
	close(release)
	joinPendingRenewal(t, coordinator)
	select {
	// late 保存底层迟到响应经 AwaitPending 取得的续期结果
	case late := <-persisted:
		if late == nil || len(late.SetCookies) == 0 {
			t.Fatalf("迟到结果应携带 Set-Cookie: %+v", late)
		}
	default:
		t.Fatal("迟到 Cookie 未持久化")
	}
	<-finished
}

// TestPendingRenewalWatchLogsPersistAndWaitFailures 验证持久化失败、响应中断与等待取消的日志分支。
func TestPendingRenewalWatchLogsPersistAndWaitFailures(t *testing.T) {
	// logBuffer 收集脱敏告警日志；只在 Join 之后读取。
	var logBuffer bytes.Buffer
	// logger 把告警写入 logBuffer。
	logger := slog.New(slog.NewTextHandler(&logBuffer, nil))

	// 持久化失败。
	release := make(chan struct{})
	// coordinator 是被测协调器实例
	coordinator := &pendingRenewalCoordinator{}
	coordinator.watch(context.Background(), func() (context.Context, func(), bool) { return context.Background(), func() {}, true },
		newPendingRenewResult(t, release, "ok"), func(context.Context, *renew.Result) error { return errors.New("persist boom") }, logger)
	close(release)
	joinPendingRenewal(t, coordinator)
	if !strings.Contains(logBuffer.String(), "保存静默续期迟到 Cookie 失败") {
		t.Fatalf("缺少持久化失败日志: %s", logBuffer.String())
	}

	// 响应头到达后正文中断：既保存 Cookie 又记录响应失败。
	logBuffer.Reset()
	// brokenRelease 是控制中断响应服务端何时返回的释放信号通道
	brokenRelease := make(chan struct{})
	// persistCalls 统计持久化回调次数。
	persistCalls := 0
	coordinator.watch(context.Background(), func() (context.Context, func(), bool) { return context.Background(), func() {}, true },
		newPendingRenewResult(t, brokenRelease, "broken"), func(context.Context, *renew.Result) error { persistCalls++; return nil }, logger)
	close(brokenRelease)
	joinPendingRenewal(t, coordinator)
	if persistCalls != 1 || !strings.Contains(logBuffer.String(), "静默续期底层响应失败") {
		t.Fatalf("响应中断应保存已收到 Cookie 并记录告警: calls=%d log=%s", persistCalls, logBuffer.String())
	}

	// 父上下文取消：等待失败且不调用持久化。
	logBuffer.Reset()
	// hold 是保持服务端阻塞的释放信号通道
	hold := make(chan struct{})
	defer close(hold)
	// parent、cancel 是账号生命周期上下文，提前取消以终止等待。
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	persistCalls = 0
	coordinator.watch(parent, func() (context.Context, func(), bool) { return context.Background(), func() {}, true },
		newPendingRenewResult(t, hold, "ok"), func(context.Context, *renew.Result) error { persistCalls++; return nil }, logger)
	joinPendingRenewal(t, coordinator)
	if persistCalls != 0 || !strings.Contains(logBuffer.String(), "等待静默续期底层响应失败") {
		t.Fatalf("取消应只记录等待失败: calls=%d log=%s", persistCalls, logBuffer.String())
	}
}

// nilContext 是刻意保持为 nil 的 Context，用于验证被测函数对空上下文的容错。
var nilContext context.Context
