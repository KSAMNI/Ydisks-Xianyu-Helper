// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { useImageLibraryOptions } from '../../shared/components/ImageLibraryContext';
import { listImageAssets } from '../features/image-library/api';
import type { ImageAsset } from '../features/image-library/models';
import { ImageLibraryProvider } from './ImageLibraryProvider';

/** sessionState 是会话 Provider 替身返回的登录态容器，用例可切换未登录场景。 */
const sessionState = vi.hoisted(() => ({ isLoggedIn: true }));

vi.mock('../features/image-library/api', /* imageLibraryApiMockFactory 只替换图片列表读取。 */ () => ({ listImageAssets: vi.fn() }));
vi.mock('./SessionProvider', /* sessionProviderMockFactory 提供可控登录态，避免真实会话校验请求。 */ () => ({ useSession: () => sessionState }));

/** listMock 是图片列表读取接口的可控替身。 */
const listMock = vi.mocked(listImageAssets);

/** buildAsset 构造一张资源中心图片，字段值与下拉选项断言一一对应。 */
const buildAsset = (id: number, name: string, reference: string): ImageAsset => ({
  id,
  name,
  reference,
  mimeType: 'image/png',
  sizeBytes: 1024,
  width: 10,
  height: 10,
  usageCount: 0,
  createdAt: '',
  updatedAt: '',
});

/** OptionsProbe 把上下文中的图片选项渲染成文本，供断言读取 Provider 提供的值。 */
const OptionsProbe = () => {
  // options 是当前上下文中的图片选项，没有 Provider 时为空数组。
  const options = useImageLibraryOptions();
  return <p>{options.length === 0 ? '无图片' : options.map(/* option 是单个图片选项。 */ option => `${option.name}:${option.reference}`).join(',')}</p>;
};

beforeEach(/* 当前回调重置登录态和列表替身。 */ () => {
  vi.clearAllMocks();
  sessionState.isLoggedIn = true;
  listMock.mockResolvedValue([buildAsset(1, '主图', '_library/main.png'), buildAsset(2, '教程图', '_library/guide.jpg')]);
});

afterEach(/* 当前回调清理 Provider 测试 DOM 和全局替身。 */ () => {
  cleanup();
  vi.restoreAllMocks();
});

describe('ImageLibraryProvider', /* 当前回调验证图片选项读取、失败降级和卸载取消。 */ () => {
  test('已登录时读取一次并向下提供裁剪后的选项', /* 当前回调确认上下文只暴露下拉需要的字段。 */ async () => {
    render(<ImageLibraryProvider><OptionsProbe /></ImageLibraryProvider>);
    expect(await screen.findByText('主图:_library/main.png,教程图:_library/guide.jpg')).toBeTruthy();
    expect(listMock).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ signal: expect.any(AbortSignal), skipAuthLogout: true }));
  });

  test('读取失败时静默降级为空列表且不阻塞子节点', /* 当前回调确认失败不会抛出到界面。 */ async () => {
    listMock.mockRejectedValueOnce(new Error('网络异常'));
    render(<ImageLibraryProvider><OptionsProbe /></ImageLibraryProvider>);
    expect(await screen.findByText('无图片')).toBeTruthy();
  });

  test('卸载时取消仍在途的列表请求', /* 当前回调确认 Provider 不遗留后台请求。 */ async () => {
    // pendingSignal 保存列表替身收到的取消信号，供卸载后断言。
    let pendingSignal: AbortSignal | undefined;
    listMock.mockImplementationOnce(/* 当前替身让列表请求保持在途。 */ options => new Promise<ImageAsset[]>(/* resolve 不会被调用，测试只验证取消。 */ () => { pendingSignal = options?.signal; }));
    // view 保存本次渲染控制器，用于在断言前卸载 Provider。
    const view = render(<ImageLibraryProvider><OptionsProbe /></ImageLibraryProvider>);
    await waitFor(/* 当前断言等待列表请求已经发出。 */ () => expect(listMock).toHaveBeenCalledTimes(1));
    view.unmount();
    expect(pendingSignal?.aborted).toBe(true);
  });

  test('未登录时不请求图片库', /* 当前回调确认登录页不会触发受保护接口。 */ async () => {
    sessionState.isLoggedIn = false;
    render(<ImageLibraryProvider><OptionsProbe /></ImageLibraryProvider>);
    expect(screen.getByText('无图片')).toBeTruthy();
    expect(listMock).not.toHaveBeenCalled();
  });
});
