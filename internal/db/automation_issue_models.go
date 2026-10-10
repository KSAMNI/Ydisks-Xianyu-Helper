package db

// AutomationRunIssue 是待人工核对的运行与订单非敏感查询投影
type AutomationRunIssue struct {
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

	// ID 是异常记录稳定主键。
	ID int64 `json:"id"`
	// CookieID 是所属账号标识，不是凭证明文。
	CookieID string `json:"cookie_id"`
	// OrderID 是当前异常固定的订单标识。
	OrderID string `json:"order_id"`
	// TriggerType 是实际业务触发类型，区分付款发货、求评价等任务。
	TriggerType string `json:"trigger_type"`
	// ErrorMessage 是进入人工核对时的原因，不应包含敏感载荷。
	ErrorMessage string `json:"error_message"`
	// IssueKind 是按快照与执行进度判定的异常类别。
	IssueKind string `json:"issue_kind"`
	// AllowedResolutions 是允许的单任务恢复动作，不代表整单停用授权。
	AllowedResolutions []string `json:"allowed_resolutions"`
	// ActionCursor 是冻结动作计划中下一步的零基位置。
	ActionCursor int `json:"action_cursor"`
	// SentCount 是已确认完成的动作数，不等于卡密张数。
	SentCount int `json:"sent_count"`
	// UpdatedAt 是最近一次状态更新时间。
	UpdatedAt string `json:"updated_at"`
}

// DeferredAutomationIssue 是延期死信的非敏感查询投影
type DeferredAutomationIssue struct {
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
	// OrderID 是当前异常固定的订单标识。
	OrderID string `json:"order_id"`

	// ID 是异常记录稳定主键。
	ID int64 `json:"id"`
	// CookieID 是所属账号标识，不是凭证明文。
	CookieID string `json:"cookie_id"`
	// TriggerType 是实际业务触发类型，区分付款发货、求评价等任务。
	TriggerType string `json:"trigger_type"`
	// ErrorMessage 是进入人工核对时的原因，不应包含敏感载荷。
	ErrorMessage string `json:"error_message"`
	// AttemptCount 是已尝试执行的累计次数。
	AttemptCount int `json:"attempt_count"`
	// UpdatedAt 是最近一次状态更新时间。
	UpdatedAt string `json:"updated_at"`
}
