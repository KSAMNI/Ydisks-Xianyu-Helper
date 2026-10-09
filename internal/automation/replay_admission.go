package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"xianyu-go/internal/db"
)

// errReplayCanceled 表示当前订单已经终态且旧运行已尝试按快照取消，协调器不得再用成功或失败覆盖取消状态。
var errReplayCanceled = errors.New("订单已不再待发货，停止历史运行")

// errReplayNeedsReview 区分准入拒绝与既有动作未知结果；队列仅对准入拒绝立即持久化死信。
var errReplayNeedsReview = fmt.Errorf("%w: 历史任务准入拒绝", errAutomationNeedsReview)

// paidReplayOrder 从 store 读取 task 的最新非凭证订单事实；ctx 控制查询，reason 非空表示必须人工核对，查询失败返回错误。
// 此检查不写订单，不允许旧任务重新创建已缺失事实，也不会把人工来源升级为调度来源。
func paidReplayOrder(ctx context.Context, store *db.Store, task Task) (order *db.Order, reason string, err error) {
	if task.Source != "ws" && task.Source != "scheduler" {
		return nil, "付款运行缺少可信的 WebSocket 或待发货兜底来源，已停止自动恢复", nil
	}
	if task.OrderRole == OrderRoleBuyer {
		return nil, "付款恢复快照为买家角色，已停止自动恢复", nil
	}
	order, err = store.Orders.Get(ctx, task.OrderID)
	if errors.Is(err, db.ErrNotFound) {
		return nil, "本地缺少订单状态，无法确认订单仍为待发货，已停止自动恢复", nil
	}
	if err != nil {
		return nil, "", err
	}
	if order.OrderStatus == "" || db.NormalizeOrderStatus(order.OrderStatus) == "unknown" {
		return nil, "订单当前阶段缺失，无法安全自动恢复", nil
	}
	if order.CookieID != task.AccountID {
		return nil, "订单归属账号与付款运行不一致，已停止自动恢复", nil
	}
	if task.ItemID != "" && !sameOrderIdentity(task.ItemID, order.ItemID) ||
		task.BuyerID != "" && !sameOrderIdentity(task.BuyerID, order.BuyerID) ||
		task.ChatID != "" && !sameOrderIdentity(task.ChatID, order.ChatID) {
		return nil, "付款恢复快照的商品、买家或会话与当前订单不一致", nil
	}
	return order, "", nil
}

// guardDeferredReplay 在 c 写回任何旧事件事实前校验 task；ctx 由队列拥有，返回是否停止、是否重新延期和处理错误。
// 已领取运行使用完整快照 CAS 收口；没有运行的未知角色任务先由原角色门禁完成本地事实回填。
func (c *Center) guardDeferredReplay(ctx context.Context, task Task) (bool, bool, error) {
	if !isDeferredReplay(task) {
		return false, false, nil
	}
	// run 仅在队列关联既有动作运行时存在，不能借用其他账号或订单的执行权。
	var run *db.AutomationRun
	if runID := taskAutomationRunID(task); runID > 0 { // runID 是延期快照引用的运行主键。
		// readErr 表示运行快照无法读取；不存在的运行不能按新事件重新发送。
		var readErr error
		run, readErr = c.store.Automation.GetRun(ctx, runID)
		if errors.Is(readErr, db.ErrNotFound) {
			return true, false, fmt.Errorf("%w: 延期运行不存在", errReplayNeedsReview)
		}
		if readErr != nil {
			return true, false, readErr
		}
		if run.CookieID != task.AccountID || run.OrderID != task.OrderID || run.TriggerType != task.TriggerType {
			return true, false, fmt.Errorf("%w: 延期快照与运行身份不一致", errReplayNeedsReview)
		}
		if task.TriggerType == TriggerOrderPaid {
			// original 校验运行最初来源与队列快照一致，不能通过改写 Source 借用合法运行的执行权。
			var original Task
			// decodeErr 将损坏的付款快照与身份变更一律归入人工处理，不自动创建替代事件。
			if decodeErr := json.Unmarshal([]byte(run.RawEventJSON), &original); decodeErr != nil || original.Source != task.Source || original.AccountID != task.AccountID || original.OrderID != task.OrderID || original.TriggerType != task.TriggerType || original.OrderRole == OrderRoleBuyer {
				return true, false, c.rejectIdleReplay(ctx, run, "延期付款快照与原始运行来源或身份不一致")
			}
		}
		if run.Status != "running" {
			return true, false, nil
		}
		if run.ActionStarted {
			return true, false, fmt.Errorf("延期运行已有在途动作，等待当前执行者收口")
		}
		// rule、ruleErr 保存当前规则启用事实，不以冻结计划代替持续授权。
		rule, ruleErr := c.store.Automation.Get(ctx, run.RuleID)
		if ruleErr != nil && !errors.Is(ruleErr, db.ErrNotFound) {
			return true, false, ruleErr
		}
		if rule == nil || !rule.Enabled {
			return true, false, c.rejectIdleReplay(ctx, run, "自动化规则不存在或已停用，停止延期运行")
		}
	}
	if task.TriggerType != TriggerOrderPaid {
		return false, false, nil
	}
	// order、reason、readErr 区分最新订单、不可恢复事实和暂时数据库故障。
	order, reason, readErr := paidReplayOrder(ctx, c.store, task)
	if readErr != nil {
		return true, false, readErr
	}
	if reason != "" {
		return true, false, c.rejectIdleReplay(ctx, run, reason)
	}
	if !isPendingShipOrder(order) || order.SystemShipped {
		if run != nil {
			// cancelErr 保留终态运行取消失败，调用方不得假报任务已收口。
			_, cancelErr := c.store.Automation.CancelObsoletePaidRecoveryRun(ctx, *run, errReplayCanceled.Error())
			return true, false, cancelErr
		}
		return true, false, nil
	}
	// enabled、settingsErr 读取账号自动发货总开关；关闭期间保留延期任务而不消耗重试次数。
	enabled, settingsErr := c.paidDeliveryAutoConfirmEnabled(ctx, task.AccountID)
	if settingsErr != nil {
		return true, false, settingsErr
	}
	if !enabled {
		// dueAt 为人工重新开启设置预留下一次检查，不执行任何平台动作。
		dueAt := time.Now().Add(10 * time.Minute).Unix()
		// deferErr 防止持久化失败被当作延期成功。
		if deferErr := c.deferTask(ctx, task, dueAt); deferErr != nil {
			return true, false, deferErr
		}
		return true, true, nil
	}
	return false, false, nil
}

// rejectIdleReplay 将 c 中 run 对应的空闲运行按快照隔离并通知；ctx 可取消，reason 为稳定拒绝描述。
// run 为空时由延期队列持久化死信及发送通知；返回人工处理哨兵以禁止普通重试。
func (c *Center) rejectIdleReplay(ctx context.Context, run *db.AutomationRun, reason string) error {
	if run == nil {
		return fmt.Errorf("%w: %s", errReplayNeedsReview, reason)
	}
	// finishCtx、cancel 在原始动作预算结束后仍给安全状态和通知入队提供有限时间。
	finishCtx, cancel := newAutomationRunCompensationContext(ctx)
	defer cancel()
	// changed、writeErr 只在精确空闲快照仍有效时改变状态，不接管其他 worker。
	changed, writeErr := c.store.Automation.QuarantineIdleRun(finishCtx, *run, reason)
	if changed {
		c.notifyRunNeedsReview(finishCtx, *run, reason)
	}
	return errors.Join(fmt.Errorf("%w: %s", errReplayNeedsReview, reason), writeErr)
}

// handleDeferredReplay 核对 s 领取的 pending 与 task 的账号和阶段身份，再交由中心处理；ctx 控制调用，返回是否再次延期及错误。
func (s *Scheduler) handleDeferredReplay(ctx context.Context, pending db.DeferredAutomationTask, task Task) (bool, error) {
	if pending.CookieID != task.AccountID || pending.TriggerType != task.TriggerType {
		return false, fmt.Errorf("%w: 延期任务与队列身份不一致", errReplayNeedsReview)
	}
	return s.center.handleTask(ctx, task)
}

// rejectPendingShipCandidate 只隔离 s 扫描到且仍未变化的 candidate 快照；ctx 控制读取，reason 是来源或计划校验失败描述。
func (s *Scheduler) rejectPendingShipCandidate(ctx context.Context, candidate db.PendingShipResume, reason string) error {
	// run、readErr 重新核对运行代次和快照，不能用旧扫描隔离已经被人工修复或续跑的运行。
	run, readErr := s.center.store.Automation.GetRun(ctx, candidate.RunID)
	if readErr != nil {
		return readErr
	}
	if run.AttemptCount != candidate.Attempt || run.RawEventJSON != candidate.RawEventJSON || run.Status != candidate.Status || run.ActionCursor != candidate.ActionCursor || run.ActionStarted {
		return nil
	}
	// changed、writeErr 保存精确快照隔离结果；成功的人工处理收口不是扫描故障。
	changed, writeErr := s.center.store.Automation.QuarantineIdleRun(ctx, *run, reason)
	if changed {
		s.center.notifyRunNeedsReview(ctx, *run, reason)
	}
	return writeErr
}

// guardReplayAction 在 r 领取下一动作前重新验证 rule 和付款重放事实；ctx 控制短数据库查询，task 是已补全任务，run 固定执行代次。
// 首次人工发货仍由人工入口授权；历史人工任务不能通过此方法获得自动重放资格。
func (r automationRunCoordinator) guardReplayAction(ctx context.Context, task Task, run *db.AutomationRun) error {
	// rule、ruleErr 检查当前规则状态，防止准备期间或两次动作之间被用户停用后继续发送。
	rule, ruleErr := r.store.Automation.Get(ctx, run.RuleID)
	if ruleErr != nil && !errors.Is(ruleErr, db.ErrNotFound) {
		return ruleErr
	}
	// reason 仅在必须隔离时记录稳定描述，不携带任务正文或凭证。
	reason := ""
	if rule == nil || !rule.Enabled {
		reason = "自动化规则不存在或已停用，停止剩余动作"
	}
	if reason == "" && task.TriggerType == TriggerOrderPaid && (isDeferredReplay(task) || taskAutomationRunID(task) > 0) {
		// order、readReason、readErr 在慢详情查询之后重新读取最新状态，旧快照不能恢复终态订单。
		order, readReason, readErr := paidReplayOrder(ctx, r.store, task)
		if readErr != nil {
			return readErr
		}
		reason = readReason
		if reason == "" && (!isPendingShipOrder(order) || order.SystemShipped) {
			// current、readErr 固定取消时的游标与租约；同代次前一动作可能已经推进检查点。
			current, readErr := r.store.Automation.GetRun(ctx, run.ID)
			if readErr != nil {
				return readErr
			}
			if current.AttemptCount != run.AttemptCount {
				return db.ErrAutomationRunLeaseLost
			}
			// cancelErr 表示取消终态旧运行的写入失败，不吞掉存储错误。
			_, cancelErr := r.store.Automation.CancelObsoletePaidRecoveryRun(ctx, *current, errReplayCanceled.Error())
			if cancelErr != nil {
				return errors.Join(errReplayNeedsReview, cancelErr)
			}
			return errReplayCanceled
		}
		if reason == "" {
			// enabled、settingsErr 确保用户在准备或前一个动作期间关闭自动发货也能阻止剩余动作。
			enabled, settingsErr := r.store.Cookies.GetAutoConfirm(ctx, task.AccountID)
			if settingsErr != nil {
				return settingsErr
			}
			if !enabled {
				return fmt.Errorf("%w: 自动发货已关闭，未执行剩余动作", errActionNotPerformed)
			}
		}
	}
	if reason == "" {
		return nil
	}
	// current、readErr 读取隔离时完整状态，避免沿用已过期的内存游标破坏新执行者。
	current, readErr := r.store.Automation.GetRun(ctx, run.ID)
	if readErr != nil {
		return readErr
	}
	if current.AttemptCount != run.AttemptCount {
		return db.ErrAutomationRunLeaseLost
	}
	// finishCtx、cancel 使取消预算后的安全收口仍然有界。
	finishCtx, cancel := newAutomationRunCompensationContext(ctx)
	defer cancel()
	// changed、writeErr 保存精确空闲快照隔离结果，副作用在任一失败时都不能继续。
	changed, writeErr := r.store.Automation.QuarantineIdleRun(finishCtx, *current, reason)
	if writeErr != nil {
		return errors.Join(errReplayNeedsReview, writeErr)
	}
	if !changed {
		return db.ErrAutomationRunLeaseLost
	}
	return fmt.Errorf("%w: %s", errReplayNeedsReview, reason)
}

// isDeferredReplay 判断 task 是否来自持久化延期队列；返回真时必须重新核对当前执行资格。
func isDeferredReplay(task Task) bool {
	return task.Raw != nil && task.Raw["automation_deferred_replay"] == true
}
