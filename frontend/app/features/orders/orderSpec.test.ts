import { expect, test } from 'vitest';
import { formatOrderSpec } from './orderSpec';

test('规格文本按名称与值拼接，缺失一侧时只展示已有内容', /* 验证规格展示不会输出空分隔符或空标签。 */ () => {
  expect(formatOrderSpec({ spec_name: '颜色', spec_value: '红' })).toBe('颜色 / 红');
  expect(formatOrderSpec({ spec_name: '颜色', spec_value: '' })).toBe('颜色');
  expect(formatOrderSpec({ spec_name: '', spec_value: '红' })).toBe('红');
  expect(formatOrderSpec({ spec_name: '', spec_value: '' })).toBe('');
  expect(formatOrderSpec({})).toBe('');
});

test('规格文本去除平台返回的首尾空白', /* 验证平台空白不会进入订单列表与详情展示文本。 */ () => {
  expect(formatOrderSpec({ spec_name: ' 颜色 ', spec_value: ' 红 ' })).toBe('颜色 / 红');
  expect(formatOrderSpec({ spec_name: '   ', spec_value: '红' })).toBe('红');
});
