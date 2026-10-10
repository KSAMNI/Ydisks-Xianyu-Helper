package db

import (
	"context"
	"database/sql"
	"errors"
)

// ListIssues 按 userID 查询异常及非敏感订单上下文；ctx 控制取消，所有游标关闭后才执行关联查询，避免单连接死锁。
func (a *AutomationRules) ListIssues(ctx context.Context, userID int64) ([]AutomationRunIssue, []DeferredAutomationIssue, error) {
	// runRows、err 是当前用户异常运行的查询游标与读取错误
	runRows, err := a.DB.QueryContext(ctx, `SELECT ar.id,ar.cookie_id,ar.order_id,ar.trigger_type,ar.error_message,
		ar.action_cursor,ar.sent_count,ar.updated_at,ar.raw_event_json,ar.action_started,COALESCE(r.enabled,0)
		FROM automation_runs ar JOIN cookies c ON c.id=ar.cookie_id
		LEFT JOIN automation_rules r ON r.id=ar.rule_id
		WHERE c.user_id=? AND ar.status='needs_review' AND r.deleted_at IS NULL ORDER BY ar.updated_at DESC,ar.id DESC`, userID)
	if err != nil {
		return nil, nil, err
	}
	// runs 收集需要人工核对的运行摘要
	runs := []AutomationRunIssue{}
	// runSnapshots 只在仓储内校验身份，不向上层传递原始事件。
	var runSnapshots []string
	for runRows.Next() {
		// issue 承载本行异常的非敏感字段
		var issue AutomationRunIssue
		// rawEventJSON 仅用于仓储内校验快照与异常身份，不向上层返回
		var rawEventJSON string
		// actionStarted 标记未知外部动作占用；ruleEnabled 决定是否仍能安全恢复
		var actionStarted, ruleEnabled int
		if // err 保存当前读取或关闭结果集的失败，禁止返回不完整列表
		err := runRows.Scan(&issue.ID, &issue.CookieID, &issue.OrderID, &issue.TriggerType, &issue.ErrorMessage,
			&issue.ActionCursor, &issue.SentCount, &issue.UpdatedAt, &rawEventJSON, &actionStarted, &ruleEnabled); err != nil {
			_ = runRows.Close()
			return nil, nil, err
		}
		issue.IssueKind, issue.AllowedResolutions = automationIssuePolicy(
			rawEventJSON, actionStarted != 0, issue.ActionCursor, ruleEnabled != 0, issue.SentCount, issue.ErrorMessage,
		)
		runs = append(runs, issue)
		runSnapshots = append(runSnapshots, rawEventJSON)
	}
	// runRowsErr 保留迭代失败，关闭后才能复用数据库连接。
	runRowsErr := runRows.Err()
	if err := runRows.Close(); err != nil { // err 表示驱动关闭游标失败。
		return nil, nil, err
	}
	if runRowsErr != nil {
		return nil, nil, runRowsErr
	}
	// taskRows、err 是当前用户延期死信的查询游标与失败原因
	taskRows, err := a.DB.QueryContext(ctx, `SELECT apt.id,apt.cookie_id,apt.trigger_type,apt.error_message,
		apt.attempt_count,apt.updated_at,apt.task_json
		FROM automation_pending_tasks apt JOIN cookies c ON c.id=apt.cookie_id
		WHERE c.user_id=? AND apt.status='dead_letter' ORDER BY apt.updated_at DESC,apt.id DESC`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer taskRows.Close()
	// tasks 收集延期异常，不向上层暴露原始载荷
	tasks := []DeferredAutomationIssue{}
	// taskSnapshots 仅供延期任务的严格订单关联。
	var taskSnapshots []string
	for taskRows.Next() {
		// issue 承载本行异常的非敏感字段
		var issue DeferredAutomationIssue
		// taskJSON 是本行延期事件快照，不能序列化到响应。
		var taskJSON string
		if // err 保存当前读取或关闭结果集的失败，禁止返回不完整列表
		err := taskRows.Scan(&issue.ID, &issue.CookieID, &issue.TriggerType, &issue.ErrorMessage,
			&issue.AttemptCount, &issue.UpdatedAt, &taskJSON); err != nil {
			return nil, nil, err
		}
		tasks = append(tasks, issue)
		taskSnapshots = append(taskSnapshots, taskJSON)
	}
	// rowsErr 在关闭结果集后保留读取失败，随后才读取账号和订单摘要。
	rowsErr := taskRows.Err()
	_ = taskRows.Close()
	if rowsErr != nil {
		return nil, nil, rowsErr
	}
	// visibleRuns、visibleTasks 排除已经整单停用的记录，保留持久化审计但不再反复告警。
	visibleRuns := make([]AutomationRunIssue, 0, len(runs))
	// visibleTasks 保留未整单停用的延期异常。
	visibleTasks := make([]DeferredAutomationIssue, 0, len(tasks))
	for index, issue := range runs { // index 对应只在仓储内保留的快照；issue 是待补全的异常摘要。
		// info、stopped、err 区分可靠订单上下文、已停用状态及读取故障。
		info, stopped, err := a.issueContext(ctx, issue.CookieID, issueOrderID(runSnapshots[index], issue.CookieID, issue.OrderID))
		if err != nil {
			return nil, nil, err
		}
		if stopped {
			continue
		}
		// 固定订单列为空时只接受同账号快照中的明确订单，不覆盖已有运行身份。
		if issue.OrderID == "" {
			issue.OrderID = info.OrderID
		}
		issue.AccountName = info.AccountName
		issue.ItemID = info.ItemID
		issue.ItemTitle = info.ItemTitle
		issue.BuyerID = info.BuyerID
		issue.ChatID = info.ChatID
		issue.OrderStatus = info.OrderStatus
		issue.CanStopOrder = info.CanStopOrder
		visibleRuns = append(visibleRuns, issue)
	}
	for index, issue := range tasks { // index 对应延期快照；issue 不包含原始事件和凭证。
		// info、stopped、err 保留缺少订单时的明确空值，而不是猜测关联。
		info, stopped, err := a.issueContext(ctx, issue.CookieID, issueOrderID(taskSnapshots[index], issue.CookieID, ""))
		if err != nil {
			return nil, nil, err
		}
		if stopped {
			continue
		}
		issue.AccountName = info.AccountName
		issue.ItemID = info.ItemID
		issue.ItemTitle = info.ItemTitle
		issue.BuyerID = info.BuyerID
		issue.ChatID = info.ChatID
		issue.OrderStatus = info.OrderStatus
		issue.CanStopOrder = info.CanStopOrder
		issue.OrderID = info.OrderID
		// 订单尚未关联时，用事件快照中与账号一致的商品/买家/会话线索补充展示，
		// 避免已知信息一律显示为“尚未获取”；线索不用于关联或停用授权。
		if issue.OrderID == "" {
			// clues 是仅从快照解析的非敏感身份线索，归属已由 issueSnapshotClues 校验。
			clues := issueSnapshotClues(taskSnapshots[index], issue.CookieID)
			if issue.ItemID == "" {
				issue.ItemID = clues.ItemID
			}
			if issue.BuyerID == "" {
				issue.BuyerID = clues.BuyerID
			}
			if issue.ChatID == "" {
				issue.ChatID = clues.ChatID
			}
		}
		visibleTasks = append(visibleTasks, issue)
	}
	return visibleRuns, visibleTasks, nil
}

// issueContext 返回 accountID/orderID 的非敏感摘要、停用状态和读取错误；没有本地订单时仍展示明确记录的订单号，但不允许整单停用。
func (a *AutomationRules) issueContext(ctx context.Context, accountID, orderID string) (AutomationRunIssue, bool, error) {
	// info 复用仓储内部摘要载体，只返回显式选择的字段，不读取 Cookie 或订单收货信息。
	info := AutomationRunIssue{OrderID: orderID}
	if err := a.DB.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(remark,''),id) FROM cookies WHERE id=?`, accountID).Scan(&info.AccountName); err != nil { // err 不允许把账号读取故障伪装为空白摘要。
		return info, false, err
	}
	if orderID == "" {
		return info, false, nil
	}
	// stopErr 区分已停止与存储故障；后者必须向调用方报告。
	stopErr := a.CheckOrderAutomation(ctx, accountID, orderID)
	if errors.Is(stopErr, ErrOrderAutomationStopped) {
		return info, true, nil
	}
	if stopErr != nil {
		return info, false, stopErr
	}
	// err 是严格按订单与账号查询的结果，不采用最近聊天订单回退。
	err := a.DB.QueryRowContext(ctx, `SELECT COALESCE(o.item_id,''),COALESCE(i.item_title,''),COALESCE(o.buyer_id,''),COALESCE(o.chat_id,''),COALESCE(o.order_status,'') FROM orders o LEFT JOIN item_info i ON i.cookie_id=o.cookie_id AND i.item_id=o.item_id AND i.deleted_at IS NULL WHERE o.order_id=? AND o.cookie_id=?`, orderID, accountID).Scan(&info.ItemID, &info.ItemTitle, &info.BuyerID, &info.ChatID, &info.OrderStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return info, false, nil
	}
	if err != nil {
		return info, false, err
	}
	info.CanStopOrder = true
	return info, false, nil
}
