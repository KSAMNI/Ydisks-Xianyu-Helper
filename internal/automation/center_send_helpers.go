package automation

import (
	"context"
	"fmt"
	"strings"

	"xianyu-go/internal/db"
)

// cardContent 获取卡密组内容的兼容入口。
func (c *Center) cardContent(ctx context.Context, card *db.CardFull) (text, imageURL string, err error) {
	return c.actions.cardContent(ctx, card)
}

// sendImage 将图片消息发送委托给发货动作执行器。
func (c *Center) sendImage(ctx context.Context, task Task, imageURL string, cardID int64) error {
	return c.actions.sendImage(ctx, task, imageURL, cardID)
}

// cookieValue 读取账号 Cookie 的兼容入口。
func (c *Center) cookieValue(ctx context.Context, cookieID string) (string, error) {
	return c.actions.cookieValue(ctx, cookieID)
}

// buildTriggerKey 封装buildTriggerKey业务协调。
func buildTriggerKey(task Task) string {
	if task.Raw != nil {
		// marker 是未知角色事件在订单号补齐前生成的稳定防重键；恢复时必须优先沿用它。
		if marker, ok := task.Raw[roleVerificationTaskKeyField].(string); ok && strings.TrimSpace(marker) != "" {
			return strings.TrimSpace(marker)
		}
	}
	if task.TriggerType == TriggerReviewMissingTimeout && task.OrderID != "" {
		if // attempt、ok 用于本次流程后续判断的attempt、ok
		attempt, ok := task.Raw["attempt"]; ok {
			return fmt.Sprintf("%s:%s:%v", task.TriggerType, task.OrderID, attempt)
		}
	}
	if task.OrderID != "" {
		return task.TriggerType + ":" + task.OrderID
	}
	if task.UpdateKey != "" {
		return task.TriggerType + ":" + task.UpdateKey
	}
	return ""
}
