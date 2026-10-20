// @vitest-environment jsdom
import { act,cleanup,fireEvent,render,screen } from '@testing-library/react';
import React from 'react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import { getDashboardStats,getItems,getOrderAnalytics,getValidOrders } from '../api';
import Dashboard from './Dashboard';

vi.mock('../api', /* dashboardApiMockFactory 保留真实导出并替换四个网络请求。 */ async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  getDashboardStats: vi.fn(),
  getItems: vi.fn(),
  getOrderAnalytics: vi.fn(),
  getValidOrders: vi.fn(),
}));
vi.mock('../DashboardTrendChart', /* trendChartFactory 用空组件替换趋势图，避免 jsdom 无法测量尺寸。 */ () => ({
  DashboardTrendChart: /* trendChartStub 不渲染任何内容。 */ () => null,
}));
vi.mock('recharts', /* rechartsFactory 用空组件替换饼图。 */ () => {
  // Empty 是渲染空内容的图表替身组件。
  const Empty = /* emptyComponent 不渲染任何内容。 */ () => null;
  return {
    Cell: Empty, Legend: Empty, Pie: Empty, PieChart: Empty, Tooltip: Empty,
    ResponsiveContainer: /* containerComponent 直接渲染子节点。 */ (props: { /** children 是图表子节点。 */ children: React.ReactNode }) => React.createElement('div', null, props.children),
  };
});

// analyticsMock 是订单分析请求的可控替身，每次范围加载会调用当前期与上一期两次。
const analyticsMock = vi.mocked(getOrderAnalytics);
// analyticsFixture 是页面渲染所需的最小订单分析数据。
const analyticsFixture = { revenue_stats: { total_amount: 100, total_orders: 2, avg_amount: 50, unique_buyers: 2, unique_items: 1 }, daily_stats: [], status_stats: [], city_stats: [], item_stats: [] };

// flush 等待已解决的请求刷新页面状态。
const flush = async (): Promise<void> => { await act(/* flushAction 刷新异步状态。 */ async () => {}); };
// dateInputs 返回当前页面的起止日期输入框。
const dateInputs = (): NodeListOf<Element> => document.querySelectorAll('input[type="date"]');

describe('Dashboard 自定义日期', /* 当前测试组验证草稿、校验与应用的页面行为。 */ () => {
  beforeEach(/* 重置请求替身。 */ () => {
    vi.clearAllMocks();
    vi.mocked(getDashboardStats).mockResolvedValue({ total_cookies: 2, active_cookies: 1, total_cards: 3, total_keywords: 4, total_orders: 5, available_card_stock: 6 });
    vi.mocked(getItems).mockResolvedValue([]);
    analyticsMock.mockResolvedValue(analyticsFixture);
    vi.mocked(getValidOrders).mockResolvedValue({ orders: [], total: 0, truncated: false });
  });
  afterEach(/* 卸载页面。 */ () => cleanup());

  test('日期填反或未填齐只提示且不请求，修正后应用并可重复应用刷新', /* 当前回调覆盖校验、修正与同范围重复应用。 */ async () => {
    render(<Dashboard />);
    await flush();
    expect(analyticsMock).toHaveBeenCalledTimes(2);
    fireEvent.click(screen.getByText('自定义'));
    fireEvent.change(dateInputs()[0], { target: { value: '2026-10-11' } });
    fireEvent.click(screen.getByText('应用'));
    expect(screen.getByRole('alert').textContent).toContain('请选择开始日期和结束日期');
    fireEvent.change(dateInputs()[1], { target: { value: '2026-10-10' } });
    fireEvent.click(screen.getByText('应用'));
    await flush();
    expect(screen.getByRole('alert').textContent).toContain('开始日期不能晚于结束日期');
    expect(analyticsMock).toHaveBeenCalledTimes(2);
    expect(dateInputs().length).toBe(2);

    fireEvent.change(dateInputs()[0], { target: { value: '2026-10-09' } });
    fireEvent.click(screen.getByText('应用'));
    await flush();
    expect(screen.queryByRole('alert')).toBeNull();
    expect(analyticsMock).toHaveBeenCalledTimes(4);
    expect(analyticsMock.mock.calls[2][0]).toEqual({ start_date: '2026-10-09', end_date: '2026-10-10' });

    fireEvent.click(screen.getByText('应用'));
    await flush();
    expect(analyticsMock).toHaveBeenCalledTimes(6);
  });
});
