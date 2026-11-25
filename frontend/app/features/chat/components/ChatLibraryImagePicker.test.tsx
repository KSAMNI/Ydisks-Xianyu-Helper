// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import type { ComponentProps } from 'react';
import { afterEach,describe,expect,test,vi } from 'vitest';
import { ImageLibraryContext } from '../../../../shared/components/ImageLibraryContext';
import ChatLibraryImagePicker from './ChatLibraryImagePicker';


afterEach(/* 当前回调清理每个图片面板场景挂载的 DOM，避免重复元素命中。 */ () => cleanup());
// libraryOptions 是资源中心提供的两张共享图片，供选择与加载态断言使用。
const libraryOptions = [
  { id: 7, name: '话术图', reference: '_library/7.png' },
  { id: 8, name: '发货图', reference: '_library/8.png' },
];

/** renderPicker 在资源中心 Provider 下渲染图片面板，供各场景复用。 */
const renderPicker = (overrides: Partial<ComponentProps<typeof ChatLibraryImagePicker>> = {}) => {
  // onSelect 保存用户选择图片的回调替身。
  const onSelect = overrides.onSelect ?? vi.fn();
  // onClose 保存收起面板的回调替身。
  const onClose = overrides.onClose ?? vi.fn();
  // view 是当前场景的渲染结果，便于断言容器内容。
  const view = render(<ImageLibraryContext.Provider value={libraryOptions}>
    <ChatLibraryImagePicker open loadingAssetID={null} disabled={false} onSelect={onSelect} onClose={onClose} {...overrides} />
  </ImageLibraryContext.Provider>);
  return { view, onSelect, onClose };
};

describe('ChatLibraryImagePicker', /* 当前回调验证资源中心图片面板的可见性、选择与加载状态。 */ () => {
  test('收起时不渲染任何内容', /* 当前回调验证 open=false 的空渲染。 */ () => {
    // view 是收起状态下的渲染结果。
    const { view } = renderPicker({ open: false });
    expect(view.container.firstChild).toBeNull();
    expect(screen.queryByRole('dialog', { name: '资源中心图片' })).toBeNull();
  });

  test('选择图片回传资源中心标识并支持关闭', /* 当前回调验证缩略图点击与关闭按钮的回调参数。 */ () => {
    // view、onSelect 和 onClose 是当前场景的渲染结果与回调替身。
    const { onSelect, onClose } = renderPicker();
    expect(screen.getByRole('dialog', { name: '资源中心图片' })).not.toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /话术图/ }));
    expect(onSelect).toHaveBeenCalledWith({ id: 7, name: '话术图' });
    fireEvent.click(screen.getByRole('button', { name: '关闭资源中心图片' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  test('读取中或账号离线时禁止再次选择', /* 当前回调验证禁用分支。 */ () => {
    // view 是读取中状态的渲染结果。
    const { view } = renderPicker({ loadingAssetID: 7 });
    expect((screen.getByRole('button', { name: /话术图/ }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByLabelText('正在读取图片')).not.toBeNull();
    view.rerender(<ImageLibraryContext.Provider value={libraryOptions}>
      <ChatLibraryImagePicker open loadingAssetID={null} disabled onSelect={vi.fn()} onClose={vi.fn()} />
    </ImageLibraryContext.Provider>);
    expect((screen.getByRole('button', { name: /发货图/ }) as HTMLButtonElement).disabled).toBe(true);
  });

  test('资源中心没有图片时给出上传指引', /* 当前回调验证空列表提示。 */ () => {
    // view 是空图片库下的渲染结果。
    render(<ImageLibraryContext.Provider value={[]}>
      <ChatLibraryImagePicker open loadingAssetID={null} disabled={false} onSelect={vi.fn()} onClose={vi.fn()} />
    </ImageLibraryContext.Provider>);
    expect(screen.getByText(/资源中心还没有图片/)).not.toBeNull();
  });
});
