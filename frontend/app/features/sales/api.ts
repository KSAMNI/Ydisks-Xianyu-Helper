import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import { type RequestControlOptions } from '../../../shared/http/client';

/** 销售统计按日聚合的 UI 模型。 */
export interface SalesDailyItem {
  /** 用户本地日期。 */
  date: string;
  /** 当天销售额合计（元），含已发货与已完成订单。 */
  salesAmount: number;
  /** 当天已收货款合计（元），仅含已完成订单。 */
  receivedAmount: number;
  /** 当天退货货款合计（元），仅含已取消订单。 */
  refundedAmount: number;
  /** 当天参与统计的订单数。 */
  orderCount: number;
}

/** 销售统计页面的 UI 模型，三项指标分列展示、互不抵扣。 */
export interface SalesStatsModel {
  /** 销售额合计（元），含已发货与已完成订单。 */
  salesAmount: number;
  /** 计入销售额的订单数。 */
  salesOrders: number;
  /** 已收货款合计（元），仅含已完成订单。 */
  receivedAmount: number;
  /** 计入已收货款的订单数。 */
  receivedOrders: number;
  /** 退货货款合计（元），仅含已取消订单。 */
  refundedAmount: number;
  /** 计入退货货款的订单数。 */
  refundedOrders: number;
  /** 按用户本地日期聚合的销售明细。 */
  daily: SalesDailyItem[];
}

/** 读取指定自然日范围的销售统计，并显式传递浏览器时区偏移。 */
export const getSalesStats = async (
  dateRange: { /** 统计起始日期。 */ start_date: string; /** 统计结束日期。 */ end_date: string },
  options?: RequestControlOptions,
): Promise<SalesStatsModel> => {
  // data 是契约客户端按生成类型返回的销售统计传输 DTO。
  const data = await runContractRequest(/* signal 是本次销售统计请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/analytics/sales', {
    params: { query: { ...dateRange, timezone_offset_minutes: -new Date().getTimezoneOffset() } },
    signal,
  }), options);
  return {
    salesAmount: data.sales_amount,
    salesOrders: data.sales_orders,
    receivedAmount: data.received_amount,
    receivedOrders: data.received_orders,
    refundedAmount: data.refunded_amount,
    refundedOrders: data.refunded_orders,
    daily: (data.daily || []).map(/* item 是服务端按日聚合的传输 DTO。 */ item => ({
      date: item.date,
      salesAmount: item.sales_amount,
      receivedAmount: item.received_amount,
      refundedAmount: item.refunded_amount,
      orderCount: item.order_count,
    })),
  };
};
