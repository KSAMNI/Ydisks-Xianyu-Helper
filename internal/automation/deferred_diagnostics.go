package automation

import (
	"errors"

	"xianyu-go/internal/db"
)

// sellerRoleEvidenceError 保留角色核验失败的精确原因与已知标识；禁止保存任务正文、Cookie 或买家消息。
type sellerRoleEvidenceError struct {
	// reason 是本地卖家门禁产生的稳定原因码。
	reason string
	// orderID 是可靠订单回填后的标识，空值仍表示未能关联。
	orderID string
	// itemID 是事件或本地事实已知的商品标识。
	itemID string
	// chatID 是已知会话标识，不表示该会话只有一笔订单。
	chatID string
}

// newSellerRoleEvidenceError 从 task 复制诊断白名单，reason 必须来自本地门禁，不保留敏感快照。
func newSellerRoleEvidenceError(task Task, reason string) error {
	return &sellerRoleEvidenceError{reason: reason, orderID: task.OrderID, itemID: task.ItemID, chatID: task.ChatID}
}

// Error 返回可持久化的原因码，e 中的身份标识只作为结构化日志字段输出。
func (e *sellerRoleEvidenceError) Error() string {
	return errSellerRoleEvidencePending.Error() + ": " + e.reason
}

// Unwrap 维持调用方通过 errors.Is 判断待补齐卖家事实的兼容语义。
func (e *sellerRoleEvidenceError) Unwrap() error { return errSellerRoleEvidencePending }

// deferredTaskLogFields 从 pending 的领取代次和 task 的白名单标识建立诊断字段；runErr 可补充角色门禁中的回填结果。
// 返回字段不包含 Cookie、Raw 或 TaskJSON；attempt 是队列领取代次而非业务提醒轮次。
func deferredTaskLogFields(pending db.DeferredAutomationTask, task Task, runErr error) []any {
	// kind 区分角色待核验、动作延迟和账号暂停，避免所有队列事件被误称为暂停。
	kind := "account_pause"
	if taskAutomationRunID(task) > 0 {
		// 已领取运行的动作延期优先；原始角色标记会为幂等身份继续保留，不能据此误判当前仍待核验。
		kind = "action_delay"
	} else if /* marker 是尚未领取运行的角色核验快照标记。 */ marker, _ := task.Raw[roleVerificationTaskKeyField].(string); marker != "" {
		kind = "role_verification"
	}
	// reason 仅输出稳定原因码；未知错误仍由调用方原有 err 字段报告。
	reason := ""
	// evidenceErr 只持有核验时已知的非敏感关联标识，不通过模糊查询补猜订单。
	var evidenceErr *sellerRoleEvidenceError
	if errors.As(runErr, &evidenceErr) {
		kind, reason = "role_verification", evidenceErr.reason
		task.OrderID, task.ItemID, task.ChatID = evidenceErr.orderID, evidenceErr.itemID, evidenceErr.chatID
	}
	return []any{"task_id", pending.ID, "account", pending.CookieID, "trigger", pending.TriggerType,
		"order_id", task.OrderID, "item_id", task.ItemID, "chat_id", task.ChatID,
		"run_id", taskAutomationRunID(task), "kind", kind, "reason", reason, "attempt", pending.ClaimVersion}
}
