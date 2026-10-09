package automation

import (
	"context"

	"xianyu-go/internal/db"
)

// ruleExecutionStatus 描述本次调用而非历史运行的结果，避免将幂等跳过解释成外部动作成功。
type ruleExecutionStatus string

const (
	// ruleExecutionSucceeded 仅在动作完成且运行成功落库后产生。
	ruleExecutionSucceeded ruleExecutionStatus = "success"
	// ruleExecutionSkipped 表示本次没有取得或不需要执行权。
	ruleExecutionSkipped ruleExecutionStatus = "skipped"
	// ruleExecutionDeferred 表示运行已进入已有的安全延期链路。
	ruleExecutionDeferred ruleExecutionStatus = "deferred"
	// ruleExecutionNeedsReview 表示外部结果或本地收口不确定，禁止自动重放。
	ruleExecutionNeedsReview ruleExecutionStatus = "needs_review"
	// ruleExecutionFailed 表示本次执行或准备失败，重试资格仍由持久化运行控制。
	ruleExecutionFailed ruleExecutionStatus = "failed"
)

// ruleExecutionResult 只携带无凭证的执行摘要；SentCount 是累计完成动作数，不是卡密张数或买家送达回执。
type ruleExecutionResult struct {
	// Status 区分本次真实成功、跳过、延期、失败与人工处理。
	Status ruleExecutionStatus
	// RunID 关联真实领取或准入检查命中的运行；未创建或竞争跳过时可为零。
	RunID int64
	// SentCount 保存本次运行已确认完成的动作累计数量。
	SentCount int
	// SkipReason 保存稳定的诊断分类，不携带原始事件或敏感内容。
	SkipReason string
}

// reviewRunAdmission 仅为调度器的新求评价尝试做只读检查；ctx 控制查询，task 和 rule 提供身份与幂等键。
// 返回 nil 表示不适用此优化；查询失败必须阻止准备和发送，显式续跑始终保留既有恢复语义。
func (r automationRunCoordinator) reviewRunAdmission(ctx context.Context, task Task, rule db.AutomationRule) (*db.AutomationRunAdmission, error) {
	if task.Source != "scheduler" || task.TriggerType != TriggerReviewMissingTimeout || taskAutomationRunID(task) > 0 {
		return nil, nil
	}
	// triggerKey 包含订单与业务提醒轮次，其他规则或未来合法轮次不会被已有运行阻挡。
	triggerKey := buildTriggerKey(task)
	if triggerKey == "" {
		return nil, nil
	}
	return r.store.Automation.CheckRunAdmission(ctx, rule.ID, triggerKey, task.AccountID, task.OrderID)
}
