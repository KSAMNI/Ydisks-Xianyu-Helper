package notify

import (
	"encoding/json"
	"strings"
)

// levelLabel 把内部级别标识转换为通知正文使用的中文标签；未知级别原样返回，避免隐藏新增级别。
func levelLabel(level string) string {
	switch level {
	case "critical":
		return "严重"
	case "warn":
		return "警告"
	case "info":
		return "提示"
	default:
		return level
	}
}

// eventLabel 把事件类型转换为订阅页和通知正文使用的中文类型名；空类型退化为通用“通知”。
func eventLabel(eventType string) string {
	switch eventType {
	case EventAccountOffline:
		return "掉线通知"
	case EventAccountRecovered:
		return "恢复通知"
	case EventAccountDisabled:
		return "禁用通知"
	case EventSecurityVerification:
		return "风控验证"
	case EventTokenRenewal:
		return "续期通知"
	case EventDeliveryResult:
		return "交易通知"
	case EventAutomationOrderCreated:
		return "拍下改价"
	case EventAutomationOrderPaid:
		return "付款发货"
	case EventAutomationBuyerReviewed:
		return "评价赠品"
	case EventAutomationReviewMissingTimeout:
		return "求评价"
	case EventAutomationAutoRate:
		return "自动评价"
	case EventManualDeliveryResult:
		return "手动发货结果"
	case EventManualInterventionRequired:
		return "需要人工处理"
	case EventSystemError:
		return "系统错误"
	default:
		if eventType == "" {
			return "通知"
		}
		return eventType
	}
}

// classifyAccountAlertEvent 依据告警标题与正文的关键词判定账号告警类别；无法识别时归类为系统错误。
func classifyAccountAlertEvent(title, body string) string {
	// msg 是小写化的标题与正文拼接结果，用于不区分大小写的关键词匹配。
	msg := strings.ToLower(title + " " + body)
	switch {
	case strings.Contains(msg, "风控"), strings.Contains(msg, "验证"),
		strings.Contains(msg, "滑块"), strings.Contains(msg, "captcha"),
		strings.Contains(msg, "risk"), strings.Contains(msg, "x5sec"):
		return EventSecurityVerification
	case strings.Contains(msg, "禁用"), strings.Contains(msg, "disabled"):
		return EventAccountDisabled
	case strings.Contains(msg, "掉线"), strings.Contains(msg, "离线"),
		strings.Contains(msg, "offline"), strings.Contains(msg, "session"),
		strings.Contains(msg, "登录凭证已失效"):
		return EventAccountOffline
	case strings.Contains(msg, "token"), strings.Contains(msg, "续期"), strings.Contains(msg, "renew"):
		return EventTokenRenewal
	default:
		return EventSystemError
	}
}

// formatEvent 把通知事件渲染为渠道正文：级别与标题、类型、账号、时间、附加字段和正文。
// 附加字段按名称排序输出，保证同一事件多次渲染得到稳定文本；空值字段不输出。
func formatEvent(ev NotificationEvent) string {
	// b 是本次渲染的正文缓冲，避免多次字符串拼接。
	var b strings.Builder
	// label 是事件类型的中文类型名。
	label := eventLabel(ev.Type)
	// level 是事件级别的中文标签；缺失时按“提示”渲染。
	level := levelLabel(ev.Level)
	if level == "" {
		level = "提示"
	}
	// title 是渲染使用的标题；未显式提供时退化为类型名。
	title := strings.TrimSpace(ev.Title)
	if title == "" {
		title = label
	}
	b.WriteString("[")
	b.WriteString(level)
	b.WriteString("] ")
	b.WriteString(title)
	b.WriteString("\n\n类型: ")
	b.WriteString(label)
	if ev.AccountID != "" {
		b.WriteString("\n账号: ")
		b.WriteString(ev.AccountID)
	}
	b.WriteString("\n时间: ")
	b.WriteString(ev.Time.Format("2006-01-02 15:04:05"))
	if len(ev.Fields) > 0 {
		// keys 是附加字段名的稳定排序结果，保证渲染顺序与 map 遍历顺序无关。
		keys := make([]string, 0, len(ev.Fields))
		// k 是当前收集的附加字段名。
		for k := range ev.Fields {
			keys = append(keys, k)
		}
		sortStrings(keys)
		// k 是当前渲染的附加字段名。
		for _, k := range keys {
			// v 是当前附加字段去空白后的取值；空值不输出。
			v := strings.TrimSpace(ev.Fields[k])
			if v == "" {
				continue
			}
			b.WriteByte('\n')
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(v)
		}
	}
	// body 是事件正文去空白后的内容。
	body := strings.TrimSpace(ev.Body)
	if body != "" {
		b.WriteString("\n\n")
		b.WriteString(body)
	}
	return b.String()
}

// eventAllowed 判断渠道订阅配置是否放行某个事件类型；空类型与空订阅列表一律放行。
// delivery_result 是旧版统一交易开关，继续兼容放行四类自动化事件、人工发货结果和人工处理告警。
func eventAllowed(raw, eventType string) (bool, error) {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return true, nil
	}
	// events、err 是订阅列表解析结果及其错误。
	events, err := parseEventTypes(raw)
	if err != nil {
		return false, err
	}
	if len(events) == 0 {
		return true, nil
	}
	if events[eventType] {
		return true, nil
	}
	if events[EventDeliveryResult] && (isAutomationEventType(eventType) || eventType == EventManualDeliveryResult || eventType == EventManualInterventionRequired) {
		return true, nil
	}
	return false, nil
}

// eventAllowedWithFallbacks 先匹配事件实际类别，再匹配来源类别，保证新增人工处理分类不会切断旧自动化订阅。
// fallbackTypes 是允许的兼容来源类别，按顺序尝试。
func eventAllowedWithFallbacks(raw, eventType string, fallbackTypes []string) (bool, error) {
	// allowed、err 保存实际事件类别的订阅判断结果。
	allowed, err := eventAllowed(raw, eventType)
	if err != nil || allowed {
		return allowed, err
	}
	// fallbackType 是当前兼容检查的来源事件类别。
	for _, fallbackType := range fallbackTypes {
		allowed, err = eventAllowed(raw, fallbackType)
		if err != nil || allowed {
			return allowed, err
		}
	}
	return false, nil
}

// isAutomationEventType 判断事件是否属于四类可单独控制的自动化任务。
func isAutomationEventType(eventType string) bool {
	switch eventType {
	case EventAutomationOrderCreated, EventAutomationOrderPaid, EventAutomationBuyerReviewed, EventAutomationReviewMissingTimeout:
		return true
	default:
		return false
	}
}

// parseEventTypes 解析渠道订阅配置：JSON 数组或逗号、分号、空白分隔的文本列表。
// 返回的事件集合为空表示未限制订阅类型；解析失败时返回错误，调用方不得按“全部放行”处理。
func parseEventTypes(raw string) (map[string]bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	// arr 是解析出的原始事件类型列表。
	var arr []string
	if strings.HasPrefix(raw, "[") {
		// err 保存 JSON 数组解析错误。
		if err := json.Unmarshal([]byte(raw), &arr); err != nil {
			return nil, err
		}
	} else {
		arr = strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
		})
	}
	if len(arr) == 0 {
		return nil, nil
	}
	// out 是去空白后的事件类型集合。
	out := make(map[string]bool, len(arr))
	// v 是当前遍历的事件类型原文。
	for _, v := range arr {
		v = strings.TrimSpace(v)
		if v != "" {
			out[v] = true
		}
	}
	return out, nil
}

// sortStrings 对 values 做原地升序插入排序；字段名数量很少，无需引入额外排序依赖。
func sortStrings(values []string) {
	// i 是当前插入位置。
	for i := 1; i < len(values); i++ {
		// j 是从 i 向前比较的下标。
		for j := i; j > 0 && values[j-1] > values[j]; j-- {
			values[j-1], values[j] = values[j], values[j-1]
		}
	}
}
