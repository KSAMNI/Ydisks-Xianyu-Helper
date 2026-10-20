// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import React from 'react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { getSalesStats } from './api';
import { useSalesStats } from './hooks';

vi.mock('./api', () => ({
  getSalesStats: vi.fn(),
}));

// statsMock 是销售统计请求的可控测试替身。
const statsMock = vi.mocked(getSalesStats);
// SalesRangeProps 是范围切换测试使用的 Hook 输入参数。
type SalesRangeProps = {
  /** range 是当前预设时间范围，测试在 7 天与 30 天之间切换。 */
  range: '7days' | '30days' | 'custom';
  /** customStartDate 是自定义起始日期，本用例不使用。 */
  customStartDate: string;
  /** customEndDate 是自定义结束日期，本用例不使用。 */
  customEndDate: string;
};
// sampleStats 是服务端返回形状的销售统计样例数据。
const sampleStats = {
  salesAmount: 28.5, salesOrders: 4,
  receivedAmount: 16, receivedOrders: 2,
  refundedAmount: 5.25, refundedOrders: 1,
  daily: [
    { date: '2026-10-10', salesAmount: 18.5, receivedAmount: 8, refundedAmount: 5.25, orderCount: 3 },
    { date: '2026-10-11', salesAmount: 10, receivedAmount: 8, refundedAmount: 0, orderCount: 1 },
  ],
};

// StrictModeWrapperProps 描述 StrictMode 测试包装器接收的 React 子节点。
interface StrictModeWrapperProps {
  /** children 保存测试 Hook 在 StrictMode 包装器中的 React 子节点。 */
  children: React.ReactNode;
}

// strictModeWrapper 用于复现应用入口的 StrictMode 开发期 effect 重挂载行为。
const strictModeWrapper = (props: StrictModeWrapperProps): React.ReactElement => {
  // children 保存测试 Hook 在 StrictMode 包装器中的 React 子节点。
  return React.createElement(React.StrictMode, null, props.children);
};

// waitInitialLoad 等待销售统计 Hook 的初始 effect 完成一次请求并刷新状态。
const waitInitialLoad = async (): Promise<void> => {
  await act(/* loadAction 等待初始销售统计请求完成。 */ async () => {});
};

describe('useSalesStats', /* 当前测试组验证销售统计 Hook 的成功、失败、刷新和过期响应隔离。 */ () => {
  beforeEach(/* 当前回调重置销售统计 API 替身状态。 */ () => {
    vi.clearAllMocks();
    statsMock.mockResolvedValue(sampleStats);
  });

  test('加载成功后返回销售统计数据', /* 当前回调验证默认 7 天范围的加载成功路径。 */ async () => {
    // hook 保存销售统计 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建销售统计 Hook 测试实例。 */ () => useSalesStats({ range: '7days', customStartDate: '', customEndDate: '' }));
    await waitInitialLoad();
    expect(statsMock).toHaveBeenCalledTimes(1);
    expect(hook.result.current.loading).toBe(false);
    expect(hook.result.current.error).toBe('');
    expect(hook.result.current.data?.salesAmount).toBe(28.5);
    expect(hook.result.current.data?.daily.length).toBe(2);
    hook.unmount();
  });

  test('加载失败时写入错误并清空数据', /* 当前回调验证错误路径的用户可见语义。 */ async () => {
    statsMock.mockRejectedValueOnce(new Error('统计失败'));
    // hook 保存销售统计 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建销售统计 Hook 测试实例。 */ () => useSalesStats({ range: 'today', customStartDate: '', customEndDate: '' }));
    await waitInitialLoad();
    expect(hook.result.current.loading).toBe(false);
    expect(hook.result.current.error).toContain('统计失败');
    expect(hook.result.current.data).toBeNull();
    hook.unmount();
  });

  test('刷新函数重新发起请求', /* 当前回调验证手动刷新重新加载最新统计。 */ async () => {
    // hook 保存销售统计 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建销售统计 Hook 测试实例。 */ () => useSalesStats({ range: '7days', customStartDate: '', customEndDate: '' }));
    await waitInitialLoad();
    // 手动刷新会再次触发同一范围的请求。
    await act(/* refreshAction 等待手动刷新请求完成。 */ async () => { await hook.result.current.refresh(); });
    expect(statsMock).toHaveBeenCalledTimes(2);
    hook.unmount();
  });

  test('切换范围后旧响应不覆盖新数据', /* 当前回调验证过期响应隔离与取消保护。 */ async () => {
    // firstPending 控制第一轮请求的完成时机。
    let firstPending: ((value: typeof sampleStats) => void) | undefined;
    statsMock.mockImplementationOnce(
      // pendingFactory 挂起第一轮请求，直到测试显式放行。
      () => new Promise(/* resolve 在测试放行时兑现第一轮挂起结果。 */ resolve => { firstPending = resolve; }),
    );
    // hook 保存销售统计 Hook 的当前测试状态；回调透传 props 以支持 rerender 切换范围。
    const hook = renderHook(
      // hookFactory 按传入的范围参数创建销售统计 Hook 测试实例。
      (props: SalesRangeProps) => useSalesStats(props),
      { initialProps: { range: '7days', customStartDate: '', customEndDate: '' } as SalesRangeProps },
    );
    await waitInitialLoad();
    // 切换到 30days 触发新一轮请求并中止旧请求。
    await act(/* rangeSwitchAction 等待范围切换后的新请求完成。 */ async () => { await hook.rerender({ range: '30days', customStartDate: '', customEndDate: '' }); });
    // 晚到的第一轮响应不得写入状态。
    await act(/* lateResponseAction 放行第一轮挂起响应并等待状态刷新。 */ async () => { firstPending?.(sampleStats); });
    expect(hook.result.current.data?.salesAmount).toBe(28.5);
    expect(statsMock).toHaveBeenCalledTimes(2);
    hook.unmount();
  });

  test('自定义日期倒置时写入日期范围错误', /* 当前回调验证非法自定义范围不会发起请求。 */ async () => {
    // hook 保存销售统计 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建销售统计 Hook 测试实例。 */ () => useSalesStats({ range: 'custom', customStartDate: '2026-10-11', customEndDate: '2026-10-10' }));
    await waitInitialLoad();
    expect(statsMock).not.toHaveBeenCalled();
    expect(hook.result.current.error).toContain('开始日期不能晚于结束日期');
    expect(hook.result.current.data).toBeNull();
    hook.unmount();
  });

  test('StrictMode 重挂载后仍能正常加载', /* 当前回调验证模拟卸载不会永久阻断最新统计响应。 */ async () => {
    // hook 保存 StrictMode 下销售统计 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建 StrictMode 销售统计 Hook 测试实例。 */ () => useSalesStats({ range: 'yesterday', customStartDate: '', customEndDate: '' }), { wrapper: strictModeWrapper });
    await waitInitialLoad();
    expect(hook.result.current.loading).toBe(false);
    expect(hook.result.current.data?.salesOrders).toBe(4);
    hook.unmount();
  });

  test('自定义日期未填齐时不发起请求', /* 当前回调验证不会静默回退为 7 天范围。 */ async () => {
    // hook 保存销售统计 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建销售统计 Hook 测试实例。 */ () => useSalesStats({ range: 'custom', customStartDate: '2026-10-10', customEndDate: '' }));
    await waitInitialLoad();
    expect(statsMock).not.toHaveBeenCalled();
    expect(hook.result.current.loading).toBe(false);
    expect(hook.result.current.error).toBe('');
    hook.unmount();
  });

  test('倒置日期被修正后恢复加载', /* 当前回调验证错误状态可通过修改输入恢复。 */ async () => {
    // hook 保存销售统计 Hook 的当前测试状态；回调透传 props 以支持 rerender 修正日期。
    const hook = renderHook(
      /* hookFactory 按传入参数创建销售统计 Hook 测试实例。 */ (props: SalesRangeProps) => useSalesStats(props),
      { initialProps: { range: 'custom', customStartDate: '2026-10-11', customEndDate: '2026-10-10' } as SalesRangeProps },
    );
    await waitInitialLoad();
    expect(hook.result.current.error).toContain('开始日期不能晚于结束日期');
    await act(/* fixAction 修正日期后等待新请求完成。 */ async () => { hook.rerender({ range: 'custom', customStartDate: '2026-10-09', customEndDate: '2026-10-10' }); });
    expect(hook.result.current.error).toBe('');
    expect(hook.result.current.data?.salesOrders).toBe(4);
    hook.unmount();
  });
});
