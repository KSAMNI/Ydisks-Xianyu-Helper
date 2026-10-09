package db

import "context"

// QuarantineIdleRun 将 a 中与 run 快照完全一致且没有在途动作的运行隔离；ctx 限制本地写入，reason 为无敏感信息的拒绝原因。
// 返回是否仍有隔离权；并发续租、游标推进或代次变化时返回 false，不改变新执行者的状态或发货凭证。
func (a *AutomationRules) QuarantineIdleRun(ctx context.Context, run AutomationRun, reason string) (bool, error) {
	// result、err 保存空闲运行的精确 CAS 更新结果，不允许取消正在执行的动作。
	result, err := a.DB.ExecContext(ctx, `UPDATE automation_runs
 SET status='needs_review',error_message=?,lease_expires_at=0,next_retry_at=0,updated_at=CURRENT_TIMESTAMP
 WHERE id=? AND attempt_count=? AND status=? AND action_cursor=? AND action_started=0
 AND lease_expires_at=? AND status IN ('running','failed','needs_review')`, reason, run.ID, run.AttemptCount, run.Status, run.ActionCursor, run.LeaseExpiresAt)
	if err != nil {
		return false, err
	}
	// affected、countErr 区分本次隔离成功、竞争失权与驱动计数失败。
	affected, countErr := result.RowsAffected()
	return affected == 1, countErr
}

// RejectDeferredTask 将 a 中 id/claimVersion 对应的已领取任务转入死信，保留人工核对快照；ctx 控制写入，reason 不含凭证。
// 旧代次不得改变新领取者；错误交由调用方发送独立人工处理通知。
func (a *AutomationRules) RejectDeferredTask(ctx context.Context, id int64, claimVersion int, reason string) error {
	// result、err 保存死信收口结果，禁止把不可安全重放的事件继续投入自动退避队列。
	result, err := a.DB.ExecContext(ctx, `UPDATE automation_pending_tasks
 SET status='dead_letter',lease_expires_at=0,error_message=?,updated_at=CURRENT_TIMESTAMP
 WHERE id=? AND status='running' AND attempt_count=?`, reason, id, claimVersion)
	return requireDeferredTaskOwner(result, err)
}
