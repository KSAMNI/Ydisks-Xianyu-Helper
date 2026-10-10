package notify

// manualTaskLabel 把 triggerType 转成明确的交易任务名称；未知类型保留原值，避免笼统提示让用户误认为发货失败。
func manualTaskLabel(triggerType string) string {
	switch triggerType {
	case "bargain_pending":
		return "砍价自动免拼"
	case "order_completed":
		return "确认收货"
	case "":
		return "未知自动化任务"
	case "order_paid", "order_created", "buyer_reviewed", "review_missing_timeout":
		return eventLabel(automationEventType(triggerType))
	default:
		return "其他任务（" + triggerType + "）"
	}
}
