package automation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

// roleVerificationTaskKeyField 保存未知角色待核验任务的稳定防重键；它会随任务快照保存，避免补齐订单号后改变延期任务身份。
const roleVerificationTaskKeyField = "automation_role_verification_key"

// roleVerificationRetryInterval 控制首条未知角色事件等待订单/商品同步的最短间隔。
const roleVerificationRetryInterval = time.Minute

// reasonOrderCreatedNotLocalItem 表示无角色拍下事件的商品不在本账号本地商品库；该原因不可重试，调用方静默跳过。
const reasonOrderCreatedNotLocalItem = "order_created_not_local_item"

// errSellerRoleEvidencePending 表示待核验任务暂时没有足够的本地卖家事实；调度器会按既有延期队列退避并最终通知人工。
var errSellerRoleEvidencePending = errors.New("未知角色事件暂缺本地卖家事实")

// isWebSocketShippingTask 判断任务是否来自 WebSocket 的付款后发货阶段；只有这类事件需要未知角色证据门禁。
func isWebSocketShippingTask(task Task) bool {
	// source、triggerType 保存经过规范化的事件来源和交易阶段，避免普通评价或人工任务误用卖家门禁。
	source, triggerType := strings.TrimSpace(task.Source), strings.TrimSpace(task.TriggerType)
	return source == "ws" && (triggerType == TriggerOrderPaid || triggerType == TriggerBargainPending)
}

// isWebSocketSellerOrderTask 判断需要确认当前账号卖家身份的 WebSocket 交易事件；评价历史兼容不在此门禁内。
func isWebSocketSellerOrderTask(task Task) bool {
	if isWebSocketShippingTask(task) {
		return true
	}
	return strings.TrimSpace(task.Source) == "ws" && strings.TrimSpace(task.TriggerType) == TriggerOrderCreated
}

// authorizeWebSocketSellerTask 校验缺少 role 的 WebSocket 发货事件是否能由本地卖家事实证明；
// 付款事件缺订单号时不得放行：非 WebSocket 卖家事件、明确买家与明确卖家一律拒绝，
// 只有未知角色的 WebSocket 卖家事件继续按本地候选细分原因（同样拒绝，仅用于诊断粒度）。
// 未通过校验时返回 false 且不写订单事实；数据库读取失败返回错误，调用方必须停止外部动作并保留后续扫描机会。
func (c *Center) authorizeWebSocketSellerTask(ctx context.Context, task Task) (Task, bool, string, error) {
	if task.TriggerType == TriggerOrderPaid && strings.TrimSpace(task.OrderID) == "" {
		// 付款发货必须由订单号证明交易身份：缺订单号时准备阶段会跳过订单事实与数量核验，
		// 因此无论事件来源、即使明确标为卖家也不得放行；该原因不可重试，事件被丢弃，订单同步后的兜底由待发货扫描负责。
		if task.OrderRole == OrderRoleBuyer {
			return task, false, "explicit_buyer_role", nil
		}
		if task.OrderRole == OrderRoleSeller || !isWebSocketSellerOrderTask(task) {
			return task, false, "missing_order_id", nil
		}
	}
	if !isWebSocketSellerOrderTask(task) {
		return task, true, "", nil
	}
	switch task.OrderRole {
	case OrderRoleSeller:
		return task, true, "", nil
	case OrderRoleBuyer:
		return task, false, "explicit_buyer_role", nil
	}
	if task.TriggerType == TriggerOrderCreated {
		if strings.TrimSpace(task.ItemID) == "" {
			// 拍下事件没有商品号时无法证明是本账号商品，自动化没有意义，直接跳过。
			return task, false, reasonOrderCreatedNotLocalItem, nil
		}
		if c == nil || c.store == nil || c.store.Items == nil {
			return task, false, "item_store_unavailable", nil
		}
		// owned、itemErr 保存拍下事件对应商品是否曾属于当前账号；商品详情不进入事件快照或日志。
		owned, itemErr := c.store.Items.ExistsByCookieItem(ctx, task.AccountID, task.ItemID)
		if itemErr != nil {
			return task, false, "", fmt.Errorf("核对未知角色拍下商品归属: %w", itemErr)
		}
		if !owned {
			// 商品不在本账号商品库时，拍下自动化本身没有意义（同一用户作为买家拍下他人商品也属此类），静默跳过：
			// 不延期、不通知、不请求平台。付款后的发货由付款事件和待发货扫描负责，不依赖拍下事件。
			return task, false, reasonOrderCreatedNotLocalItem, nil
		}
		task.OrderRole = OrderRoleSeller
		return task, true, "", nil
	}
	if c == nil || c.store == nil || c.store.Orders == nil {
		return task, false, "order_store_unavailable", nil
	}
	if strings.TrimSpace(task.OrderID) == "" {
		// 简化付款或砍价通知未能提供订单号时，按本地待发货候选细分原因，避免统一笼统的 missing_order_id。
		return task, false, c.missingOrderIDReason(ctx, task), nil
	}
	// order、orderErr 保存当前账号的本地订单事实；读取范围随后还会再次核对账号归属。
	order, orderErr := c.store.Orders.Get(ctx, task.OrderID)
	if errors.Is(orderErr, db.ErrNotFound) {
		return task, false, "missing_local_order", nil
	}
	if orderErr != nil {
		return task, false, "", fmt.Errorf("核对未知角色订单事实: %w", orderErr)
	}
	return c.authorizeWebSocketSellerTaskWithOrder(ctx, task, order)
}

// authorizeWebSocketSellerTaskWithOrder 使用已读取的订单和当前账号商品表完成未知角色核验。
// order 必须属于当前账号且仍待发货；商品、买家、会话等事件字段若已提供则必须逐项一致。
func (c *Center) authorizeWebSocketSellerTaskWithOrder(ctx context.Context, task Task, order *db.Order) (Task, bool, string, error) {
	if order == nil {
		return task, false, "missing_local_order", nil
	}
	if !sameOrderIdentity(order.CookieID, task.AccountID) {
		return task, false, "order_account_mismatch", nil
	}
	if !isPendingShipOrder(order) || order.SystemShipped {
		return task, false, "order_not_pending_ship", nil
	}
	if strings.TrimSpace(order.ItemID) == "" {
		return task, false, "incomplete_local_order_identity", nil
	}
	if strings.TrimSpace(task.ItemID) != "" && !sameOrderIdentity(task.ItemID, order.ItemID) {
		return task, false, "item_mismatch", nil
	}
	if strings.TrimSpace(task.BuyerID) != "" && !sameOrderIdentity(task.BuyerID, order.BuyerID) {
		return task, false, "buyer_mismatch", nil
	}
	if strings.TrimSpace(task.ChatID) != "" && !sameOrderIdentity(task.ChatID, order.ChatID) {
		return task, false, "chat_mismatch", nil
	}
	if c == nil || c.store == nil || c.store.Items == nil {
		return task, false, "item_store_unavailable", nil
	}
	// owned、itemErr 保存当前账号历史商品归属查询结果；软删除商品仍是卖家证据，避免售出后商品同步移除导致漏发。
	owned, itemErr := c.store.Items.ExistsByCookieItem(ctx, task.AccountID, order.ItemID)
	if itemErr != nil {
		return task, false, "", fmt.Errorf("核对未知角色商品归属: %w", itemErr)
	}
	if !owned {
		return task, false, "missing_local_item", nil
	}
	task = mergeOrderIntoTask(task, order)
	task.OrderRole = OrderRoleSeller
	return task, true, "", nil
}

// isPendingShipOrder 判断本地订单是否仍处于付款后待发货阶段；状态别名与数字码的归一规则统一由 db.IsPendingShipStatus 定义。
func isPendingShipOrder(order *db.Order) bool {
	if order == nil {
		return false
	}
	return db.IsPendingShipStatus(order.OrderStatus)
}

// paidDeliveryBlockedReason 判断订单是否已被证明不应再执行付款发货，返回空字符串表示可以继续。
// 付款 WebSocket 事件本身不写入 pending_ship，订单状态可能仍是 unknown 或 processing，因此这里只拦截已发货、已完成、
// 已取消、退款中、部分发货（partial_success、partial_pending_finalize，再发卡会重复交付）和系统已发货的订单，不要求状态必须等于待发货；订单为 nil（本地尚无记录）时放行，由后续规则与幂等键兜底。
func paidDeliveryBlockedReason(order *db.Order) string {
	if order == nil {
		return ""
	}
	if order.SystemShipped {
		return "system_shipped"
	}
	// status 是归一后的订单状态，用于判断是否已越过待发货阶段。
	status := db.NormalizeOrderStatus(strings.TrimSpace(order.OrderStatus))
	switch status {
	case "shipped", "completed", "cancelled", "refunding", "partial_success", "partial_pending_finalize":
		return "order_" + status
	default:
		return ""
	}
}

// isZeroEvidenceReminder 判断事件是否为不含任何可核验身份事实的纯待办提醒。
// 这类 redReminder 简化通知只携带会话号和一句提醒文字，订单号、商品号、买家号、角色全部为空，
// 因此无论延期核验多少次、等订单同步多久，都永远不会有可匹配的锚点。
// 与"真实订单同步滞后"的本质区别是：后者带订单号，同步到了就能核验；前者零事实，永远核验不过。
// 只有订单、商品、买家、角色全部缺失时才返回 true，任何一项存在都必须保留原有延期核验路径。
func isZeroEvidenceReminder(task Task) bool {
	return strings.TrimSpace(task.OrderID) == "" &&
		strings.TrimSpace(task.ItemID) == "" &&
		strings.TrimSpace(task.BuyerID) == "" &&
		task.OrderRole == OrderRoleUnknown
}

// shouldDiscardZeroEvidenceReminder 判断一个未通过卖家核验的事件是否应被静默丢弃而不是延期核验。
// 仅当事件不含任何身份事实（isZeroEvidenceReminder）且拒绝原因证明它与当前账号卖家身份无关时成立：
//   - no_pending_order_candidate：本地按会话已确认无任何待发货候选，真实订单必在本地有待发货记录；
//   - missing_item_id：订单创建事件连商品号都为空，不可能是当前账号的商品被拍下（真实拍下卡片必带商品号）。
//
// 只要事件带订单号、商品号或买家号等任何锚点，就必须保留延期核验，不得在此丢弃。
func (c *Center) shouldDiscardZeroEvidenceReminder(task Task, rejectReason string) bool {
	if !isZeroEvidenceReminder(task) {
		return false
	}
	// discardable 标记该拒绝原因是否足以证明事件与卖家身份无关；其它原因一律保留延期核验。
	discardable := rejectReason == "no_pending_order_candidate" || rejectReason == "missing_item_id"
	if !discardable {
		return false
	}
	if c.logger != nil {
		c.logger.Info("忽略与当前账号卖家身份无关的待办提醒", "account", task.AccountID, "trigger", task.TriggerType, "chat_id", task.ChatID, "reason", rejectReason)
	}
	return true
}

// missingOrderIDReason 为缺少订单号的未知角色发货事件计算更精确的原因码。
// 依次区分缺会话标识、无本地待发货候选、同会话多候选三类，查询失败时回退到保守的 missing_order_id。
// 该方法只做只读统计，不猜测或补全订单号。
func (c *Center) missingOrderIDReason(ctx context.Context, task Task) string {
	if strings.TrimSpace(task.ChatID) == "" {
		return "missing_chat_id"
	}
	if c == nil || c.store == nil || c.store.Orders == nil {
		return "missing_order_id"
	}
	// count、err 保存当前账号同会话待发货候选数量和统计错误；失败时不细分类别。
	count, err := c.store.Orders.CountPendingByChat(ctx, task.AccountID, task.ChatID, task.BuyerID, task.ItemID)
	if err != nil {
		return "missing_order_id"
	}
	if count == 0 {
		return "no_pending_order_candidate"
	}
	if count > 1 {
		return "multiple_pending_order_candidates"
	}
	// 唯一候选存在但 resolvePaidTaskOrder 已按唯一性回填失败，说明商品或买家约束不一致。
	return "missing_order_id"
}

// reasonOrderAmountNotPositive 表示订单成交金额为 0、负数或无法识别；服务类预约单没有真实物流订单，不得自动发货。
const reasonOrderAmountNotPositive = "order_amount_not_positive"

// reasonOrderAmountMissing 表示订单详情补全后仍没有成交金额；金额无法证明为正数时同样不得自动发货。
const reasonOrderAmountMissing = "order_amount_missing"

// paidAmountBlockedReason 判断付款发货订单的成交金额是否允许自动发货，返回空字符串表示金额为严格正数。
// raw 是订单事实中的金额文本（可带 ¥ 前缀与千位分隔符），按十进制字符串解析以避免浮点误差；
// 空金额返回 reasonOrderAmountMissing，由调用方决定是否已完成详情补全；零金额与非法金额返回 reasonOrderAmountNotPositive。
func paidAmountBlockedReason(raw string) string {
	// normalized、valid 保存规范化后的十进制金额文本及其格式是否合法。
	normalized, valid := db.NormalizeOrderAmount(raw)
	if !valid {
		return reasonOrderAmountNotPositive
	}
	if normalized == "" {
		return reasonOrderAmountMissing
	}
	// digit 逐字符检查金额是否含有 1-9 的有效数字；全为 0 与小数点即为零金额。
	for _, digit := range normalized {
		if digit >= '1' && digit <= '9' {
			return ""
		}
	}
	return reasonOrderAmountNotPositive
}

// roleVerificationRetryable 判断拒绝原因是否可能因订单或商品同步完成而恢复；身份冲突和已结束订单不应反复重放。
// missing_order_id 不可重试：延期快照不会因重放而获得订单号（不允许按会话猜订单），反复重放只会耗尽次数并对已由待发货扫描
// 处理的订单发出误报；缺订单号的付款事件直接丢弃，订单同步后的丢消息兜底由待发货扫描负责。
func roleVerificationRetryable(reason string) bool {
	switch reason {
	case "missing_local_order", "incomplete_local_order_identity", "missing_local_item", "order_store_unavailable", "item_store_unavailable":
		return true
	default:
		return false
	}
}

// deferUnknownRoleTask 将首条未知角色事件保存到统一延期队列；任务使用事件指纹去重，补齐订单号后仍沿用同一队列记录。
func (c *Center) deferUnknownRoleTask(ctx context.Context, task Task, reason string) error {
	if c == nil || c.store == nil || c.store.Automation == nil {
		return errors.New("未知角色事件缺少延期任务存储")
	}
	// key 保存本次事件跨订单同步前后的稳定任务身份。
	key := roleVerificationTaskKey(task)
	// task 保存带稳定键的副本；不修改调用方持有的原始 Raw 映射。
	task = taskWithRoleVerificationKey(task, key)
	task.CookieStr = ""
	// raw、marshalErr 保存去除凭证后的任务快照和序列化错误。
	raw, marshalErr := json.Marshal(task)
	if marshalErr != nil {
		return fmt.Errorf("序列化未知角色待核验任务: %w", marshalErr)
	}
	// dueAt 控制订单同步完成前的重试节奏，避免系统卡片高峰持续打数据库。
	dueAt := time.Now().UTC().Add(roleVerificationRetryInterval).Unix()
	// deferErr 保存未知角色待核验任务写入延期队列的错误。
	if deferErr := c.store.Automation.DeferTask(ctx, db.DeferredAutomationTask{
		TaskKey: task.AccountID + ":" + key, CookieID: task.AccountID, TriggerType: task.TriggerType,
		TaskJSON: string(raw), DueAt: dueAt, ErrorMessage: "等待本地卖家事实：" + reason,
	}); deferErr != nil {
		return fmt.Errorf("保存未知角色待核验任务: %w", deferErr)
	}
	return nil
}

// roleVerificationTaskKey 为未知角色事件生成稳定键；优先使用平台事件键，缺失时对脱敏事实和原始事件做哈希。
func roleVerificationTaskKey(task Task) string {
	if task.Raw != nil {
		// marker、ok 保存历史待核验任务中已固化的键及其类型判断结果。
		if marker, ok := task.Raw[roleVerificationTaskKeyField].(string); ok && strings.TrimSpace(marker) != "" {
			return strings.TrimSpace(marker)
		}
	}
	// key 保存平台事件已有的稳定业务键；存在时无需再次计算哈希。
	if key := buildTriggerKey(task); key != "" {
		return key
	}
	// fingerprintSource 只包含订单事件事实和原始平台报文，不包含 CookieStr 等凭证字段。
	fingerprintSource := struct {
		TriggerType string         `json:"trigger_type"`
		AccountID   string         `json:"account_id"`
		ChatID      string         `json:"chat_id"`
		OrderID     string         `json:"order_id"`
		ItemID      string         `json:"item_id"`
		BuyerID     string         `json:"buyer_id"`
		Text        string         `json:"text"`
		Raw         map[string]any `json:"raw"`
	}{
		TriggerType: task.TriggerType, AccountID: task.AccountID, ChatID: task.ChatID,
		OrderID: task.OrderID, ItemID: task.ItemID, BuyerID: task.BuyerID, Text: task.Text, Raw: task.Raw,
	}
	// encoded、marshalErr 保存用于哈希的脱敏事件字节；异常类型只影响键内容，不开放执行。
	encoded, marshalErr := json.Marshal(fingerprintSource)
	if marshalErr != nil {
		encoded = []byte(strings.Join([]string{task.TriggerType, task.AccountID, task.ChatID, task.OrderID, task.ItemID, task.BuyerID, task.Text}, "\x00"))
	}
	// digest 保存脱敏事件指纹，作为没有平台业务键时的幂等身份。
	digest := sha256.Sum256(encoded)
	return "role_verification:" + task.TriggerType + ":" + hex.EncodeToString(digest[:])
}

// taskWithRoleVerificationKey 复制任务的 Raw 映射并写入待核验键，保证后续恢复和动作延期使用同一防重身份。
func taskWithRoleVerificationKey(task Task, key string) Task {
	// raw 保存任务原始映射的浅拷贝，避免在事件接收线程中修改平台报文引用。
	raw := make(map[string]any, len(task.Raw)+1)
	// field、value 遍历原始任务字段并复制其值，避免修改接收线程共享的映射。
	for field, value := range task.Raw {
		raw[field] = value
	}
	raw[roleVerificationTaskKeyField] = key
	task.Raw = raw
	return task
}

// sameOrderIdentity 比较订单、商品、买家或会话标识，并兼容历史协议的 @goofish 后缀。
func sameOrderIdentity(left, right string) bool {
	left = strings.TrimSuffix(strings.TrimSpace(left), "@goofish")
	right = strings.TrimSuffix(strings.TrimSpace(right), "@goofish")
	return left != "" && right != "" && left == right
}
