// Package deliverytemplate 负责解析和渲染可复用的发货消息模板。
package deliverytemplate

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// DeliveryZone 是发货时间变量使用的固定时区；统一北京时间 UTC+8，避免服务器或容器时区改变渲染结果。
var DeliveryZone = time.FixedZone("Asia/Shanghai", 8*60*60)

// deliveryDateLayout 是 {{delivery_date}} 的渲染格式，只保留年月日。
const deliveryDateLayout = "2006-01-02"

// deliveryMomentLayout 是 {{delivery_time}} 的渲染格式，保留到分钟且不带时区偏移文本。
const deliveryMomentLayout = "2006-01-02 15:04"

// cardVariablePattern 匹配模板中按名称绑定卡密库存的变量。
var cardVariablePattern = regexp.MustCompile(`\{\{(?:delivery\.)?cards\.([A-Za-z0-9_-]+)\}\}`)

// builtinVariablePattern 匹配由订单、买家事实和发货时刻直接提供的模板变量。
var builtinVariablePattern = regexp.MustCompile(`\{\{(?:delivery\.)?(buyer_nickname|order_id|buyer_id|card_name|delivery_date|delivery_time)\}\}`)

// customVariablePattern 匹配发货规则传入的字符串键值变量。
var customVariablePattern = regexp.MustCompile(`\{\{(?:delivery\.)?custom\.([A-Za-z0-9_-]+)\}\}`)

// fullVariablePattern 只匹配完整且受支持的模板变量令牌。
var fullVariablePattern = regexp.MustCompile(`^\{\{(?:delivery\.)?(?:cards\.[A-Za-z0-9_-]+|custom\.[A-Za-z0-9_-]+|buyer_nickname|order_id|buyer_id|card_name|delivery_date|delivery_time)\}\}$`)

// FormatDeliveryMoment 把发货时刻转换为日期和日期时间两个变量文本；零值表示调用方没有提供时刻，渲染为空字符串而不是零值日期。
func FormatDeliveryMoment(moment time.Time) (string, string) {
	if moment.IsZero() {
		return "", ""
	}
	// local 是把发货时刻换算到 DeliveryZone 后的时间视图。
	local := moment.In(DeliveryZone)
	return local.Format(deliveryDateLayout), local.Format(deliveryMomentLayout)
}

// Parsed 保存校验后的消息副本、卡密变量键和自定义变量键。
type Parsed struct {
	// Messages 只保留文本消息，供历史纯文本调用方兼容使用。
	Messages []string
	// MessageItems 是完整的有序消息，图片不参与文本变量提取。
	MessageItems []Message
	// Keys 是模板需要外部绑定卡密组的变量键。
	Keys []string
	// CustomKeys 是模板需要规则提供的自定义变量键，按消息首次出现顺序排列。
	CustomKeys []string
}

// CardKeys 提取单条消息中按首次出现顺序使用的卡密变量键。
func CardKeys(message string) []string {
	// seen 保存已经提取的变量键，避免同一消息重复消费库存。
	seen := make(map[string]bool)
	// keys 保存当前消息需要的卡密变量键。
	keys := make([]string, 0)
	for /* match 表示当前卡密变量的正则匹配结果。 */ _, match := range cardVariablePattern.FindAllStringSubmatch(message, -1) {
		if len(match) == 2 && !seen[match[1]] {
			seen[match[1]] = true
			keys = append(keys, match[1])
		}
	}
	return keys
}

// Parse 校验消息非空并提取所有受支持的模板变量。
func Parse(messages []string) (Parsed, error) {
	return ParseMessages(TextMessages(messages))
}

// parseTextMessages 校验历史文本列表并提取变量；图片由 ParseMessages 独立处理。
func parseTextMessages(messages []string) (Parsed, error) {
	if len(messages) == 0 {
		return Parsed{}, fmt.Errorf("发货模板至少需要一条消息")
	}
	// parsed 保存不共享调用方切片的模板内容，避免后续修改影响已校验结果。
	parsed := Parsed{Messages: make([]string, len(messages))}
	// seen 记录已经提取的变量键，保证 Keys 只按首次使用顺序出现一次。
	seen := map[string]bool{}
	// customSeen 记录已经出现的自定义变量键，避免重复展示规则输入。
	customSeen := map[string]bool{}
	for /* index 表示消息顺序；message 表示当前消息正文。 */ index, message := range messages {
		if strings.TrimSpace(message) == "" {
			return Parsed{}, fmt.Errorf("发货模板第 %d 条消息不能为空", index+1)
		}
		parsed.Messages[index] = message
		// offset 表示当前消息从左到右扫描双大括号令牌的字节位置。
		for offset := 0; offset < len(message); {
			// nextOpen、nextClose 分别保存下一个开放和闭合标记的相对位置。
			nextOpen := strings.Index(message[offset:], "{{")
			// nextClose 保存下一个闭合双大括号的相对位置。
			nextClose := strings.Index(message[offset:], "}}")
			if nextClose >= 0 && (nextOpen < 0 || nextClose < nextOpen) {
				return Parsed{}, fmt.Errorf("发货模板第 %d 条消息包含未匹配闭合标记", index+1)
			}
			if nextOpen < 0 {
				break
			}
			// markerStart 保存当前开放标记的绝对字节位置。
			markerStart := offset + nextOpen
			// closingOffset 保存从开放标记后找到的闭合标记相对位置。
			closingOffset := strings.Index(message[markerStart+2:], "}}")
			if closingOffset < 0 {
				return Parsed{}, fmt.Errorf("发货模板第 %d 条消息包含未闭合变量", index+1)
			}
			// tokenEnd 是当前变量令牌闭合符之后的下一个字节位置。
			tokenEnd := markerStart + 2 + closingOffset + 2
			// token 是需要按完整语法校验并提取变量键的完整令牌。
			token := message[markerStart:tokenEnd]
			if !fullVariablePattern.MatchString(token) {
				return Parsed{}, fmt.Errorf("发货模板第 %d 条消息包含非法变量", index+1)
			}
			// cardMatch 保存当前令牌的卡密变量匹配结果。
			if cardMatch := cardVariablePattern.FindStringSubmatch(token); len(cardMatch) == 2 {
				// key 是当前消息中识别出的卡密变量键。
				key := cardMatch[1]
				if !seen[key] {
					seen[key] = true
					parsed.Keys = append(parsed.Keys, key)
				}
			}
			// customMatch 保存当前令牌的自定义变量匹配结果。
			if customMatch := customVariablePattern.FindStringSubmatch(token); len(customMatch) == 2 {
				// key 保存自定义变量在规则键值表中的名称。
				key := customMatch[1]
				if !customSeen[key] {
					customSeen[key] = true
					parsed.CustomKeys = append(parsed.CustomKeys, key)
				}
			}
			offset = tokenEnd
		}
	}
	return parsed, nil
}

// VariableValues 是渲染一条发货模板消息所需的非敏感业务值。
type VariableValues struct {
	// BuyerNickname 是购买用户昵称，缺失时按空字符串渲染。
	BuyerNickname string
	// OrderID 是订单号。
	OrderID string
	// BuyerID 是买家 ID。
	BuyerID string
	// CardName 是模板绑定的卡密库存名称。
	CardName string
	// CardValues 是模板卡密变量键对应的已取货内容。
	CardValues map[string]string
	// CustomValues 是发货规则传入的自定义字符串键值表。
	CustomValues map[string]string
	// DeliveryMoment 是本次模板动作的发货时刻，用于渲染 delivery_date 和 delivery_time；零值渲染为空字符串。
	DeliveryMoment time.Time
}

// Replace 将模板支持的变量替换为当前订单和规则上下文中的值。
func Replace(message string, values VariableValues) string {
	// deliveryDate、deliveryMoment 保存按固定时区格式化的发货日期和日期时间文本。
	deliveryDate, deliveryMoment := FormatDeliveryMoment(values.DeliveryMoment)
	// fixedValues 保存固定变量令牌和当前任务字段的对应关系。
	fixedValues := map[string]string{
		"buyer_nickname": values.BuyerNickname,
		"order_id":       values.OrderID,
		"buyer_id":       values.BuyerID,
		"card_name":      values.CardName,
		"delivery_date":  deliveryDate,
		"delivery_time":  deliveryMoment,
	}
	// out 保存逐类替换后的消息正文。
	out := builtinVariablePattern.ReplaceAllStringFunc(message, func(token string) string {
		// match 保存当前固定变量的结构化匹配结果。
		match := builtinVariablePattern.FindStringSubmatch(token)
		return fixedValues[match[1]]
	})
	out = cardVariablePattern.ReplaceAllStringFunc(out, func(token string) string {
		// match 保存当前卡密变量的结构化匹配结果。
		match := cardVariablePattern.FindStringSubmatch(token)
		// value、ok 分别保存卡密正文和变量是否存在绑定值。
		if value, ok := values.CardValues[match[1]]; ok {
			return value
		}
		return token
	})
	return customVariablePattern.ReplaceAllStringFunc(out, func(token string) string {
		// match 保存当前自定义变量的结构化匹配结果。
		match := customVariablePattern.FindStringSubmatch(token)
		// value、ok 分别保存自定义字符串和变量是否存在绑定值。
		if value, ok := values.CustomValues[match[1]]; ok {
			return value
		}
		return token
	})
}

// ReplaceCards 兼容旧调用方，只替换已经提供绑定值的卡密变量。
func ReplaceCards(message string, values map[string]string) string {
	return Replace(message, VariableValues{CardValues: values})
}
