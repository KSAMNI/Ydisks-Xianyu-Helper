import type { AutomationTriggerType, Item, ShippingRule } from './models';

/** TRADE_AUTOMATION_TRIGGERS 是商品页可筛选的四类交易自动化触发器，顺序即下拉框展示顺序。 */
export const TRADE_AUTOMATION_TRIGGERS: ReadonlyArray<{
  /** trigger 是自动化规则的触发类型标识。 */
  trigger: AutomationTriggerType;
  /** label 是筛选下拉框中展示的中文名称。 */
  label: string;
}> = [
  { trigger: 'order_paid', label: '自动发货' },
  { trigger: 'buyer_reviewed', label: '评价赠品' },
  { trigger: 'review_missing_timeout', label: '求评价' },
  { trigger: 'order_created', label: '拍下改价' },
];

/** RuleCoverageFilter 是商品页规则筛选的取值：空串表示不筛选，'any' 表示未配置任何交易自动化，其余为缺少对应触发器。 */
export type RuleCoverageFilter = '' | 'any' | AutomationTriggerType;

/**
 * ruleTakesEffect 判断规则当前是否可能被自动化中心命中，与后端 Match 的筛选条件保持一致：
 * 必须启用、规格迁移已就绪（缺省视为就绪），账号级付款发货规则还必须经用户确认适用于全部商品。
 */
const ruleTakesEffect = (rule: ShippingRule): boolean => {
  if (!rule.enabled) return false;
  if (rule.sku_migration_status && rule.sku_migration_status !== 'ready') return false;
  if (rule.trigger_type === 'order_paid' && !rule.item_id) {
    try {
      // scope 是账号级发货规则的配置对象，只读取 allow_all_items 确认标记。
      const scope = JSON.parse(rule.config_json || '{}') as {
        /** allow_all_items 是用户对账号级发货规则适用全部商品的确认标记。 */
        allow_all_items?: unknown;
      };
      return scope.allow_all_items === true;
    } catch {
      return false;
    }
  }
  return true;
};

/**
 * countTriggerRules 按后端匹配顺序统计商品在某类触发器下生效的规则条数：
 * 优先返回该商品自己的有效规则数，没有时回退到账号级有效规则数。
 */
export const countTriggerRules = (item: Item, trigger: AutomationTriggerType, rules: ShippingRule[]): number => {
  // scoped 是同账号、同触发类型且当前会生效的候选规则。
  const scoped = rules.filter(/* rule 是待判断的自动化规则。 */ rule => rule.cookie_id === item.cookie_id && rule.trigger_type === trigger && ruleTakesEffect(rule));
  // itemLevel 是仅属于该商品的有效规则。
  const itemLevel = scoped.filter(/* rule 判断是否为该商品的专属规则。 */ rule => rule.item_id === item.item_id);
  return itemLevel.length > 0 ? itemLevel.length : scoped.filter(/* rule 判断是否为账号级规则。 */ rule => !rule.item_id).length;
};

/** itemHasTrigger 判断商品是否被某类触发器覆盖，即 countTriggerRules 大于 0。 */
export const itemHasTrigger = (item: Item, trigger: AutomationTriggerType, rules: ShippingRule[]): boolean => countTriggerRules(item, trigger, rules) > 0;

/** matchesRuleCoverageFilter 判断商品是否满足当前规则筛选；筛选值为空时全部通过。 */
export const matchesRuleCoverageFilter = (item: Item, filter: RuleCoverageFilter, rules: ShippingRule[]): boolean => {
  if (filter === '') return true;
  if (filter === 'any') return !TRADE_AUTOMATION_TRIGGERS.some(/* entry 是一类交易自动化触发器。 */ entry => itemHasTrigger(item, entry.trigger, rules));
  return !itemHasTrigger(item, filter, rules);
};

/** ITEM_LIST_PAGE_SIZE 是列表视图每页展示的商品数量。 */
export const ITEM_LIST_PAGE_SIZE = 20;

/** ItemPage 描述一次分页计算的结果。 */
export interface ItemPage<T> {
  /** rows 是当前页的数据行。 */
  rows: T[];
  /** page 是修正到合法范围后的页码，从 1 开始。 */
  page: number;
  /** totalPages 是总页数，至少为 1。 */
  totalPages: number;
}

/** paginateItems 按页码切片；页码超出范围时修正到最近的合法页，避免筛选后停留在空页。 */
export const paginateItems = <T,>(items: T[], page: number, pageSize: number = ITEM_LIST_PAGE_SIZE): ItemPage<T> => {
  // totalPages 是向上取整的总页数，空列表也保留一页。
  const totalPages = Math.max(1, Math.ceil(items.length / pageSize));
  // current 是修正后的页码。
  const current = Math.min(Math.max(1, page), totalPages);
  return { rows: items.slice((current - 1) * pageSize, current * pageSize), page: current, totalPages };
};
