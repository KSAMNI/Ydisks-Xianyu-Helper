import { describe, expect, test } from 'vitest';
import type { Item, ShippingRule } from './models';
import { countTriggerRules, itemHasTrigger, matchesRuleCoverageFilter, paginateItems } from './ruleCoverage';

// itemFixture 是账号 acc1 下的商品 i1。
const itemFixture = { cookie_id: 'acc1', item_id: 'i1' } as Item;

// ruleOf 构造只含筛选所需字段的规则夹具。
const ruleOf = (patch: Partial<ShippingRule>): ShippingRule => ({
  id: '1', name: 'r', trigger_type: 'order_paid', item_keyword: '', cookie_id: 'acc1', item_id: 'i1', card_group_id: 0,
  priority: 0, enabled: true, actions: [], variants: [], ...patch,
});

describe('itemHasTrigger', /* 覆盖商品级、账号级与失效规则的判定分支。 */ () => {
  test('counts an enabled item-level rule', /* 商品级有效规则算已配置。 */ () => {
    expect(itemHasTrigger(itemFixture, 'order_paid', [ruleOf({})])).toBe(true);
  });

  test('ignores disabled rules and rules needing reconfiguration', /* 禁用或待重配规则不生效。 */ () => {
    expect(itemHasTrigger(itemFixture, 'order_paid', [ruleOf({ enabled: false })])).toBe(false);
    expect(itemHasTrigger(itemFixture, 'order_paid', [ruleOf({ sku_migration_status: 'needs_reconfiguration' })])).toBe(false);
  });

  test('ignores rules of another account, item or trigger', /* 其他账号、商品、触发类型不计入。 */ () => {
    expect(itemHasTrigger(itemFixture, 'order_paid', [ruleOf({ cookie_id: 'acc2' })])).toBe(false);
    expect(itemHasTrigger(itemFixture, 'order_paid', [ruleOf({ item_id: 'other' })])).toBe(false);
    expect(itemHasTrigger(itemFixture, 'order_paid', [ruleOf({ trigger_type: 'buyer_reviewed' })])).toBe(false);
  });

  test('falls back to account-level rules for non-delivery triggers', /* 账号级规则覆盖未单独配置的商品。 */ () => {
    expect(itemHasTrigger(itemFixture, 'review_missing_timeout', [ruleOf({ trigger_type: 'review_missing_timeout', item_id: '' })])).toBe(true);
  });

  test('requires confirmation for account-level delivery rules', /* 账号级发货规则需确认适用全部商品。 */ () => {
    // accountRule 是未经确认的账号级发货规则。
    const accountRule = ruleOf({ item_id: '' });
    expect(itemHasTrigger(itemFixture, 'order_paid', [accountRule])).toBe(false);
    expect(itemHasTrigger(itemFixture, 'order_paid', [{ ...accountRule, config_json: '{"allow_all_items":true}' }])).toBe(true);
    expect(itemHasTrigger(itemFixture, 'order_paid', [{ ...accountRule, config_json: '{bad json' }])).toBe(false);
  });
});

describe('matchesRuleCoverageFilter', /* 覆盖三种筛选取值。 */ () => {
  test('passes everything when no filter is selected', /* 空筛选不过滤。 */ () => {
    expect(matchesRuleCoverageFilter(itemFixture, '', [])).toBe(true);
  });

  test('lists items missing one trigger', /* 缺少指定触发器的商品被列出。 */ () => {
    expect(matchesRuleCoverageFilter(itemFixture, 'order_paid', [])).toBe(true);
    expect(matchesRuleCoverageFilter(itemFixture, 'order_paid', [ruleOf({})])).toBe(false);
  });

  test('lists items with no trade automation at all', /* any 仅列出四类都没有的商品。 */ () => {
    expect(matchesRuleCoverageFilter(itemFixture, 'any', [])).toBe(true);
    expect(matchesRuleCoverageFilter(itemFixture, 'any', [ruleOf({ trigger_type: 'order_created' })])).toBe(false);
  });
});

describe('countTriggerRules', /* 覆盖商品级优先与账号级回退的计数。 */ () => {
  test('prefers item-level rules over account-level ones', /* 有商品级规则时不再计入账号级。 */ () => {
    // rules 含两条商品级规则和一条账号级规则（已确认适用全部商品）。
    const rules = [ruleOf({ id: '1' }), ruleOf({ id: '2' }), ruleOf({ id: '3', item_id: '', config_json: '{"allow_all_items":true}' })];
    expect(countTriggerRules(itemFixture, 'order_paid', rules)).toBe(2);
    expect(countTriggerRules(itemFixture, 'order_paid', rules.slice(2))).toBe(1);
    expect(countTriggerRules(itemFixture, 'order_paid', [])).toBe(0);
  });
});

describe('paginateItems', /* 覆盖分页切片与页码修正。 */ () => {
  // numbers 是 45 个顺序数字，按每页 20 条共 3 页。
  const numbers = Array.from({ length: 45 }, /* _ 忽略，index 作为取值。 */ (_, index) => index);

  test('slices the requested page', /* 取中间页与末页。 */ () => {
    expect(paginateItems(numbers, 2).rows[0]).toBe(20);
    expect(paginateItems(numbers, 3).rows).toHaveLength(5);
    expect(paginateItems(numbers, 3).totalPages).toBe(3);
  });

  test('clamps out-of-range pages and keeps one page for empty input', /* 越界页码修正，空列表仍有一页。 */ () => {
    expect(paginateItems(numbers, 99).page).toBe(3);
    expect(paginateItems(numbers, 0).page).toBe(1);
    expect(paginateItems([], 5)).toEqual({ rows: [], page: 1, totalPages: 1 });
  });
});
