// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { beforeEach, afterEach, expect, test, vi } from 'vitest';
import { resolveAutomationRun, resolveDeferredAutomationTask, type AutomationRunIssue } from './api';
import { copyAutomationOrderID, useIssueActions } from './issueActions';
import { automationTaskLabel, automationOrderStatusLabel, issueResolutionPrompt, canResolveAutomationIssue } from './issueState';

/** API 替身仅记录人工决策，不访问实际订单。 */
vi.mock('./api', () => ({ resolveAutomationRun: vi.fn(), resolveDeferredAutomationTask: vi.fn() }));
/** issue 是具有可靠订单身份的求评价异常，不包含敏感载荷。 */
const issue: AutomationRunIssue = { id: 7, cookie_id: 'a', account_name: '测试账号', order_id: 'order-7', trigger_type: 'review_missing_timeout', error_message: '未知结果', issue_kind: 'external_result_unknown', allowed_resolutions: ['cancel'], action_cursor: 0, sent_count: 0, updated_at: '', can_stop_order: true };
/** 每个场景重置接口和交互，避免确认结果串扰。 */
beforeEach(() => {
  vi.resetAllMocks();
  vi.spyOn(window, 'confirm').mockReturnValue(true);
  vi.spyOn(window, 'alert').mockImplementation(/* alert 替身避免 jsdom 弹出真实窗口。 */ () => {});
  vi.mocked(resolveAutomationRun).mockResolvedValue({ success: true });
  vi.mocked(resolveDeferredAutomationTask).mockResolvedValue({ success: true });
});
/** 清理浏览器全局替身，不影响其他 feature 测试。 */
afterEach(() => { vi.restoreAllMocks(); });

/** pending 提供可手动完成或拒绝的本地请求，验证异步响应隔离。 */
const pending = () => {
  /** resolve、reject 由 Promise 构造器设置，分别模拟网络成功或失败。 */
  let resolve!: (value: { success: boolean }) => void;
  /** reject 模拟 API 失败，不泄漏外部响应内容。 */
  let reject!: (error: Error) => void;
  /** promise 在测试主动结束前保持请求在途。 */
  const promise = new Promise<{ /** success 是本地请求替身的成功标记。 */ success: boolean }>(/* done、fail 保存当前请求的完成控制权。 */ (done, fail) => { resolve = done; reject = fail; });
  return { promise, resolve, reject };
};

test('任务名称明确区分求评价与付款发货，未知类型不误标', /* 当前场景验证标签和订单状态映射。 */ () => {
  expect(['order_paid', 'order_created', 'buyer_reviewed', 'review_missing_timeout', 'bargain_pending', 'order_completed', 'auto_rate'].map(/* trigger 是待展示的业务类型。 */ trigger => automationTaskLabel(trigger))).toEqual(['付款发货', '拍下改价', '评价赠品', '求评价', '砍价自动免拼', '确认收货', '自动评价买家']);
  expect(automationTaskLabel('future')).toContain('future'); expect(automationTaskLabel('')).toBe('未知任务');
  expect(automationOrderStatusLabel('pending_ship')).toBe('待发货'); expect(automationOrderStatusLabel()).toBe('尚未获取'); expect(automationOrderStatusLabel('future')).toBe('future');
  expect(canResolveAutomationIssue(issue, 'stop_order')).toBe(true);
  expect(canResolveAutomationIssue({ ...issue, order_id: '' }, 'stop_order')).toBe(false);
  expect(canResolveAutomationIssue({ ...issue, can_stop_order: false }, 'stop_order')).toBe(false);
  expect(issueResolutionPrompt('stop_order', issue)).toContain('求评价'); expect(issueResolutionPrompt('stop_order', issue)).toContain('order-7');
  expect(issueResolutionPrompt('stop_order', issue)).toContain('重启后仍有效'); expect(issueResolutionPrompt('cancel', issue)).toContain('不会停止该订单其他');
  expect(issueResolutionPrompt('continue')).toContain('下一步'); expect(issueResolutionPrompt('retry')).toContain('重复发送'); expect(issueResolutionPrompt('dismiss')).toContain('延期异常');
});

test('确认停用后按异常主键提交并刷新，缺订单不能提交整单停用', /* 当前场景同时覆盖运行和延期入口。 */ async () => {
  /** reload 记录成功后的当前列表刷新。 */
  const reload = vi.fn().mockResolvedValue(undefined);
  /** hook 持有当前账号的决策处理器。 */
  const hook = renderHook(/* 当前回调实例化真实 Hook。 */ () => useIssueActions('a', reload));
  await act(/* 当前回调提交合法停用。 */ async () => hook.result.current.handleResolveRunIssue(7, 'stop_order', issue));
  expect(resolveAutomationRun).toHaveBeenCalledWith(7, 'stop_order'); expect(reload).toHaveBeenCalledTimes(1);
  await act(/* 当前回调尝试缺少订单身份的危险操作。 */ async () => hook.result.current.handleResolveRunIssue(8, 'stop_order'));
  await act(/* 当前回调拒绝没有后端授权的延期整单停用。 */ async () => hook.result.current.handleResolveDeferredIssue(9, 'stop_order'));
  expect(resolveAutomationRun).toHaveBeenCalledTimes(1); expect(resolveDeferredAutomationTask).not.toHaveBeenCalled();
  await act(/* 当前回调处理可靠关联的延期订单。 */ async () => hook.result.current.handleResolveDeferredIssue(9, 'stop_order', { ...issue, attempt_count: 5 }));
  expect(resolveDeferredAutomationTask).toHaveBeenCalledWith(9, 'stop_order'); expect(reload).toHaveBeenCalledTimes(2);
  expect(hook.result.current.issueActionPending).toBe(false);
});

test('用户取消不提交，失败可重试且请求期间禁止双击', /* 当前场景验证单页串行化与失败释放。 */ async () => {
  /** reload 是成功刷新替身；request 控制一次失败返回时机。 */
  const reload = vi.fn().mockResolvedValue(undefined), request = pending();
  /** hook 拥有当前账号的提交状态。 */
  const hook = renderHook(/* 当前回调构造测试 Hook。 */ () => useIssueActions('a', reload));
  vi.mocked(window.confirm).mockReturnValueOnce(false);
  await act(/* 当前回调取消二次确认。 */ async () => hook.result.current.handleResolveRunIssue(7, 'cancel', issue));
  expect(resolveAutomationRun).not.toHaveBeenCalled();
  vi.mocked(resolveAutomationRun).mockReturnValueOnce(request.promise);
  /** first 保存首个在途操作，由测试显式等待其结束。 */
  let first!: Promise<void>;
  act(/* 当前回调开始但暂不完成首个请求。 */ () => { first = hook.result.current.handleResolveRunIssue(7, 'cancel', issue); });
  expect(hook.result.current.issueActionPending).toBe(true);
  await act(/* 当前回调模拟同页重复点击。 */ async () => hook.result.current.handleResolveRunIssue(7, 'cancel', issue));
  expect(resolveAutomationRun).toHaveBeenCalledTimes(1);
  await act(/* 当前回调结束失败请求并等待 finally 释放。 */ async () => { request.reject(new Error('服务暂时不可用')); await first; });
  expect(window.alert).toHaveBeenCalledWith('处理失败：服务暂时不可用'); expect(reload).not.toHaveBeenCalled(); expect(hook.result.current.issueActionPending).toBe(false);
});

test('切换账号后旧响应不刷新或清除新请求，卸载后失败不提示', /* 当前场景验证账号切换与卸载两种代次失效。 */ async () => {
  /** oldRequest、newRequest 分别控制旧账号和新账号的返回顺序。 */
  const oldRequest = pending(), newRequest = pending();
  /** reload 只应该被仍有效的最新请求调用。 */
  const reload = vi.fn().mockResolvedValue(undefined);
  vi.mocked(resolveAutomationRun).mockReturnValueOnce(oldRequest.promise).mockReturnValueOnce(newRequest.promise);
  /** hook 的 props 模拟用户切换账号。 */
  const hook = renderHook(/* props 的账号标识限定当前页面。 */ ({ account }) => useIssueActions(account, reload), { initialProps: { account: 'a' } });
  /** oldWork、newWork 保存两次请求的完成等待句柄。 */
  let oldWork!: Promise<void>;
  /** newWork 是切换后最新操作，不能被旧 finally 释放。 */
  let newWork!: Promise<void>;
  act(/* 当前回调发出旧账号请求。 */ () => { oldWork = hook.result.current.handleResolveRunIssue(7, 'cancel'); });
  hook.rerender({ account: 'b' });
  act(/* 当前回调发出新账号请求。 */ () => { newWork = hook.result.current.handleResolveRunIssue(8, 'cancel'); });
  await act(/* 当前回调让旧结果先到达。 */ async () => { oldRequest.resolve({ success: true }); await oldWork; });
  expect(reload).not.toHaveBeenCalled(); expect(hook.result.current.issueActionPending).toBe(true);
  hook.unmount();
  await act(/* 当前回调模拟卸载后请求失败。 */ async () => { newRequest.reject(new Error('晚到失败')); await newWork; });
  expect(window.alert).not.toHaveBeenCalled(); expect(reload).not.toHaveBeenCalled();
});

test('复制订单成功或权限失败均有明确行为', /* 当前场景不要求系统剪贴板权限。 */ async () => {
  /** writeText 是隔离剪贴板写入能力的替身。 */
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
  await copyAutomationOrderID('order-7'); expect(writeText).toHaveBeenCalledWith('order-7');
  writeText.mockRejectedValueOnce(new Error('permission denied'));
  await copyAutomationOrderID('order-7'); expect(window.alert).toHaveBeenCalledWith(expect.stringContaining('手动选择'));
});
