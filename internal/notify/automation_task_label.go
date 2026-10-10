package notify

// manualTaskLabel 把 triggerType 转成事件阶段名称；这类通知没有关联到具体规则运行，
// 不能直接使用“拍下改价”“付款发货”等已确定业务任务名，避免误导用户认为规则已匹配。
func manualTaskLabel(triggerType string) string {
	switch triggerType {
	case "bargain_pending":
		return "砍价自动免拼"
	case "order_completed":
		return "确认收货"
	case "":
		return "未知自动化任务"
	case "order_created":
		return "订单创建事件（卖家身份待核验）"
	case "order_paid":
		return "付款发货事件（卖家身份待核验）"
	case "buyer_reviewed":
		return "评价赠品"
	case "review_missing_timeout":
		return "求评价"
	default:
		return "其他任务（" + triggerType + "）"
	}
}
