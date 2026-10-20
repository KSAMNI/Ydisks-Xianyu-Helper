// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test, vi } from 'vitest';
import SearchableSelect, { filterSearchableOptions } from './SearchableSelect';

// optionsFixture 是包含大小写混合名称的卡密库存候选项。
const optionsFixture = [
  { value: 1, label: 'Netflix 月卡' },
  { value: 2, label: '会员兑换码' },
  { value: 3, label: 'netflix 年卡' },
];

afterEach(cleanup);

describe('filterSearchableOptions', /* 覆盖关键字过滤的全部分支。 */ () => {
  test('returns every option for a blank keyword', /* 空白关键字不过滤。 */ () => {
    expect(filterSearchableOptions(optionsFixture, '   ', undefined)).toEqual(optionsFixture);
  });

  test('matches names case-insensitively', /* 名称匹配不区分大小写。 */ () => {
    expect(filterSearchableOptions(optionsFixture, 'NETFLIX', undefined).map(/* option 取主键。 */ option => option.value)).toEqual([1, 3]);
  });

  test('keeps the selected option even when it does not match', /* 已选项始终保留。 */ () => {
    expect(filterSearchableOptions(optionsFixture, 'netflix', 2).map(/* option 取主键。 */ option => option.value)).toEqual([1, 2, 3]);
  });
});

describe('SearchableSelect', /* 覆盖组件交互。 */ () => {
  test('filters options by typed keyword and reports the selected id', /* 输入关键字后选择匹配项。 */ () => {
    // onChange 记录组件回传的选中主键。
    const onChange = vi.fn();
    render(<SearchableSelect options={optionsFixture} value={undefined} onChange={onChange} placeholder="请选择" ariaLabel="卡密库存" />);
    fireEvent.change(screen.getByLabelText('卡密库存搜索'), { target: { value: '兑换' } });
    expect(screen.queryByRole('option', { name: 'Netflix 月卡' })).toBeNull();
    fireEvent.change(screen.getByLabelText('卡密库存'), { target: { value: '2' } });
    expect(onChange).toHaveBeenCalledWith(2);
  });

  test('reports zero when the placeholder is chosen and shows an empty hint', /* 选择占位项回传 0，无匹配时提示。 */ () => {
    // onChange 记录组件回传的选中主键。
    const onChange = vi.fn();
    render(<SearchableSelect options={optionsFixture} value={1} onChange={onChange} placeholder="请选择" ariaLabel="卡密库存" />);
    fireEvent.change(screen.getByLabelText('卡密库存'), { target: { value: '' } });
    expect(onChange).toHaveBeenCalledWith(0);
    cleanup();
    render(<SearchableSelect options={optionsFixture} value={undefined} onChange={onChange} placeholder="请选择" ariaLabel="卡密库存" />);
    fireEvent.change(screen.getByLabelText('卡密库存搜索'), { target: { value: '不存在' } });
    expect(screen.getByText(/没有匹配/)).toBeTruthy();
  });
});
