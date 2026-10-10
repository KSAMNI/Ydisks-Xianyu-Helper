package automation

import (
	"context"
	"errors"
	"fmt"

	"xianyu-go/internal/db"
)

// executePreparedRule 执行已通过准入的 task/rule/run 并负责检查点结果收口；ctx 的取消不影响短时补偿写入。
// 返回本次 outcome 和 resultErr，调用方继续统一归类延期与人工核对；没有新增后台协程或跨网络互斥锁。
func (r automationRunCoordinator) executePreparedRule(ctx context.Context, task Task, rule db.AutomationRule, run *db.AutomationRun) (outcome ruleExecutionResult, resultErr error) {
	outcome.Status = ruleExecutionFailed
	outcome.RunID = run.ID
	// status 是运行完成时写入数据库的结果状态。
	status := "success"
	// errMsg 是运行完成时记录的可重试或人工核对原因。
	errMsg := ""
	// sent 是截至当前检查点已经确认成功的动作数量。
	sent := run.SentCount
	// finish 表示函数返回时是否应执行正常运行收口。
	finish := true
	// 运行收口先持久化终态，再决定是否可报告本次实际成功。
	defer func() {
		outcome.SentCount = sent
		if !finish {
			return
		}
		// finishCtx 限制运行收口不能被原始请求取消。
		finishCtx, cancel := newAutomationRunCompensationContext(ctx)
		// cancel 在结果通知已使用 finishCtx 持久化入队后释放收口上下文，避免取消导致通知静默丢失。
		defer cancel()
		// finishErr 保存运行结果写入失败，避免覆盖原始动作错误。
		if finishErr := r.store.Automation.FinishRun(finishCtx, run.ID, run.AttemptCount, status, sent, errMsg); finishErr != nil {
			// reason 说明结果落库失败后禁止自动重放的原因，并用于人工核对记录。
			reason := "自动化运行结果保存失败，已停止自动重放，请人工核对: " + finishErr.Error()
			// status 和 errMsg 让统一通知明确告知结果未知，避免误报成功或失败可重试。
			status, errMsg = "needs_review", reason
			// quarantineErr 保存将外部动作结果转为人工核对状态时的错误。
			quarantineErr := r.store.Automation.QuarantineRunResult(finishCtx, run.ID, run.AttemptCount, sent, reason)
			if quarantineErr != nil {
				r.logger.Error("保存自动化执行结果和人工核对状态均失败", "run_id", run.ID, "finish_err", finishErr, "quarantine_err", quarantineErr)
				resultErr = errors.Join(resultErr, errAutomationNeedsReview, fmt.Errorf("保存自动化执行结果失败: %w", finishErr), fmt.Errorf("保存人工核对状态失败: %w", quarantineErr))
			} else {
				resultErr = errors.Join(resultErr, errAutomationNeedsReview, fmt.Errorf("保存自动化执行结果失败: %w", finishErr))
			}
		}
		if r.hasNotifier() {
			r.notifyResult(finishCtx, task, run.ID, status, sent, errMsg)
		}
		if status == "success" {
			outcome.Status = ruleExecutionSucceeded
			r.logger.Info("自动化规则执行成功", "run_id", run.ID, "account", task.AccountID,
				"order_id", task.OrderID, "trigger", task.TriggerType, "sent_count", sent)
		}
	}()
	// actions 是当前规则生成的完整动作计划。
	actions := task.ActionPlan
	if task.TriggerType == TriggerOrderPaid && !r.planner.hasMatchingSendCard(task, actions) {
		status, errMsg = "failed", "未匹配到订单规格对应的卡密动作"
		return outcome, errors.New(errMsg)
	}
	// deferred 表示动作已写入延迟队列；actionErr 表示动作执行或检查点失败。
	var deferred bool
	// actionErr 表示动作执行或检查点持久化失败，成功时为 nil。
	var actionErr error
	sent, deferred, actionErr = r.executeRunActions(ctx, task, rule.ID, run, actions, false)
	if deferred {
		finish = false
		return outcome, errAutomationDeferred
	}
	if errors.Is(actionErr, db.ErrOrderAutomationStopped) {
		status, errMsg = "canceled", db.ErrOrderAutomationStopped.Error()
		outcome.Status, outcome.SkipReason = ruleExecutionSkipped, "order_stopped"
		return outcome, nil
	}
	if errors.Is(actionErr, errReplayCanceled) {
		finish = false
		outcome.Status, outcome.SkipReason = ruleExecutionSkipped, "order_no_longer_pending"
		return outcome, nil
	}
	if errors.Is(actionErr, errAutomationNeedsReview) {
		finish = false
		if r.hasNotifier() {
			// notifyCtx 保证人工核对通知在动作预算取消后仍能进入持久化通知链路。
			notifyCtx, notifyCancel := newAutomationRunCompensationContext(ctx)
			r.notifyResult(notifyCtx, task, run.ID, "needs_review", sent, actionErr.Error())
			notifyCancel()
		}
		return outcome, actionErr
	}
	if actionErr != nil {
		if sent > 0 && !errors.Is(actionErr, ErrMessageNotSent) && !errors.Is(actionErr, errActionNotPerformed) {
			// reason 说明部分动作成功后为何必须人工核对。
			reason := "运行已完成部分动作，后续动作失败，已禁止从头自动重放: " + actionErr.Error()
			// quarantineCtx 保证部分成功运行的人工核对状态不被已取消的动作上下文阻断。
			quarantineCtx, quarantineCancel := newAutomationRunCompensationContext(ctx)
			// quarantineErr 保存部分成功运行的人工核对状态。
			quarantineErr := r.store.Automation.QuarantineRunResult(quarantineCtx, run.ID, run.AttemptCount, sent, reason)
			quarantineCancel()
			if quarantineErr != nil {
				finish = false
				r.logger.Error("保存自动化人工核对状态失败", "run_id", run.ID, "err", quarantineErr)
				return outcome, errors.Join(errAutomationNeedsReview, errAutomationQuarantine, actionErr, quarantineErr)
			}
			finish = false
			if r.hasNotifier() {
				// notifyCtx 保证部分成功运行的人工核对通知不被动作预算取消阻断。
				notifyCtx, notifyCancel := newAutomationRunCompensationContext(ctx)
				r.notifyResult(notifyCtx, task, run.ID, "needs_review", sent, reason)
				notifyCancel()
			}
			return outcome, fmt.Errorf("%w: %v", errAutomationNeedsReview, actionErr)
		}
		status, errMsg = "failed", actionErr.Error()
		if errors.Is(actionErr, ErrMessageNotSent) || errors.Is(actionErr, errActionNotPerformed) {
			errMsg = db.SafeRetryErrorPrefix + errMsg
		}
		return outcome, actionErr
	}
	if task.TriggerType == TriggerReviewMissingTimeout && task.OrderID != "" {
		// incrementErr 保存求评价消息成功后的提醒次数。
		if incrementErr := r.store.Automation.IncrementReviewRequest(ctx, task.OrderID); incrementErr != nil {
			// reason 记录外部求评价消息已发送而本地计数未落库的原因，用于隔离运行并通知人工核对。
			reason := "求评价消息已发送，但保存提醒次数失败，已停止自动重放: " + incrementErr.Error()
			// quarantineCtx 保证求评价运行的人工核对状态在请求取消后仍能落库。
			quarantineCtx, quarantineCancel := newAutomationRunCompensationContext(ctx)
			// quarantineErr 保存提醒次数写入失败的人工核对状态。
			quarantineErr := r.store.Automation.QuarantineRunResult(quarantineCtx, run.ID, run.AttemptCount, sent, reason)
			quarantineCancel()
			if quarantineErr != nil {
				finish = false
				r.logger.Error("保存求评价人工核对状态失败", "run_id", run.ID, "err", quarantineErr)
				return outcome, errors.Join(errAutomationNeedsReview, errAutomationQuarantine, incrementErr, quarantineErr)
			}
			finish = false
			if r.hasNotifier() {
				// notifyCtx 保证求评价人工核对通知在请求取消后仍能进入通知链路。
				notifyCtx, notifyCancel := newAutomationRunCompensationContext(ctx)
				r.notifyResult(notifyCtx, task, run.ID, "needs_review", sent, reason)
				notifyCancel()
			}
			return outcome, fmt.Errorf("%w: %v", errAutomationNeedsReview, incrementErr)
		}
	}
	return outcome, nil
}
