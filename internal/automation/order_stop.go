package automation

import (
	"context"
	"errors"
	"xianyu-go/internal/db"
)

// checkOrderAutomation 由执行器 e 在 task 的每次外部动作前查询持久停用；ctx 由当前运行拥有，查询失败同样禁止发送。
// 不持有跨网络锁；检查前已经进入外部平台的请求无法撤回，用户确认文案必须明确这一边界。
func (e *automationActionExecutor) checkOrderAutomation(ctx context.Context, task Task) error {
	if task.OrderID == "" {
		return nil
	}
	if e.store == nil || e.store.Automation == nil {
		return errors.New("订单自动化存储未初始化")
	}
	return e.store.Automation.CheckOrderAutomation(ctx, task.AccountID, task.OrderID)
}

// CheckOrderAutomation 将账号任务的订单停用检查委托到自动化仓储，不读取账号凭证。
func (r storeAccountTaskRepository) CheckOrderAutomation(ctx context.Context, accountID, orderID string) error {
	return r.store.Automation.CheckOrderAutomation(ctx, accountID, orderID)
}

// orderAutomationStopped 判断账号任务是否遇到明确的人工停用，其余存储错误不得吞掉。
func orderAutomationStopped(err error) bool { return errors.Is(err, db.ErrOrderAutomationStopped) }

// HandleTask 处理一条自动化任务。无匹配规则时安全忽略。
func (c *Center) HandleTask(ctx context.Context, task Task) error {
	// err 用于本次流程后续判断的err
	_, err := c.handleTask(ctx, task)
	return err
}
