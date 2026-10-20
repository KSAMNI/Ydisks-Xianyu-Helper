// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import React from 'react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { getSalesStats } from '../api';
import SalesStats from './SalesStats';

vi.mock('../api', () => ({ getSalesStats: vi.fn() }));
vi.mock('recharts', /* rechartsFactory 用空组件替换图表，避免 jsdom 无法测量尺寸。 */ () => {
  // Empty 是渲染空内容的图表替身组件。
  const Empty = /* emptyComponent 不渲染任何内容。 */ () => null;
  return {
    Area: Empty, AreaChart: Empty, CartesianGrid: Empty, Tooltip: Empty, XAxis: Empty, YAxis: Empty,
    ResponsiveContainer: /* containerComponent 直接渲染子节点。 */ (props: { /** children 是图表子节点。 */ children: React.ReactNode }) => React.createElement('div', null, props.children),
  };
});

// statsMock 是销售统计请求的可控测试替身。
const statsMock = vi.mocked(getSalesStats);
// sampleStats 是服务端返回形状的销售统计样例数据。
const sampleStats = { salesAmount: 10, salesOrders: 1, receivedAmount: 5, receivedOrders: 1, refundedAmount: 0, refundedOrders: 0, daily: [] };

// flush 等待已解决的请求刷新页面状态。
const flush = async (): Promise<void> => { await act(/* flushAction 刷新异步状态。 */ async () => {}); };

describe('SalesStats 自定义日期', /* 当前测试组验证草稿、校验与应用的页面行为。 */ () => {
  beforeEach(/* 重置请求替身。 */ () => { vi.clearAllMocks(); statsMock.mockResolvedValue(sampleStats); });
  afterEach(/* 卸载页面。 */ () => cleanup());

  test('日期填反只提示错误，保留数据与输入框且不请求，修正后可应用', /* 当前回调覆盖倒置日期的页内修正。 */ async () => {
    render(<SalesStats />);
    await flush();
    expect(statsMock).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByText('自定义'));
    // dates 是起止日期输入框。
    const dates = document.querySelectorAll('input[type="date"]');
    fireEvent.change(dates[0], { target: { value: '2026-10-11' } });
    fireEvent.change(dates[1], { target: { value: '2026-10-10' } });
    expect(statsMock).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByText('应用'));
    await flush();
    expect(screen.getByRole('alert').textContent).toContain('开始日期不能晚于结束日期');
    expect(statsMock).toHaveBeenCalledTimes(1);
    expect(document.querySelectorAll('input[type="date"]').length).toBe(2);
    fireEvent.change(dates[0], { target: { value: '2026-10-09' } });
    fireEvent.click(screen.getByText('应用'));
    await flush();
    expect(screen.queryByRole('alert')).toBeNull();
    expect(statsMock).toHaveBeenCalledTimes(2);
    expect(statsMock.mock.calls[1][0]).toEqual({ start_date: '2026-10-09', end_date: '2026-10-10' });
  });

  test('日期未填齐点击应用提示缺失且不请求', /* 当前回调验证不会静默回退 7 天。 */ async () => {
    render(<SalesStats />);
    await flush();
    fireEvent.click(screen.getByText('自定义'));
    fireEvent.change(document.querySelectorAll('input[type="date"]')[0], { target: { value: '2026-10-09' } });
    fireEvent.click(screen.getByText('应用'));
    expect(screen.getByRole('alert').textContent).toContain('请选择开始日期和结束日期');
    expect(statsMock).toHaveBeenCalledTimes(1);
  });

  test('对同一自定义范围再次点击应用会重新请求', /* 当前回调验证范围未变时“应用”仍等价于刷新。 */ async () => {
    render(<SalesStats />);
    await flush();
    fireEvent.click(screen.getByText('自定义'));
    // dates 是起止日期输入框。
    const dates = document.querySelectorAll('input[type="date"]');
    fireEvent.change(dates[0], { target: { value: '2026-10-09' } });
    fireEvent.change(dates[1], { target: { value: '2026-10-10' } });
    fireEvent.click(screen.getByText('应用'));
    await flush();
    expect(statsMock).toHaveBeenCalledTimes(2);
    fireEvent.click(screen.getByText('应用'));
    await flush();
    expect(statsMock).toHaveBeenCalledTimes(3);
    expect(statsMock.mock.calls[2][0]).toEqual({ start_date: '2026-10-09', end_date: '2026-10-10' });
  });
});
