import { beforeEach, describe, expect, test, vi } from 'vitest';

// getMock 是销售统计契约 GET 请求的共享客户端替身。
const { getMock } = vi.hoisted(/* getMockFactory 创建契约客户端 GET 方法替身。 */ () => ({ getMock: vi.fn() }));

// ContractMockResult 是测试契约请求回调返回的最小响应模型。
interface ContractMockResult {
  // data 是契约客户端解码出的成功响应。
  data?: unknown;
}

vi.mock('../../../shared/api-contract/client', /* contractClientModuleMockFactory 提供销售统计 API 适配测试所需的契约依赖。 */ () => ({
  // contractClientMock 只提供本测试覆盖的 GET 操作。
  contractClient: { GET: getMock },
  // runContractRequestMock 执行 feature adapter 传入的请求回调。
  runContractRequest: /* runContractRequestMock 执行 feature adapter 传入的契约请求回调。 */ async (/* execute 是 feature adapter 构造的契约请求动作。 */ execute: (signal: AbortSignal) => Promise<ContractMockResult>) => (await execute(new AbortController().signal)).data,
}));

import { contractClient } from '../../../shared/api-contract/client';
import { getSalesStats } from './api';

// salesGetMock 是带有生成契约类型的 GET 方法替身。
const salesGetMock = vi.mocked(contractClient.GET);
// transportPayload 是服务端销售统计的传输 DTO 样例，字段名与生成契约保持一致。
const transportPayload = {
  sales_amount: 28.5, sales_orders: 4,
  received_amount: 16, received_orders: 2,
  refunded_amount: 5.25, refunded_orders: 1,
  daily: [
    { date: '2026-10-10', sales_amount: 18.5, received_amount: 8, refunded_amount: 5.25, order_count: 3 },
  ],
};

// GetCallOptions 是测试中读取契约查询参数的最小参数模型。
interface GetCallOptions {
  // params 是契约客户端收到的查询参数对象。
  params?: { /** query 是本次销售统计请求的查询键值对。 */ query?: Record<string, unknown> };
}

describe('sales stats API adapter', /* 销售统计 API 适配测试覆盖传输 DTO 到 UI 模型的转换。 */ () => {
  beforeEach(/* 当前回调重置销售统计契约请求替身。 */ () => {
    vi.clearAllMocks();
    salesGetMock.mockResolvedValue({ data: transportPayload, response: { status: 200 } } as never);
  });

  test('传递日期范围与时区偏移并转换传输 DTO', /* 当前回调验证查询参数与 UI 模型字段映射。 */ async () => {
    // result 是适配器转换后的销售统计 UI 模型。
    const result = await getSalesStats({ start_date: '2026-10-10', end_date: '2026-10-11' });
    // params 是适配器传给契约客户端的查询参数。
    const params = (salesGetMock.mock.calls[0][1] as GetCallOptions).params?.query;
    expect(params?.start_date).toBe('2026-10-10');
    expect(params?.end_date).toBe('2026-10-11');
    expect(typeof params?.timezone_offset_minutes).toBe('number');
    expect(result.salesAmount).toBe(28.5);
    expect(result.salesOrders).toBe(4);
    expect(result.receivedAmount).toBe(16);
    expect(result.refundedAmount).toBe(5.25);
    expect(result.daily.length).toBe(1);
    expect(result.daily[0].salesAmount).toBe(18.5);
    expect(result.daily[0].orderCount).toBe(3);
  });

  test('服务端省略按日明细时回退为空数组', /* 当前回调验证缺省字段的容错归一。 */ async () => {
    salesGetMock.mockResolvedValueOnce({ data: { ...transportPayload, daily: undefined }, response: { status: 200 } } as never);
    // result 是服务端省略 daily 字段后的销售统计 UI 模型。
    const result = await getSalesStats({ start_date: '2026-10-10', end_date: '2026-10-10' });
    expect(result.daily).toEqual([]);
  });
});
