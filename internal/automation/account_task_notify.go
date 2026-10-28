package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// autoRateNotifyStatusFailed 表示自动评价已经耗尽自动重试或平台明确永久失败。
const autoRateNotifyStatusFailed = "failed"

// autoRateNotifyStatusNeedsReview 表示自动评价运行已被隔离为人工核对状态。
const autoRateNotifyStatusNeedsReview = "needs_review"

// notifyAutoRateTerminal 在自动评价运行进入终态失败或人工核对时发送一次通知。
// status 是调用方期望的运行终态，nextRetryAt 是请求写入的下一次重试时间，finishErr 是收口错误。
// 普通失败仍会自动重试，因此不发送通知；通知入队失败只记录中文告警，不阻断运行收口。
func (c *accountTaskCoordinator) notifyAutoRateTerminal(ctx context.Context, accountID, orderID, runKey, status, reason string, nextRetryAt int64, finishErr error) {
	if c == nil {
		return
	}
	// notifyStatus 保存实际需要通知的终态编码；空值表示本次收口不需要通知。
	notifyStatus := ""
	switch {
	case errors.Is(finishErr, errAutomationNeedsReview):
		// 收口失败已被隔离为人工核对：必须让用户知道运行已经停止且不能自动重放。
		notifyStatus = autoRateNotifyStatusNeedsReview
	case finishErr != nil:
		// 其余收口错误由调用方告警，不额外发送终态通知，避免同一次失败重复刷屏。
		return
	case status == autoRateNotifyStatusFailed && nextRetryAt == 0:
		// 永久失败：平台明确返回超期或不可评价，运行已标记为不可自动重试。
		notifyStatus = autoRateNotifyStatusFailed
	case status == autoRateNotifyStatusFailed && c.autoRateRetryExhausted(ctx, runKey):
		// 重试耗尽：数据库在累计执行次数达到上限时把 next_retry_at 置 0，需要回读确认。
		notifyStatus = autoRateNotifyStatusFailed
	default:
		// 成功、取消以及仍会自动重试的普通失败都不通知，避免刷屏。
		return
	}
	// reasonText 是通知正文使用的失败原因摘要；人工核对场景补充本地收口失败原因，便于判断是否需要补偿。
	reasonText := reason
	if notifyStatus == autoRateNotifyStatusNeedsReview {
		// parts 保存本次人工核对通知需要展示的原因片段，避免拼出多余分隔符。
		parts := make([]string, 0, 2)
		// trimmedReason 是去除首尾空白后的失败原因；空值不进入通知正文。
		trimmedReason := strings.TrimSpace(reason)
		if trimmedReason != "" {
			parts = append(parts, trimmedReason)
		}
		// finishText 是收口错误的文本摘要，用于说明本地持久化失败原因。
		finishText := strings.TrimSpace(errorString(finishErr))
		if finishText != "" {
			parts = append(parts, "运行状态收口失败："+finishText)
		}
		// reasonText 改用本次人工核对的原因片段拼接结果，避免出现空分隔符。
		reasonText = strings.Join(parts, "；")
	}
	c.sendAutoRateNotification(ctx, accountID, orderID, runKey, notifyStatus, reasonText)
}

// autoRateRetryExhausted 判断自动评价运行是否已经耗尽自动重试。
// 数据库在累计执行次数达到上限时把 next_retry_at 置 0，因此回读该字段即可确认终态失败；
// 仓储不具备回读能力或读取失败时按“未耗尽”处理，宁可少发通知也不误报终态。
func (c *accountTaskCoordinator) autoRateRetryExhausted(ctx context.Context, runKey string) bool {
	if c == nil || c.repository == nil {
		return false
	}
	// reader 表示仓储是否支持回读单条运行记录的重试状态。
	reader, ok := c.repository.(accountTaskRunStateReader)
	if !ok {
		return false
	}
	// readCtx、readCancel 为回读提供脱离已取消请求但仍受时限约束的预算。
	readCtx, readCancel := newAccountTaskCompensationContext(ctx)
	defer readCancel()
	// nextRetryAt 保存运行记录持久化的下一次自动重试时间；readErr 保存回读错误。
	nextRetryAt, readErr := reader.RunNextRetryAt(readCtx, runKey)
	if readErr != nil {
		if c.logger != nil {
			c.logger.Warn("读取自动评价运行重试状态失败，跳过终态失败通知", "run_key", runKey, "err", readErr)
		}
		return false
	}
	return nextRetryAt == 0
}

// sendAutoRateNotification 通过统一通知器发送一次自动评价终态通知。
// 通知器未注入、实现不具备该类别或入队失败时都只影响通知，不改变评价动作结果。
func (c *accountTaskCoordinator) sendAutoRateNotification(ctx context.Context, accountID, orderID, runKey, status, reason string) {
	if c == nil || c.notifier == nil {
		return
	}
	// notifier 是当前可选通知器；未注入时保持静默。
	notifier := c.notifier()
	if notifier == nil {
		return
	}
	// capable 表示通知器是否支持自动评价终态类别；旧实现静默跳过，避免影响评价结果。
	capable, ok := notifier.(autoRateFailureNotifier)
	if !ok {
		return
	}
	// notifyCtx、notifyCancel 让通知入队脱离可能已取消的扫描请求，同时保持有界预算。
	notifyCtx, notifyCancel := newAccountTaskCompensationContext(ctx)
	defer notifyCancel()
	// idempotencyKey 绑定运行键与终态：同一运行同一终态重复收口只会产生一条通知。
	idempotencyKey := fmt.Sprintf("account-task:%s:%s", runKey, status)
	capable.NotifyAutoRateFailure(notifyCtx, accountID, orderID, status, reason, idempotencyKey)
}
