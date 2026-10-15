import type { Order } from './models';

/**
 * formatOrderSpec 把订单的规格名称与规格值拼成单行展示文本，供订单列表行和详情弹窗共用。
 * 输入只依赖规格字段，因此调用方不需要构造完整订单模型；两个字段都为空时返回空字符串，
 * 调用方据此隐藏规格行，避免平台未返回规格或该订单尚未单笔同步时展示空标签。
 */
export const formatOrderSpec = (order: Pick<Order, 'spec_name' | 'spec_value'>): string => {
  // specName 是去除首尾空白后的平台规格名称；空值表示平台未返回规格名称。
  const specName = (order.spec_name || '').trim();
  // specValue 是去除首尾空白后的平台规格值；空值不参与拼接，避免出现悬空分隔符。
  const specValue = (order.spec_value || '').trim();
  if (!specName) return specValue;
  if (!specValue) return specName;
  return `${specName} / ${specValue}`;
};
