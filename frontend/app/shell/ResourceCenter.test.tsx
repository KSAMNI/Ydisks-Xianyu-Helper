// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, test } from 'vitest';
import { ResourceCenter } from './ResourceCenter';

afterEach(/* 每例卸载当前资源分类，避免重复页面标题。 */ () => cleanup());

test('统一资源中心只有三个分类且仅挂载当前分类', /* 验证模板、商品图片和消息图片不会混在一个列表。 */ () => {
  render(<ResourceCenter initialSection="templates" templates={<div>模板编辑功能</div>} images={<div>商品图片功能</div>} messages={<div>消息图片功能</div>} />);
  expect(screen.getAllByRole('heading', { name: '资源中心' })).toHaveLength(1);
  expect(screen.getAllByRole('tab')).toHaveLength(3);
  expect(screen.getByRole('tabpanel', { name: '商品模板' }).textContent).toBe('模板编辑功能');
  expect(screen.queryByText('消息图片功能')).toBeNull();
  fireEvent.click(screen.getByRole('tab', { name: '商品图片' }));
  expect(screen.getByRole('tabpanel', { name: '商品图片' }).textContent).toBe('商品图片功能');
  expect(screen.queryByText('模板编辑功能')).toBeNull();
  fireEvent.click(screen.getByRole('tab', { name: '消息图片' }));
  expect(screen.getByRole('tabpanel', { name: '消息图片' }).textContent).toBe('消息图片功能');
  expect(screen.queryByText('商品图片功能')).toBeNull();
});
