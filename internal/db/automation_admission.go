package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// automationRunReclaimPredicate 同时用于只读准入与原子重领；两个参数依次是租约和重试判断的当前 UTC 秒数。
// 保留旧重领条件：动作未开始、租约到期或安全失败到期；预检不能替代账号/订单锁中的原子更新。
const automationRunReclaimPredicate = `((status='running' AND action_started=0 AND (lease_expires_at=0 OR lease_expires_at<?))
 OR (status='failed' AND action_started=0 AND attempt_count<3 AND next_retry_at<=?
 AND ((sent_count=0 AND error_message NOT LIKE '[no_retry]%') OR error_message LIKE '[safe_retry]%')))`

// AutomationRunAdmission 是无凭证、无事件正文的运行准入投影，不对外承诺已经获得执行权。
type AutomationRunAdmission struct {
	// RunID 标识已存在的幂等运行，新事件为零。
	RunID int64
	// Allowed 表示本次只读快照允许尝试原子领取，不构成执行权。
	Allowed bool
	// Reason 保存不能领取的稳定分类，避免日志输出运行错误正文中的外部内容。
	Reason string
}

// CheckRunAdmission 使用 ctx 从 a 查询 ruleID、triggerKey 的运行，并核对 accountID、orderID 归属。
// 返回允许尝试领取或跳过原因；数据库失败返回错误，不能凭缺少读取结果执行动作。
func (a *AutomationRules) CheckRunAdmission(ctx context.Context, ruleID int64, triggerKey, accountID, orderID string) (*AutomationRunAdmission, error) {
	// admission 仅携带诊断信息；没有既有行时允许后续原子创建。
	admission := &AutomationRunAdmission{Allowed: true}
	// storedAccount、storedOrder 保存既有运行的非敏感归属，用于阻止跨身份借用幂等键。
	var storedAccount, storedOrder string
	// status、attempt、nextRetry 保存终态和重试预算，用于解释为什么未获准入。
	var status string
	// attempt 是已经领取的重试代次数，达到三次后禁止普通失败自动重放。
	var attempt int
	// nextRetry 是下一次安全重试的 UTC 秒数。
	var nextRetry int64
	// reclaimable 是复用原子重领条件得到的整数判断，兼容 SQLite、MySQL 和 PostgreSQL。
	var reclaimable int
	// now 为本次查询提供一致的秒级租约与退避判断时刻。
	now := time.Now().UTC().Unix()
	// queryErr 只读取身份和状态投影，不加载 RawEventJSON、卡密或平台凭证。
	queryErr := a.DB.QueryRowContext(ctx, `SELECT id,cookie_id,COALESCE(order_id,''),status,attempt_count,next_retry_at,
 CASE WHEN `+automationRunReclaimPredicate+` THEN 1 ELSE 0 END
 FROM automation_runs WHERE rule_id=? AND trigger_key=?`, now, now, ruleID, triggerKey).
		Scan(&admission.RunID, &storedAccount, &storedOrder, &status, &attempt, &nextRetry, &reclaimable)
	if errors.Is(queryErr, sql.ErrNoRows) {
		return admission, nil
	}
	if queryErr != nil {
		return nil, queryErr
	}
	admission.Allowed = false
	switch {
	case storedAccount != accountID || storedOrder != orderID:
		admission.Reason = "identity_mismatch"
	case reclaimable != 0:
		admission.Allowed = true
	case status == "failed" && attempt >= 3:
		admission.Reason = "retry_exhausted"
	case status == "failed" && nextRetry > now:
		admission.Reason = "retry_not_due"
	case status == "failed":
		admission.Reason = "unsafe_retry"
	default:
		admission.Reason = status
	}
	return admission, nil
}
