// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test, vi } from 'vitest';
import { ImageLibraryContext, type ImageLibraryOption } from './ImageLibraryContext';
import { ImageLibraryPicker } from './ImageLibraryPicker';

/** pickerOptions 是两张资源中心图片，覆盖同名引用与缩略图地址拼装。 */
const pickerOptions: ImageLibraryOption[] = [
  { id: 7, name: '主图', reference: '_library/main.png' },
  { id: 8, name: '教程图', reference: '_library/guide.jpg' },
];

/** renderPicker 在给定图片选项下渲染受控控件，返回 onChange 替身。 */
const renderPicker = (value: string, options: readonly ImageLibraryOption[] = []) => {
  // onChange 记录控件回传的图片引用，不模拟受控状态更新。
  const onChange = vi.fn();
  render(
    <ImageLibraryContext.Provider value={options}>
      <ImageLibraryPicker value={value} onChange={onChange} helpId="picker-help" />
    </ImageLibraryContext.Provider>,
  );
  return onChange;
};

afterEach(/* 当前回调清理资源中心控件测试 DOM。 */ () => cleanup());

describe('ImageLibraryPicker 受控图片引用控件', /* 当前回调验证无 Provider 降级、下拉写入和手填兼容。 */ () => {
  test('没有 Provider 时仍可渲染，下拉为空且手填输入框可用', /* 当前回调保证既有单测无需 Provider 也能渲染。 */ () => {
    renderPicker('');
    // select 是没有资源中心数据时的唯一选项控件，默认展示未选择文案。
    const select = screen.getByRole('combobox', { name: '资源中心图片' }) as HTMLSelectElement;
    expect(select.value).toBe('');
    expect(screen.getByRole('option', { name: '未选择资源中心图片' })).toBeTruthy();
    expect(screen.getByRole('textbox', { name: '本地图片相对路径' })).toBeTruthy();
    expect(screen.queryByRole('img')).toBeNull();
    expect(screen.getByPlaceholderText('product-a/guide.jpg')).toBeTruthy();
  });

  test('选择资源中心图片时回传 reference', /* 当前回调确认配置写入的是 _library 引用而不是展示名。 */ () => {
    // onChange 是本次下拉选择的回调替身。
    const onChange = renderPicker('', pickerOptions);
    fireEvent.change(screen.getByRole('combobox', { name: '资源中心图片' }), { target: { value: '_library/guide.jpg' } });
    expect(onChange).toHaveBeenCalledExactlyOnceWith('_library/guide.jpg');
  });

  test('手填相对路径原样回传并保留历史值选项', /* 当前回调确认旧配置不会因为不在资源中心而被丢弃。 */ () => {
    // onChange 是本次手填输入的回调替身。
    const onChange = renderPicker('goods/legacy.png', pickerOptions);
    // select 在历史值不属于资源中心时显示额外的当前值选项。
    const select = screen.getByRole('combobox', { name: '资源中心图片' }) as HTMLSelectElement;
    expect(select.value).toBe('goods/legacy.png');
    expect(screen.getByRole('option', { name: '当前值（不在资源中心）' })).toBeTruthy();
    fireEvent.change(screen.getByRole('textbox', { name: '本地图片相对路径' }), { target: { value: ' product-a/guide.jpg ' } });
    expect(onChange).toHaveBeenCalledExactlyOnceWith(' product-a/guide.jpg ');
  });

  test('命中资源中心图片时展示懒加载缩略图', /* 当前回调确认缩略图使用受登录保护的图片内容地址。 */ () => {
    renderPicker('_library/main.png', pickerOptions);
    // thumbnail 是命中资源中心图片后的缩略图元素。
    const thumbnail = screen.getByRole('img', { name: '主图 缩略图' }) as HTMLImageElement;
    expect(thumbnail.getAttribute('src')).toBe('/api/v1/image-library/7/content');
    expect(thumbnail.getAttribute('loading')).toBe('lazy');
    expect(screen.queryByRole('option', { name: '当前值（不在资源中心）' })).toBeNull();
  });

  test('自定义帮助文案替换默认目录说明并保持 id 关联', /* 当前回调确认账号专属说明可以替换通用说明。 */ () => {
    render(
      <ImageLibraryContext.Provider value={pickerOptions}>
        <ImageLibraryPicker value="" onChange={/* 当前回调不使用回传值，只验证说明文案。 */ () => undefined} helpId="picker-help" help={<p>账号专属图片说明</p>} />
      </ImageLibraryContext.Provider>,
    );
    expect(screen.getByText('账号专属图片说明')).toBeTruthy();
    expect(screen.queryByText(/XIANYU_UPLOAD_DIR/)).toBeNull();
    expect(screen.getByRole('combobox', { name: '资源中心图片' }).getAttribute('aria-describedby')).toBe('picker-help');
  });
});
