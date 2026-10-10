package server

// automationRunIssueDTO 是自动化异常运行接口对外暴露的具名 DTO。
type automationRunIssueDTO struct {
	// AccountName 是账号备注，缺省时使用账号标识。
	AccountName string `json:"account_name"`
	// ItemID 是可靠关联的商品标识。
	ItemID string `json:"item_id"`
	// ItemTitle 是本地商品标题，不包含商品详情或凭证。
	ItemTitle string `json:"item_title"`
	// BuyerID 是可靠关联的买家标识。
	BuyerID string `json:"buyer_id"`
	// ChatID 是可靠关联的会话标识。
	ChatID string `json:"chat_id"`
	// OrderStatus 是当前本地订单阶段，仅用于展示。
	OrderStatus string `json:"order_status"`
	// CanStopOrder 是订单身份及归属已确认时才允许整单停止。
	CanStopOrder bool `json:"can_stop_order"`

	// ID 是自动化运行的稳定标识。
	ID int64 `json:"id"`
	// CookieID 是关联账号标识。
	CookieID string `json:"cookie_id"`
	// OrderID 是关联订单标识。
	OrderID string `json:"order_id"`
	// TriggerType 是触发运行的事件类型。
	TriggerType string `json:"trigger_type"`
	// ErrorMessage 是运行进入人工处理状态时记录的原因。
	ErrorMessage string `json:"error_message"`
	// IssueKind 是应用层归类的异常类型。
	IssueKind string `json:"issue_kind"`
	// AllowedResolutions 是当前异常允许的人工处理动作。
	AllowedResolutions []string `json:"allowed_resolutions"`
	// ActionCursor 是下一步动作在计划中的位置。
	ActionCursor int `json:"action_cursor"`
	// SentCount 是已经确认成功的外部动作数量。
	SentCount int `json:"sent_count"`
	// UpdatedAt 是运行状态最近更新的时间文本。
	UpdatedAt string `json:"updated_at"`
}

// deferredAutomationIssueDTO 是延期任务接口对外暴露的具名 DTO。
type deferredAutomationIssueDTO struct {
	// AccountName 是账号备注，缺省时使用账号标识。
	AccountName string `json:"account_name"`
	// ItemID 是可靠关联的商品标识。
	ItemID string `json:"item_id"`
	// ItemTitle 是本地商品标题，不包含商品详情或凭证。
	ItemTitle string `json:"item_title"`
	// BuyerID 是可靠关联的买家标识。
	BuyerID string `json:"buyer_id"`
	// ChatID 是可靠关联的会话标识。
	ChatID string `json:"chat_id"`
	// OrderStatus 是当前本地订单阶段，仅用于展示。
	OrderStatus string `json:"order_status"`
	// CanStopOrder 是订单身份及归属已确认时才允许整单停止。
	CanStopOrder bool `json:"can_stop_order"`
	// OrderID 是任务明确记录的订单标识；缺失时不猜测关联。
	OrderID string `json:"order_id"`

	// ID 是延期任务的稳定标识。
	ID int64 `json:"id"`
	// CookieID 是关联账号标识。
	CookieID string `json:"cookie_id"`
	// TriggerType 是触发延期任务的事件类型。
	TriggerType string `json:"trigger_type"`
	// ErrorMessage 是任务进入死信状态时记录的原因。
	ErrorMessage string `json:"error_message"`
	// AttemptCount 是任务已经尝试执行的次数。
	AttemptCount int `json:"attempt_count"`
	// UpdatedAt 是任务状态最近更新的时间文本。
	UpdatedAt string `json:"updated_at"`
}
