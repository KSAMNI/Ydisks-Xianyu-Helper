package automation

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

// reviewRequestRuleDue 封装review请求规则Due业务协调。
func reviewRequestRuleDue(order db.Order, rule db.AutomationRule) bool {
	// cfg 用于本次流程后续判断的cfg
	cfg := parseReviewRuleConfig(rule.ConfigJSON)
	if cfg.MaxAttempts > 0 && order.ReviewRequestCount >= cfg.MaxAttempts {
		return false
	}
	// baseRaw 用于本次流程后续判断的base原始
	baseRaw := firstNonEmpty(order.ShippedAt, order.UpdatedAt, order.CreatedAt)
	// shippedAt 是订单发货时间基点，回溯窗口始终以它为准，与是否已求评过无关。
	shippedAt := parseDBTime(baseRaw)
	// 规则停用期间积压的旧订单在重新启用时不得被补发：发货已超出回溯窗口的订单一律跳过。
	if !shippedAt.IsZero() && time.Since(shippedAt) > time.Duration(cfg.effectiveLookbackHours())*time.Hour {
		return false
	}
	// waitHours 用于本次流程后续判断的waitHours
	waitHours := cfg.AfterShippedHours
	if order.ReviewRequestCount > 0 && strings.TrimSpace(order.LastReviewRequestAt) != "" {
		baseRaw = order.LastReviewRequestAt
		waitHours = cfg.RepeatIntervalHours
	}
	// base 用于本次流程后续判断的base
	base := parseDBTime(baseRaw)
	if base.IsZero() {
		return false
	}
	return time.Since(base) >= time.Duration(waitHours)*time.Hour
}

// reviewRuleConfig 用于本次流程后续判断的review规则配置
type reviewRuleConfig struct {
	AfterShippedHours   int
	RepeatIntervalHours int
	MaxAttempts         int
	// MaxLookbackHours 是回溯窗口小时数：发货时间早于该窗口的订单不再求评价；0 表示使用默认值。
	MaxLookbackHours int
}

// effectiveLookbackHours 返回实际生效的回溯窗口小时数。
//
// 未显式配置时取「走完全部求评流程所需时长 + 24 小时」，让正常流程不受影响，
// 同时过滤规则停用期间积压的历史订单；显式配置小于首次等待时长时上调到首次等待时长，
// 避免规则永远无法触发。
func (c reviewRuleConfig) effectiveLookbackHours() int {
	// minHours 是正常走完全部求评次数所需的最短时长。
	minHours := c.AfterShippedHours + (c.MaxAttempts-1)*c.RepeatIntervalHours
	if c.MaxLookbackHours <= 0 {
		return minHours + 24
	}
	if c.MaxLookbackHours < c.AfterShippedHours {
		return c.AfterShippedHours
	}
	return c.MaxLookbackHours
}

// parseReviewRuleConfig 封装parseReview规则配置业务协调。
func parseReviewRuleConfig(raw string) reviewRuleConfig {
	// cfg 用于本次流程后续判断的cfg
	cfg := reviewRuleConfig{AfterShippedHours: 72, RepeatIntervalHours: 24, MaxAttempts: 1}
	if strings.TrimSpace(raw) == "" {
		return cfg
	}
	// m 用于本次流程后续判断的m
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return cfg
	}
	if // v 用于本次流程后续判断的v
	v := intFromAny(m["after_shipped_hours"]); v > 0 {
		cfg.AfterShippedHours = v
	}
	if // v 用于本次流程后续判断的v
	v := intFromAny(m["first_delay_hours"]); v > 0 {
		cfg.AfterShippedHours = v
	}
	if // v 用于本次流程后续判断的v
	v := intFromAny(m["repeat_interval_hours"]); v > 0 {
		cfg.RepeatIntervalHours = v
	}
	if // v 用于本次流程后续判断的v
	v := intFromAny(m["max_attempts"]); v > 0 {
		cfg.MaxAttempts = v
	}
	if // v 是显式配置的回溯窗口小时数，非正值保持默认
	v := intFromAny(m["max_lookback_hours"]); v > 0 {
		cfg.MaxLookbackHours = v
	}
	return cfg
}

// intFromAny 封装intFromAny业务协调。
func intFromAny(v any) int {
	switch // x 用于本次流程后续判断的x
	x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		// n 用于本次流程后续判断的n
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	default:
		return 0
	}
}

// parseDBTime 封装parseDB时间业务协调。
func parseDBTime(s string) time.Time {
	// layout 表示当前遍历过程中的layout
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999Z07:00", // Postgres TEXT(CURRENT_TIMESTAMP)
		"2006-01-02 15:04:05.999999999Z07",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05Z07",
		"2006-01-02 15:04:05", // SQLite/MySQL 历史值；按既有 UTC 约定解释
	} {
		if // t、err 用于本次流程后续判断的t、err
		t, err := time.ParseInLocation(layout, strings.TrimSpace(s), time.UTC); err == nil {
			return t
		}
	}
	return time.Time{}
}
