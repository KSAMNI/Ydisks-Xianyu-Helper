// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test, vi } from 'vitest';
import type { Item, ShippingRule } from '../models';
import ItemTable from './ItemTable';

// itemFixture 是账号 acc1 下的一个商品。
const itemFixture = { cookie_id: 'acc1', item_id: 'i1', item_title: '示例商品', item_price: '9.9', ai_override: 'enabled' } as Item;

// paidRule 是该商品专属的付款后自动发货规则。
const paidRule = { id: '1', name: 'r', trigger_type: 'order_paid', item_keyword: '', cookie_id: 'acc1', item_id: 'i1', card_group_id: 0, priority: 0, enabled: true, actions: [], variants: [] } as ShippingRule;

afterEach(cleanup);

// renderTable 渲染列表视图并返回各回调的记录函数。
const renderTable = (items: Item[], rules: ShippingRule[]) => {
  // handlers 记录表格触发的全部用户操作回调。
  const handlers = { onConfigureDelivery: vi.fn(), onOpenAISettings: vi.fn(), onEdit: vi.fn(), onDelete: vi.fn() };
  render(<ItemTable items={items} rules={rules} accountNickname={/* 返回固定昵称。 */ () => '店铺A'} formatPrice={/* 原样返回价格。 */ price => `¥${price}`} aiStatusText={{ inherit: '继承账号', enabled: '已启用', disabled: '已停用' }} emptyText="暂无商品" {...handlers} />);
  return handlers;
};

describe('ItemTable', /* 覆盖列表视图渲染与操作回调。 */ () => {
  test('shows rule coverage per trigger and fires row actions', /* 已配置显示条数，其余显示未配置，操作回调带上商品。 */ () => {
    // handlers 是各行操作的记录函数。
    const handlers = renderTable([itemFixture], [paidRule]);
    expect(screen.getByText('1 条')).toBeTruthy();
    expect(screen.getAllByText('未配置')).toHaveLength(3);
    fireEvent.click(screen.getByText('发货规则'));
    fireEvent.click(screen.getByLabelText('编辑'));
    fireEvent.click(screen.getByLabelText('删除'));
    fireEvent.click(screen.getByLabelText('配置商品 AI 客服：示例商品'));
    expect(handlers.onConfigureDelivery).toHaveBeenCalledWith(itemFixture);
    expect(handlers.onEdit).toHaveBeenCalledWith(itemFixture);
    expect(handlers.onDelete).toHaveBeenCalledWith(itemFixture);
    expect(handlers.onOpenAISettings).toHaveBeenCalledWith(itemFixture);
  });

  test('renders the empty text when there are no items', /* 无商品时只显示提示。 */ () => {
    renderTable([], []);
    expect(screen.getByText('暂无商品')).toBeTruthy();
  });
});
