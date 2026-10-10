// @vitest-environment jsdom
import { fireEvent, render, screen, cleanup } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { AutomationIssuePanel } from './AutomationIssuePanel';
import type { AutomationRunIssue } from '../api';

/** 清理前一场景的 DOM，避免多个同名操作相互影响。 */
afterEach(() => cleanup());
/** run 是允许整单停用但禁止安全重试的求评价异常。 */
const run: AutomationRunIssue = { id: 1, cookie_id: 'a', order_id: 'order-1', trigger_type: 'review_missing_timeout', error_message: '平台结果未知', issue_kind: 'external_result_unknown', allowed_resolutions: ['cancel'], action_cursor: 1, sent_count: 1, updated_at: '2026-10-10', can_stop_order: true, item_title: '测试商品', buyer_id: 'buyer' };

test('面板明确标注两类任务并传递实际订单给停用确认', /* 当前场景验证用户实际看到的信息与操作载荷。 */ () => {
  /** onRun、onTask 捕获用户决策，不调用后端。 */
  const onRun = vi.fn(), onTask = vi.fn();
  render(<AutomationIssuePanel runs={[run]} pendingTasks={[{ id: 2, cookie_id: 'a', trigger_type: 'order_paid', error_message: '缺少订单', attempt_count: 5, updated_at: '' }]} onResolveRun={onRun} onResolveDeferredTask={onTask} />);
  expect(screen.getByText(/任务：求评价/)).toBeTruthy(); expect(screen.getByText(/任务：付款发货/)).toBeTruthy();
  expect(screen.getByText('order-1')).toBeTruthy(); expect(screen.getByText('尚未关联订单')).toBeTruthy();
  expect(screen.getByText(/已确认完成 1 个动作/)).toBeTruthy();
  expect(screen.queryByRole('button', { name: '未执行，安全重试' })).toBeNull();
  expect(screen.getAllByRole('button', { name: '停止此订单全部自动化' })).toHaveLength(1);
  expect(screen.getByRole('link', { name: '查看闲鱼订单' }).getAttribute('href')).toContain('orderId=order-1');
  fireEvent.click(screen.getByRole('button', { name: '停止此订单全部自动化' }));
  expect(onRun).toHaveBeenCalledWith(1, 'stop_order', run);
  fireEvent.click(screen.getByRole('button', { name: '终止本次任务' })); expect(onRun).toHaveBeenLastCalledWith(1, 'cancel', run);
});

test('请求期间停用与终止按钮禁用', /* 当前场景避免用户在等待结果时重复提交。 */ () => {
  /** onRun 捕获本不应该发生的重复提交。 */
  const onRun = vi.fn();
  render(<AutomationIssuePanel runs={[run]} pendingTasks={[]} busy onResolveRun={onRun} onResolveDeferredTask={vi.fn()} />);
  fireEvent.click(screen.getByRole('button', { name: '停止此订单全部自动化' }));
  expect(onRun).not.toHaveBeenCalled();
});
