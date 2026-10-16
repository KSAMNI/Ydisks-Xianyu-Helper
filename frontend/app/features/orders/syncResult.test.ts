import { expect, test } from 'vitest';
import type { OrderRefreshResponse,OrderSingleRefreshResponse } from './models';
import { formatOrderSyncResult,formatSingleSyncResult } from './syncResult';

/** successResult 提供零计数的完整 UI 模型，保证成功提示与原文逐字兼容。 */
const successResult: OrderRefreshResponse = {
  partial_failure: false, message: '发现 2 单，同步完成',
  summary: { restored: 0, reassigned: 0, discovered: 2, list_updated: 0, soft_deleted: 0, detail_total: 0, total: 0, updated: 0, no_change: 0, failed: 0 }, results: [],
};

test('普通成功保持原消息与空消息语义，恢复修正追加独立统计', /* 验证恢复计数展示不重写既有成功语义。 */ () => {
  expect(formatOrderSyncResult(successResult)).toBe(successResult.message);
  expect(formatOrderSyncResult({ ...successResult, message: '' })).toBe('');
  expect(formatOrderSyncResult({ ...successResult, summary: { ...successResult.summary, restored: 3, reassigned: 2 } })).toBe(`${successResult.message}\n恢复 3 单，修正 2 单`);
});

test('部分失败标记单独生效，逐项说明保留 error 和 message 且不混入成功项', /* 验证 failed 为零时仍按 partial_failure 判定未完成。 */ () => {
  // message 是仅依赖批次标记及失败明细生成的提示。
  const message = formatOrderSyncResult({ ...successResult, partial_failure: true, results: [
    { success: false, cookie_id: 'account-a', order_id: 'order-a', error: '归属错误', message: '核验卖家' },
    { success: false, message: '列表不可用' },
    { success: false, cookie_id: 'account-b', error: '会话失效', message: '会话失效' },
    { success: false },
    { success: true, message: '成功项不应展示' },
  ] });
  expect(message).toContain('未完成');
  expect(message).toContain('账号 account-a');
  expect(message).toContain('订单 order-a');
  expect(message).toContain('失败原因：归属错误');
  expect(message).toContain('归属错误');
  expect(message).toContain('核验卖家');
  expect(message).toContain('列表不可用');
  expect(message).toContain('未提供失败原因');
  expect(message.match(/会话失效/g)).toHaveLength(1);
  expect(message).not.toContain(successResult.message);
  expect(message).not.toContain('成功项不应展示');
});

/** singleResult 提供单笔刷新 UI 模型，用于验证限流等待与强制获取文案。 */
const singleResult = (patch: Partial<OrderSingleRefreshResponse>): OrderSingleRefreshResponse => ({
  success: true, message: '订单刷新完成', throttle_wait_ms: 0, forced: false,
  order: { quantity: '1', spec_name: '', spec_value: '', order_status: 'pending_ship', amount: '10.00' },
  ...patch,
});

test('单笔同步提示账号限流等待时长，未等待时保留服务端消息', /* 验证用户能看到限流等待而不是静默转圈。 */ () => {
  expect(formatSingleSyncResult(singleResult({ throttle_wait_ms: 4200 }))).toContain('4.2 秒');
  expect(formatSingleSyncResult(singleResult({}))).toBe('订单刷新完成');
});

test('强制获取提示与普通同步可区分，失败保留服务端原因', /* 验证强制获取的提示语义和失败分支。 */ () => {
  expect(formatSingleSyncResult(singleResult({ forced: true }), 'force')).toContain('已强制获取最新订单详情');
  expect(formatSingleSyncResult(singleResult({ success: false, message: '平台要求完成风控验证，请稍后重试' }))).toBe('平台要求完成风控验证，请稍后重试');
});
