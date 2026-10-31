package server

// 本文件只承载后台管理、数据概览、订单分析与销售统计的具名响应 DTO；
// 其余 success 契约 DTO 仍留在 success_contract_support.go，避免单文件无限膨胀。

// adminUserResponse 是管理员用户列表项的具名响应 DTO。
type adminUserResponse struct {
	// ID 是用户主键。
	ID int64 `json:"id"`
	// Username 是用户登录名。
	Username string `json:"username"`
	// Email 是用户邮箱。
	Email string `json:"email"`
	// IsActive 表示用户是否启用。
	IsActive bool `json:"is_active"`
	// IsAdmin 表示用户是否为管理员。
	IsAdmin bool `json:"is_admin"`
	// CreatedAt 是用户创建时间。
	CreatedAt string `json:"created_at"`
	// CookieCount 是用户拥有的账号数量。
	CookieCount int `json:"cookie_count"`
}

// adminCookieResponse 是管理员账号列表项的具名响应 DTO。
type adminCookieResponse struct {
	// ID 是账号稳定标识。
	ID string `json:"id"`
	// UserID 是账号所属用户主键。
	UserID int64 `json:"user_id"`
	// Remark 是账号备注。
	Remark string `json:"remark"`
	// CreatedAt 是账号创建时间。
	CreatedAt string `json:"created_at"`
	// Owner 是账号所属用户名。
	Owner string `json:"owner"`
	// Enabled 表示账号是否启用。
	Enabled bool `json:"enabled"`
}

// adminStatsResponse 是管理员全局统计的具名响应 DTO。
type adminStatsResponse struct {
	// TotalUsers 是用户总数。
	TotalUsers int64 `json:"total_users"`
	// TotalCookies 是账号总数。
	TotalCookies int64 `json:"total_cookies"`
	// ActiveCookies 是活跃账号数。
	ActiveCookies int64 `json:"active_cookies"`
	// TotalCards 是卡券总数。
	TotalCards int64 `json:"total_cards"`
	// TotalKeywords 是关键词规则总数。
	TotalKeywords int64 `json:"total_keywords"`
	// TotalOrders 是有效订单总数。
	TotalOrders int64 `json:"total_orders"`
}

// dashboardStatsResponse 是当前用户数据概览的具名响应 DTO。
type dashboardStatsResponse struct {
	// TotalCookies 是账号总数。
	TotalCookies int64 `json:"total_cookies"`
	// ActiveCookies 是活跃账号数。
	ActiveCookies int64 `json:"active_cookies"`
	// TotalCards 是卡券总数。
	TotalCards int64 `json:"total_cards"`
	// AvailableCardStock 是可用卡券库存数量。
	AvailableCardStock int64 `json:"available_card_stock"`
	// TotalKeywords 是关键词规则总数。
	TotalKeywords int64 `json:"total_keywords"`
	// TotalOrders 是订单总数。
	TotalOrders int64 `json:"total_orders"`
}

// analyticsRevenueStatsResponse 是订单收益统计的具名 DTO。
type analyticsRevenueStatsResponse struct {
	// TotalOrders 是统计范围内的订单数。
	TotalOrders int `json:"total_orders"`
	// TotalAmount 是统计范围内的订单总金额。
	TotalAmount float64 `json:"total_amount"`
	// AvgAmount 是订单平均金额。
	AvgAmount float64 `json:"avg_amount"`
	// UniqueBuyers 是买家数量。
	UniqueBuyers int `json:"unique_buyers"`
	// UniqueItems 是商品数量。
	UniqueItems int `json:"unique_items"`
}

// analyticsDailyStatsResponse 是按日期聚合的订单统计 DTO。
type analyticsDailyStatsResponse struct {
	// Date 是用户本地日期。
	Date string `json:"date"`
	// OrderCount 是当天订单数。
	OrderCount int `json:"order_count"`
	// Amount 是当天订单金额。
	Amount float64 `json:"amount"`
}

// analyticsStatusStatsResponse 是按订单状态聚合的统计 DTO。
type analyticsStatusStatsResponse struct {
	// Status 是归一化后的订单状态。
	Status string `json:"status"`
	// Count 是该状态订单数。
	Count int `json:"count"`
	// Amount 是该状态订单金额。
	Amount float64 `json:"amount"`
}

// analyticsCityStatsResponse 是按收货城市聚合的统计 DTO。
type analyticsCityStatsResponse struct {
	// City 是收货城市。
	City string `json:"city"`
	// OrderCount 是该城市订单数。
	OrderCount int `json:"order_count"`
	// TotalAmount 是该城市订单金额。
	TotalAmount float64 `json:"total_amount"`
}

// analyticsItemStatsResponse 是按商品聚合的统计 DTO。
type analyticsItemStatsResponse struct {
	// ItemID 是商品平台标识。
	ItemID string `json:"item_id"`
	// OrderCount 是该商品订单数。
	OrderCount int `json:"order_count"`
	// TotalAmount 是该商品订单金额。
	TotalAmount float64 `json:"total_amount"`
	// AvgAmount 是该商品订单平均金额。
	AvgAmount float64 `json:"avg_amount"`
}

// orderAnalyticsResponse 是订单分析接口的具名响应 DTO。
type orderAnalyticsResponse struct {
	// RevenueStats 是收益统计。
	RevenueStats analyticsRevenueStatsResponse `json:"revenue_stats"`
	// DailyStats 是按日统计。
	DailyStats []analyticsDailyStatsResponse `json:"daily_stats"`
	// StatusStats 是按状态统计。
	StatusStats []analyticsStatusStatsResponse `json:"status_stats"`
	// CityStats 是按城市统计。
	CityStats []analyticsCityStatsResponse `json:"city_stats"`
	// ItemStats 是按商品统计。
	ItemStats []analyticsItemStatsResponse `json:"item_stats"`
}

// validOrderResponse 是有效订单明细项的具名响应 DTO。
type validOrderResponse struct {
	// OrderID 是平台订单标识。
	OrderID string `json:"order_id"`
	// ItemID 是商品平台标识。
	ItemID string `json:"item_id"`
	// BuyerID 是买家平台标识。
	BuyerID string `json:"buyer_id"`
	// ItemTitle 是商品标题。
	ItemTitle string `json:"item_title"`
	// ItemImage 是商品图片地址。
	ItemImage string `json:"item_image"`
	// Quantity 是订单数量文本。
	Quantity string `json:"quantity"`
	// Amount 是订单金额文本。
	Amount string `json:"amount"`
	// OrderStatus 是兼容保留的订单状态字段。
	OrderStatus string `json:"order_status"`
	// Status 是归一化后的订单状态。
	Status string `json:"status"`
	// CookieID 是订单所属账号标识。
	CookieID string `json:"cookie_id"`
	// CreatedAt 是订单创建时间。
	CreatedAt string `json:"created_at"`
}

// salesDailyStatsResponse 是销售统计按日聚合的具名 DTO。
type salesDailyStatsResponse struct {
	// Date 是用户本地日期。
	Date string `json:"date"`
	// SalesAmount 是当天销售额合计（元）。
	SalesAmount float64 `json:"sales_amount"`
	// ReceivedAmount 是当天已收货款合计（元）。
	ReceivedAmount float64 `json:"received_amount"`
	// RefundedAmount 是当天退货货款合计（元）。
	RefundedAmount float64 `json:"refunded_amount"`
	// OrderCount 是当天参与统计的订单数。
	OrderCount int `json:"order_count"`
}

// salesStatsResponse 是销售统计接口的具名响应 DTO。
type salesStatsResponse struct {
	// SalesAmount 是销售额合计（元），包含已发货与已完成订单。
	SalesAmount float64 `json:"sales_amount"`
	// SalesOrders 是计入销售额的订单数。
	SalesOrders int `json:"sales_orders"`
	// ReceivedAmount 是已收货款合计（元），仅包含已完成订单。
	ReceivedAmount float64 `json:"received_amount"`
	// ReceivedOrders 是计入已收货款的订单数。
	ReceivedOrders int `json:"received_orders"`
	// RefundedAmount 是退货货款合计（元），仅包含已取消订单。
	RefundedAmount float64 `json:"refunded_amount"`
	// RefundedOrders 是计入退货货款的订单数。
	RefundedOrders int `json:"refunded_orders"`
	// Daily 是按用户本地日期聚合的销售明细。
	Daily []salesDailyStatsResponse `json:"daily"`
}

// validOrdersResponse 是有效订单分页查询的具名响应 DTO。
type validOrdersResponse struct {
	// Orders 是当前页有效订单。
	Orders []validOrderResponse `json:"orders"`
	// Total 是符合条件的订单总数。
	Total int `json:"total"`
	// Page 是当前页码。
	Page int `json:"page"`
	// PageSize 是当前页大小。
	PageSize int `json:"page_size"`
	// Truncated 表示是否还有未返回的订单。
	Truncated bool `json:"truncated"`
}
