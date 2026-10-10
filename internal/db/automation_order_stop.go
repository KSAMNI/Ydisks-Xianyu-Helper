package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// ErrOrderAutomationStopped 表示用户持久停止了此账号订单的全部自动化；不是可重试的外部动作失败。
var ErrOrderAutomationStopped = errors.New("用户已停止此订单全部自动化")

// ErrOrderAutomationIdentity 表示无法可靠确认异常对应的当前订单，客户端不能强制指定订单。
var ErrOrderAutomationIdentity = errors.New("订单身份缺失或归属不一致，只能处理本次任务，不能停止整单")

// automationIssueIdentity 只解析快照中的非敏感身份；不反序列化消息、卡密或凭证。
type automationIssueIdentity struct {
	// AccountID 是当前格式的账号标识。
	AccountID string
	// OrderID 是当前格式的订单标识。
	OrderID string
	// LegacyAccountID 兼容旧快照的账号字段。
	LegacyAccountID string `json:"account_id"`
	// LegacyOrderID 兼容旧快照的订单字段。
	LegacyOrderID string `json:"order_id"`
}

// issueOrderID 从 raw 提取与 accountID 一致的订单；storedID 为运行固定身份，冲突或损坏快照一律拒绝关联。
func issueOrderID(raw, accountID, storedID string) string {
	// identity 只在本次关联检查中存在，绝不作为 HTTP 响应输出。
	var identity automationIssueIdentity
	if json.Unmarshal([]byte(raw), &identity) != nil {
		return ""
	}
	if identity.AccountID != "" && identity.LegacyAccountID != "" && identity.AccountID != identity.LegacyAccountID {
		return ""
	}
	if identity.OrderID != "" && identity.LegacyOrderID != "" && identity.OrderID != identity.LegacyOrderID {
		return ""
	}
	if identity.AccountID == "" {
		identity.AccountID = identity.LegacyAccountID
	}
	if identity.OrderID == "" {
		identity.OrderID = identity.LegacyOrderID
	}
	if identity.AccountID != "" && identity.AccountID != accountID {
		return ""
	}
	if storedID != "" {
		if identity.OrderID != "" && identity.OrderID != storedID {
			return ""
		}
		return storedID
	}
	if identity.AccountID != accountID {
		return ""
	}
	return strings.TrimSpace(identity.OrderID)
}

// CheckOrderAutomation 用 ctx 检查 accountID/orderID 的持久停用记录；无订单的非订单任务不受影响，查询失败必须停止动作。
func (a *AutomationRules) CheckOrderAutomation(ctx context.Context, accountID, orderID string) error {
	return checkOrderAutomation(ctx, a.DB, accountID, orderID)
}

// checkOrderAutomation 通过 reader 在普通读取或账号→订单事务锁域内校验停用；返回明确停用哨兵或数据库错误。
func checkOrderAutomation(ctx context.Context, reader sqlQueryExecer, accountID, orderID string) error {
	if strings.TrimSpace(orderID) == "" {
		return nil
	}
	// count 是该账号订单的持久停用记录数，不读取任何账号秘密。
	var count int
	if err := reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_automation_stops WHERE cookie_id=? AND order_id=?`, accountID, orderID).Scan(&count); err != nil { // err 阻止查询失败时放行动作。
		return err
	}
	if count > 0 {
		return ErrOrderAutomationStopped
	}
	return nil
}

// StopOrderForIssue 按 userID 的异常 issueID 停用整单；deferred 区分死信与运行。只进行短数据库事务，不撤销已发生的外部动作。
// 锁顺序为账号→订单→异常记录；身份、归属与异常状态在锁内重新核对，任何失败全部回滚。
func (a *AutomationRules) StopOrderForIssue(ctx context.Context, userID, issueID int64, deferred bool) error {
	// query 只在两条固定查询中选择，不包含用户提供的 SQL。
	query := `SELECT ar.cookie_id,COALESCE(ar.order_id,''),ar.raw_event_json FROM automation_runs ar JOIN cookies c ON c.id=ar.cookie_id WHERE ar.id=? AND c.user_id=? AND ar.status='needs_review'`
	if deferred {
		query = `SELECT ar.cookie_id,'',ar.task_json FROM automation_pending_tasks ar JOIN cookies c ON c.id=ar.cookie_id WHERE ar.id=? AND c.user_id=? AND ar.status='dead_letter'`
	}
	// accountID、storedID、raw 保存异常固定身份和仅供本地解析的快照。
	var accountID, storedID, raw string
	if err := a.DB.QueryRowContext(ctx, query, issueID, userID).Scan(&accountID, &storedID, &raw); err != nil { // err 统一隐藏不存在与越权异常。
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	// orderID 必须来自本条异常，而不是客户端输入或最近订单推测。
	orderID := issueOrderID(raw, accountID, storedID)
	if orderID == "" {
		return ErrOrderAutomationIdentity
	}
	// transaction、err 保存短事务及创建错误；不使用拒绝已停用订单的执行准入方法。
	transaction, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if err := lockAutomationOwnership(ctx, transaction, accountID, orderID); err != nil { // err 防止账号归属修复与订单停用交错。
		if errors.Is(err, ErrForbidden) {
			return ErrOrderAutomationIdentity
		}
		return err
	}
	// currentAccount、currentStored、currentRaw 在写锁后重新读取，防止使用已处理或被替换的异常。
	var currentAccount, currentStored, currentRaw string
	if err := transaction.QueryRowContext(ctx, query, issueID, userID).Scan(&currentAccount, &currentStored, &currentRaw); err != nil { // err 对并发处理统一返回未找到。
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if currentAccount != accountID || issueOrderID(currentRaw, currentAccount, currentStored) != orderID {
		return ErrNotFound
	}
	// exists 在相同锁域再次确认本地订单存在且属于当前用户账号，不能为不明订单建立封禁。
	var exists int
	if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders o JOIN cookies c ON c.id=o.cookie_id WHERE o.order_id=? AND o.cookie_id=? AND c.user_id=?`, orderID, accountID, userID).Scan(&exists); err != nil { // err 不允许跳过最终归属验证。
		return err
	}
	if exists != 1 {
		return ErrOrderAutomationIdentity
	}
	if _, err := transaction.ExecContext(ctx, dialectInsertIgnorePrefix(a.Dialect)+` INTO order_automation_stops(cookie_id,order_id,stopped_by) VALUES (?,?,?)`, accountID, orderID, userID); err != nil { // err 使停用失败不改变任何任务状态。
		return err
	}
	// 未开始的运行立即收口；在途动作保留检查点和结果，由执行者结束后处理，绝不伪造已撤回。
	if _, err := transaction.ExecContext(ctx, `UPDATE automation_runs SET status='canceled',next_retry_at=0,lease_expires_at=0,error_message='用户已停止此订单全部自动化',updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND order_id=? AND action_started=0 AND status IN ('needs_review','failed','running')`, accountID, orderID); err != nil { // err 保证任务收口与停用标记原子提交。
		return err
	}
	// rows、readErr 读取同账号延期任务的最小关联字段；不能依赖三种数据库不同的 JSON 操作符。
	rows, readErr := transaction.QueryContext(ctx, `SELECT id,task_json FROM automation_pending_tasks WHERE cookie_id=? AND status IN ('pending','dead_letter')`, accountID)
	if readErr != nil {
		return readErr
	}
	// ids 保存明确属于此订单的延期主键，关闭结果集后才执行更新。
	var ids []int64
	for rows.Next() {
		// id、taskJSON 是当前延期任务的身份和仅供解析的快照。
		var id int64
		// taskJSON 仅在仓储内解析订单身份，不暴露消息及凭证。
		var taskJSON string
		// err 阻止部分读取后误提交整单停用。
		if err := rows.Scan(&id, &taskJSON); err != nil {
			_ = rows.Close()
			return err
		} // err 阻止部分读取后误提交。
		if issueOrderID(taskJSON, accountID, "") == orderID {
			ids = append(ids, id)
		}
	}
	// rowsErr 保存迭代错误，关闭游标后再返回，保证连接可用于后续更新。
	rowsErr := rows.Err()
	_ = rows.Close()
	if rowsErr != nil {
		return rowsErr
	}
	for _, id := range ids { // id 是已验证归属的延期任务，保留记录供审计，不物理删除。
		if _, err := transaction.ExecContext(ctx, `UPDATE automation_pending_tasks SET status='canceled',lease_expires_at=0,error_message='用户已停止此订单全部自动化',updated_at=CURRENT_TIMESTAMP WHERE id=?`, id); err != nil {
			return err
		} // err 回滚整次停用。
	}
	return transaction.Commit()
}
